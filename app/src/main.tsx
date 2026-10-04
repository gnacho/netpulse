// Cuando el service worker instala una versión nueva y toma el control,
// recargamos la pestaña UNA vez: sin esto, tras cada despliegue el usuario
// quedaba en la app vieja hasta hacer dos recargas manuales.
let swReloaded = false
if ('serviceWorker' in navigator) {
  navigator.serviceWorker.addEventListener('controllerchange', () => {
    if (swReloaded) return
    swReloaded = true
    window.location.reload()
  })
}
import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router'
import './index.css'
import './i18n'
import { applyBootPreferences } from './lib/theme-boot'
import App from './App.tsx'

// Preferencias (tema/acento/densidad/reduce-motion) antes del primer paint.
applyBootPreferences()

createRoot(document.getElementById('root')!).render(
  <BrowserRouter>
    <App />
  </BrowserRouter>,
)
