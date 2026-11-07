/**
 * Modelo de la carta (restaurante y productos) y su almacenamiento en un
 * archivo JSON dentro de la carpeta de datos. Es deliberadamente simple: un
 * solo documento que el editor web lee y guarda completo.
 **/
package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

/**
 * IdiomaBase es el idioma en que se escribe la carta; los demás son traducciones.
 **/
const IdiomaBase = "es"

/**
 * Idiomas son los idiomas que admite la carta (los mismos del catálogo El
 * Regalo), en el orden en que se muestran, con su nombre en su propio idioma.
 * Todos usan el alfabeto latino, que la fuente del PDF puede dibujar.
 **/
var Idiomas = []struct{ Codigo, Nombre string }{
	{"es", "Español"},
	{"en", "English"},
	{"fr", "Français"},
	{"de", "Deutsch"},
	{"pt", "Português"},
}

/**
 * idiomaValido indica si el código pertenece a la lista de idiomas admitidos.
 **/
func idiomaValido(codigo string) bool {
	for _, i := range Idiomas {
		if i.Codigo == codigo {
			return true
		}
	}
	return false
}

/**
 * Traduccion son los textos de un producto en otro idioma. Un campo vacío
 * significa "sin traducir": en el PDF se usa el texto en español.
 **/
type Traduccion struct {
	Nombre       string   `json:"nombre"`
	Descripcion  string   `json:"descripcion"`
	Ingredientes []string `json:"ingredientes"`
}

/**
 * Producto es un plato o bebida de la carta.
 * - Precio va en pesos colombianos enteros (sin centavos).
 * - Imagen es el nombre del archivo guardado en data/img (vacío = sin foto).
 * - Incluir indica si el producto entra en el PDF que se publica con el QR.
 * - Traducciones guarda los textos por código de idioma ("en", "fr", …).
 **/
type Producto struct {
	ID           string                `json:"id"`
	Nombre       string                `json:"nombre"`
	Categoria    string                `json:"categoria"`
	Descripcion  string                `json:"descripcion"`
	Precio       int64                 `json:"precio"`
	Ingredientes []string              `json:"ingredientes"`
	Imagen       string                `json:"imagen"`
	Incluir      bool                  `json:"incluir"`
	Traducciones map[string]Traduccion `json:"traducciones"`
}

/**
 * Carta es el documento completo: datos del restaurante, idiomas activos,
 * traducciones del lema y de las categorías, y la lista de productos en el
 * orden en que el usuario los creó.
 * - Idiomas: códigos publicados; siempre incluye el español.
 * - Lemas: lema por idioma.
 * - Categorias: nombre de la categoría en español → idioma → traducción (se
 *   traduce una vez por categoría, no en cada producto).
 * - OrdenCategorias: orden en que salen las categorías en la carta (nombres en
 *   español); las que no estén en la lista van después, en orden de aparición.
 * - Eslogan / Eslogans: frase de la portada y sus traducciones (si está vacío,
 *   la portada muestra el lema). SinPortada quita la portada del PDF.
 **/
type Carta struct {
	Nombre     string                       `json:"nombre"`
	Lema       string                       `json:"lema"`
	Idiomas    []string                     `json:"idiomas"`
	Lemas      map[string]string            `json:"lemas"`
	Categorias map[string]map[string]string `json:"categorias"`
	Productos  []Producto                   `json:"productos"`

	OrdenCategorias []string `json:"ordenCategorias"`

	SinPortada bool              `json:"sinPortada"`
	Eslogan    string            `json:"eslogan"`
	Eslogans   map[string]string `json:"eslogans"`
}

/**
 * Store guarda la carta en disco protegida por un mutex, porque el servidor
 * atiende peticiones en paralelo.
 **/
type Store struct {
	mu   sync.Mutex
	path string
}

/**
 * cartaInicial es la carta que se muestra la primera vez, con el nombre del
 * restaurante ya puesto y sin productos.
 **/
func cartaInicial() Carta {
	return normalizar(Carta{Nombre: "El Regalo", Lema: "Comida típica colombiana", Idiomas: []string{"es", "en"}})
}

/**
 * Leer devuelve la carta guardada o la inicial si todavía no existe el archivo.
 **/
func (s *Store) Leer() (Carta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Sin archivo todavía: carta vacía con el nombre por defecto.
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return cartaInicial(), nil
	}
	if err != nil {
		return Carta{}, err
	}
	var c Carta
	if err := json.Unmarshal(b, &c); err != nil {
		return Carta{}, err
	}
	return normalizar(c), nil
}

/**
 * normalizar deja la carta lista para el editor y el PDF: listas y mapas sin
 * null, idiomas válidos sin repetir y en el orden oficial, con el español
 * siempre presente.
 **/
func normalizar(c Carta) Carta {
	// Idiomas: el español primero, luego los activos en el orden de la lista.
	activos := map[string]bool{IdiomaBase: true}
	for _, l := range c.Idiomas {
		activos[l] = true
	}
	c.Idiomas = []string{}
	for _, i := range Idiomas {
		if activos[i.Codigo] {
			c.Idiomas = append(c.Idiomas, i.Codigo)
		}
	}
	// Evita null en JSON para que el frontend siempre reciba listas y mapas.
	if c.Lemas == nil {
		c.Lemas = map[string]string{}
	}
	if c.Categorias == nil {
		c.Categorias = map[string]map[string]string{}
	}
	if c.OrdenCategorias == nil {
		c.OrdenCategorias = []string{}
	}
	if c.Eslogans == nil {
		c.Eslogans = map[string]string{}
	}
	if c.Productos == nil {
		c.Productos = []Producto{}
	}
	for i := range c.Productos {
		p := &c.Productos[i]
		if p.Ingredientes == nil {
			p.Ingredientes = []string{}
		}
		if p.Traducciones == nil {
			p.Traducciones = map[string]Traduccion{}
		}
	}
	return c
}

