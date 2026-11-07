/**
 * Pruebas del publicador de GitHub Pages contra una API de GitHub simulada
 * (repositorio, commits, árboles, blobs y Pages): primera publicación en un
 * repositorio vacío, republicación sin cambios, idioma desactivado, URL
 * pública y errores de configuración.
 **/
package main

import (
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
)

/**
 * apiFalsa simula los endpoints de GitHub que usa el publicador: guarda
 * blobs, árboles (ruta → blob), commits (commit → árbol) y la punta de la rama.
 **/
type apiFalsa struct {
	mu      sync.Mutex
	privado bool
	pages   bool
	blobs   map[string][]byte
	arboles map[string]map[string]string
	commits map[string]string
	punta   string // "" = repositorio vacío
	nuevos  int    // commits creados
}

func nuevaAPIFalsa() *apiFalsa {
	return &apiFalsa{blobs: map[string][]byte{}, arboles: map[string]map[string]string{}, commits: map[string]string{}}
}

/**
 * hash da un identificador estable a cualquier valor (como los SHA de git).
 **/
func hash(v any) string {
	b, _ := json.Marshal(v)
	s := sha1.Sum(b)
	return hex.EncodeToString(s[:])
}

/**
 * commit crea un commit con el árbol dado y mueve la punta de la rama.
 **/
func (f *apiFalsa) commit(arbol map[string]string) string {
	ta := hash(arbol)
	f.arboles[ta] = arbol
	c := hash([]string{f.punta, ta})
	f.commits[c] = ta
	f.punta = c
	f.nuevos++
	return c
}

/**
 * archivos devuelve el contenido de los archivos en la punta de la rama.
 **/
func (f *apiFalsa) archivos() map[string]string {
	out := map[string]string{}
	for ruta, b := range f.arboles[f.commits[f.punta]] {
		out[ruta] = string(f.blobs[b])
	}
	return out
}

func (f *apiFalsa) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Toda petición debe ir autenticada con el token.
	if r.Header.Get("Authorization") != "Bearer tk" {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	const base = "/repos/Usuario/carta"
	ruta := strings.TrimPrefix(r.URL.Path, base)
	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)
	switch {
	case r.Method == "GET" && ruta == "":
		json.NewEncoder(w).Encode(map[string]any{"private": f.privado, "default_branch": "main"})
	case r.Method == "GET" && ruta == "/pages":
		if !f.pages {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"html_url": "https://usuario.github.io/carta/"})
	case r.Method == "POST" && ruta == "/pages":
		f.pages = true
		w.WriteHeader(http.StatusCreated)
	case r.Method == "GET" && ruta == "/git/ref/heads/main":
		if f.punta == "" {
			w.WriteHeader(http.StatusConflict)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"object": map[string]string{"sha": f.punta}})
	case r.Method == "PUT" && ruta == "/contents/.nojekyll":
		f.blobs["vacio"] = nil
		f.commit(map[string]string{".nojekyll": "vacio"})
		w.WriteHeader(http.StatusCreated)
	case r.Method == "GET" && strings.HasPrefix(ruta, "/git/commits/"):
		json.NewEncoder(w).Encode(map[string]any{"tree": map[string]string{"sha": f.commits[strings.TrimPrefix(ruta, "/git/commits/")]}})
	case r.Method == "GET" && strings.HasPrefix(ruta, "/git/trees/"):
		sha := strings.TrimPrefix(ruta, "/git/trees/")
		var tree []map[string]string
		for p := range f.arboles[sha] {
			tree = append(tree, map[string]string{"path": p})
		}
		json.NewEncoder(w).Encode(map[string]any{"sha": sha, "tree": tree})
	case r.Method == "POST" && ruta == "/git/blobs":
		b, _ := base64.StdEncoding.DecodeString(body["content"].(string))
		sha := hash(string(b))
		f.blobs[sha] = b
		json.NewEncoder(w).Encode(map[string]string{"sha": sha})
	case r.Method == "POST" && ruta == "/git/trees":
		// Árbol nuevo = base + entradas; sha nulo borra (y falla si no existe, como GitHub).
		nuevo := map[string]string{}
		for p, b := range f.arboles[body["base_tree"].(string)] {
			nuevo[p] = b
		}
		for _, e := range body["tree"].([]any) {
			e := e.(map[string]any)
			p := e["path"].(string)
			if e["sha"] == nil {
				if _, ok := nuevo[p]; !ok {
					w.WriteHeader(http.StatusUnprocessableEntity)
					return
				}
				delete(nuevo, p)
				continue
			}
			nuevo[p] = e["sha"].(string)
		}
		sha := hash(nuevo)
		f.arboles[sha] = nuevo
		json.NewEncoder(w).Encode(map[string]string{"sha": sha})
	case r.Method == "POST" && ruta == "/git/commits":
		padres := body["parents"].([]any)
		if len(padres) != 1 || padres[0] != f.punta {
			w.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		c := hash([]any{f.punta, body["tree"]})
		f.commits[c] = body["tree"].(string)
		json.NewEncoder(w).Encode(map[string]string{"sha": c})
	case r.Method == "PATCH" && ruta == "/git/refs/heads/main":
		f.punta = body["sha"].(string)
		f.nuevos++
		w.Write([]byte(`{}`))
	default:
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, `{"message":"ruta no simulada %s %s"}`, r.Method, ruta)
	}
}

