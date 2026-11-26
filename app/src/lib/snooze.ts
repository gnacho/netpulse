/**
 * Cálculo de la posposición de alertas (#2185). Vive fuera del componente
 * porque lo usan el provider (estado optimista) y el menú (opciones).
 */

export type SnoozeDuration = '1h' | '2h' | '8h' | '7d' | 'forever'

export type SnoozeWhen = { duration: SnoozeDuration } | { until: number }

const HOURS: Record<SnoozeDuration, number> = { '1h': 1, '2h': 2, '8h': 8, '7d': 7 * 24, forever: 0 }

/** Unix SEGUNDOS del fin de posposición (0 = sin fecha). */
export function snoozeUntil(when: SnoozeWhen): number {
  if ('until' in when) return when.until
  const hours = HOURS[when.duration]
  if (hours === 0) return 0
  return Math.floor(Date.now() / 1000) + hours * 3600
}

/** Mañana a las 09:00 locales, en unix SEGUNDOS. */
export function tomorrowAtNine(now = new Date()): number {
  const d = new Date(now)
  d.setDate(d.getDate() + 1)
  d.setHours(9, 0, 0, 0)
  return Math.floor(d.getTime() / 1000)
}

/** Texto corto del fin de posposición: hora si es hoy, fecha y hora si no. */
export function snoozeLabel(until: number, locale: string): string {
  if (until <= 0) return ''
  const target = new Date(until * 1000)
  const sameDay = target.toDateString() === new Date().toDateString()
  return sameDay
    ? target.toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit' })
    : target.toLocaleString(locale, { dateStyle: 'short', timeStyle: 'short' })
}
