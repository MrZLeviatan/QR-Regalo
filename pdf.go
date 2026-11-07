/**
 * Generación del PDF de la carta con el mismo diseño general del catálogo El
 * Regalo: fondo crema, franja tricolor arriba, cabecera verde con el logo y el
 * nombre, categorías en verde oscuro y cada producto en una tarjeta blanca con
 * su foto cuadrada a la derecha, precio, descripción e ingredientes.
 **/
package main

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/phpdave11/gofpdf"
)

/**
 * Medidas de la página A4 en milímetros y de las tarjetas.
 **/
const (
	pageW   = 210.0
	pageH   = 297.0
	margin  = 14.0
	bottom  = 16.0 // espacio reservado para el pie de página
	pad     = 4.0  // relleno interior de cada tarjeta
	imgSize = 30.0 // lado de la foto cuadrada
	gap     = 4.0  // separación entre tarjetas
)

/**
 * rgb es un color de la identidad de El Regalo (los mismos valores que el
 * tema compartido del catálogo).
 **/
type rgb struct{ r, g, b int }

var (
	verde       = rgb{0x2b, 0x7a, 0x57}
	verdeOscuro = rgb{0x1d, 0x5a, 0x40}
	amarillo    = rgb{0xfc, 0xd1, 0x16}
	azul        = rgb{0x1f, 0x5f, 0xb8}
	rojo        = rgb{0xce, 0x11, 0x26}
	crema       = rgb{0xff, 0xf8, 0xe7}
	tinta       = rgb{0x1a, 0x1c, 0x20}
	gris        = rgb{0x6b, 0x6d, 0x70}
	borde       = rgb{0xe6, 0xe1, 0xd4}
)

/**
 * etiquetas son los textos fijos del PDF en cada idioma.
 **/
var etiquetas = map[string]map[string]string{
	"es": {"ingredientes": "Ingredientes", "pagina": "página", "otros": "Otros", "vacio": "No hay productos seleccionados.", "idioma": "Idioma", "carta": "Carta"},
	"en": {"ingredientes": "Ingredients", "pagina": "page", "otros": "Others", "vacio": "No products selected.", "idioma": "Language", "carta": "Menu"},
	"fr": {"ingredientes": "Ingrédients", "pagina": "page", "otros": "Autres", "vacio": "Aucun produit sélectionné.", "idioma": "Langue", "carta": "Carte"},
	"de": {"ingredientes": "Zutaten", "pagina": "Seite", "otros": "Sonstiges", "vacio": "Keine Produkte ausgewählt.", "idioma": "Sprache", "carta": "Speisekarte"},
	"pt": {"ingredientes": "Ingredientes", "pagina": "página", "otros": "Outros", "vacio": "Nenhum produto selecionado.", "idioma": "Idioma", "carta": "Cardápio"},
}

/**
 * etiqueta devuelve un texto fijo en el idioma dado (o en español si falta).
 **/
func etiqueta(idioma, clave string) string {
	if v := etiquetas[idioma][clave]; v != "" {
		return v
	}
	return etiquetas[IdiomaBase][clave]
}

/**
 * pesos formatea un precio en pesos colombianos: 25000 → "$ 25.000".
 **/
func pesos(v int64) string {
	s := fmt.Sprintf("%d", v)
	// Inserta un punto cada tres cifras desde la derecha.
	var b strings.Builder
	for i, ch := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(ch)
	}
	return "$ " + b.String()
}

/**
 * GenerarPDF arma el PDF en un idioma con los productos seleccionados
 * agrupados por categoría (en el orden en que aparece cada categoría por
 * primera vez). Los textos sin traducir salen en español.
 * Si la carta tiene portada, la primera página es la portada verde con el
 * logo, el nombre y el eslogan.
 * Entradas: la carta, los productos a incluir, el idioma, el formato de la URL
 * pública del PDF de cada idioma (con %s en lugar del código, para los enlaces
 * "Idioma: ES · EN …" de la cabecera), la carpeta de imágenes, el logo en PNG
 * y el fondo de la portada en JPEG.
 * Salida: los bytes del PDF.
 **/
