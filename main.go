/**
 * QR Regalo: programa simple para armar la carta de El Regalo (productos,
 * precios, imágenes e ingredientes) y publicarla como un PDF al que apunta un
 * código QR descargable. Es un único binario: sirve el editor web embebido, la
 * API JSON, las imágenes, los PDF publicados y los QR.
 *
 * Variables de entorno (también se leen de un archivo .env junto al programa):
 *   GITHUB_TOKEN  token de GitHub con permiso de Contents y Pages (lectura y escritura).
 *   GITHUB_REPO   repositorio público donde se publican los PDF: usuario/nombre.
 *                 Con ambos, los QR apuntan a GitHub Pages y funcionan desde cualquier red.
 *   PORT          puerto de escucha (por defecto 8090).
 *   DATA_DIR      carpeta de datos (por defecto ./data).
 *   BASE_URL      solo sin GitHub: URL que irá en el QR (por defecto http://IP-de-la-red:PORT).
 **/
package main

import (
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

/**
 * webFS contiene el editor (HTML, CSS, JS y la marca) embebido en el binario.
 **/
//go:embed web
var webFS embed.FS

/**
 * maxImagen es el tamaño máximo de una foto subida (el navegador ya la reduce
 * a un JPEG cuadrado de 800 px, así que esto es solo un tope de seguridad).
 **/
const maxImagen = 8 << 20

/**
 * idValido limita los identificadores de PDF a hexadecimal, para que nunca
 * puedan usarse como rutas.
 **/
var idValido = regexp.MustCompile(`^[0-9a-f]{12}$`)

/**
 * App reúne la configuración y el almacenamiento que usan los handlers.
 **/
type App struct {
	store   *Store
	imgDir  string
	pdfDir  string
	baseURL string
	logo    []byte
	gh      *GitHub // nil = modo local (el QR apunta a este equipo)
	ghError string  // por qué no se pudo usar GitHub, para mostrarlo en el editor
}

/**
 * main prepara las carpetas de datos, registra las rutas y arranca el servidor.
 **/
func main() {
	// Configuración desde .env y el entorno, con valores por defecto.
	cargarEnv(".env")
	port := env("PORT", "8090")
	dataDir := env("DATA_DIR", "data")
	base := strings.TrimRight(env("BASE_URL", "http://"+ipLocal()+":"+port), "/")

	// Carpetas de datos: imágenes subidas y PDF publicados.
	a := &App{
		store:   &Store{path: filepath.Join(dataDir, "carta.json")},
		imgDir:  filepath.Join(dataDir, "img"),
		pdfDir:  filepath.Join(dataDir, "pdf"),
		baseURL: base,
	}
	for _, d := range []string{a.imgDir, a.pdfDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			log.Fatal(err)
		}
	}
	a.logo, _ = webFS.ReadFile("web/logo.png")

	// Publicación en GitHub Pages si está configurada; si falla, modo local con aviso.
	if token, repo := os.Getenv("GITHUB_TOKEN"), os.Getenv("GITHUB_REPO"); token != "" || repo != "" {
		gh, err := NuevoGitHub(token, repo)
		if err != nil {
			a.ghError = err.Error()
			log.Printf("GitHub Pages no disponible: %v", err)
		} else {
			a.gh = gh
		}
	}

	// Rutas: editor estático, API, imágenes, PDF y QR.
	static, _ := fs.Sub(webFS, "web")
	mux := http.NewServeMux()
	mux.Handle("GET /", http.FileServerFS(static))
	mux.HandleFunc("GET /api/carta", a.getCarta)
	mux.HandleFunc("PUT /api/carta", a.putCarta)
	mux.HandleFunc("POST /api/imagenes", a.subirImagen)
	mux.HandleFunc("GET /api/config", a.config)
	mux.HandleFunc("POST /api/publicar", a.publicar)
	mux.HandleFunc("GET /api/disponible/{id}", a.disponible)
	mux.HandleFunc("GET /api/qr/{id}", a.qr)
	mux.HandleFunc("GET /carta/{id}", a.verPDF)
	mux.Handle("GET /img/", http.StripPrefix("/img/", http.FileServer(http.Dir(a.imgDir))))

	log.Printf("Editor de la carta: http://localhost:%s", port)
	if a.gh != nil {
		log.Printf("Los QR apuntarán a GitHub Pages: %scarta/", a.gh.Sitio())
	} else {
		log.Printf("Los QR apuntarán a: %s  (solo red local: configura GITHUB_TOKEN y GITHUB_REPO)", base)
	}
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

/**
 * getCarta devuelve la carta completa en JSON.
 **/
func (a *App) getCarta(w http.ResponseWriter, r *http.Request) {
	c, err := a.store.Leer()
	if err != nil {
		fallo(w, http.StatusInternalServerError, err)
		return
	}
	responder(w, c)
}

/**
 * putCarta reemplaza la carta completa con la que envía el editor (que guarda
 * automáticamente tras cada cambio).
 **/
func (a *App) putCarta(w http.ResponseWriter, r *http.Request) {
	var c Carta
	if err := json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&c); err != nil {
		fallo(w, http.StatusBadRequest, errors.New("carta inválida"))
		return
	}
	if err := a.store.Guardar(c); err != nil {
		fallo(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

/**
 * subirImagen recibe una foto (campo "imagen"), verifica que sea JPEG o PNG
 * por su contenido real y la guarda con un nombre aleatorio.
 * Respuesta: {"nombre": "a1b2c3….jpg"}.
 **/
func (a *App) subirImagen(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImagen)
	f, _, err := r.FormFile("imagen")
	if err != nil {
		fallo(w, http.StatusBadRequest, errors.New("imagen no recibida o demasiado grande"))
		return
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		fallo(w, http.StatusBadRequest, err)
		return
	}
	// Solo JPEG y PNG: son los formatos que el PDF puede incrustar.
	ext := map[string]string{"image/jpeg": ".jpg", "image/png": ".png"}[http.DetectContentType(b)]
	if ext == "" {
		fallo(w, http.StatusUnsupportedMediaType, errors.New("solo se aceptan imágenes JPG o PNG"))
		return
	}
	nombre := aleatorio() + ext
	if err := os.WriteFile(filepath.Join(a.imgDir, nombre), b, 0o644); err != nil {
		fallo(w, http.StatusInternalServerError, err)
		return
	}
	responder(w, map[string]string{"nombre": nombre})
}

/**
 * publicar genera el PDF con los productos seleccionados y lo guarda con un
 * identificador derivado de su contenido: la misma selección produce siempre
 * el mismo enlace (y el mismo QR). Devuelve el id, la URL del PDF y la del QR.
 **/
func (a *App) publicar(w http.ResponseWriter, r *http.Request) {
	c, err := a.store.Leer()
	if err != nil {
		fallo(w, http.StatusInternalServerError, err)
		return
	}
	sel := c.Seleccionados()
	if len(sel) == 0 {
		fallo(w, http.StatusBadRequest, errors.New("selecciona al menos un producto"))
		return
	}
	pdf, err := GenerarPDF(c, sel, a.imgDir, a.logo)
	if err != nil {
		fallo(w, http.StatusInternalServerError, err)
		return
	}
	// El id es un hash del PDF: mismo contenido → mismo QR.
	sum := sha256.Sum256(pdf)
	id := hex.EncodeToString(sum[:6])
	// Copia local (para la vista previa) y, si hay GitHub, publicación en Pages.
	if err := os.WriteFile(filepath.Join(a.pdfDir, id+".pdf"), pdf, 0o644); err != nil {
		fallo(w, http.StatusInternalServerError, err)
		return
	}
	if a.gh != nil {
		if err := a.gh.Subir(id, pdf, len(sel)); err != nil {
			fallo(w, http.StatusBadGateway, err)
			return
		}
	}
	responder(w, map[string]any{
		"id":        id,
		"url":       a.urlPDF(id),
		"qr":        "/api/qr/" + id,
		"productos": len(sel),
		"web":       a.gh != nil,
	})
}

/**
 * config informa al editor dónde se publican los PDF: "web" (GitHub Pages,
 * funciona desde cualquier red) o "local" (solo misma red), con el motivo si
 * GitHub está configurado pero no se pudo usar.
 **/
func (a *App) config(w http.ResponseWriter, r *http.Request) {
	modo, destino := "local", a.baseURL
	if a.gh != nil {
		modo, destino = "web", a.gh.Sitio()
	}
	responder(w, map[string]string{"modo": modo, "destino": destino, "error": a.ghError})
}

/**
 * disponible indica si el PDF publicado ya se puede abrir desde internet
 * (GitHub Pages tarda cerca de un minuto en desplegar). En modo local siempre
 * está disponible.
 **/
func (a *App) disponible(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !idValido.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	responder(w, map[string]bool{"disponible": a.gh == nil || a.gh.Disponible(id)})
}

/**
 * qr devuelve el código QR (PNG de 1024 px) que apunta al PDF publicado. Con
 * ?descargar=1 el navegador lo guarda como archivo.
 **/
func (a *App) qr(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(r.PathValue("id"), ".png")
	if !idValido.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	png, err := qrcode.Encode(a.urlPDF(id), qrcode.Medium, 1024)
	if err != nil {
		fallo(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	if r.URL.Query().Has("descargar") {
		w.Header().Set("Content-Disposition", `attachment; filename="qr-carta-`+id+`.png"`)
	}
	w.Write(png)
}

/**
 * verPDF sirve un PDF publicado. Por defecto se descarga (es lo que abre el
 * QR); con ?ver=1 se muestra en el navegador como vista previa.
 **/
func (a *App) verPDF(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSuffix(r.PathValue("id"), ".pdf")
	if !idValido.MatchString(id) {
		http.NotFound(w, r)
		return
	}
	modo := "attachment"
	if r.URL.Query().Has("ver") {
		modo = "inline"
	}
	w.Header().Set("Content-Disposition", modo+`; filename="carta-el-regalo.pdf"`)
	http.ServeFile(w, r, filepath.Join(a.pdfDir, id+".pdf"))
}

/**
 * urlPDF es la dirección pública del PDF que se codifica en el QR.
 **/
func (a *App) urlPDF(id string) string {
	if a.gh != nil {
		return a.gh.URL(id)
	}
	return a.baseURL + "/carta/" + id + ".pdf"
}

/**
 * responder escribe v como JSON.
 **/
func responder(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

/**
 * fallo responde {"error": "..."} con el código HTTP dado.
 **/
func fallo(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

/**
 * aleatorio devuelve 16 caracteres hexadecimales aleatorios para nombrar archivos.
 **/
func aleatorio() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

/**
 * env lee una variable de entorno o devuelve el valor por defecto.
 **/
func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

/**
 * cargarEnv lee un archivo .env sencillo (líneas CLAVE=valor, # para
 * comentarios) y define las variables que no estén ya en el entorno. Si el
 * archivo no existe no hace nada.
 **/
func cargarEnv(path string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, linea := range strings.Split(string(b), "\n") {
		// Ignora vacías y comentarios.
		linea = strings.TrimSpace(linea)
		if linea == "" || strings.HasPrefix(linea, "#") {
			continue
		}
		k, v, ok := strings.Cut(linea, "=")
		if !ok {
			continue
		}
		// El entorno real tiene prioridad sobre el archivo.
		k, v = strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`)
		if _, existe := os.LookupEnv(k); !existe {
			os.Setenv(k, v)
		}
	}
}

/**
 * ipLocal devuelve la IP de este equipo en la red local, para que el QR
 * funcione desde un celular conectado al mismo Wi-Fi. No envía datos: UDP no
 * establece conexión, solo consulta qué interfaz se usaría.
 **/
func ipLocal() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "localhost"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}
