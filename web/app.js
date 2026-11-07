/**
 * Lógica del editor de la carta: carga la carta del servidor, permite agregar,
 * editar, eliminar y seleccionar productos (con foto e ingredientes), guarda
 * automáticamente cada cambio y, con "Descargar QR", publica el PDF de los
 * productos seleccionados y descarga el código QR que apunta a él.
 **/
'use strict';

/* Estado de la aplicación: la carta y el producto que se edita en el formulario. */
let carta = { nombre: '', lema: '', productos: [] };
let editandoId = null;
let ingredientes = [];
let imagen = '';

/** $ es un atajo para buscar un elemento por id. **/
const $ = (id) => document.getElementById(id);

/**
 * pesos formatea un número como precio colombiano: 25000 → "$ 25.000".
 **/
const pesos = (v) => '$ ' + Number(v || 0).toLocaleString('es-CO', { maximumFractionDigits: 0 });

/**
 * esc escapa texto para insertarlo en HTML sin riesgo.
 **/
const esc = (s) => String(s ?? '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]);

/**
 * api hace una petición JSON y lanza un Error con el mensaje del servidor si falla.
 **/
async function api(url, opts = {}) {
  const res = await fetch(url, opts);
  if (!res.ok) {
    const body = await res.json().catch(() => ({}));
    throw new Error(body.error || `Error ${res.status}`);
  }
  return res.status === 204 ? null : res.json();
}

/**
 * toast muestra un aviso breve abajo de la pantalla.
 **/
let toastTimer;
function toast(msg, error = false) {
  const t = $('toast');
  t.textContent = msg;
  t.className = 'toast' + (error ? ' error' : '');
  t.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => (t.hidden = true), 3500);
}

/* ------------------------------------------------------------------ */
/* Guardado automático                                                */
/* ------------------------------------------------------------------ */

/**
 * guardar envía la carta al servidor 400 ms después del último cambio, para no
 * hacer una petición por cada tecla. Devuelve una promesa que se cumple al guardar.
 **/
let guardarTimer;
let pendiente = Promise.resolve();
function guardar() {
  $('estado').textContent = 'Guardando…';
  clearTimeout(guardarTimer);
  pendiente = new Promise((ok) => {
    guardarTimer = setTimeout(async () => {
      try {
        await api('/api/carta', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(carta) });
        $('estado').textContent = 'Cambios guardados ✓';
      } catch (e) {
        $('estado').textContent = 'No se pudo guardar';
        toast(e.message, true);
      }
      ok();
    }, 400);
  });
  return pendiente;
}

/* ------------------------------------------------------------------ */
/* Lista de productos                                                 */
/* ------------------------------------------------------------------ */

/**
 * render dibuja la lista agrupada por categoría (en orden de aparición), el
 * resumen de seleccionados y las sugerencias de categorías del formulario.
 **/
function render() {
  // Agrupa conservando el orden en que aparece cada categoría.
  const grupos = new Map();
  for (const p of carta.productos) {
    const cat = p.categoria || 'Otros';
    if (!grupos.has(cat)) grupos.set(cat, []);
    grupos.get(cat).push(p);
  }

  // Secciones con una tarjeta por producto.
  const lista = $('lista');
  if (!carta.productos.length) {
    lista.innerHTML = '<p class="vacio">Aún no hay productos. Agrega el primero con el formulario de arriba.</p>';
  } else {
    lista.innerHTML = [...grupos].map(([cat, items]) => `
      <section class="categoria">
        <h3>${esc(cat)}</h3>
        <div class="grilla">${items.map(tarjeta).join('')}</div>
      </section>`).join('');
  }

  // Resumen de la barra y estado del botón principal.
  const n = carta.productos.filter((p) => p.incluir).length;
  $('resumen').textContent = `${n} de ${carta.productos.length} producto${carta.productos.length === 1 ? '' : 's'} en la carta`;
  $('descargar-qr').disabled = n === 0;
  $('ver-pdf').disabled = n === 0;

  // Sugerencias de categoría para el formulario.
  $('categorias').innerHTML = [...grupos.keys()].filter((c) => c !== 'Otros').map((c) => `<option value="${esc(c)}">`).join('');
}

/**
 * tarjeta devuelve el HTML de un producto, con el mismo diseño que la tarjeta
 * de plato del catálogo, más la casilla "incluir" y los botones de acción.
 **/
