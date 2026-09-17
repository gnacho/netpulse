import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import HttpBackend from 'i18next-http-backend'

// 'auto' means "follow the browser", and it is an explicit choice in
// Settings: the detector must not keep a cached language for it.
const savedLang = localStorage.getItem('netpulse-lang')
if (!savedLang || savedLang === 'auto') {
  localStorage.removeItem('i18nextLng')
}

// Builds with a forced language (public demo, VITE_FORCE_LANG=en).
const forcedLang = import.meta.env.VITE_FORCE_LANG as 'es' | 'en' | undefined
// DEFAULT_LNG: English is the language this deployment shows unless somebody
// asks for another. Following the browser as the default meant a Spanish or
// Romanian locale opened the panel in Spanish, which is not what the people
// running it read. A choice made in Settings still wins, and choosing 'auto'
// there goes back to following the browser.
const DEFAULT_LNG = 'en'
const initialLng = savedLang
  ? savedLang === 'auto'
    ? undefined // explicit "follow the browser"
    : savedLang
  : (forcedLang ?? DEFAULT_LNG)

i18n
  .use(HttpBackend)
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    // With no previous choice the panel starts in English. Only an explicit
    // 'auto' in Settings leaves this undefined so the LanguageDetector reads
    // the browser (localStorage → navigator). (issue #3, #237)
    lng: initialLng,
    fallbackLng: 'en',
    supportedLngs: ['es', 'en'],
    nonExplicitSupportedLngs: true,
    load: 'languageOnly',
    backend: {
      loadPath: '/locales/{{lng}}/translation.json',
    },
    detection: {
      order: ['localStorage', 'navigator'],
      caches: ['localStorage'],
      lookupLocalStorage: 'i18nextLng',
    },
    interpolation: { escapeValue: false },
    react: { useSuspense: true },
    returnNull: false,
  })

// #666: the document's lang must follow the active language. Without it the
// browser believes the page is in another language and offers to translate
// it -- and the translator mangles the data (hostnames "crowed" →
// "crowded"). With the right lang, a translated UI raises no such offer.
const applyDocLang = (lng?: string) => {
  const l = (lng ?? i18n.language ?? DEFAULT_LNG).slice(0, 2).toLowerCase()
  document.documentElement.lang = l === 'en' ? 'en' : 'es'
}
applyDocLang()
i18n.on('languageChanged', applyDocLang)

export default i18n

/** Locale numérico según el idioma activo */
export function numLocale(): string {
  return i18n.language?.startsWith('en') ? 'en-US' : 'es-ES'
}

const REL_RE = /^hace\s+(\d+)\s*(s|min|h|días?|d)$/
const REL_RE_EN = /^(\d+)\s*(s|sec|min|h|hr|d|days?)\s+ago$/

/** Convierte tiempos relativos canónicos del dataset ("hace 12 min", "12 min ago", "hoy") al idioma activo */
export function relTime(s: string): string {
  const t = s.trim()
  if (t === 'hoy' || t.toLowerCase() === 'today') return i18n.t('common.today')
  // A peer that has never handshaked: the server sends the canonical
  // Spanish and it was reaching the screen untranslated.
  if (t === 'nunca' || t.toLowerCase() === 'never') return i18n.t('common.never')
  const m = REL_RE.exec(t) ?? REL_RE_EN.exec(t)
  if (!m) return s
  const n = Number(m[1])
  const u = m[2]
  const unit = u === 's' || u === 'sec' ? 's' : u === 'min' ? 'min' : u === 'h' || u === 'hr' ? 'h' : 'd'
  return i18n.t(`common.ago.${unit}`, { count: n, n })
}

/**
 * Tiempo relativo desde un timestamp unix en SEGUNDOS (SPEC-ALERTAS §1: el
 * frontend calcula el relativo; el string `time` queda como fallback legado).
 * Devuelve null si `ts` no es usable (0/NaN/futuro lejano) → usar `relTime(time)`.
 */
