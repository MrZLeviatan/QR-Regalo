/**
 * Página selectora de idioma: es el index.html de la dirección fija a la
 * que apunta el QR. Es estática (sirve igual en GitHub Pages que en este
 * servidor): lee los idiomas del celular y abre al instante el PDF que
 * corresponde, con el español como respaldo. Mientras tanto, y si se abre con #elegir, muestra
 * un botón por idioma con el diseño de El Regalo.
 **/
package main

import (
	"bytes"
	"html/template"
)

/**
 * plantillaSelector es el HTML de la página. Los colores son los del tema
 * compartido del catálogo; no carga nada externo para abrir rápido con mala señal.
 **/
var plantillaSelector = template.Must(template.New("selector").Parse(`<!doctype html>
<html lang="es">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="theme-color" content="#2b7a57">
<meta name="version" content="{{.Version}}">
<title>{{.Nombre}}</title>
<style>
  body { margin: 0; min-height: 100dvh; background: #fff8e7; color: #1a1c20;
    font-family: system-ui, -apple-system, 'Segoe UI', Roboto, Arial, sans-serif; }
  .tricolor { height: 6px; background: linear-gradient(90deg, #fcd116 0 50%, #1f5fb8 50% 75%, #ce1126 75%); }
  header { background: #2b7a57; color: #fff; padding: 20px 16px; text-align: center; }
  h1 { margin: 0; font-size: 1.6rem; font-weight: 900; }
  header p { margin: 4px 0 0; opacity: .85; font-size: .9rem; }
  main { max-width: 22rem; margin: 0 auto; padding: 24px 16px; display: grid; gap: 12px; }
  a { display: flex; align-items: center; justify-content: space-between; min-height: 52px; padding: 0 16px;
    border-radius: 12px; background: #fff; color: #1a1c20; text-decoration: none; font-weight: 700;
    box-shadow: 0 1px 2px rgb(0 0 0 / .05), 0 0 0 1px rgb(0 0 0 / .06); }
  a:active { transform: scale(.98); }
  a span { color: #1d5a40; font-size: .8rem; letter-spacing: .05em; }
</style>
</head>
<body>
<div class="tricolor"></div>
<header><h1>{{.Nombre}}</h1>{{if .Lema}}<p>{{.Lema}}</p>{{end}}</header>
<main>
  {{range .Idiomas}}<a href="{{.Codigo}}.pdf?v={{$.Version}}" hreflang="{{.Codigo}}">{{.Nombre}} <span>{{.Codigo}} · PDF</span></a>
  {{end}}
</main>
<script>
  // Abre el PDF del primer idioma del celular que esté disponible (o el
  // español). Con #elegir se queda en esta página para escoger a mano.
  (function () {
    if (location.hash === '#elegir') return;
    var disponibles = {{.Codigos}};
    var prefs = navigator.languages && navigator.languages.length ? navigator.languages : [navigator.language || 'es'];
    var elegido = 'es';
    for (var i = 0; i < prefs.length; i++) {
      var c = String(prefs[i]).slice(0, 2).toLowerCase();
      if (disponibles.indexOf(c) >= 0) { elegido = c; break; }
    }
    // replace: al volver atrás no se regresa a esta página intermedia.
    location.replace(elegido + '.pdf?v=' + {{.Version}});
  })();
</script>
</body>
</html>
`))

/**
 * Selector genera el index.html para los idiomas activos de la carta. La
 * versión va en una etiqueta meta (para comprobar que Pages ya la publicó) y
 * en los enlaces a los PDF (?v=…), para que el celular no muestre un PDF
 * viejo guardado en caché.
 **/
func Selector(c Carta, version string) ([]byte, error) {
	// Idiomas activos con su nombre propio, en el orden oficial.
	type idioma struct{ Codigo, Nombre string }
	var lista []idioma
	for _, i := range Idiomas {
		for _, l := range c.Idiomas {
			if l == i.Codigo {
				lista = append(lista, idioma{i.Codigo, i.Nombre})
			}
		}
	}
	var buf bytes.Buffer
	err := plantillaSelector.Execute(&buf, map[string]any{
		"Nombre":  c.Nombre,
		"Lema":    c.Lema,
		"Idiomas": lista,
		"Codigos": c.Idiomas,
		"Version": version,
	})
	return buf.Bytes(), err
}
