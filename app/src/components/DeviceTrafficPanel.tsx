/**
 * DeviceTrafficPanel — volumen de tráfico de un dispositivo (#2076).
 *
 * El servidor ya guarda la serie por cliente desde #551 (`/api/devices/{mac}
 * /traffic`, tiers raw/5m/daily). Lo que faltaba era poder leer el volumen de
 * un cliente en las escalas que pide el reportero: una hora, un día, un mes.
 *
 * Suma los bytes del rango elegido y dibuja la serie del mismo rango. En demo
 * no se pinta: la demo no tiene series de cliente.
 */
import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Area, AreaChart, ResponsiveContainer, Tooltip, XAxis } from 'recharts'
import { SegmentedControl } from '@/components/SegmentedControl'
import { fmtBytes } from '@/data/mock'
import { cn } from '@/lib/utils'

interface TrafficPoint {
  ts: string
  rxBytes: number
  txBytes: number
  rxBps: number
  txBps: number
}

interface TrafficResponse {
  points: TrafficPoint[]
  resolution: string
}

type WindowKey = '1h' | '24h' | '30d' | '12m'

const WINDOW_MS: Record<WindowKey, number> = {
  '1h': 60 * 60 * 1000,
  '24h': 24 * 60 * 60 * 1000,
  '30d': 30 * 24 * 60 * 60 * 1000,
  '12m': 365 * 24 * 60 * 60 * 1000,
}

export function DeviceTrafficPanel({ mac }: { mac: string }) {
  const { t, i18n } = useTranslation()
  const [win, setWin] = useState<WindowKey>('24h')
  const [points, setPoints] = useState<TrafficPoint[] | null>(null)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    const ctl = new AbortController()
    setPoints(null)
    setFailed(false)
    const to = Date.now()
    const from = to - WINDOW_MS[win]
    const qs = new URLSearchParams({
      from: String(Math.floor(from / 1000)),
      to: String(Math.floor(to / 1000)),
    })
    fetch(`/api/devices/${encodeURIComponent(mac)}/traffic?${qs.toString()}`, {
      signal: ctl.signal,
      credentials: 'same-origin',
    })
      .then((res) => (res.ok ? res.json() : Promise.reject(new Error(String(res.status)))))
      .then((body: TrafficResponse) => setPoints(body.points ?? []))
      .catch((err: Error) => {
        if (err.name !== 'AbortError') setFailed(true)
      })
    return () => ctl.abort()
  }, [mac, win])

  const totals = useMemo(() => {
    let rx = 0
    let tx = 0
    for (const p of points ?? []) {
      rx += p.rxBytes
      tx += p.txBytes
    }
    return { rx, tx }
  }, [points])

  const data = useMemo(
    () =>
      (points ?? []).map((p) => ({
        t: new Date(p.ts).getTime(),
        down: p.rxBytes,
        up: p.txBytes,
      })),
    [points],
  )

  const fmtTick = (v: number) => {
    const d = new Date(v)
    if (win === '1h' || win === '24h') {
      return d.toLocaleTimeString(i18n.language, { hour: '2-digit', minute: '2-digit' })
    }
    return d.toLocaleDateString(i18n.language, { day: '2-digit', month: 'short' })
  }

  const empty = points !== null && points.length === 0

  return (
    <div className="border-t border-border/60 px-4 py-4 md:px-5">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="text-label uppercase text-text-muted">{t('devices.traffic.title')}</div>
        <SegmentedControl<WindowKey>
          size="sm"
          value={win}
          onChange={setWin}
          ariaLabel={t('devices.traffic.title')}
          options={[
            { value: '1h', label: '1 h' },
            { value: '24h', label: '24 h' },
            { value: '30d', label: '30 d' },
            { value: '12m', label: '12 m' },
          ]}
        />
      </div>

      {!points && !failed && (
        <div className="mt-3 h-10 animate-pulse rounded-lg bg-elevated" aria-hidden />
      )}

      {(failed || empty) && (
        <p className="mt-3 rounded-lg bg-elevated px-3 py-2 text-caption leading-relaxed text-text-muted">
          {failed ? t('devices.traffic.error') : t('devices.traffic.empty')}
        </p>
      )}

      {points && !failed && !empty && (
        <>
          <div className="mt-3 flex flex-wrap items-baseline gap-x-6 gap-y-1">
            <span className="text-caption text-text-muted">
              {t('devices.traffic.down')}{' '}
              <span className="font-mono text-mono-sm text-accent">{fmtBytes(totals.rx)}</span>
            </span>
            <span className="text-caption text-text-muted">
              {t('devices.traffic.up')}{' '}
              <span className="font-mono text-mono-sm text-tunnel">{fmtBytes(totals.tx)}</span>
            </span>
            <span className="text-caption text-text-muted">
              {t('devices.traffic.total')}{' '}
              <span className="font-mono text-mono-sm text-text-primary">{fmtBytes(totals.rx + totals.tx)}</span>
            </span>
          </div>
          <div className={cn('mt-2 h-24')} aria-hidden>
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={data} margin={{ top: 4, right: 0, bottom: 0, left: 0 }}>
                <XAxis dataKey="t" hide tickFormatter={fmtTick} />
                <Tooltip
                  labelFormatter={(v) => new Date(Number(v)).toLocaleString(i18n.language)}
                  formatter={(value) => fmtBytes(Number(value))}
                />
                <Area type="monotone" dataKey="down" stroke="#22D3EE" fill="#22D3EE" fillOpacity={0.18} strokeWidth={1.5} dot={false} />
                <Area type="monotone" dataKey="up" stroke="#A78BFA" fill="#A78BFA" fillOpacity={0.14} strokeWidth={1.5} dot={false} />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </>
      )}
    </div>
  )
}
