import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Activity } from 'lucide-react'
import { relTimeFromTs } from '@/i18n'
import { fetchJson } from '@/lib/utils'
import { useNetPulse } from '@/data/DataProvider'

// PresenceSection — timeline de presencia de un cliente (#771): bandas de
// 24 h por día con los tramos conectado coloreados por AP (router), más el
// feed de eventos recientes. Los datos salen de GET
// /api/devices/{mac}/presence (device_events #184 fundidos con los
// AP-STA-CONNECTED de roam_events, Fase 14.5).

interface Interval {
  routerId: string
  startMs: number
  endMs: number | null
}

interface PresenceEvent {
  ts_ms: number
  state: string
  router_id?: string
}

interface PresenceData {
  mac: string
  days: number
  nowMs: number
  intervals: Interval[]
  events: PresenceEvent[]
  connects24h: number
}

/** Color estable por router: hue derivado del id (misma AP = mismo color). */
function routerHue(id: string): number {
  let h = 0
  for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) % 360
  return h
}

const DAY_MS = 24 * 3600 * 1000

export function PresenceSection({ mac }: { mac: string }) {
  const { t, i18n } = useTranslation()
  const { isDemo } = useNetPulse()
  const [data, setData] = useState<PresenceData | null>(null)
  const [error, setError] = useState(false)

  useEffect(() => {
    if (isDemo) return // la muestra la genera useMemo de abajo
    let cancelled = false
    fetchJson<PresenceData>(`/api/devices/${encodeURIComponent(mac)}/presence?days=7`)
      .then((res) => {
        if (cancelled) return
        if (res.ok && res.data) setData(res.data)
        else setError(true)
      })
      .catch(() => {
        if (!cancelled) setError(true)
      })
    return () => {
      cancelled = true
    }
  }, [mac, isDemo])

  // Demo: muestra determinista local (el modo demo no tiene sesión API;
  // mismo criterio que el resto del dataset demo).
  const demoData = useMemo<PresenceData | null>(() => {
    if (!isDemo) return null
    const nowMs = Date.now()
    const now = new Date(nowMs)
    const todayStart = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
    let h = 0
    for (let i = 0; i < mac.length; i++) h = (h * 31 + mac.charCodeAt(i)) % 240
    const mk = (dayStart: number, h1: number, h2: number, router: string): Interval => ({
      routerId: router,
      startMs: dayStart + h1 * 3600_000 + (h % 60) * 60_000,
      endMs: h2 >= 0 ? dayStart + h2 * 3600_000 : null,
    })
    const intervals: Interval[] = [
      mk(todayStart - DAY_MS, 18, 23, 'salon'),
      mk(todayStart, 8, 12, 'salon'),
      mk(todayStart, 13, -1, h % 2 === 0 ? 'patio' : 'salon'),
    ]
    return { mac, days: 7, nowMs, intervals, events: [], connects24h: 2 + (h % 3) }
  }, [isDemo, mac])

  const effective = data ?? demoData

  // Bandas de los últimos N días (hoy primero) con sus tramos recortados al día.
  const days = useMemo(() => {
    if (!effective) return []
    const data = effective
    const now = new Date(data.nowMs)
    const todayStart = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime()
    const out: { label: string; startMs: number; endMs: number; bars: { left: number; width: number; hue: number; router: string }[] }[] = []
    for (let d = 0; d < data.days; d++) {
      const dayStart = todayStart - d * DAY_MS
      const dayEnd = dayStart + DAY_MS
      const bars = data.intervals
        .map((iv) => {
          const s = Math.max(iv.startMs, dayStart)
          const e = Math.min(iv.endMs ?? data.nowMs, Math.min(dayEnd, data.nowMs))
          if (e <= s) return null
          return {
            left: ((s - dayStart) / DAY_MS) * 100,
            width: ((e - s) / DAY_MS) * 100,
            hue: routerHue(iv.routerId),
            router: iv.routerId,
          }
        })
        .filter((b): b is NonNullable<typeof b> => b !== null)
      const dt = new Date(dayStart)
      const label = new Intl.DateTimeFormat(i18n.language, { weekday: 'short', day: 'numeric' }).format(dt)
      out.push({ label, startMs: dayStart, endMs: dayEnd, bars })
    }
    return out
  }, [effective, i18n.language])

  if (error) return null // sin datos de presencia: la sección no se muestra

  return (
    <div className="col-span-2 space-y-3 md:col-span-3">
      <div className="flex items-center gap-2">
        <Activity className="h-4 w-4 text-accent" strokeWidth={1.75} />
        <span className="text-label uppercase text-text-muted">{t('devices.presence.title')}</span>
      </div>
      {!effective ? (
        <p className="text-caption text-text-muted">{t('common.loading')}</p>
      ) : days.every((d) => d.bars.length === 0) ? (
        <p className="text-caption text-text-muted">{t('devices.presence.empty')}</p>
      ) : (
        <div className="space-y-1.5">
          {days.map((d) => (
            <div key={d.startMs} className="flex items-center gap-2">
              <span className="w-16 shrink-0 truncate text-caption capitalize text-text-muted">{d.label}</span>
              <div className="relative h-3 flex-1 overflow-hidden rounded-full bg-border/40">
                {d.bars.map((b, i) => (
                  <div
                    key={i}
                    title={b.router}
                    className="absolute top-0 h-full rounded-full"
                    style={{
                      left: `${b.left}%`,
                      width: `${Math.max(b.width, 0.4)}%`,
                      backgroundColor: `hsl(${b.hue} 65% 55%)`,
                    }}
                  />
                ))}
              </div>
            </div>
          ))}
        </div>
      )}
      {effective && effective.events.length > 0 && (
        <ul className="space-y-0.5 pt-1">
          {effective.events.slice(0, 6).map((ev, i) => (
            <li key={i} className="flex items-center gap-2 text-caption text-text-secondary">
              <span className={ev.state === 'online' ? 'text-ok' : 'text-text-muted'}>
                {ev.state === 'online'
                  ? t('devices.presence.online', { router: ev.router_id || '—' })
                  : t('devices.presence.offline')}
              </span>
              <span className="ml-auto shrink-0 text-text-muted">{relTimeFromTs(Math.floor(ev.ts_ms / 1000))}</span>
            </li>
          ))}
        </ul>
      )}
      {effective && effective.connects24h > 1 && (
        <p className="text-caption text-warn">{t('devices.presence.flappy', { count: effective.connects24h })}</p>
      )}
    </div>
  )
}
