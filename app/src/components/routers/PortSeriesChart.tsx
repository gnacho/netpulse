import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis } from 'recharts'
import { SectionHeader } from '@/components/SectionHeader'
import type { EthPort } from '@/components/routers/routerExtras'
import { portPeerLabel } from '@/components/routers/routerExtras'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
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

type Range = '24h' | '7d' | '30d' | '12m'
export type TrafficUnit = 'bps' | 'fps' | 'bytes'

const RANGES: Range[] = ['24h', '7d', '30d', '12m']

/** Días válidos de inicio de ciclo (1-28: existe en todos los meses). */
const CYCLE_DAYS = Array.from({ length: 28 }, (_, i) => i + 1)
const CYCLE_DAY_KEY = 'netpulse.cycleDay'

// #1035: con más de 4 bocas la fila de botones desborda la tarjeta; con 8 o
// más además ofrecemos el filtro All/Active/Inactive para acotar el dropdown.
const PORT_BUTTON_MAX = 4
const PORT_FILTER_MIN = 8
type PortFilter = 'all' | 'active' | 'inactive'
const PORT_FILTERS: PortFilter[] = ['all', 'active', 'inactive']
const PORT_FILTER_LABEL: Record<PortFilter, string> = {
  all: 'routerDetail.ports.filterAll',
  active: 'routerDetail.ports.filterActive',
  inactive: 'routerDetail.ports.filterInactive',
}

function rangeToSeconds(range: Range): number {
  switch (range) {
    case '24h': return 24 * 3600
    case '7d': return 7 * 24 * 3600
    case '30d': return 30 * 24 * 3600
    case '12m': return 400 * 24 * 3600 // #1762: retención diaria de 365 días, pedimos margen
  }
}

