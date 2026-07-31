import { createRoot } from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import './index.css'
import './i18n'
import App from './App.tsx'

// Tema antes del primer paint: dark por defecto, claro si el usuario lo eligió
if (localStorage.getItem('netpulse-theme') === 'light') {
  document.documentElement.classList.remove('dark')
  document.documentElement.classList.add('light')
}

createRoot(document.getElementById('root')!).render(
  <BrowserRouter>
    <App />
  </BrowserRouter>,
)
