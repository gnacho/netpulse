import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis } from 'recharts'
import { SectionHeader } from '@/components/SectionHeader'
import type { EthPort } from '@/components/routers/routerExtras'
import { cn } from '@/lib/utils'

interface PortPoint {
  ts: string
  rxBytes: number
  txBytes: number
  rxErrors: number
  txErrors: number
  rxBps: number
  txBps: number
  rxFps: number
  txFps: number
  speedMbps: number
}

interface PortSeriesResponse {
  points: PortPoint[]
  resolution: string
}

type Range = '24h' | '7d' | '30d'
export type TrafficUnit = 'bps' | 'fps'

const RANGES: Range[] = ['24h', '7d', '30d']

function rangeToSeconds(range: Range): number {
  switch (range) {
    case '24h': return 24 * 3600
    case '7d': return 7 * 24 * 3600
    case '30d': return 30 * 24 * 3600
  }
}

/** Formatea una tasa: bps → bps/kbps/Mbps/Gbps; fps → fps/kfps. */
export function fmtRate(unit: TrafficUnit, v: number): string {
  if (unit === 'bps') {
    if (v >= 1e9) return `${(v / 1e9).toFixed(1)} Gbps`
    if (v >= 1e6) return `${(v / 1e6).toFixed(1)} Mbps`
    if (v >= 1e3) return `${Math.round(v / 1e3)} kbps`
    return `${Math.round(v)} bps`
  }
  if (v >= 1e3) return `${(v / 1e3).toFixed(1)} kfps`
  return `${Math.round(v)} fps`
}

/** Etiqueta temporal del punto según el rango (hora / día+hora / fecha). */
function fmtTime(iso: string, range: Range): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  if (range === '24h') return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  if (range === '7d')
    return d.toLocaleDateString([], { weekday: 'short' }) + ' ' + d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  return d.toLocaleDateString([], { day: 'numeric', month: 'short' })
}

interface ChartPoint {
  t: string
  rx: number
  tx: number
}

/** Reduce a ≤ maxPoints conservando picos (toma el punto de mayor actividad
 *  de cada grupo, en vez de promediar, para no aplanar los picos de tráfico). */
function downscale(pts: ChartPoint[], maxPoints: number): ChartPoint[] {
  if (pts.length <= maxPoints) return pts
  const size = Math.ceil(pts.length / maxPoints)
  const out: ChartPoint[] = []
  for (let i = 0; i < pts.length; i += size) {
    let best = pts[i]!
    for (let j = i + 1; j < Math.min(i + size, pts.length); j++) {
      if (pts[j]!.rx + pts[j]!.tx > best.rx + best.tx) best = pts[j]!
    }
    out.push(best)
  }
  return out
}

/** Tooltip del historial: hora + RX/TX formateadas con su color. */
function PortTooltip({
  active,
  payload,
  unit,
}: {
  active?: boolean
  payload?: { payload?: ChartPoint }[]
  unit: TrafficUnit
}) {
  if (!active || !payload?.length || !payload[0]?.payload) return null
  const p = payload[0].payload
  return (
    <div className="rounded-[10px] border border-border-strong bg-elevated px-3 py-2 shadow-lg">
      <div className="mb-1 font-mono text-caption text-text-muted">{p.t}</div>
      <div className="flex items-center gap-2 font-mono text-mono-sm text-text-primary">
        <span className="h-2 w-2 rounded-full bg-[#3b82f6]" />
        RX {fmtRate(unit, p.rx)}
      </div>
      <div className="flex items-center gap-2 font-mono text-mono-sm text-text-primary">
        <span className="h-2 w-2 rounded-full bg-[#10b981]" />
        TX {fmtRate(unit, p.tx)}
      </div>
    </div>
  )
}

/**
 * Historial de tráfico del router (issue #302, #654): tarjeta propia del
 * detalle a todo el ancho, con selector de puerto y de rango. NO va embebida
 * en la tarjeta de Puertos Ethernet: cada puerto tiene su propia serie.
 */
