import { useEffect, useMemo, useState } from 'react'
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis } from 'recharts'
import { motion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import type { TimeRange, TrafficPoint } from '@/data/mock'
import { fmtEs } from '@/data/mock'
import { useNetPulse } from '@/data/DataProvider'
import { SectionHeader } from '@/components/SectionHeader'
import { SegmentedControl, TIME_RANGE_OPTIONS } from '@/components/SegmentedControl'
import { StatusPill } from '@/components/StatusPill'

// ---------------------------------------------------------------------------
// Tooltip custom (design.md §7): superficie elevated, valores mono con unidad
// ---------------------------------------------------------------------------

function TrafficTooltip({ active, payload, label }: { active?: boolean; payload?: { dataKey?: string | number; value?: number | string }[]; label?: string }) {
  if (!active || !payload?.length) return null
  const down = payload.find((p) => p.dataKey === 'down')?.value
  const up = payload.find((p) => p.dataKey === 'up')?.value
  return (
    <div className="rounded-[10px] border border-border-strong bg-elevated px-3 py-2 shadow-lg">
      <div className="mb-1 font-mono text-caption text-text-muted">{label}</div>
      <div className="flex items-center gap-2 font-mono text-mono-sm text-text-primary">
        <span className="h-2 w-2 rounded-full bg-accent" />↓ {fmtEs(Number(down), 1)} Mbps
      </div>
      <div className="flex items-center gap-2 font-mono text-mono-sm text-text-primary">
        <span className="h-2 w-2 rounded-full bg-tunnel" />↑ {fmtEs(Number(up), 1)} Mbps
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Punto "ahora" con halo pulsante en el extremo de la serie
// ---------------------------------------------------------------------------

function makeLiveDot(dataLength: number, color: string) {
  return function LiveDot(props: { cx?: number; cy?: number; index?: number }) {
    const { cx, cy, index } = props
    if (index !== dataLength - 1 || cx === undefined || cy === undefined) return <g />
    return (
      <g>
        <circle cx={cx} cy={cy} r={4} fill={color}>
          <animate attributeName="r" values="4;7;4" dur="1.6s" repeatCount="indefinite" />
          <animate attributeName="opacity" values="0.5;0.12;0.5" dur="1.6s" repeatCount="indefinite" />
        </circle>
        <circle cx={cx} cy={cy} r={3} fill={color} stroke="#070B12" strokeWidth={1.5} />
      </g>
    )
  }
}

/** ② Tráfico WAN — área doble serie (home.md §②) */
export function WanTraffic() {
  const { t } = useTranslation()
  const { traffic: trafficByRange, wan } = useNetPulse()
  const [range, setRange] = useState<TimeRange>('24h')
  const [liveData, setLiveData] = useState<TrafficPoint[]>(trafficByRange['24h'])

  const FOOTER_METRICS = [
    { label: t('home.traffic.peakToday'), value: `${wan.peakTodayMbps} Mbps ↓` },
    { label: t('home.traffic.average'), value: `${wan.avgDownMbps} Mbps ↓` },
    { label: t('home.traffic.total24h'), value: wan.total24h },
    { label: t('home.traffic.loss'), value: `${wan.lossPct} %` },
  ] as const

  // Carga de datos por rango (crossfade vía key en el chart)
  useEffect(() => {
    setLiveData(trafficByRange[range])
  }, [range, trafficByRange])

  // Polling simulado 3s: en 1h la ventana hace scroll suave; en el resto "respira" el último punto
  useEffect(() => {
    const id = window.setInterval(() => {
      setLiveData((prev) => {
        if (prev.length === 0) return prev
        const last = prev[prev.length - 1]
        const jitter = () => 0.9 + Math.random() * 0.2
        const next = { ...last, down: Math.max(1, last.down * jitter()), up: Math.max(0.5, last.up * jitter()) }
        if (range === '1h') {
          const [hh, mm] = last.t.split(':').map(Number)
          const d = new Date(2024, 0, 1, hh, mm + 3)
          const t = `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
          return [...prev.slice(1), { ...next, t }]
        }
        return [...prev.slice(0, -1), next]
      })
    }, 3000)
    return () => window.clearInterval(id)
  }, [range])

  const liveDotDown = useMemo(() => makeLiveDot(liveData.length, '#22D3EE'), [liveData.length])
  const liveDotUp = useMemo(() => makeLiveDot(liveData.length, '#A78BFA'), [liveData.length])

  return (
    <section className="flex h-full flex-col rounded-2xl border border-border bg-surface p-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <SectionHeader title={t('home.traffic.title')}>
          <StatusPill tone="ok" label={t('topbar.live')} pulse />
        </SectionHeader>
        <div className="shrink-0">
          <SegmentedControl
            options={TIME_RANGE_OPTIONS}
            value={range}
            onChange={(v) => setRange(v as TimeRange)}
            ariaLabel={t('home.traffic.rangeAria')}
          />
        </div>
      </div>

      <motion.div
        key={range}
        initial={{ opacity: 0 }}
        animate={{ opacity: 1 }}
        transition={{ duration: 0.25 }}
        className="mt-4 h-[180px] md:h-[260px]"
        role="img"
        aria-label={t('home.traffic.chartAria', { range, down: wan.downMbps, up: wan.upMbps })}
      >
        <ResponsiveContainer width="100%" height="100%">
          <AreaChart data={liveData} margin={{ top: 8, right: 4, bottom: 0, left: 4 }}>
            <defs>
              <linearGradient id="grad-down" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="#22D3EE" stopOpacity={0.25} />
                <stop offset="100%" stopColor="#22D3EE" stopOpacity={0} />
              </linearGradient>
              <linearGradient id="grad-up" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="#A78BFA" stopOpacity={0.2} />
                <stop offset="100%" stopColor="#A78BFA" stopOpacity={0} />
              </linearGradient>
            </defs>
            <CartesianGrid vertical={false} stroke="rgb(var(--border) / 0.5)" strokeDasharray="3 6" />
            <XAxis
              dataKey="t"
              tickLine={false}
              axisLine={false}
              tick={{ fill: 'rgb(var(--text-muted))', fontSize: 10, fontFamily: '"JetBrains Mono", monospace' }}
              interval="preserveStartEnd"
              minTickGap={40}
            />
            <Tooltip content={<TrafficTooltip />} cursor={{ stroke: 'rgb(var(--border-strong))', strokeWidth: 1 }} />
            <Area
              type="monotone"
              dataKey="down"
              stroke="#22D3EE"
              strokeWidth={2}
              fill="url(#grad-down)"
              dot={liveDotDown}
              activeDot={{ r: 4, strokeWidth: 0, fill: '#22D3EE' }}
              animationDuration={900}
              animationEasing="ease-out"
            />
            <Area
              type="monotone"
              dataKey="up"
              stroke="#A78BFA"
              strokeWidth={2}
              fill="url(#grad-up)"
              dot={liveDotUp}
              activeDot={{ r: 4, strokeWidth: 0, fill: '#A78BFA' }}
              animationDuration={900}
              animationEasing="ease-out"
            />
          </AreaChart>
        </ResponsiveContainer>
      </motion.div>

      {/* Descripción + tabla accesible (visually-hidden). El texto va en un
          span sr-only porque <caption> escapa del clip del table sr-only
          (display: table-caption) y quedaría visible encima del header (#63). */}
      <span className="sr-only">{t('home.traffic.tableCaption', { range })}</span>
      <table className="sr-only">
        <thead>
          <tr><th>{t('home.traffic.time')}</th><th>{t('home.traffic.downloadMbps')}</th><th>{t('home.traffic.uploadMbps')}</th></tr>
        </thead>
        <tbody>
          {liveData.map((p) => (
            <tr key={p.t}><td>{p.t}</td><td>{p.down}</td><td>{p.up}</td></tr>
          ))}
        </tbody>
      </table>

      <div className="mt-4 grid grid-cols-2 gap-3 border-t border-border pt-4 sm:grid-cols-4">
        {FOOTER_METRICS.map((m, i) => (
          <motion.div
            key={m.label}
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.3, ease: 'easeOut', delay: 0.2 + i * 0.08 }}
          >
            <div className="text-label uppercase text-text-muted">{m.label}</div>
            <div className="mt-0.5 font-mono text-mono-sm font-semibold text-text-primary">{m.value}</div>
          </motion.div>
        ))}
      </div>
    </section>
  )
}
