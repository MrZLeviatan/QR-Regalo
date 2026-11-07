/**
 * Lógica del editor de la carta: carga la carta del servidor, permite agregar,
 * editar, eliminar y seleccionar productos (con foto, ingredientes y
 * traducciones), activar idiomas y traducir automáticamente lo que falte,
 * guarda cada cambio y, con "Publicar carta", reemplaza la carta publicada
 * (un PDF por idioma) en la dirección fija del QR, que se imprime una sola vez.
 **/
'use strict';

/* Estado de la aplicación: la carta y el producto que se edita en el formulario. */
let carta = { nombre: '', lema: '', productos: [] };
let editandoId = null;
let ingredientes = [];
let imagen = '';
// Traducciones del producto en el formulario ({en: {nombre, descripcion, ingredientes}}).
let tradForm = {};
// Pestaña de idioma activa en el formulario y en la tarjeta de idiomas.
let tabForm = null;
let tabIdiomas = null;
// Configuración del servidor: idiomas admitidos, destino de publicación y traductor.
let config = { idiomas: [{ Codigo: 'es', Nombre: 'Español' }], modo: 'local', traductor: '' };

/** $ es un atajo para buscar un elemento por id. **/
const $ = (id) => document.getElementById(id);

/**
 * pesos formatea un número como precio colombiano: 25000 → "$ 25.000".
 **/
const pesos = (v) => '$ ' + Number(v || 0).toLocaleString('es-CO', { maximumFractionDigits: 0 });

/**
 * esc escapa texto para insertarlo en HTML sin riesgo.
 **/
/** extras devuelve los idiomas activos distintos del español. **/
const extras = () => carta.idiomas.filter((l) => l !== 'es');

/** nombreIdioma devuelve el nombre propio de un idioma ("English"). **/
const nombreIdioma = (l) => config.idiomas.find((i) => i.Codigo === l)?.Nombre || l.toUpperCase();

/**
 * categoriasDe devuelve las categorías en español usadas por los productos,
 * en el orden de la carta: primero las de ordenCategorias y luego las nuevas
 * en orden de aparición (igual que el PDF).
 **/
function categoriasDe() {
  const usadas = [...new Set(carta.productos.map((p) => p.categoria).filter(Boolean))];
  const orden = (carta.ordenCategorias || []).filter((c) => usadas.includes(c));
  return [...orden, ...usadas.filter((c) => !orden.includes(c))];
}

/**
 * faltantes devuelve los idiomas activos en los que un producto no tiene el
 * nombre traducido (para avisar en su tarjeta).
 **/
const faltantes = (p) => extras().filter((l) => !p.traducciones?.[l]?.nombre);

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
        await actualizarEstado();
      } catch (e) {
        $('estado').textContent = 'No se pudo guardar';
        toast(e.message, true);
      }
      ok();
    }, 400);
  });
  return pendiente;
}

/**
 * actualizarEstado muestra en la barra si lo guardado ya está publicado o si
 * hay cambios que las mesas todavía no ven (falta pulsar "Publicar carta").
 **/
async function actualizarEstado() {
  const el = $('estado');
  try {
    const e = await api('/api/estado');
    el.className = 'estado' + (e.pendientes ? ' pendiente' : '');
    el.textContent = e.pendientes
      ? e.publicada ? '● Guardado · cambios sin publicar' : '● Guardado · aún no publicada'
      : e.publicada ? '✓ Guardado y publicado' : 'Cambios guardados ✓';
  } catch {
    el.className = 'estado';
    el.textContent = 'Cambios guardados ✓';
  }
}

/* ------------------------------------------------------------------ */
/* Lista de productos                                                 */
/* ------------------------------------------------------------------ */

/**
 * render dibuja la lista agrupada por categoría (en orden de aparición), el
 * resumen de seleccionados y las sugerencias de categorías del formulario.
 **/
function render() {
  // Agrupa en el orden de la carta (el mismo del PDF); "Otros" al final.
  const grupos = new Map(categoriasDe().map((c) => [c, []]));
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
  renderIdiomas();
  renderOrden();
  pintarTraducciones();
}

/**
 * renderOrden dibuja la lista de categorías en el orden de la carta, con
 * flechas para subir o bajar cada una (oculta si hay menos de dos).
 **/
