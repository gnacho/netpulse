/**
 * Boot de preferencias (anti-FOUC) — webapp-shell.
 * Corrige los caveats de NetPulse: el modo `system` se restaura al arrancar,
 * el listener de matchMedia vive aquí (no en Ajustes) y el acento/densidad
 * se aplican antes del primer render (sobreviven reload).
 */

export const ACCENTS = [
  { id: 'cyan', dark: '34 211 238', light: '8 145 178', swatch: '#22D3EE', labelKey: 'settings.accentCyan' },
  { id: 'violet', dark: '167 139 250', light: '124 58 237', swatch: '#A78BFA', labelKey: 'settings.accentViolet' },
  { id: 'emerald', dark: '52 211 153', light: '5 150 105', swatch: '#34D399', labelKey: 'settings.accentEmerald' },
  { id: 'amber', dark: '251 191 36', light: '217 119 6', swatch: '#FBBF24', labelKey: 'settings.accentAmber' },
] as const

export type AccentId = (typeof ACCENTS)[number]['id']
export type ThemeMode = 'dark' | 'light' | 'system'

const MODE_KEY = 'netpulse-theme-mode'
const LEGACY_KEY = 'netpulse-theme' // último valor resuelto (compat con ThemeToggle viejo)
const ACCENT_KEY = 'netpulse-accent'
const DENSITY_KEY = 'netpulse-density'
const REDUCE_MOTION_KEY = 'netpulse-reduce-motion'

function readJson<T>(key: string): T | null {
  try {
    const raw = localStorage.getItem(key)
    return raw !== null ? (JSON.parse(raw) as T) : null
  } catch {
    return null
  }
}

export function readMode(): ThemeMode {
  const m = readJson<ThemeMode>(MODE_KEY)
  if (m === 'dark' || m === 'light' || m === 'system') return m
  return localStorage.getItem(LEGACY_KEY) === 'light' ? 'light' : 'dark'
}

export function resolveLight(mode: ThemeMode): boolean {
  if (mode === 'light') return true
  if (mode === 'dark') return false
  return window.matchMedia('(prefers-color-scheme: light)').matches
}

export function applyTheme(mode: ThemeMode) {
  const light = resolveLight(mode)
  document.documentElement.classList.toggle('light', light)
  document.documentElement.classList.toggle('dark', !light)
  try {
    localStorage.setItem(LEGACY_KEY, light ? 'light' : 'dark')
  } catch {
    /* noop */
  }
  applyAccent(light)
}

function applyAccent(light: boolean) {
  const id = readJson<AccentId>(ACCENT_KEY) ?? 'cyan'
  const a = ACCENTS.find((x) => x.id === id) ?? ACCENTS[0]
  document.documentElement.style.setProperty('--accent', light ? a.light : a.dark)
}

function applyDensity() {
  const d = readJson<string>(DENSITY_KEY)
  document.documentElement.style.fontSize = d === 'compacta' ? '13.5px' : ''
}

function applyReduceMotion() {
  const v = readJson<boolean>(REDUCE_MOTION_KEY) === true
  document.documentElement.classList.toggle('reduce-motion', v)
  const STYLE_ID = 'netpulse-reduce-motion-style'
  if (v && !document.getElementById(STYLE_ID)) {
    const el = document.createElement('style')
    el.id = STYLE_ID
    el.textContent =
      'html.reduce-motion *,html.reduce-motion *::before,html.reduce-motion *::after{animation-duration:0.01ms !important;animation-iteration-count:1 !important;transition-duration:0.01ms !important;scroll-behavior:auto !important}'
    document.head.appendChild(el)
  }
}

/** Llamar en main.tsx ANTES del primer render. */
export function applyBootPreferences() {
  const mode = readMode()
  applyTheme(mode)
  applyDensity()
  applyReduceMotion()

  // El listener de system vive aquí (global), no en la página de Ajustes.
  window.matchMedia('(prefers-color-scheme: light)').addEventListener('change', () => {
    if (readMode() === 'system') applyTheme('system')
  })
}