function tarjeta(p) {
  return `
    <article class="card plato ${p.incluir ? '' : 'fuera'}" data-id="${p.id}">
      <input type="checkbox" class="plato-check" data-accion="incluir" ${p.incluir ? 'checked' : ''}
        aria-label="Incluir ${esc(p.nombre)} en la carta" title="Incluir en el PDF" />
      <div class="plato-cuerpo">
        <h4 class="plato-nombre">${esc(p.nombre)}</h4>
        ${p.descripcion ? `<p class="plato-desc">${esc(p.descripcion)}</p>` : ''}
        ${p.ingredientes.length ? `<p class="plato-ing">🌿 ${esc(p.ingredientes.join(', '))}</p>` : ''}
        <div class="plato-pie">
          <span class="precio">${pesos(p.precio)}</span>
          <button class="btn btn-ghost btn-sm icono" data-accion="editar" title="Editar" aria-label="Editar ${esc(p.nombre)}">✏️</button>
          <button class="btn btn-ghost btn-sm icono" data-accion="eliminar" title="Eliminar" aria-label="Eliminar ${esc(p.nombre)}">🗑️</button>
        </div>
      </div>
      ${p.imagen ? `<img src="/img/${esc(p.imagen)}" alt="" class="plato-img" loading="lazy" />` : ''}
    </article>`;
}

// Acciones de las tarjetas (delegadas en la lista): incluir, editar, eliminar.
$('lista').addEventListener('click', (e) => {
  const btn = e.target.closest('[data-accion]');
  if (!btn) return;
  const id = btn.closest('.plato').dataset.id;
  const p = carta.productos.find((x) => x.id === id);
  if (!p) return;
  const accion = btn.dataset.accion;
  if (accion === 'incluir') {
    p.incluir = btn.checked;
  } else if (accion === 'editar') {
    editar(p);
    return;
  } else if (accion === 'eliminar') {
    if (!confirm(`¿Eliminar "${p.nombre}" de la carta?`)) return;
    carta.productos = carta.productos.filter((x) => x.id !== id);
    if (editandoId === id) limpiarFormulario();
  }
  render();
  guardar();
});

// Marcar o desmarcar todos los productos de una vez.
for (const [id, valor] of [['todos', true], ['ninguno', false]]) {
  $(id).addEventListener('click', () => {
    carta.productos.forEach((p) => (p.incluir = valor));
    render();
    guardar();
  });
}

/* ------------------------------------------------------------------ */
/* Formulario                                                         */
/* ------------------------------------------------------------------ */

/**
 * pintarChips muestra los ingredientes como chips con botón para quitarlos.
 **/
function pintarChips() {
  const caja = $('chips');
  caja.querySelectorAll('.chip').forEach((c) => c.remove());
  ingredientes.forEach((ing, i) => {
    const chip = document.createElement('span');
    chip.className = 'badge chip';
    chip.innerHTML = `${esc(ing)} <button type="button" aria-label="Quitar ${esc(ing)}" data-i="${i}">×</button>`;
    caja.insertBefore(chip, $('f-ingrediente'));
  });
}

/**
 * agregarIngredientes toma el texto escrito (puede tener varias comas) y lo
 * convierte en chips, sin repetir ingredientes.
 **/
function agregarIngredientes() {
  const campo = $('f-ingrediente');
  for (const parte of campo.value.split(',')) {
    const ing = parte.trim();
    if (ing && !ingredientes.some((x) => x.toLowerCase() === ing.toLowerCase())) ingredientes.push(ing);
  }
  campo.value = '';
  pintarChips();
}

// Enter o coma confirman el ingrediente; Backspace en vacío borra el último.
$('f-ingrediente').addEventListener('keydown', (e) => {
  if (e.key === 'Enter' || e.key === ',') {
    e.preventDefault();
    agregarIngredientes();
  } else if (e.key === 'Backspace' && !e.target.value && ingredientes.length) {
    ingredientes.pop();
    pintarChips();
  }
});
$('f-ingrediente').addEventListener('blur', agregarIngredientes);
$('chips').addEventListener('click', (e) => {
  if (e.target.dataset.i !== undefined) {
    ingredientes.splice(Number(e.target.dataset.i), 1);
    pintarChips();
  }
  $('f-ingrediente').focus();
});

// El precio se muestra con puntos de miles mientras se escribe.
$('f-precio').addEventListener('input', (e) => {
  const n = e.target.value.replace(/\D/g, '');
  e.target.value = n ? Number(n).toLocaleString('es-CO') : '';
});

/**
 * pintarImagen muestra la foto actual del formulario o el ícono vacío.
 **/
function pintarImagen() {
  const prev = $('f-preview');
  prev.outerHTML = imagen
    ? `<img id="f-preview" class="preview" src="/img/${esc(imagen)}" alt="Foto del producto" />`
    : '<div id="f-preview" class="preview vacia">🍽️</div>';
  $('f-quitar').hidden = !imagen;
}

/**
 * recortarCuadrada recorta la foto al centro en un cuadrado de 800 px y la
 * convierte a JPEG, para que pese poco y se vea igual en la lista y en el PDF.
 **/
