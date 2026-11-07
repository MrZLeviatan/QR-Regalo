/**
 * Traducción automática de los textos de la carta (del español a otro
 * idioma), usada por el botón "Traducir automáticamente" del editor. Hay tres
 * proveedores, igual que en el catálogo El Regalo más uno sin registro:
 *   - LibreTranslate (LIBRETRANSLATE_URL y, si el servidor la pide, LIBRETRANSLATE_API_KEY).
 *   - DeepL API Free (DEEPL_API_KEY).
 *   - MyMemory, por defecto: gratis y sin registro (MYMEMORY_EMAIL opcional
 *     sube el límite diario de ~5.000 a ~50.000 caracteres).
 * Las traducciones se guardan en memoria para no repetir peticiones.
 **/
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

/**
 * Traductor traduce lotes de textos del español al idioma destino. Devuelve
 * un resultado por texto, en el mismo orden.
 **/
type Traductor struct {
	nombre   string
	traducir func(textos []string, destino string) ([]string, error)
	client   *http.Client

	mu    sync.Mutex
	cache map[string]string // "destino\x00texto" → traducción
}

/**
 * NuevoTraductor elige el proveedor según las variables de entorno:
 * LibreTranslate, luego DeepL y, si no hay ninguno, MyMemory.
 **/
func NuevoTraductor() *Traductor {
	t := &Traductor{client: &http.Client{Timeout: 30 * time.Second}, cache: map[string]string{}}
	switch {
	case os.Getenv("LIBRETRANSLATE_URL") != "":
		t.nombre, t.traducir = "LibreTranslate", t.libre
	case os.Getenv("DEEPL_API_KEY") != "":
		t.nombre, t.traducir = "DeepL", t.deepl
	default:
		t.nombre, t.traducir = "MyMemory", t.mymemory
	}
	return t
}

/**
 * Traducir traduce los textos al idioma destino. Los textos vacíos se
 * devuelven vacíos y los ya traducidos antes salen de la caché; solo se
 * consulta al proveedor por los nuevos.
 **/
func (t *Traductor) Traducir(textos []string, destino string) ([]string, error) {
	if !idiomaValido(destino) || destino == IdiomaBase {
		return nil, fmt.Errorf("idioma de destino no válido: %q", destino)
	}
	out := make([]string, len(textos))
	// Separa los que ya están en caché de los que hay que pedir.
	var faltan []string
	var pos []int
	t.mu.Lock()
	for i, s := range textos {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if v, ok := t.cache[destino+"\x00"+s]; ok {
			out[i] = v
			continue
		}
		faltan = append(faltan, s)
		pos = append(pos, i)
	}
	t.mu.Unlock()
	if len(faltan) == 0 {
		return out, nil
	}
	// Pide los nuevos al proveedor y los guarda en caché.
	res, err := t.traducir(faltan, destino)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", t.nombre, err)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for j, v := range res {
		v = pulir(faltan[j], v)
		out[pos[j]] = v
		t.cache[destino+"\x00"+faltan[j]] = v
	}
	return out, nil
}

/**
 * pulir corrige dos defectos frecuentes de la traducción automática en
 * textos cortos: signos agregados al final que el original no tenía ("Plats
 * principaux :") y resultados en MAYÚSCULAS cuando el original no lo estaba
 * (se deja solo la primera letra en mayúscula).
 **/
func pulir(original, traducido string) string {
	t := strings.TrimSpace(traducido)
	// Quita la puntuación final que no estaba en el original.
	if !strings.ContainsAny(original[len(original)-1:], ".:;!?") {
		t = strings.TrimRight(t, " .:;!?")
	}
	// Mayúsculas sostenidas que no venían del original.
	if t == strings.ToUpper(t) && original != strings.ToUpper(original) && t != strings.ToLower(t) {
		r := []rune(strings.ToLower(t))
		t = strings.ToUpper(string(r[0])) + string(r[1:])
	}
	if t == "" {
		return traducido
	}
	return t
}

/**
 * libre traduce con la API /translate de un servidor LibreTranslate, todos
 * los textos en una sola petición.
 **/