/**
 * Guardar limpia la carta (espacios, ingredientes vacíos, imágenes con rutas
 * sospechosas) y la escribe de forma atómica: primero a un temporal y luego
 * se renombra, para no dejar el JSON a medias si el programa se cierra.
 **/
func (s *Store) Guardar(c Carta) error {
	// Normaliza los textos y descarta datos inválidos.
	c.Nombre = strings.TrimSpace(c.Nombre)
	c.Lema = strings.TrimSpace(c.Lema)
	c.Eslogan = strings.TrimSpace(c.Eslogan)
	for i := range c.Productos {
		p := &c.Productos[i]
		p.Nombre = strings.TrimSpace(p.Nombre)
		p.Categoria = strings.TrimSpace(p.Categoria)
		p.Descripcion = strings.TrimSpace(p.Descripcion)
		p.Precio = max(p.Precio, 0)
		// Solo se aceptan nombres de archivo simples (sin carpetas).
		if p.Imagen != filepath.Base(p.Imagen) {
			p.Imagen = ""
		}
		p.Ingredientes = limpiarLista(p.Ingredientes)
		// Traducciones: solo idiomas admitidos (no el base) y textos limpios.
		for l, t := range p.Traducciones {
			if l == IdiomaBase || !idiomaValido(l) {
				delete(p.Traducciones, l)
				continue
			}
			t.Nombre = strings.TrimSpace(t.Nombre)
			t.Descripcion = strings.TrimSpace(t.Descripcion)
			t.Ingredientes = limpiarLista(t.Ingredientes)
			p.Traducciones[l] = t
		}
	}
	c.OrdenCategorias = limpiarLista(c.OrdenCategorias)
	c = normalizar(c)
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// Escritura atómica: temporal + renombrar.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

/**
 * limpiarLista quita espacios y elementos vacíos de una lista de textos.
 **/
func limpiarLista(xs []string) []string {
	out := []string{}
	for _, x := range xs {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}

/**
 * Seleccionados devuelve solo los productos marcados para incluir en el PDF,
 * conservando el orden de la carta.
 **/
func (c Carta) Seleccionados() []Producto {
	out := []Producto{}
	for _, p := range c.Productos {
		if p.Incluir && p.Nombre != "" {
			out = append(out, p)
		}
	}
	return out
}

/**
 * Ordenados devuelve los productos agrupados por categoría según
 * OrdenCategorias: primero las categorías de la lista (en ese orden), luego
 * las demás en orden de aparición y al final los productos sin categoría.
 * Dentro de cada categoría se conserva el orden de la carta.
 **/
func (c Carta) Ordenados(productos []Producto) []Producto {
	// Posición de cada categoría: la de la lista o, si no está, por aparición.
	rango := map[string]int{}
	for i, cat := range c.OrdenCategorias {
		if _, ok := rango[cat]; !ok {
			rango[cat] = i
		}
	}
	for _, p := range productos {
		if _, ok := rango[p.Categoria]; !ok && p.Categoria != "" {
			rango[p.Categoria] = len(c.OrdenCategorias) + len(rango)
		}
	}
	// Sin categoría ("Otros") siempre al final.
	rango[""] = len(c.OrdenCategorias) + len(rango) + 1
	out := slices.Clone(productos)
	slices.SortStableFunc(out, func(a, b Producto) int { return rango[a.Categoria] - rango[b.Categoria] })
	return out
}

/**
 * Textos son los textos de un producto ya resueltos para un idioma: la
 * traducción si existe y, campo por campo, el español si falta.
 **/
type Textos struct {
	Nombre, Categoria, Descripcion string
	Ingredientes                   []string
}

/**
 * EnIdioma devuelve los textos del producto p en el idioma l, usando el
 * español como respaldo para cada campo sin traducir.
 **/
func (c Carta) EnIdioma(p Producto, l string) Textos {
	t := Textos{Nombre: p.Nombre, Categoria: p.Categoria, Descripcion: p.Descripcion, Ingredientes: p.Ingredientes}
	if l == IdiomaBase {
		return t
	}
	tr := p.Traducciones[l]
	if tr.Nombre != "" {
		t.Nombre = tr.Nombre
	}
	if tr.Descripcion != "" {
		t.Descripcion = tr.Descripcion
	}
	// Ingredientes: se usa la lista traducida solo si está completa.
	if len(tr.Ingredientes) == len(p.Ingredientes) && len(tr.Ingredientes) > 0 {
		t.Ingredientes = tr.Ingredientes
	}
	if cat := c.Categorias[p.Categoria][l]; cat != "" {
		t.Categoria = cat
	}
	return t
}

/**
 * LemaEn devuelve el lema en el idioma l o el español si no está traducido.
 **/
func (c Carta) LemaEn(l string) string {
	if v := c.Lemas[l]; l != IdiomaBase && v != "" {
		return v
	}
	return c.Lema
}

/**
 * EsloganEn devuelve el eslogan de la portada en el idioma l: la traducción,
 * el eslogan en español o, si no hay eslogan, el lema en ese idioma.
 **/
func (c Carta) EsloganEn(l string) string {
	if c.Eslogan == "" {
		return c.LemaEn(l)
	}
	if v := c.Eslogans[l]; l != IdiomaBase && v != "" {
		return v
	}
	return c.Eslogan
}
