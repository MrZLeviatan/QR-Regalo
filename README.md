<p align="center"><img src="web/logo.svg" width="110" alt="Logo El Regalo: barranquero"></p>

<h1 align="center">QR Regalo · Carta en PDF por QR</h1>

Programa simple, basado en el diseño de **Catálogo El Regalo**, para armar la carta
(productos, precios, imágenes e ingredientes) y publicarla como un **PDF** al que apunta
un **código QR** descargable.

## Cómo funciona

1. Escribe el nombre y el lema del restaurante en la cabecera verde.
2. Agrega productos con el formulario: nombre, categoría, precio, descripción,
   ingredientes (Enter o coma para cada uno) y una foto (se recorta cuadrada).
3. Marca con la casilla qué productos van en la carta (los nuevos entran marcados).
4. Pulsa **Descargar QR**: se genera el PDF con los productos marcados y se descarga
   el QR en PNG. Al escanearlo, el celular descarga el PDF de la carta.

Todo se guarda solo. La misma selección produce siempre el mismo PDF y el mismo QR;
si cambias algo, el QR nuevo apunta al PDF nuevo (los anteriores siguen funcionando).

## Publicación web (GitHub Pages)

Para que el QR funcione **desde cualquier red** (datos móviles, cualquier Wi‑Fi) y sin
dejar el PC encendido, los PDF se publican gratis en GitHub Pages:

1. Crea en GitHub un repositorio **público** (puede estar vacío), por ejemplo
   `carta-el-regalo`.
2. Crea un token *fine-grained* en <https://github.com/settings/personal-access-tokens>
   con acceso **solo a ese repositorio** y permisos **Contents** y **Pages** en
   *Read and write*.
3. Copia `.env.example` como `.env` y completa `GITHUB_TOKEN` y `GITHUB_REPO`.

Al pulsar **Descargar QR**, el PDF se sube a `carta/<id>.pdf` del repositorio, Pages se
activa solo la primera vez y el QR apunta a
`https://<usuario>.github.io/<repositorio>/carta/<id>.pdf`. GitHub tarda cerca de un
minuto en desplegar: el diálogo avisa cuando ya está disponible (la dirección no cambia,
así que el QR se puede imprimir enseguida). La barra superior muestra
**🌐 QR en la web** o **⚠️ QR solo en red local** si falta o falla la configuración.

> Todo lo publicado en el repositorio es público: no subas datos privados.

## Ejecutar

```bash
go run .
```

Abre <http://localhost:8090>. Para un binario único (el editor va embebido):

```bash
go build -o qr-regalo . && ./qr-regalo
```

| Variable       | Por defecto                  | Uso                                                     |
|----------------|------------------------------|---------------------------------------------------------|
| `GITHUB_TOKEN` | —                            | Token con Contents y Pages (lectura/escritura).          |
| `GITHUB_REPO`  | —                            | Repositorio público `usuario/nombre` para los PDF.       |
| `PORT`         | `8090`                       | Puerto del servidor.                                    |
| `DATA_DIR`     | `./data`                     | Carta (`carta.json`), fotos (`img/`) y copia de los PDF. |
| `BASE_URL`     | `http://<IP-de-la-red>:PORT` | Solo sin GitHub: dirección del QR (misma red Wi‑Fi).     |

Las variables se leen del entorno o de un archivo `.env` (el entorno tiene prioridad).

## Estructura

| Archivo          | Contenido                                                    |
|------------------|--------------------------------------------------------------|
| `main.go`        | Servidor HTTP, API, fotos, publicación del PDF y QR.          |
| `github.go`      | Subida de los PDF a GitHub y activación de Pages.             |
| `carta.go`       | Modelo de la carta y guardado en JSON.                        |
| `pdf.go`         | Diseño del PDF (franja tricolor, cabecera verde, tarjetas).   |
| `web/`           | Editor (HTML, CSS y JS sin compilación) y marca de El Regalo. |

## API

| Método y ruta            | Descripción                                          |
|--------------------------|------------------------------------------------------|
| `GET /api/carta`         | Carta completa.                                      |
| `PUT /api/carta`         | Reemplaza la carta.                                  |
| `POST /api/imagenes`     | Sube una foto JPG/PNG (campo `imagen`).              |
| `GET /api/config`        | Dónde se publica (`web` o `local`) y errores.        |
| `POST /api/publicar`     | Genera el PDF de los productos marcados y lo publica. |
| `GET /api/disponible/{id}` | Si el PDF ya se puede abrir desde internet.        |
| `GET /api/qr/{id}`       | QR en PNG (`?descargar=1` para guardarlo).           |
| `GET /carta/{id}.pdf`    | PDF publicado (descarga; `?ver=1` lo muestra).       |
