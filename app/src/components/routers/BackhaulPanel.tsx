import { Cable, Wifi } from 'lucide-react'
import { motion, useReducedMotion } from 'framer-motion'
import { Area, AreaChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis } from 'recharts'
import { useTranslation } from 'react-i18next'
import type { Router } from '@/data/mock'
import { SectionHeader } from '@/components/SectionHeader'
import { Sparkline } from '@/components/Sparkline'
import { LatencyGauge } from '@/components/routers/LatencyGauge'
import { getRouterExtras } from '@/components/routers/routerExtras'

function SignalTooltip({
  active,
  payload,
  label,
  wireless,
}: {
  active?: boolean
  payload?: { value?: number | string }[]
  label?: string
  wireless: boolean
}) {
  if (!active || !payload?.length) return null
  return (
    <div className="rounded-[10px] border border-border-strong bg-elevated px-3 py-2 shadow-lg">
      <div className="mb-0.5 font-mono text-caption text-text-muted">{label}</div>
      <div className="font-mono text-mono-sm text-text-primary">
        {payload[0].value} {wireless ? 'dBm' : '% carga enlace'}
      </div>
    </div>
  )
}

/** ④ variante OpenWrt — Backhaul + latencia al gateway (router-detail.md §Variante). */
export function BackhaulPanel({ router }: { router: Router }) {
  const { t } = useTranslation()
  const extras = getRouterExtras(router.id)
  const backhaul = extras.backhaul
  const reduce = useReducedMotion()
  if (!backhaul) return null

  const wireless = backhaul.kind === 'wireless'
  const data = extras.backhaulSignal.map((v, i) => ({
    t: `${String(i).padStart(2, '0')}:00`,
    v,
  }))
  const Icon = wireless ? Wifi : Cable

  return (
    <div className="grid grid-cols-1 gap-4 md:gap-5 lg:col-span-12 lg:grid-cols-12">
      {/* Backhaul */}
      <section className="rounded-2xl border border-border bg-surface p-5 md:p-6 lg:col-span-7">
        <SectionHeader title="Backhaul" />
        <div className="mt-3 flex items-center gap-2.5 rounded-xl bg-elevated/60 px-3.5 py-3">
          <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-accent-soft text-accent">
            <Icon className="h-[18px] w-[18px]" strokeWidth={1.75} />
          </span>
          <div>
            <div className="font-mono text-mono-sm font-semibold text-text-primary">{backhaul.headline}</div>
            <div className="text-caption text-text-muted">
              {wireless ? t('routerDetail.backhaul.wirelessLink') : t('routerDetail.backhaul.wiredLink')}
            </div>
          </div>
        </div>
        <motion.div
          initial={reduce ? false : { opacity: 0 }}
          animate={{ opacity: 1 }}
          transition={{ duration: 0.3 }}
          className="mt-4 h-[160px]"
          role="img"
          aria-label={wireless ? t('routerDetail.backhaul.signalAria') : t('routerDetail.backhaul.loadAria')}
        >
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={data} margin={{ top: 8, right: 4, bottom: 0, left: 4 }}>
              <defs>
                <linearGradient id="backhaul-grad" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor="#22D3EE" stopOpacity={0.25} />
                  <stop offset="100%" stopColor="#22D3EE" stopOpacity={0} />
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
              <Tooltip content={<SignalTooltip wireless={wireless} />} cursor={{ stroke: 'rgb(var(--border-strong))', strokeWidth: 1 }} />
              <Area
                type="monotone"
                dataKey="v"
                stroke="#22D3EE"
                strokeWidth={2}
                fill="url(#backhaul-grad)"
                dot={false}
                animationDuration={900}
                animationEasing="ease-out"
              />
            </AreaChart>
          </ResponsiveContainer>
        </motion.div>
      </section>

      {/* Latencia al gateway */}
      <section className="flex flex-col rounded-2xl border border-border bg-surface p-5 md:p-6 lg:col-span-5">
        <SectionHeader title={t('routers.gatewayLatency')} />
        <div className="mt-4 flex flex-1 flex-col items-center justify-center gap-4">
          <LatencyGauge valueMs={backhaul.latencyMs} caption={t('routerDetail.latency.toHost', { host: '192.168.8.1' })} />
          <div className="w-full">
            <div className="mb-1 text-[10px] font-medium uppercase tracking-[0.06em] text-text-muted">{t('routerDetail.latency.24h')}</div>
            <Sparkline data={extras.gatewayLatencySpark} width={560} height={48} color="#34D399" area className="w-full" />
          </div>
        </div>
      </section>
    </div>
  )
}
