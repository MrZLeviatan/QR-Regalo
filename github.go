/**
 * Publicación de los PDF en GitHub Pages, para que el QR funcione desde
 * cualquier red (datos móviles, cualquier Wi-Fi) sin dejar el PC encendido.
 * Usa la API REST de GitHub con un token personal: sube el PDF al repositorio
 * (carpeta carta/) y activa Pages la primera vez si todavía no lo está.
 **/
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

/**
 * apiGitHub es la dirección de la API REST de GitHub.
 **/
var apiGitHub = "https://api.github.com"

/**
 * GitHub sube archivos a un repositorio y conoce la URL pública de su sitio
 * de Pages. repo tiene la forma "usuario/nombre".
 **/
type GitHub struct {
	token  string
	repo   string
	apiURL string // https://api.github.com (se cambia solo en las pruebas)
	client *http.Client

	mu       sync.Mutex
	pagesURL string // termina en "/", p. ej. https://usuario.github.io/carta/
	pagesOK  bool   // Pages ya está activo en el repositorio
}

/**
 * NuevoGitHub crea el publicador y calcula la URL de Pages por defecto
 * (https://usuario.github.io/nombre/); si Pages ya está activo, toma la URL
 * real (que puede ser un dominio propio).
 **/
func NuevoGitHub(token, repo string) (*GitHub, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || owner == "" || name == "" {
		return nil, errors.New("GITHUB_REPO debe tener la forma usuario/repositorio")
	}
	g := &GitHub{
		token:    token,
		repo:     repo,
		apiURL:   apiGitHub,
		client:   &http.Client{Timeout: 30 * time.Second},
		pagesURL: fmt.Sprintf("https://%s.github.io/%s/", strings.ToLower(owner), name),
	}
	// Comprueba que el token y el repositorio sean válidos.
	var info struct {
		Private bool `json:"private"`
	}
	if err := g.api("GET", "/repos/"+repo, nil, &info); err != nil {
		return nil, fmt.Errorf("no se pudo acceder a %s: %w", repo, err)
	}
	if info.Private {
		return nil, fmt.Errorf("el repositorio %s es privado: GitHub Pages gratis exige un repositorio público", repo)
	}
	// Si Pages ya está activo, usa su URL real.
	g.leerPages()
	return g, nil
}

/**
 * Sitio devuelve la URL pública del sitio de Pages (termina en "/").
 **/
func (g *GitHub) Sitio() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.pagesURL
}

/**
 * URL devuelve la dirección pública del PDF con el id dado.
 **/
func (g *GitHub) URL(id string) string { return g.Sitio() + "carta/" + id + ".pdf" }

/**
 * Subir guarda el PDF en carta/{id}.pdf del repositorio. Como el id es un
 * hash del contenido, si el archivo ya existe no se vuelve a subir. Después
 * se asegura de que Pages esté activo.
 **/
func (g *GitHub) Subir(id string, pdf []byte, productos int) error {
	ruta := "/repos/" + g.repo + "/contents/carta/" + id + ".pdf"
	// Ya publicado: mismo contenido, nada que subir.
	err := g.api("GET", ruta, nil, nil)
	if err == nil {
		return g.activarPages()
	}
	if !esNoEncontrado(err) {
		return err
	}
	// Sube el archivo a la rama por defecto (sirve también con un repositorio vacío).
	body := map[string]string{
		"message": fmt.Sprintf("Carta publicada (%d productos)", productos),
		"content": base64.StdEncoding.EncodeToString(pdf),
	}
	if err := g.api("PUT", ruta, body, nil); err != nil {
		return fmt.Errorf("no se pudo subir el PDF a GitHub: %w", err)
	}
	return g.activarPages()
}

/**
 * Disponible indica si el PDF ya se puede abrir en Pages: tras subirlo, GitHub
 * tarda alrededor de un minuto en desplegar el sitio.
 **/
func (g *GitHub) Disponible(id string) bool {
	res, err := g.client.Head(g.URL(id))
	if err != nil {
		return false
	}
	res.Body.Close()
	return res.StatusCode == http.StatusOK
}

/**
 * activarPages activa GitHub Pages sobre la rama por defecto (carpeta raíz)
 * si todavía no lo está, y actualiza la URL pública. Solo consulta a GitHub
 * hasta que Pages queda activo.
 **/
func (g *GitHub) activarPages() error {
	g.mu.Lock()
	ok := g.pagesOK
	g.mu.Unlock()
	if ok || g.leerPages() {
		return nil
	}
	// Rama por defecto del repositorio (ya existe porque se acaba de subir un archivo).
	var repo struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := g.api("GET", "/repos/"+g.repo, nil, &repo); err != nil {
		return err
	}
	body := map[string]any{"source": map[string]string{"branch": repo.DefaultBranch, "path": "/"}}
	if err := g.api("POST", "/repos/"+g.repo+"/pages", body, nil); err != nil {
		return fmt.Errorf("no se pudo activar GitHub Pages (actívalo a mano en Settings → Pages): %w", err)
	}
	g.leerPages()
	return nil
}

/**
 * leerPages consulta la configuración de Pages; si está activo guarda su URL
 * pública y devuelve true.
 **/
func (g *GitHub) leerPages() bool {
	var p struct {
		HTMLURL string `json:"html_url"`
	}
	if err := g.api("GET", "/repos/"+g.repo+"/pages", nil, &p); err != nil || p.HTMLURL == "" {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	g.pagesURL = strings.TrimRight(p.HTMLURL, "/") + "/"
	g.pagesOK = true
	return true
}

/**
 * errorAPI es una respuesta de error de GitHub con su código HTTP.
 **/
type errorAPI struct {
	code int
	msg  string
}

func (e *errorAPI) Error() string { return fmt.Sprintf("GitHub respondió %d: %s", e.code, e.msg) }

/**
 * esNoEncontrado indica si el error es un 404 de GitHub.
 **/
func esNoEncontrado(err error) bool {
	var e *errorAPI
	return errors.As(err, &e) && e.code == http.StatusNotFound
}

/**
 * api hace una petición autenticada a la API de GitHub. body (opcional) se
 * envía como JSON y la respuesta se decodifica en out (opcional).
 **/
func (g *GitHub) api(method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, g.apiURL+path, r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+g.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	res, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	// Errores: se devuelve el mensaje que explica GitHub.
	if res.StatusCode >= 300 {
		var e struct {
			Message string `json:"message"`
		}
		json.NewDecoder(res.Body).Decode(&e)
		return &errorAPI{code: res.StatusCode, msg: e.Message}
	}
	if out != nil {
		return json.NewDecoder(res.Body).Decode(out)
	}
	return nil
}
