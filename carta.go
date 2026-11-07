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
	"strings"
	"sync"
)

/**
 * Producto es un plato o bebida de la carta.
 * - Precio va en pesos colombianos enteros (sin centavos).
 * - Imagen es el nombre del archivo guardado en data/img (vacío = sin foto).
 * - Incluir indica si el producto entra en el PDF que se publica con el QR.
 **/
type Producto struct {
	ID           string   `json:"id"`
	Nombre       string   `json:"nombre"`
	Categoria    string   `json:"categoria"`
	Descripcion  string   `json:"descripcion"`
	Precio       int64    `json:"precio"`
	Ingredientes []string `json:"ingredientes"`
	Imagen       string   `json:"imagen"`
	Incluir      bool     `json:"incluir"`
}

/**
 * Carta es el documento completo: datos del restaurante y lista de productos
 * en el orden en que el usuario los creó.
 **/
type Carta struct {
	Nombre    string     `json:"nombre"`
	Lema      string     `json:"lema"`
	Productos []Producto `json:"productos"`
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
	return Carta{Nombre: "El Regalo", Lema: "Comida típica colombiana", Productos: []Producto{}}
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
	// Evita null en JSON para que el frontend siempre reciba una lista.
	if c.Productos == nil {
		c.Productos = []Producto{}
	}
	return c, nil
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
		limpios := []string{}
		for _, ing := range p.Ingredientes {
			if ing = strings.TrimSpace(ing); ing != "" {
				limpios = append(limpios, ing)
			}
		}
		p.Ingredientes = limpios
	}
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
