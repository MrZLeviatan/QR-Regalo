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

/**
 * TestOrdenados: primero las categorías en el orden elegido, luego las que no
 * están en la lista (por aparición) y al final los productos sin categoría;
 * dentro de cada categoría se conserva el orden de la carta.
 **/
func TestOrdenados(t *testing.T) {
	c := Carta{OrdenCategorias: []string{"Fritos", "Postres"}}
	ps := []Producto{
		{Nombre: "Latte", Categoria: "Cafés"},
		{Nombre: "Torta", Categoria: "Postres"},
		{Nombre: "Suelto"},
		{Nombre: "Empanada", Categoria: "Fritos"},
		{Nombre: "Dedos", Categoria: "Fritos"},
		{Nombre: "Expreso", Categoria: "Cafés"},
	}
	var got []string
	for _, p := range c.Ordenados(ps) {
		got = append(got, p.Nombre)
	}
	want := []string{"Empanada", "Dedos", "Torta", "Latte", "Expreso", "Suelto"}
	if !slices.Equal(got, want) {
		t.Fatalf("orden = %v, se esperaba %v", got, want)
	}
}

/**
 * TestEsloganEn: traducción si existe, español si falta y, sin eslogan, el
 * lema del idioma.
 **/
func TestEsloganEn(t *testing.T) {
	c := normalizar(Carta{Lema: "Comida típica", Lemas: map[string]string{"en": "Typical food"}})
	if c.EsloganEn("en") != "Typical food" {
		t.Fatal("sin eslogan debe usarse el lema traducido")
	}
	c.Eslogan, c.Eslogans = "Sabor de casa", map[string]string{"en": "Taste of home"}
	if c.EsloganEn("en") != "Taste of home" || c.EsloganEn("fr") != "Sabor de casa" || c.EsloganEn("es") != "Sabor de casa" {
		t.Fatal("eslogan por idioma con respaldo en español incorrecto")
	}
}