/** Formatea una tasa: bps → bps/kbps/Mbps/Gbps; fps → fps/kfps. */
export function fmtRate(unit: TrafficUnit, v: number): string {
  if (unit === 'bytes') {
    // #1762: totales por ciclo, no tasas.
    if (v >= 1e12) return `${(v / 1e12).toFixed(2)} TB`
    if (v >= 1e9) return `${(v / 1e9).toFixed(1)} GB`
    if (v >= 1e6) return `${(v / 1e6).toFixed(1)} MB`
    if (v >= 1e3) return `${Math.round(v / 1e3)} kB`
    return `${Math.round(v)} B`
  }
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

/** #1762: inicio del ciclo que contiene la fecha d (UTC: las filas diarias del
 *  servidor son fechas UTC). Con cycleDay=1 son meses naturales. */
function cycleStartOf(d: Date, cycleDay: number): Date {
  const y = d.getUTCFullYear()
  const m = d.getUTCMonth()
  return d.getUTCDate() >= cycleDay
    ? new Date(Date.UTC(y, m, cycleDay))
    : new Date(Date.UTC(y, m - 1, cycleDay))
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
 * Historial de tráfico del router (issues #302, #654, #1762): tarjeta propia del
 * detalle a todo el ancho, con selector de puerto y de rango. El rango 12m agrega
 * las filas diarias del servidor en ciclos que empiezan el día elegido (ciclo de
 * facturación, caso 5G de #1757): totales en bytes, o media de fps por ciclo para
 * fuentes sin contadores de bytes. NO va embebida
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
  const [portFilter, setPortFilter] = useState<PortFilter>('all')
  const [data, setData] = useState<PortPoint[]>([])
  const [loading, setLoading] = useState(false)
  // #1762: día de inicio del ciclo de facturación, recordado por navegador.
  const [cycleDay, setCycleDayState] = useState<number>(() => {
    const v = Number(localStorage.getItem(CYCLE_DAY_KEY))
    return v >= 1 && v <= 28 ? v : 1
  })
  const setCycleDay = (d: number) => {
    setCycleDayState(d)
    localStorage.setItem(CYCLE_DAY_KEY, String(d))
  }

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

  // Opciones visibles según el filtro All/Active/Inactive (#1035).
  const visiblePorts = useMemo(() => {
    if (portFilter === 'active') return ports.filter((p) => p.up)
    if (portFilter === 'inactive') return ports.filter((p) => !p.up)
    return ports
  }, [ports, portFilter])

  // Si el filtro excluye el puerto activo, recoloca la selección dentro del
  // subconjunto visible (si el subconjunto queda vacío, sobre toda la flota).
  useEffect(() => {
    if (ports.length === 0) return
    setPortId((prev) => {
      if (prev && visiblePorts.some((p) => p.id === prev)) return prev
      return (visiblePorts[0] ?? ports[0])!.id
    })
  }, [visiblePorts, ports])

  const fetchData = useCallback(async () => {
    if (!portId) return
    setLoading(true)
    try {
      const now = Math.floor(Date.now() / 1000)
      const from = now - rangeToSeconds(range)
      // #1762: explícito aunque el default ya saldría daily, por si cambia Resolution().
      const extra = range === '12m' ? '&resolution=daily' : ''
      const res = await fetch(`/api/routers/${encodeURIComponent(routerId)}/ports/${encodeURIComponent(portId)}/series?from=${from}&to=${now}${extra}`)
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

  // #1502: la unidad la deciden los datos, no solo el tipo de la fuente: si
  // la serie trae tasas de bytes se pinta bps (unidades con agente), si solo
  // trae tramas, fps (beacons, #641); el prop queda como fallback para
  // series vacías. Así un caller que clasifique mal no puede dejar el
  // gráfico plano a cero teniendo contadores de bytes.
  const effUnit: TrafficUnit = useMemo(() => {
    if (range === '12m') {
      // #1762: totales por ciclo en bytes; sin contadores de bytes, media de
      // fps por ciclo (el fps diario del servidor es la media del día).
      if (data.some((p) => p.rxBytes > 0 || p.txBytes > 0)) return 'bytes'
      if (data.some((p) => p.rxFps > 0 || p.txFps > 0)) return 'fps'
      return unit
    }
    return data.some((p) => p.rxBps > 0 || p.txBps > 0)
      ? 'bps'
      : data.some((p) => p.rxFps > 0 || p.txFps > 0)
        ? 'fps'
        : unit
  }, [data, unit, range])

  const chartData = useMemo(() => {
    if (range === '12m') {
      const useBytes = effUnit === 'bytes'
      const cycles = new Map<number, { rx: number; tx: number; n: number }>()
      let prev: PortPoint | null = null
      for (const p of data) {
        const d = new Date(p.ts)
        if (Number.isNaN(d.getTime())) continue
        const key = cycleStartOf(d, cycleDay).getTime()
        const c = cycles.get(key) ?? { rx: 0, tx: 0, n: 0 }
        if (useBytes) {
          // Delta diaria de contadores: si el contador bajó (reset del equipo),
          // ese día no suma, igual que hace deriveBps en el servidor.
          if (prev) {
            c.rx += Math.max(0, p.rxBytes - prev.rxBytes)
            c.tx += Math.max(0, p.txBytes - prev.txBytes)
          }
        } else {
          c.rx += p.rxFps
          c.tx += p.txFps
          c.n += 1
        }
        cycles.set(key, c)
        prev = p
      }
      return [...cycles.entries()]
        .sort(([a], [b]) => a - b)
        .map(([k, c]) => ({
          t: new Date(k).toLocaleDateString([], { day: 'numeric', month: 'short' }),
          rx: useBytes || c.n === 0 ? c.rx : c.rx / c.n,
          tx: useBytes || c.n === 0 ? c.tx : c.tx / c.n,
        }))
    }
    const pts = data
      .map((p) => ({
        t: fmtTime(p.ts, range),
        rx: effUnit === 'fps' ? p.rxFps : p.rxBps,
        tx: effUnit === 'fps' ? p.txFps : p.txBps,
      }))
      .filter((p) => !Number.isNaN(p.rx) && !Number.isNaN(p.tx))
    return downscale(pts, 600)
  }, [data, range, effUnit, cycleDay])

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
          {/* Selector de puerto: fila de botones hasta 4 bocas (#1035) */}
          {ports.length <= PORT_BUTTON_MAX ? (
            <div className="flex max-w-full items-center gap-1 overflow-x-auto rounded-lg border border-border bg-elevated p-0.5">
              {ports.map((p) => (
                <button
                  key={p.id}
                  type="button"
                  onClick={() => setPortId(p.id)}
                  title={(() => { const peer = portPeerLabel(p); return peer ? `${p.label} · ${peer}` : p.label })()}
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
          ) : (
            <div className="flex items-center gap-2">
              {ports.length >= PORT_FILTER_MIN && (
                <div className="flex gap-0.5 rounded-lg border border-border bg-elevated p-0.5">
                  {PORT_FILTERS.map((f) => (
                    <button
                      key={f}
                      type="button"
                      onClick={() => setPortFilter(f)}
                      className={cn(
                        'rounded px-1.5 py-0.5 text-[10px] font-medium transition-colors',
                        portFilter === f
                          ? 'bg-accent/15 text-accent'
                          : 'text-text-muted hover:bg-canvas hover:text-text-secondary',
                      )}
                    >
                      {t(PORT_FILTER_LABEL[f])}
                    </button>
                  ))}
                </div>
              )}
              <Select value={portId ?? ''} onValueChange={setPortId}>
                <SelectTrigger
                  aria-label={t('routerDetail.ports.seriesPort')}
                  className="h-7 w-auto max-w-56 gap-1.5 rounded-lg border-border bg-elevated px-2 font-mono text-[10px] font-medium text-text-secondary"
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {visiblePorts.map((p) => (
                    <SelectItem key={p.id} value={p.id}>
                      {p.label}
                      {(() => { const peer = portPeerLabel(p); return peer && peer !== p.label ? ` · ${peer}` : '' })()}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}
          {/* Selector del día de inicio del ciclo, solo en 12m (#1762) */}
          {range === '12m' && (
            <Select value={String(cycleDay)} onValueChange={(v) => setCycleDay(Number(v))}>
              <SelectTrigger
                aria-label={t('routerDetail.ports.cycleStart')}
                className="h-7 w-auto gap-1.5 rounded-lg border-border bg-elevated px-2 font-mono text-[10px] font-medium text-text-secondary"
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent className="max-h-56">
                {CYCLE_DAYS.map((d) => (
                  <SelectItem key={d} value={String(d)}>
                    {d}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
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

      {(() => {
        const activePeer = activePort ? portPeerLabel(activePort) : ''
        return (
          <p className="mt-1 truncate text-caption text-text-muted" title={activePeer || activePort?.label}>
            {activePort
              ? activePeer && activePeer !== activePort.label
                ? `${activePort.label} · ${activePeer}`
                : activePort.label
              : ''}
            {activePort?.speed ? ` · ${activePort.speed}` : ''}
          </p>
        )
      })()}

      <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-1 text-caption text-text-secondary">
        <span className="inline-flex items-center gap-1.5">
          <span className="h-2 w-2 rounded-full bg-[#3b82f6]" /> RX {t('routerDetail.ports.seriesRx')}
        </span>
        <span className="inline-flex items-center gap-1.5">
          <span className="h-2 w-2 rounded-full bg-[#10b981]" /> TX {t('routerDetail.ports.seriesTx')}
        </span>
        {hasData && (
          <span className="font-mono text-[10px] text-text-muted">
            ↓ {fmtRate(effUnit, peakRx)} · ↑ {fmtRate(effUnit, peakTx)}
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
              <Tooltip content={<PortTooltip unit={effUnit} />} cursor={{ stroke: 'rgb(var(--border-strong))', strokeWidth: 1 }} />
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