func GenerarPDF(c Carta, productos []Producto, idioma, enlaceIdioma, imgDir string, logo, fondoPortada []byte) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(margin, margin, margin)
	pdf.SetAutoPageBreak(false, bottom)
	pdf.SetTitle(c.Nombre, true)
	pdf.SetAuthor(c.Nombre, true)
	// gofpdf usa cp1252: el traductor convierte tildes, ñ y signos de español.
	tr := pdf.UnicodeTranslatorFromDescriptor("")

	// Fondo crema y franja tricolor en todas las páginas.
	pdf.SetHeaderFuncMode(func() {
		fill(pdf, crema)
		pdf.Rect(0, 0, pageW, pageH, "F")
		franja(pdf)
	}, false)
	// Pie: nombre del restaurante y número de página. La portada no lleva pie
	// ni cuenta como página de la carta.
	conPortada := !c.SinPortada
	pdf.SetFooterFunc(func() {
		n := pdf.PageNo()
		if conPortada {
			if n == 1 {
				return
			}
			n--
		}
		pdf.SetY(-11)
		pdf.SetFont("Helvetica", "", 8)
		text(pdf, gris)
		pdf.CellFormat(0, 5, tr(fmt.Sprintf("%s  ·  %s %d", c.Nombre, etiqueta(idioma, "pagina"), n)), "", 0, "C", false, 0, "")
	})

	if len(logo) > 0 {
		pdf.RegisterImageOptionsReader("logo", gofpdf.ImageOptions{ImageType: "PNG"}, bytes.NewReader(logo))
	}
	if conPortada {
		pdf.AddPage()
		portada(pdf, c, idioma, enlaceIdioma, len(logo) > 0, fondoPortada, tr)
	}
	pdf.AddPage()
	cabecera(pdf, c, idioma, enlaceIdioma, len(logo) > 0, tr)

	// Agrupa por categoría (ya traducida) en el orden elegido en el editor.
	// Cada producto se reemplaza por su versión en el idioma del PDF.
	orden := []string{}
	grupos := map[string][]Producto{}
	for _, p := range c.Ordenados(productos) {
		t := c.EnIdioma(p, idioma)
		p.Nombre, p.Descripcion, p.Ingredientes = t.Nombre, t.Descripcion, t.Ingredientes
		cat := t.Categoria
		if cat == "" {
			cat = etiqueta(idioma, "otros")
		}
		if _, ok := grupos[cat]; !ok {
			orden = append(orden, cat)
		}
		grupos[cat] = append(grupos[cat], p)
	}

	y := pdf.GetY()
	if len(productos) == 0 {
		// Carta sin productos: aviso centrado en lugar de una página vacía.
		pdf.SetFont("Helvetica", "I", 11)
		text(pdf, gris)
		pdf.SetXY(margin, y+10)
		pdf.CellFormat(pageW-2*margin, 8, tr(etiqueta(idioma, "vacio")), "", 0, "C", false, 0, "")
	}
	for _, cat := range orden {
		items := grupos[cat]
		// El título nunca queda solo al final de la página: se reserva su
		// altura más la de la primera tarjeta.
		if y+12+alturaTarjeta(pdf, items[0], idioma, tr) > pageH-bottom {
			pdf.AddPage()
			y = margin + 4
		}
		pdf.SetFont("Helvetica", "B", 16)
		text(pdf, verdeOscuro)
		pdf.SetXY(margin, y)
		pdf.CellFormat(0, 9, tr(cat), "", 0, "L", false, 0, "")
		y += 12
		for _, p := range items {
			// Salto de página cuando la tarjeta no cabe completa.
			h := alturaTarjeta(pdf, p, idioma, tr)
			if y+h > pageH-bottom {
				pdf.AddPage()
				y = margin + 4
			}
			tarjeta(pdf, p, y, h, idioma, imgDir, tr)
			y += h + gap
		}
		y += 4
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

/**
 * portada dibuja la página de inicio de la carta: fondo verde con el mural
 * del barranquero desvanecido (como el banner del editor), franjas tricolor
 * arriba y abajo, el logo grande en un círculo crema, el nombre, el eslogan
 * traducido y la palabra "Carta" en el idioma del PDF.
 **/
func portada(pdf *gofpdf.Fpdf, c Carta, idioma, enlaceIdioma string, hayLogo bool, fondo []byte, tr func(string) string) {
	// Fondo: imagen pregenerada (verde + mural); sin ella, verde liso.
	if len(fondo) > 0 {
		opt := gofpdf.ImageOptions{ImageType: "JPG"}
		pdf.RegisterImageOptionsReader("portada", opt, bytes.NewReader(fondo))
		pdf.ImageOptions("portada", 0, 0, pageW, pageH, false, opt, 0, "")
	} else {
		fill(pdf, verde)
		pdf.Rect(0, 0, pageW, pageH, "F")
	}
	franja(pdf)
	franjaEn(pdf, pageH-2.5)
	selectorIdiomas(pdf, c, idioma, enlaceIdioma, tr)

	// Logo grande al centro, dentro de un círculo crema con sombra suave.
	cx, cy, r := pageW/2, 104.0, 46.0
	pdf.SetAlpha(0.18, "Normal")
	fill(pdf, rgb{0, 0, 0})
	pdf.Circle(cx+1.2, cy+1.8, r, "F")
	pdf.SetAlpha(1, "Normal")
	fill(pdf, crema)
	pdf.Circle(cx, cy, r, "F")
	if hayLogo {
		pdf.ImageOptions("logo", cx-r+2, cy-r+2, 2*r-4, 2*r-4, false, gofpdf.ImageOptions{ImageType: "PNG"}, 0, "")
	}

	// Nombre del restaurante.
	pdf.SetTextColor(255, 255, 255)
	pdf.SetFont("Helvetica", "B", 40)
	pdf.SetXY(margin, cy+r+14)
	pdf.CellFormat(pageW-2*margin, 16, tr(c.Nombre), "", 0, "C", false, 0, "")

	// Separador amarillo y eslogan (o el lema si no hay eslogan).
	y := cy + r + 34
	pdf.SetDrawColor(amarillo.r, amarillo.g, amarillo.b)
	pdf.SetLineWidth(1.2)
	pdf.Line(cx-14, y, cx+14, y)
	if eslogan := c.EsloganEn(idioma); eslogan != "" {
		pdf.SetFont("Helvetica", "", 16)
		ancho := 150.0
		for i, l := range lineas(pdf, tr(eslogan), ancho) {
			pdf.SetXY(cx-ancho/2, y+6+float64(i)*8)
			pdf.CellFormat(ancho, 8, l, "", 0, "C", false, 0, "")
		}
	}

	// "CARTA" abajo, espaciada, como rótulo.
	pdf.SetFont("Helvetica", "B", 11)
	pdf.SetTextColor(255, 255, 255)
	pdf.SetXY(margin, pageH-26)
	rotulo := strings.Join(strings.Split(strings.ToUpper(etiqueta(idioma, "carta")), ""), " ")
	pdf.CellFormat(pageW-2*margin, 6, tr(rotulo), "", 0, "C", false, 0, "")
}

/**
 * selectorIdiomas dibuja arriba a la derecha los enlaces "Idioma: ES · EN …":
 * cada código enlaza a su PDF y el actual va resaltado en amarillo. Solo si
 * hay más de un idioma y una URL pública.
 **/
func selectorIdiomas(pdf *gofpdf.Fpdf, c Carta, idioma, enlaceIdioma string, tr func(string) string) {
	if len(c.Idiomas) < 2 || enlaceIdioma == "" {
		return
	}
	const ancho = 8.0
	xi := pageW - margin - ancho*float64(len(c.Idiomas))
	pdf.SetTextColor(255, 255, 255)
	pdf.SetFont("Helvetica", "", 7.5)
	pdf.SetXY(xi-30, 6)
	pdf.CellFormat(29, 5, tr(etiqueta(idioma, "idioma")+":"), "", 0, "R", false, 0, "")
	for i, l := range c.Idiomas {
		xl := xi + ancho*float64(i)
		pdf.SetFont("Helvetica", "B", 8.5)
		if l == idioma {
			fill(pdf, amarillo)
			pdf.RoundedRect(xl+0.5, 6, ancho-1, 5, 1.2, "1234", "F")
			text(pdf, tinta)
		} else {
			pdf.SetTextColor(255, 255, 255)
		}
		pdf.SetXY(xl, 6)
		pdf.CellFormat(ancho, 5, strings.ToUpper(l), "", 0, "C", false, 0, fmt.Sprintf(enlaceIdioma, l))
	}
}

/**
 * cabecera dibuja la banda verde de la primera página de la carta con el logo
 * en un círculo crema, el nombre del restaurante y su lema traducido y el
 * selector de idioma. Deja el cursor debajo.
 **/
func cabecera(pdf *gofpdf.Fpdf, c Carta, idioma, enlaceIdioma string, hayLogo bool, tr func(string) string) {
	const h = 38.0
	fill(pdf, verde)
	pdf.Rect(0, 2.5, pageW, h, "F")
	// Logo dentro de un círculo crema, igual que en la carta web.
	cx, cy, r := margin+13, 2.5+h/2, 13.0
	fill(pdf, crema)
	pdf.Circle(cx, cy, r, "F")
	if hayLogo {
		pdf.ImageOptions("logo", cx-r+1, cy-r+1, 2*r-2, 2*r-2, false, gofpdf.ImageOptions{ImageType: "PNG"}, 0, "")
	}
	// Nombre y lema en blanco a la derecha del logo.
	x := cx + r + 6
	pdf.SetTextColor(255, 255, 255)
	pdf.SetFont("Helvetica", "B", 24)
	pdf.SetXY(x, cy-10)
	pdf.CellFormat(pageW-x-margin, 11, tr(c.Nombre), "", 0, "L", false, 0, "")
	if lema := c.LemaEn(idioma); lema != "" {
		pdf.SetFont("Helvetica", "", 11)
		pdf.SetXY(x, cy+2)
		pdf.CellFormat(pageW-x-margin, 6, tr(lema), "", 0, "L", false, 0, "")
	}
	selectorIdiomas(pdf, c, idioma, enlaceIdioma, tr)
	pdf.SetY(2.5 + h + 8)
}

/**
 * franja dibuja la franja tricolor superior: amarillo 50 %, azul 25 % y rojo
 * 25 %, como la barra de la carta web.
 **/
func franja(pdf *gofpdf.Fpdf) { franjaEn(pdf, 0) }

/**
 * franjaEn dibuja la franja tricolor a la altura y dada.
 **/
func franjaEn(pdf *gofpdf.Fpdf, y float64) {
	const h = 2.5
	fill(pdf, amarillo)
	pdf.Rect(0, y, pageW*0.5, h, "F")
	fill(pdf, azul)
	pdf.Rect(pageW*0.5, y, pageW*0.25, h, "F")
	fill(pdf, rojo)
	pdf.Rect(pageW*0.75, y, pageW*0.25, h, "F")
}

/**
 * anchoTexto es el ancho disponible para el texto de una tarjeta: se le resta
 * el relleno y, si hay foto, el espacio de la foto.
 **/
func anchoTexto(p Producto) float64 {
	w := pageW - 2*margin - 2*pad
	if p.Imagen != "" {
		w -= imgSize + pad
	}
	return w
}

/**
 * lineas parte un texto en líneas que caben en el ancho dado con la fuente
 * actual (el texto ya debe estar traducido a cp1252). Usa SplitLines, que
 * trabaja por bytes: SplitText falla con letras como "ñ" o "á" en cp1252.
 **/
func lineas(pdf *gofpdf.Fpdf, s string, w float64) []string {
	if s == "" {
		return nil
	}
	out := []string{}
	for _, l := range pdf.SplitLines([]byte(s), w) {
		out = append(out, string(l))
	}
	return out
}

/**
 * alturaTarjeta calcula el alto de la tarjeta de un producto: nombre, línea de
 * precio, descripción e ingredientes, o el alto de la foto si es mayor.
 **/
func alturaTarjeta(pdf *gofpdf.Fpdf, p Producto, idioma string, tr func(string) string) float64 {
	w := anchoTexto(p)
	// Nombre (puede ocupar varias líneas) + precio.
	pdf.SetFont("Helvetica", "B", 12)
	h := float64(len(lineas(pdf, tr(p.Nombre), w))) * 5.5
	h += 6.5
	// Descripción.
	pdf.SetFont("Helvetica", "", 9.5)
	h += float64(len(lineas(pdf, tr(p.Descripcion), w))) * 4.5
	// Ingredientes.
	if len(p.Ingredientes) > 0 {
		pdf.SetFont("Helvetica", "I", 8.5)
		h += 1.5 + float64(len(lineas(pdf, tr(ingredientesTexto(p, idioma)), w)))*4.2
	}
	if p.Imagen != "" {
		h = max(h, imgSize)
	}
	return h + 2*pad
}

/**
 * ingredientesTexto devuelve la línea "Ingredientes: a, b, c" en el idioma dado.
 **/
func ingredientesTexto(p Producto, idioma string) string {
	return etiqueta(idioma, "ingredientes") + ": " + strings.Join(p.Ingredientes, ", ")
}

/**
 * tarjeta dibuja un producto en una tarjeta blanca redondeada en la posición
 * y dada, con la foto cuadrada (esquinas redondeadas) a la derecha.
 **/
func tarjeta(pdf *gofpdf.Fpdf, p Producto, y, h float64, idioma, imgDir string, tr func(string) string) {
	x, w := margin, pageW-2*margin
	// Fondo blanco con borde suave, como la clase "card" del tema.
	fill(pdf, rgb{255, 255, 255})
	pdf.SetDrawColor(borde.r, borde.g, borde.b)
	pdf.SetLineWidth(0.3)
	pdf.RoundedRect(x, y, w, h, 3, "1234", "FD")

	tw := anchoTexto(p)
	ty := y + pad
	// Nombre en negrita.
	pdf.SetFont("Helvetica", "B", 12)
	text(pdf, tinta)
	for _, l := range lineas(pdf, tr(p.Nombre), tw) {
		pdf.SetXY(x+pad, ty)
		pdf.CellFormat(tw, 5.5, l, "", 0, "L", false, 0, "")
		ty += 5.5
	}
	// Precio en verde oscuro.
	pdf.SetFont("Helvetica", "B", 11)
	text(pdf, verdeOscuro)
	pdf.SetXY(x+pad, ty+0.5)
	pdf.CellFormat(tw, 5.5, tr(pesos(p.Precio)), "", 0, "L", false, 0, "")
	ty += 6.5
	// Descripción en gris tinta.
	pdf.SetFont("Helvetica", "", 9.5)
	text(pdf, gris)
	for _, l := range lineas(pdf, tr(p.Descripcion), tw) {
		pdf.SetXY(x+pad, ty)
		pdf.CellFormat(tw, 4.5, l, "", 0, "L", false, 0, "")
		ty += 4.5
	}
	// Ingredientes en cursiva verde.
	if len(p.Ingredientes) > 0 {
		ty += 1.5
		pdf.SetFont("Helvetica", "I", 8.5)
		text(pdf, verde)
		for _, l := range lineas(pdf, tr(ingredientesTexto(p, idioma)), tw) {
			pdf.SetXY(x+pad, ty)
			pdf.CellFormat(tw, 4.2, l, "", 0, "L", false, 0, "")
			ty += 4.2
		}
	}
	// Foto: se recorta con esquinas redondeadas; si falla se omite sin romper el PDF.
	if p.Imagen != "" {
		ix, iy := x+w-pad-imgSize, y+(h-imgSize)/2
		if b, err := os.ReadFile(filepath.Join(imgDir, p.Imagen)); err == nil {
			tipo := map[string]string{"image/jpeg": "JPG", "image/png": "PNG"}[http.DetectContentType(b)]
			if tipo != "" {
				opt := gofpdf.ImageOptions{ImageType: tipo}
				pdf.RegisterImageOptionsReader(p.Imagen, opt, bytes.NewReader(b))
				pdf.ClipRoundedRect(ix, iy, imgSize, imgSize, 3, false)
				pdf.ImageOptions(p.Imagen, ix, iy, imgSize, imgSize, false, opt, 0, "")
				pdf.ClipEnd()
			}
		}
	}
}

/**
 * fill y text fijan el color de relleno y de texto a partir de un rgb.
 **/
func fill(pdf *gofpdf.Fpdf, c rgb) { pdf.SetFillColor(c.r, c.g, c.b) }
func text(pdf *gofpdf.Fpdf, c rgb) { pdf.SetTextColor(c.r, c.g, c.b) }