async function recortarCuadrada(file) {
  const bmp = await createImageBitmap(file);
  const lado = Math.min(bmp.width, bmp.height);
  const canvas = document.createElement('canvas');
  canvas.width = canvas.height = Math.min(800, lado);
  const ctx = canvas.getContext('2d');
  // Fondo blanco por si la imagen tiene transparencia (JPEG no la admite).
  ctx.fillStyle = '#fff';
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  ctx.drawImage(bmp, (bmp.width - lado) / 2, (bmp.height - lado) / 2, lado, lado, 0, 0, canvas.width, canvas.height);
  return new Promise((ok) => canvas.toBlob(ok, 'image/jpeg', 0.85));
}

// Al elegir una foto: se recorta, se sube y se muestra.
$('f-imagen').addEventListener('change', async (e) => {
  const file = e.target.files[0];
  e.target.value = '';
  if (!file) return;
  try {
    $('f-preview').outerHTML = '<div id="f-preview" class="preview vacia">⏳</div>';
    const fd = new FormData();
    fd.append('imagen', await recortarCuadrada(file), 'foto.jpg');
    imagen = (await api('/api/imagenes', { method: 'POST', body: fd })).nombre;
  } catch (err) {
    toast('No se pudo cargar la imagen: ' + err.message, true);
  }
  pintarImagen();
});
$('f-quitar').addEventListener('click', () => {
  imagen = '';
  pintarImagen();
});

/**
 * editar carga un producto en el formulario y lo resalta.
 **/
function editar(p) {
  editandoId = p.id;
  $('f-nombre').value = p.nombre;
  $('f-categoria').value = p.categoria;
  $('f-precio').value = p.precio ? Number(p.precio).toLocaleString('es-CO') : '';
  $('f-descripcion').value = p.descripcion;
  ingredientes = [...p.ingredientes];
  imagen = p.imagen;
  pintarChips();
  pintarImagen();
  $('form-titulo').textContent = '✏️ Editar producto';
  $('f-guardar').textContent = 'Guardar cambios';
  $('f-cancelar').hidden = false;
  $('formulario').classList.add('editando');
  $('formulario').scrollIntoView({ behavior: 'smooth', block: 'start' });
  $('f-nombre').focus({ preventScroll: true });
}

/**
 * limpiarFormulario vuelve el formulario al modo "agregar" y vacía los campos
 * (conserva la categoría para cargar varios productos seguidos).
 **/
function limpiarFormulario() {
  editandoId = null;
  $('formulario').reset();
  $('f-categoria').value = $('f-categoria').dataset.ultima || '';
  ingredientes = [];
  imagen = '';
  pintarChips();
  pintarImagen();
  $('f-error').textContent = '';
  $('form-titulo').textContent = '➕ Agregar producto';
  $('f-guardar').textContent = 'Agregar a la carta';
  $('f-cancelar').hidden = true;
  $('formulario').classList.remove('editando');
}
$('f-cancelar').addEventListener('click', limpiarFormulario);

// Guardar el producto: valida, crea o actualiza, y limpia el formulario.
$('formulario').addEventListener('submit', (e) => {
  e.preventDefault();
  agregarIngredientes();
  const nombre = $('f-nombre').value.trim();
  const precio = Number($('f-precio').value.replace(/\D/g, ''));
  // Validaciones mínimas: nombre y precio.
  if (!nombre) return ($('f-error').textContent = 'Escribe el nombre del producto.');
  if (!precio) return ($('f-error').textContent = 'Escribe un precio mayor que cero.');
  const datos = {
    nombre,
    categoria: $('f-categoria').value.trim(),
    descripcion: $('f-descripcion').value.trim(),
    precio,
    ingredientes: [...ingredientes],
    imagen,
  };
  if (editandoId) {
    Object.assign(carta.productos.find((p) => p.id === editandoId), datos);
    toast('Producto actualizado');
  } else {
    // Los productos nuevos entran seleccionados en la carta.
    carta.productos.push({ id: crypto.randomUUID().slice(0, 8), incluir: true, ...datos });
    toast(`"${nombre}" agregado a la carta`);
  }
  $('f-categoria').dataset.ultima = datos.categoria;
  limpiarFormulario();
  render();
  guardar();
  $('f-nombre').focus();
});

/* ------------------------------------------------------------------ */
/* Nombre, lema y publicación                                         */
/* ------------------------------------------------------------------ */

// Nombre y lema del restaurante se guardan al escribir.
for (const campo of ['nombre', 'lema']) {
  $(campo).addEventListener('input', (e) => {
    carta[campo] = e.target.value;
    guardar();
  });
}

/**
 * publicar espera a que termine el guardado pendiente y pide al servidor
 * generar el PDF de los productos seleccionados. Devuelve {id, url, qr}.
 **/
