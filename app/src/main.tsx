import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
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
