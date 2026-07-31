import { useMemo } from 'react'
import { ExternalLink, ShieldCheck } from 'lucide-react'
import { motion, useReducedMotion } from 'framer-motion'
import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis } from 'recharts'
import { useTranslation } from 'react-i18next'
import { fmtInt } from '@/data/mock'
import { useNetPulse } from '@/data/DataProvider'
import { CountUp } from '@/components/CountUp'
import { StatusPill } from '@/components/StatusPill'
import { buildAdGuardSeries } from '@/components/routers/routerExtras'

function AdGuardTooltip({
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
  const permitidas = Number(payload.find((p) => p.dataKey === 'permitidas')?.value ?? 0)
  const bloqueadas = Number(payload.find((p) => p.dataKey === 'bloqueadas')?.value ?? 0)
  return (
    <div className="rounded-[10px] border border-border-strong bg-elevated px-3 py-2 shadow-lg">
      <div className="mb-1 font-mono text-caption text-text-muted">{label}</div>
      <div className="font-mono text-mono-sm text-text-primary">
        {t('routerDetail.adguard.tooltip', { total: fmtInt(permitidas + bloqueadas), blocked: fmtInt(bloqueadas) })}
      </div>
    </div>
  )
}

/** ⑤ AdGuard Home panel (router-detail.md §⑤) — id="adguard". */
export function AdGuardPanel() {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const { adguard } = useNetPulse()
  const adguardSeries24h = useMemo(() => buildAdGuardSeries(adguard), [adguard])
  const maxTop = Math.max(...adguard.topBlocked.map((d) => d.count))
  const clientsPct = Math.round((adguard.clientsUsing / adguard.clientsTotal) * 100)

  const STATS = [
    { label: t('routerDetail.adguard.queries24h'), value: adguard.queries24h, unit: '' },
    { label: t('routerDetail.adguard.blocked'), value: adguard.blocked24h, unit: '' },
    { label: t('routerDetail.adguard.blockedPct'), value: adguard.blockedPct, unit: '%', decimals: 1 },
    { label: t('home.services.avgDns'), value: adguard.dnsLatencyMs, unit: 'ms' },
  ] as const

  return (
    <section id="adguard" className="scroll-mt-4 rounded-2xl border border-border bg-surface p-5 transition-shadow duration-500 md:p-6 lg:col-span-7">
      {/* Cabecera */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-ok/10 text-ok">
            <ShieldCheck className="h-[18px] w-[18px]" strokeWidth={1.75} />
          </div>
          <h3 className="font-display text-h2 text-text-primary">AdGuard Home</h3>
          <StatusPill tone="ok" label={t('common.active')} />
        </div>
        <a
          href="#"
          onClick={(e) => e.preventDefault()}
          className="inline-flex items-center gap-1 text-caption font-semibold text-text-secondary transition-colors hover:text-accent"
        >
          {t('home.services.openPanel')}
          <ExternalLink className="h-3.5 w-3.5" strokeWidth={1.75} />
        </a>
      </div>

      {/* 4 stats compactas */}
      <div className="mt-4 grid grid-cols-2 gap-3 sm:grid-cols-4">
        {STATS.map((s, i) => (
          <motion.div
            key={s.label}
            initial={reduce ? false : { opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.3, ease: 'easeOut', delay: i * 0.08 }}
            className="rounded-xl bg-elevated/60 px-3.5 py-3"
          >
            <div className="text-[10px] font-medium uppercase tracking-[0.06em] text-text-muted">{s.label}</div>
            <div className="mt-1 font-mono text-lg font-semibold text-text-primary">
              <CountUp value={s.value} decimals={'decimals' in s ? s.decimals : 0} />
              {s.unit && <span className="ml-1 text-xs font-medium text-text-secondary">{s.unit}</span>}
            </div>
          </motion.div>
        ))}
      </div>

      {/* Barras apiladas 24h */}
      <div
        className="mt-4 h-[160px]"
        role="img"
        aria-label={t('routerDetail.adguard.chartAria', { total: fmtInt(adguard.queries24h), blocked: fmtInt(adguard.blocked24h) })}
      >
        <ResponsiveContainer width="100%" height="100%">
          <BarChart data={adguardSeries24h} margin={{ top: 8, right: 4, bottom: 0, left: 4 }} barCategoryGap="28%">
            <CartesianGrid vertical={false} stroke="rgb(var(--border) / 0.5)" strokeDasharray="3 6" />
            <XAxis
              dataKey="t"
              tickLine={false}
              axisLine={false}
              tick={{ fill: 'rgb(var(--text-muted))', fontSize: 10, fontFamily: '"JetBrains Mono", monospace' }}
              interval={5}
            />
            <Tooltip content={<AdGuardTooltip />} cursor={{ fill: 'rgb(var(--hover) / 0.5)' }} />
            <Bar dataKey="permitidas" stackId="dns" fill="#22D3EE" fillOpacity={0.25} animationDuration={700} animationEasing="ease-out" />
            <Bar dataKey="bloqueadas" stackId="dns" fill="#34D399" radius={[3, 3, 0, 0]} animationDuration={700} animationEasing="ease-out" />
          </BarChart>
        </ResponsiveContainer>
      </div>

      {/* Top dominios bloqueados */}
      <div className="mt-5">
        <div className="mb-2 text-[10px] font-medium uppercase tracking-[0.06em] text-text-muted">
          {t('routerDetail.adguard.topBlocked')}
        </div>
        <div className="space-y-2">
          {adguard.topBlocked.map((d, i) => (
            <div key={d.domain} className="flex items-center gap-3">
              <span className="w-40 shrink-0 truncate font-mono text-mono-sm text-text-secondary sm:w-52">{d.domain}</span>
              <div className="h-1.5 flex-1 overflow-hidden rounded-full bg-border/50">
                <motion.div
                  className="h-full rounded-full bg-ok"
                  initial={reduce ? { width: `${(d.count / maxTop) * 100}%` } : { width: 0 }}
                  animate={{ width: `${(d.count / maxTop) * 100}%` }}
                  transition={{ duration: 0.6, ease: 'easeOut', delay: 0.2 + i * 0.06 }}
                />
              </div>
              <span className="w-14 shrink-0 text-right font-mono text-mono-sm text-text-primary">
                <CountUp value={d.count} duration={0.6} />
              </span>
            </div>
          ))}
        </div>
      </div>

      {/* Footer */}
      <div className="mt-5 grid grid-cols-1 gap-3 border-t border-border pt-4 sm:grid-cols-2">
        <div className="text-caption text-text-secondary">
          {t('routerDetail.adguard.filterLists')} <span className="font-mono text-text-primary">{t('routerDetail.adguard.listsActive', { count: adguard.filterLists })}</span>
          <span className="text-text-muted"> · {t('routerDetail.adguard.rules', { count: fmtInt(adguard.rules) })}</span>
        </div>
        <div>
          <div className="mb-1.5 flex items-center justify-between text-caption">
            <span className="text-text-secondary">
              {t('routerDetail.adguard.protectedClients')} <span className="font-mono text-text-primary">{adguard.clientsUsing}/{adguard.clientsTotal}</span>
            </span>
            <span className="font-mono text-text-muted">{clientsPct} %</span>
          </div>
          <div className="h-1.5 overflow-hidden rounded-full bg-border/50">
            <motion.div
              className="h-full rounded-full bg-ok"
              initial={reduce ? { width: `${clientsPct}%` } : { width: 0 }}
              animate={{ width: `${clientsPct}%` }}
              transition={{ duration: 0.7, ease: 'easeOut', delay: 0.3 }}
            />
          </div>
        </div>
      </div>
    </section>
  )
}