/**
 * claves devuelve las rutas de un mapa ordenadas, para comparar.
 **/
func claves(m map[string]string) string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return strings.Join(ks, ",")
}

/**
 * TestPublicarEnDireccionFija: en un repositorio vacío crea .nojekyll y la
 * carta en un solo commit y activa Pages; republicar lo mismo no crea commit;
 * desactivar un idioma borra su PDF.
 **/
func TestPublicarEnDireccionFija(t *testing.T) {
	f := nuevaAPIFalsa()
	srv := httptest.NewServer(f)
	defer srv.Close()
	apiGitHub = srv.URL

	g, err := NuevoGitHub("tk", "Usuario/carta")
	if err != nil {
		t.Fatal(err)
	}
	// Primera publicación: commit inicial (.nojekyll) + commit de la carta.
	v1 := []Archivo{{"es.pdf", []byte("%PDF-es")}, {"en.pdf", []byte("%PDF-en")}, {"index.html", []byte("<html>")}}
	if err := g.Publicar(v1, []string{"fr.pdf", "de.pdf", "pt.pdf"}, "prueba"); err != nil {
		t.Fatal(err)
	}
	if got := claves(f.archivos()); got != ".nojekyll,en.pdf,es.pdf,index.html" {
		t.Fatalf("archivos publicados = %s", got)
	}
	if f.nuevos != 2 || !f.pages {
		t.Fatalf("commits = %d, pages = %v", f.nuevos, f.pages)
	}
	if got, want := g.Sitio(), "https://usuario.github.io/carta/"; got != want {
		t.Fatalf("Sitio = %s, se esperaba %s", got, want)
	}
	// Misma carta: no se crea otro commit.
	if err := g.Publicar(v1, []string{"fr.pdf"}, "prueba"); err != nil {
		t.Fatal(err)
	}
	if f.nuevos != 2 {
		t.Fatalf("republicar sin cambios creó un commit (total %d)", f.nuevos)
	}
	// Inglés desactivado y español nuevo: se reemplaza es.pdf y se borra en.pdf.
	v2 := []Archivo{{"es.pdf", []byte("%PDF-es-2")}, {"index.html", []byte("<html>2")}}
	if err := g.Publicar(v2, []string{"en.pdf", "fr.pdf"}, "prueba"); err != nil {
		t.Fatal(err)
	}
	a := f.archivos()
	if claves(a) != ".nojekyll,es.pdf,index.html" || a["es.pdf"] != "%PDF-es-2" {
		t.Fatalf("archivos tras actualizar = %v", a)
	}
}

/**
 * TestRepositorioPrivadoYFormato: un repositorio privado, un nombre mal
 * escrito o un token inválido se rechazan con un error claro.
 **/
func TestRepositorioPrivadoYFormato(t *testing.T) {
	f := nuevaAPIFalsa()
	f.privado = true
	srv := httptest.NewServer(f)
	defer srv.Close()
	apiGitHub = srv.URL

	if _, err := NuevoGitHub("tk", "Usuario/carta"); err == nil {
		t.Fatal("se esperaba error por repositorio privado")
	}
	if _, err := NuevoGitHub("tk", "sin-barra"); err == nil {
		t.Fatal("se esperaba error por formato de GITHUB_REPO")
	}
	if _, err := NuevoGitHub("malo", "Usuario/carta"); err == nil {
		t.Fatal("se esperaba error por token inválido")
	}
}
