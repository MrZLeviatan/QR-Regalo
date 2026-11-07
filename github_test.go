/**
 * Pruebas del publicador de GitHub Pages contra una API de GitHub simulada:
 * subida del PDF, activación de Pages, URL pública y repositorio privado.
 **/
package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

/**
 * apiFalsa simula los endpoints de GitHub que usa el publicador y guarda los
 * archivos subidos y si Pages está activo.
 **/
type apiFalsa struct {
	mu       sync.Mutex
	privado  bool
	pages    bool
	archivos map[string][]byte
	subidas  int
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
	switch {
	case r.Method == "GET" && r.URL.Path == base:
		json.NewEncoder(w).Encode(map[string]any{"private": f.privado, "default_branch": "main"})
	case r.Method == "GET" && r.URL.Path == base+"/pages":
		if !f.pages {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"html_url": "https://usuario.github.io/carta/"})
	case r.Method == "POST" && r.URL.Path == base+"/pages":
		f.pages = true
		w.WriteHeader(http.StatusCreated)
	case r.Method == "GET" && len(r.URL.Path) > len(base+"/contents/"):
		if _, ok := f.archivos[r.URL.Path]; !ok {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{}`))
	case r.Method == "PUT":
		var body struct{ Content string }
		json.NewDecoder(r.Body).Decode(&body)
		b, _ := base64.StdEncoding.DecodeString(body.Content)
		f.archivos[r.URL.Path] = b
		f.subidas++
		w.WriteHeader(http.StatusCreated)
	default:
		http.NotFound(w, r)
	}
}

/**
 * TestSubirYActivarPages: la primera publicación sube el PDF y activa Pages;
 * la segunda con el mismo id no vuelve a subirlo.
 **/
func TestSubirYActivarPages(t *testing.T) {
	f := &apiFalsa{archivos: map[string][]byte{}}
	srv := httptest.NewServer(f)
	defer srv.Close()
	apiGitHub = srv.URL

	g, err := NuevoGitHub("tk", "Usuario/carta")
	if err != nil {
		t.Fatal(err)
	}
	// Sube el PDF y comprueba contenido, Pages activo y URL pública.
	if err := g.Subir("abc123abc123", []byte("%PDF-prueba"), 2); err != nil {
		t.Fatal(err)
	}
	if got := string(f.archivos["/repos/Usuario/carta/contents/carta/abc123abc123.pdf"]); got != "%PDF-prueba" {
		t.Fatalf("contenido subido = %q", got)
	}
	if !f.pages {
		t.Fatal("Pages no se activó")
	}
	if got, want := g.URL("abc123abc123"), "https://usuario.github.io/carta/carta/abc123abc123.pdf"; got != want {
		t.Fatalf("URL = %s, se esperaba %s", got, want)
	}
	// Mismo id: ya existe, no se sube otra vez.
	if err := g.Subir("abc123abc123", []byte("%PDF-prueba"), 2); err != nil {
		t.Fatal(err)
	}
	if f.subidas != 1 {
		t.Fatalf("subidas = %d, se esperaba 1", f.subidas)
	}
}

/**
 * TestRepositorioPrivadoYFormato: un repositorio privado o un nombre mal
 * escrito se rechazan con un error claro.
 **/
func TestRepositorioPrivadoYFormato(t *testing.T) {
	srv := httptest.NewServer(&apiFalsa{privado: true, archivos: map[string][]byte{}})
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