func (t *Traductor) libre(textos []string, destino string) ([]string, error) {
	body := map[string]any{"q": textos, "source": IdiomaBase, "target": destino, "format": "text"}
	if k := os.Getenv("LIBRETRANSLATE_API_KEY"); k != "" {
		body["api_key"] = k
	}
	var out struct {
		TranslatedText []string `json:"translatedText"`
		Error          string   `json:"error"`
	}
	u := strings.TrimRight(os.Getenv("LIBRETRANSLATE_URL"), "/") + "/translate"
	if err := t.postJSON(u, nil, body, &out); err != nil {
		return nil, err
	}
	if out.Error != "" || len(out.TranslatedText) != len(textos) {
		return nil, fmt.Errorf("respuesta inesperada: %s", out.Error)
	}
	return out.TranslatedText, nil
}

/**
 * deepl traduce con DeepL API (las claves gratuitas terminan en ":fx" y van a
 * api-free.deepl.com). Inglés y portugués exigen variante.
 **/
func (t *Traductor) deepl(textos []string, destino string) ([]string, error) {
	key := os.Getenv("DEEPL_API_KEY")
	u := "https://api.deepl.com/v2/translate"
	if strings.HasSuffix(key, ":fx") {
		u = "https://api-free.deepl.com/v2/translate"
	}
	target := map[string]string{"en": "EN-US", "pt": "PT-BR"}[destino]
	if target == "" {
		target = strings.ToUpper(destino)
	}
	body := map[string]any{"text": textos, "source_lang": "ES", "target_lang": target}
	var out struct {
		Translations []struct {
			Text string `json:"text"`
		} `json:"translations"`
	}
	if err := t.postJSON(u, map[string]string{"Authorization": "DeepL-Auth-Key " + key}, body, &out); err != nil {
		return nil, err
	}
	if len(out.Translations) != len(textos) {
		return nil, errors.New("respuesta incompleta")
	}
	res := make([]string, len(textos))
	for i, tr := range out.Translations {
		res[i] = tr.Text
	}
	return res, nil
}

/**
 * mymemory traduce con la API pública de MyMemory, un texto por petición
 * (hasta 4 en paralelo para no saturar el servicio).
 **/
func (t *Traductor) mymemory(textos []string, destino string) ([]string, error) {
	res := make([]string, len(textos))
	errs := make([]error, len(textos))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, s := range textos {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res[i], errs[i] = t.mymemoryUno(s, destino)
		}()
	}
	wg.Wait()
	return res, errors.Join(errs...)
}

/**
 * mymemoryUno traduce un solo texto con MyMemory y revisa el estado que
 * devuelve dentro del JSON (p. ej. 429 si se agotó la cuota del día).
 **/
func (t *Traductor) mymemoryUno(s, destino string) (string, error) {
	q := url.Values{"q": {s}, "langpair": {IdiomaBase + "|" + destino}}
	if e := os.Getenv("MYMEMORY_EMAIL"); e != "" {
		q.Set("de", e)
	}
	res, err := t.client.Get("https://api.mymemory.translated.net/get?" + q.Encode())
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var out struct {
		ResponseData struct {
			TranslatedText string `json:"translatedText"`
		} `json:"responseData"`
		ResponseStatus  any    `json:"responseStatus"`
		ResponseDetails string `json:"responseDetails"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out); err != nil {
		return "", err
	}
	// El estado puede llegar como número o como texto.
	if fmt.Sprint(out.ResponseStatus) != "200" {
		return "", fmt.Errorf("estado %v: %s", out.ResponseStatus, out.ResponseDetails)
	}
	return out.ResponseData.TranslatedText, nil
}

/**
 * postJSON envía body como JSON con las cabeceras dadas y decodifica la
 * respuesta en out; un código HTTP de error se devuelve como error.
 **/
func (t *Traductor) postJSON(u string, headers map[string]string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", u, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 500))
		return fmt.Errorf("HTTP %d: %s", res.StatusCode, strings.TrimSpace(string(msg)))
	}
	return json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(out)
}
