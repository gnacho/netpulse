/**
 * Boot de preferencias (anti-FOUC) - webapp-shell.
 * Corrige los caveats de NetPulse: el modo `system` se restaura al arrancar,
 * el listener de matchMedia vive aqui (no en Ajustes) y el acento/densidad
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

export interface PaletteDef {
  id: string
  labelKey: string
  swatch: string
  dark: Record<string, string>
  light: Record<string, string>
}

export const PALETTES: PaletteDef[] = [
  {
    id: 'netpulse',
    labelKey: 'settings.paletteNetpulse',
    swatch: '#22D3EE',
    dark: {
      canvas: '7 11 18', surface: '13 20 32', elevated: '19 28 44', hover: '26 36 54',
      border: '30 42 61', 'border-strong': '42 58 82',
      'text-primary': '232 238 247', 'text-secondary': '154 168 188', 'text-muted': '91 107 130',
      accent: '34 211 238', tunnel: '167 139 250',
      ok: '52 211 153', warn: '251 191 36', danger: '248 113 113', info: '96 165 250',
    },
    light: {
      canvas: '243 245 249', surface: '255 255 255', elevated: '255 255 255', hover: '241 245 249',
      border: '226 232 240', 'border-strong': '203 213 225',
      'text-primary': '15 23 42', 'text-secondary': '71 85 105', 'text-muted': '100 116 139',
      accent: '8 145 178', tunnel: '124 58 237',
      ok: '5 150 105', warn: '217 119 6', danger: '220 38 38', info: '37 99 235',
    },
  },
  {
    id: 'noc',
    labelKey: 'settings.paletteNoc',
    swatch: '#F59E0B',
    dark: {
      canvas: '10 10 10', surface: '20 18 16', elevated: '30 26 22', hover: '40 34 28',
      border: '50 42 34', 'border-strong': '70 58 46',
      'text-primary': '245 240 230', 'text-secondary': '180 168 150', 'text-muted': '120 108 92',
      accent: '245 158 11', tunnel: '249 115 22',
      ok: '132 204 22', warn: '249 115 22', danger: '239 68 68', info: '56 189 248',
    },
    light: {
      canvas: '250 248 245', surface: '255 255 255', elevated: '255 255 255', hover: '245 240 232',
      border: '230 222 210', 'border-strong': '210 198 180',
      'text-primary': '28 25 20', 'text-secondary': '80 72 60', 'text-muted': '130 118 100',
      accent: '180 110 0', tunnel: '194 80 0',
      ok: '100 160 10', warn: '194 80 0', danger: '200 40 40', info: '14 165 233',
    },
  },
  {
    id: 'phosphor',
    labelKey: 'settings.palettePhosphor',
    swatch: '#00FF41',
    dark: {
      canvas: '8 12 8', surface: '14 22 14', elevated: '20 32 20', hover: '26 42 26',
      border: '32 52 32', 'border-strong': '46 72 46',
      'text-primary': '220 245 220', 'text-secondary': '140 185 140', 'text-muted': '80 120 80',
      accent: '0 255 65', tunnel: '0 212 170',
      ok: '0 255 65', warn: '255 200 0', danger: '255 68 68', info: '0 180 220',
    },
    light: {
      canvas: '245 250 245', surface: '255 255 255', elevated: '255 255 255', hover: '238 248 238',
      border: '215 235 215', 'border-strong': '185 215 185',
      'text-primary': '15 30 15', 'text-secondary': '55 85 55', 'text-muted': '95 130 95',
      accent: '0 140 35', tunnel: '0 130 105',
      ok: '0 140 35', warn: '180 140 0', danger: '200 40 40', info: '0 130 160',
    },
  },
  {
    id: 'electric',
    labelKey: 'settings.paletteElectric',
    swatch: '#06B6D4',
    dark: {
      canvas: '5 10 20', surface: '12 20 36', elevated: '18 28 50', hover: '24 36 62',
      border: '32 46 74', 'border-strong': '48 66 100',
      'text-primary': '235 242 255', 'text-secondary': '160 175 210', 'text-muted': '95 112 150',
      accent: '6 182 212', tunnel: '224 64 251',
      ok: '45 212 191', warn: '255 107 107', danger: '244 63 94', info: '99 102 241',
    },
    light: {
      canvas: '240 245 255', surface: '255 255 255', elevated: '255 255 255', hover: '235 240 252',
      border: '215 225 245', 'border-strong': '190 205 235',
      'text-primary': '10 18 40', 'text-secondary': '55 70 110', 'text-muted': '100 115 155',
      accent: '6 140 165', tunnel: '168 30 200',
      ok: '13 148 130', warn: '200 60 60', danger: '190 30 60', info: '79 70 229',
    },
  },
] as const

export type PaletteId = (typeof PALETTES)[number]['id']

const MODE_KEY = 'netpulse-theme-mode'
const LEGACY_KEY = 'netpulse-theme'
const ACCENT_KEY = 'netpulse-accent'
const PALETTE_KEY = 'netpulse-palette'
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

export function readPalette(): PaletteId {
  const p = readJson<PaletteId>(PALETTE_KEY)
  if (p && PALETTES.some((x) => x.id === p)) return p
  return 'netpulse'
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
  applyPalette(light)
}

function applyPalette(light: boolean) {
  const id = readPalette()
  const palette = PALETTES.find((x) => x.id === id) ?? PALETTES[0]
  const vars = light ? palette.light : palette.dark
  const root = document.documentElement
  for (const [key, value] of Object.entries(vars)) {
    root.style.setProperty(`--${key}`, value)
  }
  root.setAttribute('data-palette', id)
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

export function applyBootPreferences() {
  const mode = readMode()
  applyTheme(mode)
  applyDensity()
  applyReduceMotion()

  window.matchMedia('(prefers-color-scheme: light)').addEventListener('change', () => {
    if (readMode() === 'system') applyTheme('system')
  })
}
