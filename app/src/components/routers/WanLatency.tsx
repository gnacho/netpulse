import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis } from 'recharts'
import { motion, useReducedMotion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import { fmtEs } from '@/data/mock'
import { useNetPulse } from '@/data/DataProvider'
import { SectionHeader } from '@/components/SectionHeader'
import { Sparkline } from '@/components/Sparkline'
import { LatencyGauge } from '@/components/routers/LatencyGauge'
import { WAN_LATENCY_24H, WAN_LATENCY_STATS } from '@/components/routers/routerExtras'

function WanTooltip({
  active,
  payload,
  label,
}: {
  active?: boolean
  payload?: { dataKey?: string | number; value?: number | string }[]
  label?: string
}) {
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

/** ④ WAN & Latencia del gateway (router-detail.md §④) — split 7/5. */
export function WanLatency() {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const { traffic: trafficByRange, wan } = useNetPulse()
  const data = trafficByRange['24h']

  const WAN_METRICS = [
    { label: t('routerDetail.wan.publicIp'), value: wan.publicIp },
    { label: 'ISP', value: `${wan.isp} · ${t('routerDetail.wan.fiber', { plan: wan.plan.replace(' Mbps', '') })}` },
    { label: t('routerDetail.wan.totalToday'), value: wan.total24h },
  ] as const

  return (
    <div className="grid grid-cols-1 gap-4 md:gap-5 lg:col-span-12 lg:grid-cols-12">
      {/* 4a. WAN */}
      <section className="rounded-2xl border border-border bg-surface p-5 md:p-6 lg:col-span-7">
        <SectionHeader title={t('routerDetail.wan.title')} />
        <motion.div
          initial={reduce ? false : { opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.3 }}
          className="mt-4 h-[200px]"
          role="img"
          aria-label={t('routerDetail.wan.chartAria', { down: wan.downMbps, up: wan.upMbps })}
        >
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={data} margin={{ top: 8, right: 4, bottom: 0, left: 4 }}>
              <defs>
                <linearGradient id="wan-detail-down" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor="#22D3EE" stopOpacity={0.25} />
                  <stop offset="100%" stopColor="#22D3EE" stopOpacity={0} />
                </linearGradient>
                <linearGradient id="wan-detail-up" x1="0" y1="0" x2="0" y2="1">
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
              <Tooltip content={<WanTooltip />} cursor={{ stroke: 'rgb(var(--border-strong))', strokeWidth: 1 }} />
              <Area type="monotone" dataKey="down" stroke="#22D3EE" strokeWidth={2} fill="url(#wan-detail-down)" dot={false} animationDuration={900} animationEasing="ease-out" />
              <Area type="monotone" dataKey="up" stroke="#A78BFA" strokeWidth={2} fill="url(#wan-detail-up)" dot={false} animationDuration={900} animationEasing="ease-out" />
            </AreaChart>
          </ResponsiveContainer>
        </motion.div>
        <div className="mt-4 grid grid-cols-3 gap-3 border-t border-border pt-4">
          {WAN_METRICS.map((m) => (
            <div key={m.label}>
              <div className="text-label uppercase text-text-muted">{m.label}</div>
              <div className="mt-0.5 font-mono text-mono-sm font-semibold text-text-primary">{m.value}</div>
            </div>
          ))}
        </div>
      </section>

      {/* 4b. Latencia */}
      <section className="flex flex-col rounded-2xl border border-border bg-surface p-5 md:p-6 lg:col-span-5">
        <SectionHeader title={t('home.latency')} />
        <div className="mt-4 flex flex-1 flex-col items-center justify-center gap-4">
          <LatencyGauge valueMs={wan.latencyMs} caption={t('routerDetail.latency.toHost', { host: '1.1.1.1' })} />
          <div className="w-full">
            <div className="mb-1 text-[10px] font-medium uppercase tracking-[0.06em] text-text-muted">{t('routerDetail.latency.24h')}</div>
            <Sparkline data={WAN_LATENCY_24H} width={560} height={48} color="#34D399" area className="w-full" />
          </div>
          <div className="grid w-full grid-cols-3 gap-3 border-t border-border pt-3.5">
            {[
              [t('home.traffic.average'), `${WAN_LATENCY_STATS.avgMs} ms`],
              ['Jitter', `${fmtEs(WAN_LATENCY_STATS.jitterMs, 1)} ms`],
              [t('home.traffic.loss'), `${WAN_LATENCY_STATS.lossPct} %`],
            ].map(([k, v]) => (
              <div key={k} className="text-center">
                <div className="text-label uppercase text-text-muted">{k}</div>
                <div className="mt-0.5 font-mono text-mono-sm font-semibold text-text-primary">{v}</div>
              </div>
            ))}
          </div>
        </div>
      </section>
    </div>
  )
}
