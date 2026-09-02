import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/**
 * Copiar al portapapeles con fallback para orígenes no seguros (#466).
 * La API async clipboard (navigator.clipboard) solo existe en contexto
 * seguro (HTTPS/localhost); NetPulse se suele servir por http://<ip>:3000
 * en la LAN, así que se cae a textarea + execCommand, que sigue
 * funcionando dentro de un gesto de usuario. Devuelve false si ambas
 * vías fallan.
 */
export async function copyToClipboard(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    /* sin permiso o contexto no seguro: probar el fallback */
  }
  try {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.setAttribute('readonly', '')
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    // focus() antes de select(): con el foco aún en el botón, Chromium
    // devuelve false en execCommand aunque la selección exista.
    ta.focus()
    ta.select()
    ta.setSelectionRange(0, text.length)
    const ok = document.execCommand('copy')
    document.body.removeChild(ta)
    return ok
  } catch {
    return false
  }
}

/**
 * Salir del modo demo (frontend estático sin backend): aquí no hay sesión ni
 * login al que volver. Se intenta cerrar la ventana (la demo se abre en una
 * pestaña propia desde la landing); si el navegador bloquea window.close()
 * (pestañas no abiertas por script), se vuelve a la pantalla de login de la
 * propia app, que explica que la demo es de solo lectura y permite volver a
 * entrar como demo.
 */
export function exitDemo() {
  try {
    sessionStorage.removeItem('netpulse-demo')
  } catch {
    /* modo privado */
  }
  window.close()
  window.setTimeout(() => {
    window.location.assign('/login')
  }, 300)
}

export type JsonFetch<T> =
  | { ok: true; data: T }
  | { ok: false; kind: 'error' | 'no-api' | 'unauthorized' }

/**
 * Fetch de un endpoint que debe devolver JSON. Distingue el fallo de red/HTTP
 * del caso "demo sin API": la demo estática (sin backend) sirve el HTML del
 * SPA para /api/* vía fallback de nginx (200 con content-type text/html),
 * así que un body que no es JSON se reporta como `kind: 'no-api'`. Los
 * paneles que hacen fetch directo (Reports/Roaming) deciden qué mostrar.
 */
export async function fetchJson<T>(url: string, init?: RequestInit): Promise<JsonFetch<T>> {
  try {
    const res = await fetch(url, init)
    if (res.status === 401) return { ok: false, kind: 'unauthorized' }
    if (!res.ok) throw new Error(`status ${res.status}`)
    const contentType = res.headers.get('content-type') ?? ''
    if (!contentType.includes('application/json')) {
      return { ok: false, kind: 'no-api' }
    }
    return { ok: true, data: (await res.json()) as T }
  } catch {
    return { ok: false, kind: 'error' }
  }
}