export function relTimeFromTs(ts: number | undefined, nowMs = Date.now()): string | null {
  if (!ts || !Number.isFinite(ts) || ts <= 0) return null
  const diffS = Math.floor(nowMs / 1000) - ts
  if (diffS < 0) return null
  if (diffS < 60) return i18n.t('common.ago.s', { count: diffS, n: diffS })
  const min = Math.floor(diffS / 60)
  if (min < 60) return i18n.t('common.ago.min', { count: min, n: min })
  const h = Math.floor(min / 60)
  if (h < 24) return i18n.t('common.ago.h', { count: h, n: h })
  const d = Math.floor(h / 24)
  return i18n.t('common.ago.d', { count: d, n: d })
}

/** Relativo de una alerta: desde `ts` si es válido; si no, el string legado. */
export function alertRelTime(ev: { ts?: number; time: string }): string {
  return relTimeFromTs(ev.ts) ?? relTime(ev.time)
}

/** Uptime canónico tipo "12d 4h" → "12 días, 4 h" / "12 days, 4 h" */
export function fmtUptime(uptime: string): string {
  const m = /^(\d+)d\s*(\d+)h$/.exec(uptime.trim())
  if (!m) return uptime
  const d = Number(m[1])
  const h = Number(m[2])
  return i18n.t('common.uptime', { d, h, count: d })
}

const LEASE_RE = /^renueva en\s+(\d+)\s*h(?:\s*(\d+)\s*min)?$/

/** Concesión DHCP canónica: "renueva en 7 h 48 min" / "IP fija (reserva)" */
export function dhcpLease(s: string): string {
  const t = s.trim()
  if (t === 'IP fija (reserva)') return i18n.t('common.staticLease')
  const m = LEASE_RE.exec(t)
  if (!m) return s
  const h = Number(m[1])
  if (!m[2]) return i18n.t('common.leaseRenewsH', { h })
  return i18n.t('common.leaseRenews', { h, min: Number(m[2]) })
}

const HEALTH_KEYS: Record<string, string> = {
  Excelente: 'excellent',
  Bueno: 'good',
  Atención: 'warning',
  Crítico: 'critical',
}

/** Etiqueta de salud canónica del dataset ("Excelente"…) → idioma activo */
export function healthLabel(label: string): string {
  const k = HEALTH_KEYS[label]
  return k ? i18n.t(`common.health.${k}`) : label
}

/** Subscore de salud por clave ("wan" | "wifi" | "infra" | "services") → idioma activo */
export function subscoreLabel(key: string, label: string): string {
  const known = ['wan', 'wifi', 'infra', 'services']
  return known.includes(key) ? i18n.t(`common.health.subscores.${key}`) : label
}

/** Fabricante OUI canónico del dataset ("Desconocido" = sin match) → idioma activo */
export function manufacturerLabel(manufacturer: string): string {
  if (manufacturer === 'Desconocido') return i18n.t('devices.unknownManufacturer')
  return manufacturer
}

/** Rol canónico del dataset ("Principal" | "AP") → idioma activo */
export function roleLabel(role: string): string {
  if (role === 'Principal') return i18n.t('common.rolePrimary')
  if (role === 'AP') return i18n.t('common.roleAP')
  return role
}

const DAY_KEYS = ['mon', 'tue', 'wed', 'thu', 'fri', 'sat', 'sun'] as const

/** Abreviatura de día de la semana: dayAbbr(0) = Lun/Mon … dayAbbr(6) = Dom/Sun */
export function dayAbbr(index: number): string {
  return i18n.t(`common.days.${DAY_KEYS[((index % 7) + 7) % 7]}`)
}

const HEALTH_BREAKDOWN_RE = {
  offline: /^(.+) offline$/,
  temp: /^temp\. (.+)$/,
}

/**
 * Canonical health-breakdown label from the server ("AdGuard inactivo",
 * "temp. Patio", "Salon offline") → active language. These are built by
 * concatenation server-side, so they are matched by shape, and anything
 * unrecognised is passed through rather than mangled.
 */
export function healthBreakdownLabel(label: string): string {
  const t = label.trim()
  if (t === 'AdGuard inactivo') return i18n.t('common.health.adguardInactive')
  const off = HEALTH_BREAKDOWN_RE.offline.exec(t)
  if (off) return i18n.t('common.health.routerOffline', { name: off[1] })
  const temp = HEALTH_BREAKDOWN_RE.temp.exec(t)
  if (temp) return i18n.t('common.health.routerTemp', { name: temp[1] })
  return label
}
