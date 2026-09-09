import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'

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

const RANGES: Range[] = ['24h', '7d', '30d']

function rangeToSeconds(range: Range): number {
  switch (range) {
    case '24h': return 24 * 3600
    case '7d': return 7 * 24 * 3600
    case '30d': return 30 * 24 * 3600
  }
}

/** Formatea una tasa: bps → bps/kbps/Mbps/Gbps; fps → fps/kfps. */
function fmtRate(unit: 'bps' | 'fps', v: number): string {
  if (unit === 'bps') {
    if (v >= 1e9) return `${(v / 1e9).toFixed(1)} Gbps`
    if (v >= 1e6) return `${(v / 1e6).toFixed(1)} Mbps`
    if (v >= 1e3) return `${Math.round(v / 1e3)} kbps`
    return `${Math.round(v)} bps`
  }
  if (v >= 1e3) return `${(v / 1e3).toFixed(1)} kfps`
  return `${Math.round(v)} fps`
}

function Sparkline({ points, colorKey, unit }: { points: PortPoint[]; colorKey: 'rx' | 'tx'; unit: 'bps' | 'fps' }) {
  if (points.length < 2) return null
  const W = 200
  const H = 40
  const PAD = 2
  const field = (colorKey === 'rx' ? 'rxBps' : 'txBps') as 'rxBps' | 'txBps'
  const fpsField = (colorKey === 'rx' ? 'rxFps' : 'txFps') as 'rxFps' | 'txFps'
  const values = points.map((p) => (unit === 'fps' ? p[fpsField] : p[field]))
  const max = Math.max(...values, 1)
  const stepX = (W - PAD * 2) / (values.length - 1)
  const path = values
    .map((v, i) => {
      const x = PAD + i * stepX
      const y = H - PAD - ((v / max) * (H - PAD * 2))
      return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')
  const fillPath = path + ` L${(W - PAD).toFixed(1)},${H - PAD} L${PAD},${H - PAD} Z`
  const color = colorKey === 'rx' ? '#3b82f6' : '#10b981'
  const fillColor = colorKey === 'rx' ? 'rgba(59,130,246,0.12)' : 'rgba(16,185,129,0.12)'
  return (
    <svg width={W} height={H} viewBox={`0 0 ${W} ${H}`} className="block" aria-hidden="true">
      <path d={fillPath} fill={fillColor} />
      <path d={path} fill="none" stroke={color} strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

export function PortSeriesChart({
  routerId,
  portId,
  unit = 'bps',
}: {
  routerId: string
  portId: string
  /** 'bps' para fuentes con byte counters (OpenWrt); 'fps' para beacons/SNMP sin bytes (issue #641). */
  unit?: 'bps' | 'fps'
}) {
  const { t } = useTranslation()
  const [range, setRange] = useState<Range>('24h')
  const [data, setData] = useState<PortPoint[]>([])
  const [loading, setLoading] = useState(false)

  const fetchData = useCallback(async () => {
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

  const hasData = data.length > 1
  const peakRx = hasData ? Math.max(...data.map((p) => (unit === 'fps' ? p.rxFps : p.rxBps))) : 0
  const peakTx = hasData ? Math.max(...data.map((p) => (unit === 'fps' ? p.txFps : p.txBps))) : 0

  return (
    <div className="mt-3 rounded-xl border border-border/60 bg-elevated/30 p-3">
      <div className="mb-2 flex items-center justify-between">
        <span className="text-caption font-medium text-text-secondary">{t('routerDetail.ports.seriesTitle')}</span>
        <div className="flex gap-1">
          {RANGES.map((r) => (
            <button
              key={r}
              type="button"
              onClick={() => setRange(r)}
              className={`rounded px-1.5 py-0.5 text-[10px] font-medium transition-colors ${
                range === r
                  ? 'bg-accent/15 text-accent'
                  : 'text-text-muted hover:bg-canvas hover:text-text-secondary'
              }`}
            >
              {r}
            </button>
          ))}
        </div>
      </div>
      {loading ? (
        <div className="flex h-10 items-center justify-center text-caption text-text-muted">...</div>
      ) : hasData ? (
        <div className="space-y-1">
          <div className="flex items-center gap-2">
            <span className="w-5 text-[10px] font-medium text-blue-500">RX</span>
            <Sparkline points={data} colorKey="rx" unit={unit} />
            <span className="ml-auto font-mono text-[10px] text-text-muted">{fmtRate(unit, peakRx)}</span>
          </div>
          <div className="flex items-center gap-2">
            <span className="w-5 text-[10px] font-medium text-emerald-500">TX</span>
            <Sparkline points={data} colorKey="tx" unit={unit} />
            <span className="ml-auto font-mono text-[10px] text-text-muted">{fmtRate(unit, peakTx)}</span>
          </div>
        </div>
      ) : (
        <div className="flex h-10 items-center justify-center text-caption text-text-muted">
          {t('routerDetail.ports.seriesNoData')}
        </div>
      )}
    </div>
  )
}
