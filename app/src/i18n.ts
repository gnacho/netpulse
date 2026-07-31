import i18n from 'i18next'
import { initReactI18next } from 'react-i18next'
import LanguageDetector from 'i18next-browser-languagedetector'
import HttpBackend from 'i18next-http-backend'

// 'auto' => seguir al navegador: no debe quedar idioma cacheado en localStorage
if (localStorage.getItem('netpulse-lang') === 'auto') {
  localStorage.removeItem('i18nextLng')
}

i18n
  .use(HttpBackend)
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    // Inglés por defecto salvo elección previa (español opcional en Ajustes;
    // la opción 'auto' sigue al navegador vía detector manual)
    lng: localStorage.getItem('i18nextLng') ?? 'en',
    fallbackLng: 'en',
    supportedLngs: ['es', 'en'],
    nonExplicitSupportedLngs: true,
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

export default i18n

/** Locale numérico según el idioma activo */
export function numLocale(): string {
  return i18n.language?.startsWith('en') ? 'en-US' : 'es-ES'
}

const REL_RE = /^hace\s+(\d+)\s*(s|min|h|días?|d)$/

/** Convierte tiempos relativos canónicos del dataset ("hace 12 min", "hoy") al idioma activo */
export function relTime(s: string): string {
  const t = s.trim()
  if (t === 'hoy') return i18n.t('common.today')
  const m = REL_RE.exec(t)
  if (!m) return s
  const n = Number(m[1])
  const unit = m[2] === 's' ? 's' : m[2] === 'min' ? 'min' : m[2] === 'h' ? 'h' : 'd'
  return i18n.t(`common.ago.${unit}`, { count: n, n })
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