async function publicar() {
  clearTimeout(guardarTimer);
  await api('/api/carta', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(carta) });
  $('estado').textContent = 'Cambios guardados ✓';
  return api('/api/publicar', { method: 'POST' });
}

/**
 * descargar dispara la descarga de un archivo desde una URL del mismo sitio.
 **/
function descargar(url) {
  const a = document.createElement('a');
  a.href = url;
  a.download = '';
  document.body.append(a);
  a.click();
  a.remove();
}

// Botón principal: publica el PDF, descarga el QR y muestra el diálogo.
$('descargar-qr').addEventListener('click', async (e) => {
  const btn = e.currentTarget;
  btn.disabled = true;
  try {
    const r = await publicar();
    const urlDescarga = r.qr + '?descargar=1';
    descargar(urlDescarga);
    $('d-resumen').textContent = `${r.productos} producto${r.productos === 1 ? '' : 's'} en el PDF. El QR se descargó como imagen PNG.`;
    $('d-qr').src = r.qr;
    $('d-url').href = r.url;
    $('d-url').textContent = r.url;
    $('d-pdf').href = `/carta/${r.id}.pdf?ver=1`;
    $('d-descargar').href = urlDescarga;
    $('dialogo').showModal();
    esperarPublicacion(r);
  } catch (err) {
    toast(err.message, true);
  }
  btn.disabled = false;
});

// Vista previa: publica y abre el PDF en otra pestaña.
$('ver-pdf').addEventListener('click', async () => {
  // La pestaña se abre antes de la petición para que el navegador no la bloquee.
  const win = window.open('about:blank', '_blank');
  try {
    const r = await publicar();
    if (win) win.location = `/carta/${r.id}.pdf?ver=1`;
  } catch (err) {
    win?.close();
    toast(err.message, true);
  }
});
$('d-cerrar').addEventListener('click', () => $('dialogo').close());

/**
 * esperarPublicacion muestra en el diálogo si el PDF ya se puede abrir desde
 * internet. En GitHub Pages el despliegue tarda cerca de un minuto, así que se
 * consulta cada 5 s (hasta 4 min o hasta que se cierre el diálogo).
 **/
let esperaId = 0;
async function esperarPublicacion(r) {
  const aviso = $('d-aviso');
  const turno = ++esperaId;
  // Modo local: el QR solo sirve en la misma red Wi‑Fi.
  if (!r.web) {
    aviso.className = 'aviso';
    aviso.textContent = '⚠️ Modo local: el QR solo funciona en la misma red Wi‑Fi que este equipo. Configura GITHUB_TOKEN y GITHUB_REPO para que funcione desde cualquier red.';
    return;
  }
  aviso.className = 'aviso';
  aviso.textContent = '⏳ Publicando en GitHub Pages… puede tardar cerca de un minuto. Ya puedes descargar e imprimir el QR: la dirección no cambia.';
  for (let i = 0; i < 48 && turno === esperaId && $('dialogo').open; i++) {
    try {
      if ((await api(`/api/disponible/${r.id}`)).disponible) {
        aviso.className = 'aviso listo';
        aviso.textContent = '✓ Disponible en internet: el QR funciona con datos móviles o cualquier Wi‑Fi y descarga el PDF.';
        return;
      }
    } catch {
      // Error de red puntual: se reintenta en la siguiente vuelta.
    }
    await new Promise((ok) => setTimeout(ok, 5000));
  }
}

/**
 * mostrarDestino indica en la barra dónde se publica la carta: en la web
 * (GitHub Pages) o solo en la red local, con el motivo si GitHub falló.
 **/
async function mostrarDestino() {
  try {
    const c = await api('/api/config');
    const el = $('destino');
    el.className = 'estado' + (c.modo === 'web' ? '' : ' local');
    el.textContent = c.modo === 'web' ? `🌐 QR en la web: ${c.destino.replace(/^https?:\/\//, '')}` : '⚠️ QR solo en red local';
    el.title = c.error || c.destino;
    // GitHub configurado pero con error: se avisa para que se corrija el .env.
    if (c.error) toast('GitHub Pages no disponible: ' + c.error, true);
  } catch {
    // Sin configuración visible no se bloquea el editor.
  }
}

/* ------------------------------------------------------------------ */
/* Inicio                                                             */
/* ------------------------------------------------------------------ */

/**
 * iniciar carga la carta del servidor y pinta la pantalla.
 **/
async function iniciar() {
  try {
    carta = await api('/api/carta');
  } catch (err) {
    toast('No se pudo cargar la carta: ' + err.message, true);
  }
  $('nombre').value = carta.nombre;
  $('lema').value = carta.lema;
  render();
  mostrarDestino();
}
iniciar();