export function PortSeriesChart({
  routerId,
  ports,
  unit = 'bps',
  className,
}: {
  routerId: string
  ports: EthPort[]
  /** 'bps' para fuentes con byte counters (OpenWrt); 'fps' para beacons/SNMP sin bytes (issue #641). */
  unit?: TrafficUnit
  className?: string
}) {
  const { t } = useTranslation()
  const [range, setRange] = useState<Range>('24h')
  const [portId, setPortId] = useState<string | null>(null)
  const [data, setData] = useState<PortPoint[]>([])
  const [loading, setLoading] = useState(false)

  // Puerto por defecto: el primero con link; si no hay ninguno up, el primero
  // de la lista (permite ver el histórico de una boca caída).
  useEffect(() => {
    if (ports.length === 0) {
      setPortId(null)
      return
    }
    setPortId((prev) => {
      if (prev && ports.some((p) => p.id === prev)) return prev
      return (ports.find((p) => p.up) ?? ports[0])!.id
    })
  }, [ports])

  const fetchData = useCallback(async () => {
    if (!portId) return
    setLoading(true)
    try {
      const now = Math.floor(Date.now() / 1000)
      const from = now - rangeToSeconds(range)
      const res = await fetch(`/api/routers/${encodeURIComponent(routerId)}/ports/${encodeURIComponent(portId)}/series?from=${from}&to=${now}`)
      if (res.ok) {
        const json: PortSeriesResponse = await res.json()
        setData(json.points ?? [])
      } else {
        setData([])
      }
    } catch {
      setData([])
    } finally {
      setLoading(false)
    }
  }, [routerId, portId, range])

  useEffect(() => {
    fetchData()
  }, [fetchData])

  const chartData = useMemo(() => {
    const pts = data
      .map((p) => ({
        t: fmtTime(p.ts, range),
        rx: unit === 'fps' ? p.rxFps : p.rxBps,
        tx: unit === 'fps' ? p.txFps : p.txBps,
      }))
      .filter((p) => !Number.isNaN(p.rx) && !Number.isNaN(p.tx))
    return downscale(pts, 600)
  }, [data, range, unit])

  const hasData = chartData.length > 1
  const peakRx = hasData ? Math.max(...chartData.map((p) => p.rx)) : 0
  const peakTx = hasData ? Math.max(...chartData.map((p) => p.tx)) : 0
  const activePort = ports.find((p) => p.id === portId)

  if (ports.length === 0) return null

  return (
    <section className={cn('rounded-2xl border border-border bg-surface p-5 md:p-6', className)}>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <SectionHeader title={t('routerDetail.ports.seriesTitle')} />
        <div className="flex flex-wrap items-center gap-2">
          {/* Selector de puerto */}
          <div className="flex max-w-full items-center gap-1 overflow-x-auto rounded-lg border border-border bg-elevated p-0.5">
            {ports.map((p) => (
              <button
                key={p.id}
                type="button"
                onClick={() => setPortId(p.id)}
                title={p.connectedTo ? `${p.label} · ${p.connectedTo}` : p.label}
                className={cn(
                  'shrink-0 rounded-md px-2 py-0.5 font-mono text-[10px] font-medium transition-colors',
                  portId === p.id
                    ? 'bg-accent/15 text-accent'
                    : 'text-text-muted hover:bg-canvas hover:text-text-secondary',
                  !p.up && portId !== p.id && 'opacity-50',
                )}
              >
                {p.label.replace(/\s+/g, '')}
              </button>
            ))}
          </div>
          {/* Selector de rango */}
          <div className="flex gap-0.5 rounded-lg border border-border bg-elevated p-0.5">
            {RANGES.map((r) => (
              <button
                key={r}
                type="button"
                onClick={() => setRange(r)}
                className={cn(
                  'rounded px-1.5 py-0.5 text-[10px] font-medium transition-colors',
                  range === r
                    ? 'bg-accent/15 text-accent'
                    : 'text-text-muted hover:bg-canvas hover:text-text-secondary',
                )}
              >
                {r}
              </button>
            ))}
          </div>
        </div>
      </div>

      <p className="mt-1 truncate text-caption text-text-muted" title={activePort?.connectedTo ?? activePort?.label}>
        {activePort
          ? activePort.connectedTo && activePort.connectedTo !== activePort.label
            ? `${activePort.label} · ${activePort.connectedTo}`
            : activePort.label
          : ''}
        {activePort?.speed ? ` · ${activePort.speed}` : ''}
      </p>

      <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-1 text-caption text-text-secondary">
        <span className="inline-flex items-center gap-1.5">
          <span className="h-2 w-2 rounded-full bg-[#3b82f6]" /> RX {t('routerDetail.ports.seriesRx')}
        </span>
        <span className="inline-flex items-center gap-1.5">
          <span className="h-2 w-2 rounded-full bg-[#10b981]" /> TX {t('routerDetail.ports.seriesTx')}
        </span>
        {hasData && (
          <span className="font-mono text-[10px] text-text-muted">
            ↓ {fmtRate(unit, peakRx)} · ↑ {fmtRate(unit, peakTx)}
          </span>
        )}
      </div>

      {loading ? (
        <div className="mt-2 flex h-44 items-center justify-center rounded-xl border border-border/60 bg-elevated/30 text-caption text-text-muted">
          ...
        </div>
      ) : hasData ? (
        <div className="mt-2 h-44 w-full" role="img" aria-label={t('routerDetail.ports.seriesTitle')}>
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={chartData} margin={{ top: 4, right: 0, bottom: 0, left: 0 }}>
              <defs>
                <linearGradient id="port-rx-grad" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor="#3b82f6" stopOpacity={0.25} />
                  <stop offset="100%" stopColor="#3b82f6" stopOpacity={0} />
                </linearGradient>
                <linearGradient id="port-tx-grad" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor="#10b981" stopOpacity={0.22} />
                  <stop offset="100%" stopColor="#10b981" stopOpacity={0} />
                </linearGradient>
              </defs>
              <CartesianGrid vertical={false} stroke="rgb(var(--border) / 0.5)" strokeDasharray="3 6" />
              <XAxis dataKey="t" hide />
              <Tooltip content={<PortTooltip unit={unit} />} cursor={{ stroke: 'rgb(var(--border-strong))', strokeWidth: 1 }} />
              <Area
                type="monotone"
                dataKey="rx"
                name="RX"
                stroke="#3b82f6"
                strokeWidth={1.5}
                fill="url(#port-rx-grad)"
                dot={false}
                activeDot={{ r: 3, strokeWidth: 0, fill: '#3b82f6' }}
                isAnimationActive={false}
              />
              <Area
                type="monotone"
                dataKey="tx"
                name="TX"
                stroke="#10b981"
                strokeWidth={1.5}
                fill="url(#port-tx-grad)"
                dot={false}
                activeDot={{ r: 3, strokeWidth: 0, fill: '#10b981' }}
                isAnimationActive={false}
              />
            </AreaChart>
          </ResponsiveContainer>
        </div>
      ) : (
        <div className="mt-2 flex h-44 items-center justify-center rounded-xl border border-border/60 bg-elevated/30 text-caption text-text-muted">
          {t('routerDetail.ports.seriesNoData')}
        </div>
      )}
    </section>
  )
}
