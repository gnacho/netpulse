// Red de seguridad (bug pantalla negra tras update): si el bundle no
// carga, #root queda vacío (el splash CSS pinta negro). Tras 8 s se
// recarga UNA vez (guard en sessionStorage evita bucles); si vuelve a
// fallar, mensaje en vez de recargar.
window.setTimeout(function () {
  var r = document.getElementById('root')
  if (!r || r.childElementCount > 0) { sessionStorage.removeItem('np-blank'); return }
  if (!sessionStorage.getItem('np-blank')) {
    sessionStorage.setItem('np-blank', '1')
    location.reload()
  } else {
    r.textContent = 'La app no pudo cargar (¿actualización en curso?). Recarga la página.'
    r.style.cssText = 'padding:2rem;text-align:center;color:#98a2b3;font-family:Inter,system-ui,sans-serif;font-size:14px'
  }
}, 8000)
