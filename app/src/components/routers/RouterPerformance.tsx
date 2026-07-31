import { useMemo, useState } from 'react'
import { Area, AreaChart, CartesianGrid, ReferenceArea, ReferenceLine, ResponsiveContainer, Tooltip, XAxis } from 'recharts'
import { motion, useReducedMotion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import type { Router } from '@/data/mock'
import { CountUp } from '@/components/CountUp'
import { SectionHeader } from '@/components/SectionHeader'
import { SegmentedControl } from '@/components/SegmentedControl'
import type { PerfPoint } from '@/components/routers/routerExtras'
import { perfCaptions, perfSeries } from '@/components/routers/routerExtras'

type PerfRange = '1h' | '24h' | '7d'

const RANGE_OPTIONS = [
  { value: '1h', label: '1h' },
  { value: '24h', label: '24h' },
  { value: '7d', label: '7d' },
] as const

const TEMP_THRESHOLD = 65
const SYNC_ID = 'router-perf'

interface SeriesDef {
  key: 'cpu' | 'ram' | 'temp'
  labelKey: string
  unit: string
  color: string
  gradId: string
}

const SERIES: SeriesDef[] = [
  { key: 'cpu', labelKey: 'routers.colCpu', unit: '%', color: '#22D3EE', gradId: 'perf-cpu' },
  { key: 'ram', labelKey: 'common.memory', unit: '%', color: '#A78BFA', gradId: 'perf-ram' },
  { key: 'temp', labelKey: 'common.temperature', unit: '°C', color: '#FBBF24', gradId: 'perf-temp' },
]

function PerfTooltip({
  active,
  payload,
  label,
}: {
  active?: boolean
  payload?: { dataKey?: string | number; value?: number | string }[]
  label?: string
}) {
  const { t } = useTranslation()
  if (!active || !payload?.length) return null
  const p = payload[0]
  const def = SERIES.find((s) => s.key === p.dataKey)
  return (
    <div className="rounded-[10px] border border-border-strong bg-elevated px-3 py-2 shadow-lg">
      <div className="mb-0.5 font-mono text-caption text-text-muted">{label}</div>
      <div className="flex items-center gap-2 font-mono text-mono-sm text-text-primary">
        <span className="h-2 w-2 rounded-full" style={{ background: def?.color ?? '#22D3EE' }} />
        {def ? t(def.labelKey) : ''}: {p.value} {def?.unit}
      </div>
    </div>
  )
}

/** ② Rendimiento (router-detail.md §②) — 3 áreas con crosshair sincronizado. */
export function RouterPerformance({ router }: { router: Router }) {
  const { t } = useTranslation()
  const [range, setRange] = useState<PerfRange>('24h')
  const reduce = useReducedMotion()
  const data = useMemo(() => perfSeries(router, range), [router, range])
  const captions = useMemo(() => perfCaptions(router, data), [router, data])

  const current: Record<SeriesDef['key'], number> = { cpu: router.cpu, ram: router.ram, temp: router.temp }
  const captionByKey: Record<SeriesDef['key'], string> = { cpu: captions.cpu, ram: captions.ram, temp: captions.temp }

  return (
    <section className="rounded-2xl border border-border bg-surface p-5 md:p-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <SectionHeader title={t('routerDetail.performance')} />
        <SegmentedControl
          options={RANGE_OPTIONS}
          value={range}
          onChange={(v) => setRange(v as PerfRange)}
          ariaLabel={t('routerDetail.performanceRange')}
        />
      </div>

      <div className="mt-4 space-y-4">
        {SERIES.map((s, si) => (
          <motion.div
            key={`${range}-${s.key}`}
            initial={reduce ? false : { opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.35, ease: 'easeOut', delay: si * 0.15 }}
            className="flex items-center gap-4"
          >
            <div className="min-w-0 flex-1">
              <div className="mb-1 flex items-baseline justify-between gap-2">
                <span className="text-label uppercase text-text-muted">{t(s.labelKey)}</span>
                <span className="text-caption text-text-muted sm:hidden">{captionByKey[s.key]}</span>
              </div>
              <div className="h-[90px]" role="img" aria-label={t('routerDetail.perf.chartAria', { label: t(s.labelKey), name: router.name, range, value: current[s.key], unit: s.unit })}>
                <ResponsiveContainer width="100%" height="100%">
                  <AreaChart data={data} syncId={SYNC_ID} syncMethod="index" margin={{ top: 4, right: 0, bottom: 0, left: 0 }}>
                    <defs>
                      <linearGradient id={s.gradId} x1="0" y1="0" x2="0" y2="1">
                        <stop offset="0%" stopColor={s.color} stopOpacity={0.25} />
                        <stop offset="100%" stopColor={s.color} stopOpacity={0} />
                      </linearGradient>
                    </defs>
                    <CartesianGrid vertical={false} stroke="rgb(var(--border) / 0.5)" strokeDasharray="3 6" />
                    <XAxis dataKey="t" hide />
                    <Tooltip content={<PerfTooltip />} cursor={{ stroke: 'rgb(var(--border-strong))', strokeWidth: 1 }} />
                    {s.key === 'temp' && (
                      <>
                        <ReferenceArea y1={TEMP_THRESHOLD} y2={100} fill="#F87171" fillOpacity={0.08} />
                        <ReferenceLine
                          y={TEMP_THRESHOLD}
                          stroke="#FBBF24"
                          strokeDasharray="4 4"
                          strokeOpacity={0.8}
                          label={{ value: '65 °C', position: 'insideTopRight', fill: '#FBBF24', fontSize: 9, fontFamily: '"JetBrains Mono", monospace' }}
                        />
                      </>
                    )}
                    <Area
                      type="monotone"
                      dataKey={s.key}
                      stroke={s.color}
                      strokeWidth={1.75}
                      fill={`url(#${s.gradId})`}
                      dot={false}
                      activeDot={{ r: 3.5, strokeWidth: 0, fill: s.color }}
                      animationDuration={900}
                      animationEasing="ease-out"
                    />
                  </AreaChart>
                </ResponsiveContainer>
              </div>
            </div>
            <div className="w-20 shrink-0 text-right sm:w-24">
              <div className="font-mono text-xl font-semibold text-text-primary">
                <CountUp key={range} value={current[s.key]} />
                <span className="ml-0.5 text-xs font-medium text-text-secondary">{s.unit}</span>
              </div>
              <div className="mt-0.5 hidden text-caption text-text-muted sm:block">{captionByKey[s.key]}</div>
            </div>
          </motion.div>
        ))}
      </div>

      {/* Tabla accesible */}
      <table className="sr-only">
        <caption>{t('routerDetail.perf.tableCaption', { range })}</caption>
        <thead>
          <tr><th>{t('home.traffic.time')}</th><th>CPU (%)</th><th>{t('common.memory')} (%)</th><th>{t('common.temperature')} (°C)</th></tr>
        </thead>
        <tbody>
          {data.map((p: PerfPoint) => (
            <tr key={p.t}><td>{p.t}</td><td>{p.cpu}</td><td>{p.ram}</td><td>{p.temp}</td></tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}