function renderOrden() {
  const cats = categoriasDe();
  $('orden-card').hidden = cats.length < 2;
  $('orden-lista').innerHTML = cats
    .map((c, i) => `<li><span class="num">${i + 1}</span><span class="nombre">${esc(c)}</span>
      <button type="button" class="btn btn-ghost btn-sm icono" data-mover="-1" data-i="${i}" ${i === 0 ? 'disabled' : ''} aria-label="Subir ${esc(c)}">↑</button>
      <button type="button" class="btn btn-ghost btn-sm icono" data-mover="1" data-i="${i}" ${i === cats.length - 1 ? 'disabled' : ''} aria-label="Bajar ${esc(c)}">↓</button></li>`)
    .join('');
}

// Subir o bajar una categoría: se guarda el orden completo y se vuelve a pintar.
$('orden-lista').addEventListener('click', (e) => {
  const b = e.target.closest('[data-mover]');
  if (!b || b.disabled) return;
  const cats = categoriasDe();
  const i = Number(b.dataset.i);
  const j = i + Number(b.dataset.mover);
  [cats[i], cats[j]] = [cats[j], cats[i]];
  carta.ordenCategorias = cats;
  render();
  guardar();
  // Mantiene el foco en la categoría movida para seguir moviéndola con el teclado.
  $('orden-lista').querySelector(`[data-i="${j}"][data-mover="${b.dataset.mover}"]:not(:disabled)`)?.focus();
});

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
          ${faltantes(p).map((l) => `<span class="badge falta" title="Falta traducir el nombre">${l.toUpperCase()}</span>`).join('')}
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
  tradForm = structuredClone(p.traducciones || {});
  pintarChips();
  pintarImagen();
  pintarTraducciones();
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
  tradForm = {};
  pintarChips();
  pintarImagen();
  pintarTraducciones();
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
    traducciones: structuredClone(tradForm),
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
 * guardarYa guarda la carta de inmediato (sin esperar el guardado diferido).
 **/
async function guardarYa() {
  clearTimeout(guardarTimer);
  await api('/api/carta', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(carta) });
}

/**
 * publicar guarda y pide al servidor reemplazar la carta publicada en la
 * dirección fija del QR. Devuelve {version, url, qr, productos, idiomas, web}.
 **/
