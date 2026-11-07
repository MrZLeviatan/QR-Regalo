/**
 * Publicación de la carta en GitHub Pages, para que el QR funcione desde
 * cualquier red (datos móviles, cualquier Wi-Fi) sin dejar el PC encendido.
 * Usa la API REST de GitHub con un token personal: reemplaza en un solo commit
 * los PDF y la página selectora en la raíz del repositorio (la dirección del
 * QR nunca cambia) y activa Pages la primera vez si todavía no lo está.
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

	ultimoCommit string // padre del próximo commit (lo fija punta)
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
 * Archivo es un archivo publicado, con su ruta dentro del sitio (p. ej. "es.pdf").
 **/
type Archivo struct {
	Nombre string
	Datos  []byte
}

/**
 * Publicar reemplaza la carta del sitio en un solo commit: sube los archivos
 * a la raíz del repositorio y borra los de la lista borrar que existan (PDF
 * de idiomas desactivados). Un solo commit = un solo despliegue de Pages, así
 * nadie ve una mezcla de versiones. Si nada cambió no crea commit. Después se
 * asegura de que Pages esté activo.
 **/
func (g *GitHub) Publicar(archivos []Archivo, borrar []string, mensaje string) error {
	rama, base, err := g.punta()
	if err != nil {
		return err
	}
	// Árbol actual: para saber qué archivos borrables existen de verdad.
	var arbolActual struct {
		SHA  string `json:"sha"`
		Tree []struct {
			Path string `json:"path"`
		} `json:"tree"`
	}
	if err := g.api("GET", "/repos/"+g.repo+"/git/trees/"+base+"?recursive=1", nil, &arbolActual); err != nil {
		return err
	}
	existe := map[string]bool{}
	for _, e := range arbolActual.Tree {
		existe[e.Path] = true
	}
	// Sube cada archivo como blob y arma el árbol nuevo sobre el actual.
	entradas := []map[string]any{}
	for _, f := range archivos {
		var blob struct {
			SHA string `json:"sha"`
		}
		body := map[string]string{"content": base64.StdEncoding.EncodeToString(f.Datos), "encoding": "base64"}
		if err := g.api("POST", "/repos/"+g.repo+"/git/blobs", body, &blob); err != nil {
			return fmt.Errorf("no se pudo subir %s a GitHub: %w", f.Nombre, err)
		}
		entradas = append(entradas, map[string]any{"path": f.Nombre, "mode": "100644", "type": "blob", "sha": blob.SHA})
	}
	for _, ruta := range borrar {
		if existe[ruta] {
			entradas = append(entradas, map[string]any{"path": ruta, "mode": "100644", "type": "blob", "sha": nil})
		}
	}
	var arbol struct {
		SHA string `json:"sha"`
	}
	if err := g.api("POST", "/repos/"+g.repo+"/git/trees", map[string]any{"base_tree": base, "tree": entradas}, &arbol); err != nil {
		return err
	}
	// Sin cambios (misma carta): no hace falta commit.
	if arbol.SHA != arbolActual.SHA {
		var commit struct {
			SHA string `json:"sha"`
		}
		body := map[string]any{"message": mensaje, "tree": arbol.SHA, "parents": []string{g.ultimoCommit}}
		if err := g.api("POST", "/repos/"+g.repo+"/git/commits", body, &commit); err != nil {
			return err
		}
		if err := g.api("PATCH", "/repos/"+g.repo+"/git/refs/heads/"+rama, map[string]any{"sha": commit.SHA}, nil); err != nil {
			return fmt.Errorf("no se pudo actualizar la rama %s: %w", rama, err)
		}
	}
	return g.activarPages()
}

/**
 * punta devuelve la rama por defecto y el árbol de su último commit (y guarda
 * ese commit como padre del siguiente). Con el repositorio vacío crea primero
 * un archivo .nojekyll (Pages publica tal cual, sin procesar con Jekyll),
 * porque la API de árboles no funciona sin un commit inicial.
 **/
func (g *GitHub) punta() (rama, arbol string, err error) {
	var repo struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := g.api("GET", "/repos/"+g.repo, nil, &repo); err != nil {
		return "", "", err
	}
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	err = g.api("GET", "/repos/"+g.repo+"/git/ref/heads/"+repo.DefaultBranch, nil, &ref)
	if e := (*errorAPI)(nil); errors.As(err, &e) && (e.code == http.StatusConflict || e.code == http.StatusNotFound) {
		// Repositorio vacío: commit inicial con .nojekyll y se vuelve a leer.
		body := map[string]string{"message": "Inicia el sitio de la carta", "content": ""}
		if err := g.api("PUT", "/repos/"+g.repo+"/contents/.nojekyll", body, nil); err != nil {
			return "", "", fmt.Errorf("no se pudo iniciar el repositorio: %w", err)
		}
		return g.punta()
	}
	if err != nil {
		return "", "", err
	}
	var commit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := g.api("GET", "/repos/"+g.repo+"/git/commits/"+ref.Object.SHA, nil, &commit); err != nil {
		return "", "", err
	}
	g.ultimoCommit = ref.Object.SHA
	return repo.DefaultBranch, commit.Tree.SHA, nil
}

/**
 * Publicada indica si el sitio ya muestra la versión dada: tras publicar,
 * GitHub tarda alrededor de un minuto en desplegar. Lee la marca de versión
 * del index.html (con un parámetro distinto cada vez para evitar la caché).
 **/
func (g *GitHub) Publicada(version string) bool {
	res, err := g.client.Get(g.Sitio() + "?comprobar=" + aleatorio())
	if err != nil {
		return false
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 64<<10))
	return res.StatusCode == http.StatusOK && bytes.Contains(b, []byte(`content="`+version+`"`))
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
	// Rama por defecto del repositorio (ya existe porque se acaba de publicar).
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
