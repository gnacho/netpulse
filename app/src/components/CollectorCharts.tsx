import { useCallback, useEffect, useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import { Activity, Clock } from 'lucide-react'
import { SegmentedControl } from '@/components/SegmentedControl'

type Metric = { id: number; key: string; unit: string; kind: string }
type DataPoint = [number, number]

type Range = '1h' | '24h' | '7d'

const RANGES: { value: Range; label: string; seconds: number }[] = [
  { value: '1h', label: '1h', seconds: 3600 },
  { value: '24h', label: '24h', seconds: 86400 },
  { value: '7d', label: '7d', seconds: 604800 },
]

function targetName(key: string): string {
  return key.replace(/^tcp_latency_ms\./, '').replace(/^tcp_ok\./, '')
}

function isLatency(m: Metric): boolean {
  return m.key.startsWith('tcp_latency_ms.')
}

function fmtTime(ts: number): string {
  return new Date(ts * 1000).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
}

function ChartTooltip({ active, payload }: { active?: boolean; payload?: { value?: number; payload?: { ts: number } }[] }) {
  const { t } = useTranslation()
  if (!active || !payload?.length) return null
  const p = payload[0]!
  return (
    <div className="rounded-lg border border-border bg-elevated px-3 py-2 text-xs shadow-lg">
      <p className="font-mono font-semibold text-text-primary">
        {p.value?.toFixed(1)} {t('collector.ms')}
      </p>
      <p className="text-text-muted">{fmtTime(p.payload?.ts ?? 0)}</p>
    </div>
  )
}

function LatencyChart({ metric, range }: { metric: Metric; range: Range }) {
  const { t } = useTranslation()
  const [data, setData] = useState<{ ts: number; value: number }[]>([])
  const [loading, setLoading] = useState(true)

  const fetchSeries = useCallback(async () => {
    setLoading(true)
    const now = Math.floor(Date.now() / 1000)
    const rangeSec = RANGES.find((r) => r.value === range)?.seconds ?? 3600
    const from = now - rangeSec
    try {
      const r = await fetch(`/api/collector/series?metric=${encodeURIComponent(metric.key)}&from=${from}&to=${now}&points=300`)
      if (r.ok) {
        const d = (await r.json()) as { points: DataPoint[] }
        setData((d.points ?? []).map((p) => ({ ts: p[0], value: p[1] })))
      }
    } catch {
      /* ignore */
    } finally {
      setLoading(false)
    }
  }, [metric.key, range])

  useEffect(() => {
    void fetchSeries()
  }, [fetchSeries])

  const name = targetName(metric.key)
  const avg = useMemo(() => {
    if (!data.length) return 0
    return data.reduce((s, p) => s + p.value, 0) / data.length
  }, [data])

  if (loading) {
    return (
      <div className="flex h-[140px] items-center justify-center text-xs text-text-muted">
        {t('common.loading')}
      </div>
    )
  }

  if (!data.length) {
    return (
      <div className="flex h-[140px] items-center justify-center text-xs text-text-muted">
        {t('collector.noData')}
      </div>
    )
  }

  return (
    <div>
      <div className="mb-2 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Activity className="h-3.5 w-3.5 text-accent" strokeWidth={1.75} />
          <span className="text-sm font-medium text-text-primary">{name}</span>
        </div>
        <span className="font-mono text-xs text-text-muted">
          avg {avg.toFixed(1)} {t('collector.ms')}
        </span>
      </div>
      <div className="h-[120px]">
        <ResponsiveContainer width="100%" height="100%">
          <AreaChart data={data} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
            <defs>
              <linearGradient id={`coll-${name}`} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="var(--accent)" stopOpacity={0.3} />
                <stop offset="100%" stopColor="var(--accent)" stopOpacity={0} />
              </linearGradient>
            </defs>
            <CartesianGrid stroke="var(--border)" strokeDasharray="3 3" vertical={false} />
            <XAxis
              dataKey="ts"
              tickFormatter={fmtTime}
              tick={{ fontSize: 10, fill: 'var(--text-muted)' }}
              axisLine={false}
              tickLine={false}
              minTickGap={40}
            />
            <YAxis hide domain={[0, 'auto']} />
            <Tooltip content={<ChartTooltip />} />
            <Area
              type="monotone"
              dataKey="value"
              stroke="var(--accent)"
              strokeWidth={1.5}
              fill={`url(#coll-${name})`}
              isAnimationActive={false}
            />
          </AreaChart>
        </ResponsiveContainer>
      </div>
    </div>
  )
}

export function CollectorCharts() {
  const { t } = useTranslation()
  const [metrics, setMetrics] = useState<Metric[]>([])
  const [available, setAvailable] = useState(false)
  const [range, setRange] = useState<Range>('1h')
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let alive = true
    void fetch('/api/collector/metrics')
      .then((r) => (r.ok ? r.json() : null))
      .then((d) => {
        if (!alive) return
        const m = (d?.metrics ?? []) as Metric[]
        setMetrics(m)
        setAvailable(!!d?.available)
      })
      .catch(() => undefined)
      .finally(() => {
        if (alive) setLoading(false)
      })
    return () => {
      alive = false
    }
  }, [])

  if (loading) return null
  if (!available) return null

  const latencyMetrics = metrics.filter(isLatency)

  if (!latencyMetrics.length) return null


  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <Clock className="h-4 w-4 text-text-muted" strokeWidth={1.75} />
          <h3 className="text-sm font-semibold text-text-primary">{t('collector.title')}</h3>
          <span className="rounded-full bg-ok/10 px-2 py-0.5 text-[10px] font-semibold text-ok">
            {t('collector.sidecar')}
          </span>
        </div>
        <SegmentedControl
          options={RANGES.map((r) => ({ value: r.value, label: r.label }))}
          value={range}
          onChange={(v) => setRange(v as Range)}
          ariaLabel={t('collector.range')}
        />
      </div>

      <div className="grid gap-4 sm:grid-cols-2">
        {latencyMetrics.map((m) => (
          <LatencyChart key={m.key} metric={m} range={range} />
        ))}
      </div>
    </div>
  )
}
