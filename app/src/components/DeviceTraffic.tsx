import { useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Sparkline } from '@/components/Sparkline'
import { fmtEs } from '@/data/mock'

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

/** Formatea bytes a unidad legible estilo "2,1 GB" / "340 MB" (locale activo). */
function fmtBytes(b: number): string {
  const gb = b / 1e9
  if (gb >= 1) return `${fmtEs(gb, 1)} GB`
  const mb = b / 1e6
  if (mb >= 1) return `${fmtEs(mb, mb >= 10 ? 0 : 1)} MB`
  const kb = b / 1e3
  return `${fmtEs(kb, kb >= 10 ? 0 : 1)} KB`
}

const RANGES = [
  { key: '24h', secs: 24 * 3600 },
  { key: '7d', secs: 7 * 24 * 3600 },
  { key: '30d', secs: 30 * 24 * 3600 },
] as const
type RangeKey = (typeof RANGES)[number]['key']

function MiniBwChart({ points }: { points: TrafficPoint[] }) {
  if (points.length < 2) return null
  const W = 220
  const H = 34
  const PAD = 3
  const max = Math.max(...points.map((p) => p.rxBps + p.txBps), 1)
  const stepX = (W - PAD * 2) / (points.length - 1)
  const line = (key: 'rxBps' | 'txBps') =>
    points
      .map((p, i) => {
        const x = PAD + i * stepX
        const y = H - PAD - ((p[key] / max) * (H - PAD * 2))
        return `${i === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`
      })
      .join(' ')
  return (
    <svg width={W} height={H} viewBox={`0 0 ${W} ${H}`} className="block" aria-hidden="true">
      <path d={line('rxBps')} fill="none" stroke="#3b82f6" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
      <path d={line('txBps')} fill="none" stroke="#10b981" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  )
}

/**
 * Tráfico real por cliente (#551). Fetch bajo demanda del detalle en live
 * (patrón PortSeriesChart); en demo no se monta (los campos del canon ya
 * viajan en el snapshot y el endpoint no existe).
 */
export function DeviceTrafficDetail({ mac, online }: { mac: string; online: boolean }) {
  const { t } = useTranslation()
  const [range, setRange] = useState<RangeKey>('24h')
  const [points, setPoints] = useState<TrafficPoint[] | null>(null)
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    let cancelled = false
    if (!online || mac === '—') {
      setPoints([])
      return
    }
    const secs = RANGES.find((r) => r.key === range)!.secs
    setLoading(true)
    const now = Math.floor(Date.now() / 1000)
    const from = now - secs
    fetch(`/api/devices/${encodeURIComponent(mac)}/traffic?from=${from}&to=${now}`)
      .then((res) => (res.ok ? res.json() : Promise.reject(res.status)))
      .then((json: TrafficResponse) => {
        if (!cancelled) setPoints(json.points ?? [])
      })
      .catch(() => {
        if (!cancelled) setPoints([])
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [mac, online, range])

  const summary = useMemo(() => {
    if (!points || points.length === 0) return null
    const rx = points.reduce((a, p) => a + (p.rxBytes || 0), 0)
    const tx = points.reduce((a, p) => a + (p.txBytes || 0), 0)
    const last = points[points.length - 1]
    const mbps = last ? ((last.rxBps || 0) + (last.txBps || 0)) / 1e6 : 0
    return { rx, tx, mbps }
  }, [points])

  if (!online) return null

  return (
    <div className="rounded-xl border border-border bg-elevated/40 px-3 py-2.5">
      <div className="flex items-center justify-between gap-2">
        <span className="text-caption font-medium text-text-secondary">
          {t('devices.detail.traffic')}
        </span>
        <div className="flex gap-1">
          {RANGES.map((r) => (
            <button
              key={r.key}
              type="button"
              onClick={() => setRange(r.key)}
              className={`rounded px-1.5 py-0.5 text-[10px] font-medium transition-colors ${
                range === r.key ? 'bg-accent/15 text-accent' : 'text-text-muted hover:bg-canvas hover:text-text-secondary'
              }`}
            >
              {r.key}
            </button>
          ))}
        </div>
      </div>
      {loading ? (
        <div className="flex h-8 items-center justify-center text-caption text-text-muted">...</div>
      ) : summary && points && points.length > 1 ? (
        <div className="mt-1.5 space-y-1">
          <div className="flex items-center justify-between gap-2">
            <span className="font-mono text-mono-sm text-text-secondary">
              ↓ {fmtBytes(summary.rx)} · ↑ {fmtBytes(summary.tx)}
            </span>
            <span className="font-mono text-mono-sm text-accent">
              {summary.mbps >= 1 ? fmtEs(summary.mbps, 1) : fmtEs(summary.mbps, 2)} Mbps
            </span>
          </div>
          <MiniBwChart points={points} />
        </div>
      ) : (
        <div className="flex h-8 items-center justify-center text-caption text-text-muted">
          {t('routerDetail.ports.seriesNoData')}
        </div>
      )}
    </div>
  )
}

/** Sparkline del tráfico actual (24 h) para filas compactas en live. */
export function DeviceTrafficSparkline({ mac, online, width = 60, height = 20 }: { mac: string; online: boolean; width?: number; height?: number }) {
  const [points, setPoints] = useState<TrafficPoint[]>([])
  useEffect(() => {
    let cancelled = false
    if (!online || mac === '—') {
      setPoints([])
      return
    }
    const now = Math.floor(Date.now() / 1000)
    const from = now - 24 * 3600
    fetch(`/api/devices/${encodeURIComponent(mac)}/traffic?from=${from}&to=${now}`)
      .then((res) => (res.ok ? res.json() : Promise.reject(res.status)))
      .then((json: TrafficResponse) => {
        if (!cancelled) setPoints(json.points ?? [])
      })
      .catch(() => {
        if (!cancelled) setPoints([])
      })
    return () => {
      cancelled = true
    }
  }, [mac, online])
  const data = useMemo(
    () => points.filter((_, i, a) => a.length <= 48 || i % Math.ceil(a.length / 48) === 0).map((p) => (p.rxBps + p.txBps) / 1e6),
    [points],
  )
  if (data.length < 2) return null
  return <Sparkline data={data} width={width} height={height} className="text-accent" />
}