async function publicar() {
  await guardarYa();
  const r = await api('/api/publicar', { method: 'POST' });
  await actualizarEstado();
  return r;
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

// Botón principal: publica la carta y muestra el QR fijo para descargarlo.
$('descargar-qr').addEventListener('click', async (e) => {
  const btn = e.currentTarget;
  btn.disabled = true;
  try {
    const r = await publicar();
    const n = r.idiomas.length;
    $('d-resumen').textContent = `${r.productos} producto${r.productos === 1 ? '' : 's'} en ${n} idioma${n === 1 ? '' : 's'}. Este QR es fijo: imprímelo una vez y cada vez que publiques se actualiza solo.`;
    $('d-qr').src = r.qr + '?v=' + r.version;
    $('d-url').href = r.url;
    $('d-url').textContent = r.url;
    $('d-pdf').href = '/carta/es.pdf?ver=1';
    $('d-idiomas').innerHTML = r.idiomas
      .map((l) => `<a href="/carta/${l}.pdf?ver=1&v=${r.version}" target="_blank" rel="noopener">${esc(nombreIdioma(l))}</a>`)
      .join(' · ');
    $('d-descargar').href = r.qr + '?descargar=1';
    $('dialogo').showModal();
    esperarPublicacion(r);
  } catch (err) {
    toast('No se pudo publicar: ' + err.message, true);
  }
  btn.disabled = false;
});

// Vista previa: genera el PDF en español con lo guardado, sin publicarlo.
$('ver-pdf').addEventListener('click', async () => {
  // La pestaña se abre antes de la petición para que el navegador no la bloquee.
  const win = window.open('about:blank', '_blank');
  try {
    await guardarYa();
    if (win) win.location = '/api/vista/es.pdf?t=' + Date.now();
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
  aviso.textContent = '⏳ Actualizando la carta en internet… GitHub tarda cerca de un minuto. Mientras tanto, los celulares ven la versión anterior.';
  for (let i = 0; i < 48 && turno === esperaId && $('dialogo').open; i++) {
    try {
      if ((await api(`/api/disponible/${r.version}`)).disponible) {
        aviso.className = 'aviso listo';
        aviso.textContent = '✓ Carta actualizada en internet: el QR la abre con datos móviles o cualquier Wi‑Fi. Un celular que la abrió hace poco puede tardar unos minutos en ver los cambios.';
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
function mostrarDestino() {
  const c = config;
  const el = $('destino');
  el.className = 'estado' + (c.modo === 'web' ? '' : ' local');
  el.textContent = c.modo === 'web' ? `🌐 QR en la web: ${c.destino.replace(/^https?:\/\//, '')}` : '⚠️ QR solo en red local';
  el.title = c.error || c.destino;
  // GitHub configurado pero con error: se avisa para que se corrija el .env.
  if (c.error) toast('GitHub Pages no disponible: ' + c.error, true);
}

/* ------------------------------------------------------------------ */
/* Portada                                                            */
/* ------------------------------------------------------------------ */

/**
 * pintarPortada muestra si la portada está activa y su eslogan; si no hay
 * eslogan, la guía es el lema (que es lo que saldría en la portada).
 **/
function pintarPortada() {
  $('portada-activa').checked = !carta.sinPortada;
  $('eslogan').value = carta.eslogan || '';
  $('eslogan').placeholder = carta.lema ? `Vacío = se usa el lema: ${carta.lema}` : 'Ej.: Sabor de nuestra tierra';
  $('eslogan').disabled = !!carta.sinPortada;
}

// Mostrar u ocultar la portada.
$('portada-activa').addEventListener('change', (e) => {
  carta.sinPortada = !e.target.checked;
  pintarPortada();
  guardar();
});

// Escribir el eslogan (la fila de traducción aparece al salir del campo).
$('eslogan').addEventListener('input', (e) => {
  carta.eslogan = e.target.value;
  guardar();
});
$('eslogan').addEventListener('change', renderIdiomas);

/* ------------------------------------------------------------------ */
/* Idiomas y traducciones                                             */
/* ------------------------------------------------------------------ */

/**
 * pestanas dibuja una fila de pestañas (una por idioma) en el contenedor y
 * llama a alElegir con el código al tocar una. marca(l) puede devolver true
 * para señalar con un punto rojo que a ese idioma le falta algo.
 **/
function pestanas(contenedor, idiomas, activa, alElegir, marca = () => false) {
  contenedor.innerHTML = idiomas
    .map((l) => `<button type="button" role="tab" class="tab" data-l="${l}" aria-selected="${l === activa}">
      ${esc(nombreIdioma(l))}${marca(l) ? ' <span class="falta" aria-label="incompleto">●</span>' : ''}</button>`)
    .join('');
  contenedor.onclick = (e) => {
    const b = e.target.closest('[data-l]');
    if (b) alElegir(b.dataset.l);
  };
}

/**
 * renderIdiomas dibuja la tarjeta de idiomas: interruptores de idiomas
 * activos y, por idioma, la traducción del lema y de cada categoría.
 **/
function renderIdiomas() {
  // Interruptores: el español siempre activo.
  $('idiomas-chips').innerHTML = config.idiomas
    .map((i) => `<button type="button" class="idioma-chip" data-l="${i.Codigo}" aria-pressed="${carta.idiomas.includes(i.Codigo)}"
      ${i.Codigo === 'es' ? 'disabled title="Idioma base de la carta"' : ''}>${i.Codigo.toUpperCase()} · ${esc(i.Nombre)}</button>`)
    .join('');

  // Detalle por idioma: lema y categorías.
  const ls = extras();
  const det = $('idiomas-detalle');
  if (!ls.length) {
    det.innerHTML = '';
    return;
  }
  if (!ls.includes(tabIdiomas)) tabIdiomas = ls[0];
  const l = tabIdiomas;
  const cats = categoriasDe();
  const incompleto = (x) => !carta.lemas[x] || (carta.eslogan && !carta.eslogans[x]) || cats.some((c) => !carta.categorias[c]?.[x]);
  det.innerHTML = `<div class="tabs" role="tablist"></div>
    <div class="tab-panel">
      <div class="traduccion-fila"><span>Lema</span>
        <input class="input" data-lema="${l}" value="${esc(carta.lemas[l] || '')}" placeholder="${esc(carta.lema)}" /></div>
      ${carta.eslogan ? `<div class="traduccion-fila"><span>Eslogan</span>
        <input class="input" data-eslogan="${l}" value="${esc(carta.eslogans[l] || '')}" placeholder="${esc(carta.eslogan)}" /></div>` : ''}
      ${cats.map((c) => `<div class="traduccion-fila"><span>${esc(c)}</span>
        <input class="input" data-cat="${esc(c)}" value="${esc(carta.categorias[c]?.[l] || '')}" placeholder="${esc(c)}" /></div>`).join('')}
    </div>`;
  pestanas(det.querySelector('.tabs'), ls, l, (x) => { tabIdiomas = x; renderIdiomas(); }, incompleto);
}

// Activar o desactivar un idioma.
$('idiomas-chips').addEventListener('click', (e) => {
  const b = e.target.closest('[data-l]');
  if (!b || b.disabled) return;
  const l = b.dataset.l;
  carta.idiomas = carta.idiomas.includes(l) ? carta.idiomas.filter((x) => x !== l) : [...carta.idiomas, l];
  // Mantiene el orden oficial de los idiomas.
  carta.idiomas = config.idiomas.map((i) => i.Codigo).filter((x) => carta.idiomas.includes(x));
  render();
  guardar();
});

// Escribir la traducción del lema o de una categoría.
$('idiomas-detalle').addEventListener('input', (e) => {
  const v = e.target.value;
  if (e.target.dataset.lema) carta.lemas[e.target.dataset.lema] = v;
  if (e.target.dataset.eslogan) carta.eslogans[e.target.dataset.eslogan] = v;
  if (e.target.dataset.cat !== undefined) (carta.categorias[e.target.dataset.cat] ??= {})[tabIdiomas] = v;
  guardar();
});

/**
 * pintarTraducciones muestra las pestañas de traducción del formulario con
 * los textos del idioma elegido; los campos vacíos muestran el español como
 * guía (y así saldrán en el PDF).
 **/
function pintarTraducciones() {
  const ls = extras();
  $('f-trad-bloque').hidden = !ls.length;
  if (!ls.length) return;
  if (!ls.includes(tabForm)) tabForm = ls[0];
  const t = tradForm[tabForm] || {};
  pestanas($('f-tabs'), ls, tabForm, (l) => { tabForm = l; pintarTraducciones(); }, (l) => !tradForm[l]?.nombre);
  $('t-nombre').value = t.nombre || '';
  $('t-descripcion').value = t.descripcion || '';
  $('t-ingredientes').value = (t.ingredientes || []).join(', ');
  // Guías con el texto en español.
  $('t-nombre').placeholder = $('f-nombre').value || 'Nombre en ' + nombreIdioma(tabForm);
  $('t-descripcion').placeholder = $('f-descripcion').value;
  $('t-ingredientes').placeholder = ingredientes.join(', ');
}

// Escribir una traducción en el formulario.
for (const [id, campo] of [['t-nombre', 'nombre'], ['t-descripcion', 'descripcion'], ['t-ingredientes', 'ingredientes']]) {
  $(id).addEventListener('input', (e) => {
    const t = (tradForm[tabForm] ??= {});
    t[campo] = campo === 'ingredientes' ? e.target.value.split(',').map((x) => x.trim()).filter(Boolean) : e.target.value;
  });
}

/**
 * traducirTareas envía a traducir los textos de las tareas ({texto,
 * aplicar}) al idioma destino, en lotes de 20 para avanzar por partes; cada
 * resultado se aplica con su función. Devuelve cuántas se tradujeron.
 **/
async function traducirTareas(tareas, destino, alAvanzar = () => {}) {
  let hechas = 0;
  for (let i = 0; i < tareas.length; i += 20) {
    const lote = tareas.slice(i, i + 20);
    const r = await api('/api/traducir', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ destino, textos: lote.map((t) => t.texto) }),
    });
    lote.forEach((t, j) => r.textos[j] && t.aplicar(r.textos[j]));
    hechas += lote.length;
    alAvanzar(hechas);
  }
  return hechas;
}

/**
 * tareasProducto arma las tareas para traducir al idioma l solo lo que le
 * falta al producto p (nombre, descripción e ingredientes). Los ingredientes
 * se traducen todos juntos si la lista traducida está incompleta.
 **/
function tareasProducto(p, l) {
  const t = ((p.traducciones ??= {})[l] ??= {});
  const tareas = [];
  if (!t.nombre && p.nombre) tareas.push({ texto: p.nombre, aplicar: (v) => (t.nombre = v) });
  if (!t.descripcion && p.descripcion) tareas.push({ texto: p.descripcion, aplicar: (v) => (t.descripcion = v) });
  if (p.ingredientes.length && (t.ingredientes || []).length !== p.ingredientes.length) {
    const nuevos = [...p.ingredientes];
    p.ingredientes.forEach((ing, k) => tareas.push({ texto: ing, aplicar: (v) => { nuevos[k] = v; t.ingredientes = nuevos; } }));
  }
  return tareas;
}

// Traducir el producto del formulario a todos los idiomas activos (solo lo vacío).
$('f-traducir').addEventListener('click', async (e) => {
  agregarIngredientes();
  const btn = e.currentTarget;
  const p = { nombre: $('f-nombre').value.trim(), descripcion: $('f-descripcion').value.trim(), ingredientes, traducciones: tradForm };
  if (!p.nombre) return ($('f-error').textContent = 'Escribe primero el nombre en español.');
  btn.disabled = true;
  try {
    for (const l of extras()) await traducirTareas(tareasProducto(p, l), l);
    toast('Traducción lista: revísala antes de guardar');
  } catch (err) {
    toast('No se pudo traducir: ' + err.message, true);
  }
  btn.disabled = false;
  pintarTraducciones();
});

// Traducir todo lo que falta en la carta: lema, categorías y productos.
$('traducir-todo').addEventListener('click', async (e) => {
  const btn = e.currentTarget;
  const ls = extras();
  if (!ls.length) return toast('Activa primero otro idioma (EN, FR, DE o PT)', true);
  btn.disabled = true;
  const estado = $('traduccion-estado');
  let total = 0;
  try {
    for (const l of ls) {
      // Lema y categorías que faltan en este idioma.
      const tareas = [];
      if (!carta.lemas[l] && carta.lema) tareas.push({ texto: carta.lema, aplicar: (v) => (carta.lemas[l] = v) });
      if (!carta.eslogans[l] && carta.eslogan) tareas.push({ texto: carta.eslogan, aplicar: (v) => (carta.eslogans[l] = v) });
      for (const c of categoriasDe()) {
        if (!carta.categorias[c]?.[l]) tareas.push({ texto: c, aplicar: (v) => ((carta.categorias[c] ??= {})[l] = v) });
      }
      for (const p of carta.productos) tareas.push(...tareasProducto(p, l));
      // Traduce por lotes, guardando el avance para no perderlo si algo falla.
      await traducirTareas(tareas, l, (n) => {
        estado.textContent = `Traduciendo al ${nombreIdioma(l)}: ${n} de ${tareas.length}…`;
        guardar();
      });
      total += tareas.length;
    }
    estado.textContent = total ? `✓ ${total} textos traducidos con ${config.traductor}. Revisa los nombres de platos típicos: a veces es mejor dejarlos en español.` : '✓ No falta nada por traducir.';
  } catch (err) {
    estado.textContent = '';
    toast('No se pudo traducir: ' + err.message, true);
  }
  btn.disabled = false;
  render();
  guardar();
});

/* ------------------------------------------------------------------ */
/* Inicio                                                             */
/* ------------------------------------------------------------------ */

/**
 * iniciar carga la carta del servidor y pinta la pantalla.
 **/
async function iniciar() {
  try {
    [carta, config] = await Promise.all([api('/api/carta'), api('/api/config')]);
  } catch (err) {
    toast('No se pudo cargar la carta: ' + err.message, true);
  }
  $('nombre').value = carta.nombre;
  $('lema').value = carta.lema;
  carta.eslogans ??= {};
  pintarPortada();
  render();
  mostrarDestino();
  actualizarEstado();
}
iniciar();
