/**
 * Pruebas del modelo de la carta (textos por idioma con respaldo en español)
 * y del pulido de las traducciones automáticas.
 **/
package main

import (
	"slices"
	"testing"
)

/**
 * TestEnIdiomaConRespaldo: cada campo sin traducir sale en español, y los
 * ingredientes traducidos solo se usan si la lista está completa.
 **/
func TestEnIdiomaConRespaldo(t *testing.T) {
	c := normalizar(Carta{
		Lema:       "Comida típica",
		Lemas:      map[string]string{"en": "Typical food"},
		Categorias: map[string]map[string]string{"Sopas": {"en": "Soups"}},
	})
	p := Producto{
		Nombre: "Ajiaco", Categoria: "Sopas", Descripcion: "Con pollo", Ingredientes: []string{"papa", "pollo"},
		Traducciones: map[string]Traduccion{"en": {Nombre: "Ajiaco soup", Ingredientes: []string{"potato"}}},
	}
	got := c.EnIdioma(p, "en")
	if got.Nombre != "Ajiaco soup" || got.Categoria != "Soups" || got.Descripcion != "Con pollo" {
		t.Fatalf("textos en inglés = %+v", got)
	}
	if !slices.Equal(got.Ingredientes, []string{"papa", "pollo"}) {
		t.Fatalf("ingredientes incompletos deberían salir en español: %v", got.Ingredientes)
	}
	if c.LemaEn("en") != "Typical food" || c.LemaEn("fr") != "Comida típica" {
		t.Fatal("lema por idioma con respaldo en español incorrecto")
	}
	// Francés sin traducción: todo en español.
	if fr := c.EnIdioma(p, "fr"); fr.Nombre != "Ajiaco" || fr.Categoria != "Sopas" {
		t.Fatalf("textos en francés = %+v", fr)
	}
}

/**
 * TestNormalizarIdiomas: el español siempre está, sin repetidos ni códigos
 * desconocidos, en el orden oficial.
 **/
func TestNormalizarIdiomas(t *testing.T) {
	c := normalizar(Carta{Idiomas: []string{"de", "xx", "en", "de"}})
	if !slices.Equal(c.Idiomas, []string{"es", "en", "de"}) {
		t.Fatalf("idiomas = %v", c.Idiomas)
	}
}

/**
 * TestPulir: quita puntuación agregada y mayúsculas sostenidas.
 **/
func TestPulir(t *testing.T) {
	casos := []struct{ orig, trad, want string }{
		{"Platos fuertes", "Plats principaux :", "Plats principaux"},
		{"Platos fuertes", "HAUPTGERICHTE", "Hauptgerichte"},
		{"Refrescante y cremosa.", "Erfrischend und cremig.", "Erfrischend und cremig."},
		{"BBQ", "BBQ", "BBQ"},
	}
	for _, c := range casos {
		if got := pulir(c.orig, c.trad); got != c.want {
			t.Errorf("pulir(%q, %q) = %q, se esperaba %q", c.orig, c.trad, got, c.want)
		}
	}
}
