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
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
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
 * idValido limita las versiones de la carta a 12 caracteres hexadecimales.
 **/
var idValido = regexp.MustCompile(`^[0-9a-f]{12}$`)

/**
 * App reúne la configuración y el almacenamiento que usan los handlers.
 **/
type App struct {
	store   *Store
	imgDir  string
	pubDir  string // copia local de la carta publicada
	baseURL string
	logo    []byte
	gh      *GitHub // nil = modo local (el QR apunta a este equipo)
	ghError string  // por qué no se pudo usar GitHub, para mostrarlo en el editor

	traductor *Traductor
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

	// Carpetas de datos: imágenes subidas y copia de la carta publicada.
	a := &App{
		store:   &Store{path: filepath.Join(dataDir, "carta.json")},
		imgDir:  filepath.Join(dataDir, "img"),
		pubDir:  filepath.Join(dataDir, "publicada"),
		baseURL: base,
	}
	for _, d := range []string{a.imgDir, a.pubDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			log.Fatal(err)
		}
	}
	a.logo, _ = webFS.ReadFile("web/logo.png")
	a.traductor = NuevoTraductor()

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
	mux.HandleFunc("POST /api/traducir", a.traducir)
	mux.HandleFunc("POST /api/publicar", a.publicar)
	mux.HandleFunc("GET /api/estado", a.estado)
	mux.HandleFunc("GET /api/vista/{archivo}", a.vista)
	mux.HandleFunc("GET /api/disponible/{version}", a.disponible)
	mux.HandleFunc("GET /api/qr", a.qr)
	mux.HandleFunc("GET /carta/{archivo...}", a.verCarta)
	mux.Handle("GET /img/", http.StripPrefix("/img/", http.FileServer(http.Dir(a.imgDir))))

	log.Printf("Editor de la carta: http://localhost:%s", port)
	log.Printf("Traducción automática: %s", a.traductor.nombre)
	if a.gh != nil {
		log.Printf("QR fijo de la carta (GitHub Pages): %s", a.urlCarta())
	} else {
		log.Printf("QR fijo de la carta: %s  (solo red local: configura GITHUB_TOKEN y GITHUB_REPO)", a.urlCarta())
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
 * versionDiseno cambia cuando cambia el diseño del PDF o del selector, para
 * que una carta igual publicada con el diseño nuevo cuente como cambio.
 **/
const versionDiseno = 3

/**
 * version devuelve la huella de todo lo que se ve en la carta publicada
 * (textos, precios, fotos, idiomas y selección). Dos cartas iguales tienen la
 * misma versión; sirve para saber si hay cambios sin publicar.
 **/
func version(c Carta) string {
	huella, _ := json.Marshal(map[string]any{
		"v": versionDiseno, "nombre": c.Nombre, "lema": c.Lema, "lemas": c.Lemas,
		"idiomas": c.Idiomas, "categorias": c.Categorias, "productos": c.Seleccionados(),
	})
	sum := sha256.Sum256(huella)
	return hex.EncodeToString(sum[:6])
}

/**
 * publicar genera la carta (un PDF por idioma activo y el index.html que
 * elige el idioma del celular) y reemplaza con ella la publicada en la
 * dirección fija del QR: el QR impreso siempre muestra la última versión.
 * Guarda una copia local (vista previa y modo local) y, si hay GitHub, la
 * sube a Pages en un solo commit. Devuelve la versión, la URL fija, la del QR
 * y los idiomas publicados.
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
	v := version(c)

	// Un PDF por idioma (con enlaces entre ellos) y la página selectora. El
	// ?v= de los enlaces obliga a los celulares a bajar la versión nueva.
	sitio := a.urlCarta()
	var archivos []Archivo
	for _, l := range c.Idiomas {
		pdf, err := GenerarPDF(c, sel, l, sitio+"%s.pdf?v="+v, a.imgDir, a.logo)
		if err != nil {
			fallo(w, http.StatusInternalServerError, err)
			return
		}
		archivos = append(archivos, Archivo{Nombre: l + ".pdf", Datos: pdf})
	}
	index, err := Selector(c, v)
	if err != nil {
		fallo(w, http.StatusInternalServerError, err)
		return
	}
	archivos = append(archivos, Archivo{Nombre: "index.html", Datos: index})
	// PDF de idiomas desactivados: se borran para que no queden desactualizados.
	var borrar []string
	for _, i := range Idiomas {
		if !slices.Contains(c.Idiomas, i.Codigo) {
			borrar = append(borrar, i.Codigo+".pdf")
		}
	}

	// Primero GitHub (si falla, la copia local no se toca y se puede reintentar).
	if a.gh != nil {
		msg := fmt.Sprintf("Carta actualizada: %d productos en %s", len(sel), strings.Join(c.Idiomas, ", "))
		if err := a.gh.Publicar(archivos, borrar, msg); err != nil {
			fallo(w, http.StatusBadGateway, err)
			return
		}
	}
	// Copia local: archivos nuevos, borrados y la versión publicada.
	for _, f := range archivos {
		if err := os.WriteFile(filepath.Join(a.pubDir, f.Nombre), f.Datos, 0o644); err != nil {
			fallo(w, http.StatusInternalServerError, err)
			return
		}
	}
	for _, f := range borrar {
		os.Remove(filepath.Join(a.pubDir, f))
	}
	os.WriteFile(filepath.Join(a.pubDir, "version"), []byte(v), 0o644)

	responder(w, map[string]any{
		"version":   v,
		"url":       sitio,
		"qr":        "/api/qr",
		"productos": len(sel),
		"idiomas":   c.Idiomas,
		"web":       a.gh != nil,
	})
}

/**
 * vista genera al vuelo el PDF de un idioma con la carta guardada, sin
 * publicarlo: es la vista previa del editor (la carta de las mesas no cambia).
 **/
func (a *App) vista(w http.ResponseWriter, r *http.Request) {
	l, ok := strings.CutSuffix(r.PathValue("archivo"), ".pdf")
	if !ok || !idiomaValido(l) {
		http.NotFound(w, r)
		return
	}
	c, err := a.store.Leer()
	if err != nil {
		fallo(w, http.StatusInternalServerError, err)
		return
	}
	// Sin enlaces de idioma: la vista previa no está publicada en ninguna dirección.
	pdf, err := GenerarPDF(c, c.Seleccionados(), l, "", a.imgDir, a.logo)
	if err != nil {
		fallo(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(pdf)
}

/**
 * estado indica si la carta guardada tiene cambios que todavía no se han
 * publicado (o si nunca se ha publicado), para avisar en el editor.
 **/
func (a *App) estado(w http.ResponseWriter, r *http.Request) {
	c, err := a.store.Leer()
	if err != nil {
		fallo(w, http.StatusInternalServerError, err)
		return
	}
	pub, _ := os.ReadFile(filepath.Join(a.pubDir, "version"))
	responder(w, map[string]bool{
		"publicada":  len(pub) > 0,
		"pendientes": string(pub) != version(c) && len(c.Seleccionados()) > 0,
	})
}

/**
 * traducir traduce textos del español al idioma pedido con el proveedor
 * configurado. Entrada: {"destino": "en", "textos": [...]}. Salida:
 * {"textos": [...]} en el mismo orden (los vacíos siguen vacíos).
 **/
func (a *App) traducir(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Destino string   `json:"destino"`
		Textos  []string `json:"textos"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 256<<10)).Decode(&in); err != nil || len(in.Textos) > 500 {
		fallo(w, http.StatusBadRequest, errors.New("petición de traducción inválida"))
		return
	}
	out, err := a.traductor.Traducir(in.Textos, in.Destino)
	if err != nil {
		fallo(w, http.StatusBadGateway, err)
		return
	}
	responder(w, map[string]any{"textos": out})
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
	responder(w, map[string]any{
		"modo": modo, "destino": destino, "error": a.ghError,
		"traductor": a.traductor.nombre, "idiomas": Idiomas,
	})
}

/**
 * disponible indica si la versión dada ya se ve en internet (GitHub Pages
 * tarda cerca de un minuto en desplegar). En modo local siempre lo está.
 **/
func (a *App) disponible(w http.ResponseWriter, r *http.Request) {
	v := r.PathValue("version")
	if !idValido.MatchString(v) {
		http.NotFound(w, r)
		return
	}
	responder(w, map[string]bool{"disponible": a.gh == nil || a.gh.Publicada(v)})
}

/**
 * qr devuelve el código QR (PNG de 1024 px) de la dirección fija de la carta.
 * Es siempre el mismo: se imprime una vez y cada publicación lo actualiza.
 * Con ?descargar=1 el navegador lo guarda como archivo.
 **/
func (a *App) qr(w http.ResponseWriter, r *http.Request) {
	png, err := qrcode.Encode(a.urlCarta(), qrcode.Medium, 1024)
	if err != nil {
		fallo(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	if r.URL.Query().Has("descargar") {
		w.Header().Set("Content-Disposition", `attachment; filename="qr-carta-el-regalo.png"`)
	}
	w.Write(png)
}

/**
 * archivoValido limita lo que se sirve de la carta publicada: su página
 * selectora y los PDF de los idiomas admitidos.
 **/
var archivoValido = regexp.MustCompile(`^(index\.html|(es|en|fr|de|pt)\.pdf)?$`)

/**
 * verCarta sirve la copia local de la carta publicada: la página selectora
 * (raíz) o un PDF. Los PDF se descargan (es lo que abre el QR en modo local);
 * con ?ver=1 se muestran en el navegador como vista previa. Se pide no
 * guardar en caché para que siempre se vea la última versión.
 **/
func (a *App) verCarta(w http.ResponseWriter, r *http.Request) {
	archivo := r.PathValue("archivo")
	if !archivoValido.MatchString(archivo) {
		http.NotFound(w, r)
		return
	}
	if archivo == "" {
		archivo = "index.html"
	}
	w.Header().Set("Cache-Control", "no-cache")
	if strings.HasSuffix(archivo, ".pdf") {
		modo := "attachment"
		if r.URL.Query().Has("ver") {
			modo = "inline"
		}
		w.Header().Set("Content-Disposition", modo+`; filename="carta-el-regalo-`+strings.TrimSuffix(archivo, ".pdf")+`.pdf"`)
	}
	// ServeContent en vez de ServeFile: ServeFile redirige ".../index.html" a "./".
	f, err := os.Open(filepath.Join(a.pubDir, archivo))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeContent(w, r, archivo, st.ModTime(), f)
}

/**
 * urlCarta es la dirección fija de la carta publicada, la que va en el QR:
 * la raíz del sitio de GitHub Pages o, en modo local, /carta/ de este equipo.
 **/
func (a *App) urlCarta() string {
	if a.gh != nil {
		return a.gh.Sitio()
	}
	return a.baseURL + "/carta/"
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
