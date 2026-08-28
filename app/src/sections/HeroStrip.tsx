import { motion, useReducedMotion } from 'framer-motion'
import { Fragment, useEffect, useState } from 'react'
import { AlertTriangle, CheckCircle2, Gauge, MonitorSmartphone } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { healthLabel } from '@/i18n'
import { CountUp } from '@/components/CountUp'
import { HealthRing } from '@/components/HealthRing'
import { StatusPill } from '@/components/StatusPill'
import { useNetPulse } from '@/data/DataProvider'
import { useAuth } from '@/data/AuthContext'
import { useDashboard } from '@/hooks/useDashboard'

function greetingKey(): string {
  const h = new Date().getHours()
  if (h < 13) return 'home.greetingMorning'
  if (h < 21) return 'home.greetingAfternoon'
  return 'home.greetingEvening'
}

function MiniStat({
  icon: Icon,
  label,
  children,
  colorClass,
  index,
}: {
  icon: React.ComponentType<{ className?: string; strokeWidth?: number }>
  label: string
  children: React.ReactNode
  colorClass: string
  index: number
}) {
  return (
    <motion.div
      initial={{ opacity: 0, y: 16 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.4, ease: 'easeOut', delay: 0.35 + index * 0.08 }}
      className="flex flex-col items-center gap-1"
    >
      <span className="flex items-center gap-1.5 text-label uppercase text-text-muted">
        <Icon className={`h-3.5 w-3.5 ${colorClass}`} strokeWidth={1.75} />
        {label}
      </span>
      <span className={`kpi-value text-lg font-semibold ${colorClass}`}>{children}</span>
    </motion.div>
  )
}

/** ① Hero strip — saludo + estado arriba, donut de salud grande y centrado,
 *  stats en fila debajo (home.md §①) */
export function HeroStrip() {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const { refreshKey } = useDashboard()
  const { deviceTotals, health: healthScore, wan } = useNetPulse()
  const auth = useAuth()
  // SPEC-65 D65-5: saluda con displayName || username. En demo (sin backend)
  // el nombre vive en localStorage ('netpulse-displayname').
  const [demoName, setDemoName] = useState('')
  useEffect(() => {
    if (auth) return
    const read = () => {
      try {
        setDemoName(localStorage.getItem('netpulse-displayname') ?? '')
      } catch {
        /* modo privado */
      }
    }
    read()
    window.addEventListener('netpulse-auth-refresh', read)
    return () => window.removeEventListener('netpulse-auth-refresh', read)
  }, [auth])
  const name = auth ? auth.displayName || auth.user : demoName
  const greeting = t(greetingKey()) + (name ? t('home.greetingName', { name }) : '')
  const words = greeting.split(' ')

  const alerts = healthScore.breakdown?.length ?? 0
  const statusLine = alerts > 0 ? t('home.importantAlerts', { count: alerts }) : t('home.noImportantAlerts')

  return (
    <section className="surface-featured mesh-bg relative h-full overflow-hidden rounded-2xl border bg-surface p-5 md:p-6">
      {/* Halo radial cyan que respira */}
      {!reduce && (
        <motion.div
          className="pointer-events-none absolute -right-24 -top-24 h-72 w-72 rounded-full bg-accent/[0.08] blur-3xl"
          animate={{ scale: [1, 1.06, 1] }}
          transition={{ duration: 6, repeat: Infinity, ease: 'easeInOut' }}
        />
      )}
      <div className="relative flex flex-col items-center gap-5">
        {/* Saludo + estado, arriba y centrado */}
        <div className="text-center">
          <h1 className="font-display text-h1 text-text-primary md:text-2xl" aria-label={greeting}>
            {words.map((w, i) => (
              <Fragment key={`${w}-${i}`}>
                <motion.span
                  className="inline-block"
                  initial={{ opacity: 0, y: 12 }}
                  animate={{ opacity: 1, y: 0 }}
                  transition={{ duration: 0.35, ease: 'easeOut', delay: i * 0.04 }}
                >
                  {w}
                </motion.span>
                {i < words.length - 1 ? ' ' : ''}
              </Fragment>
            ))}
          </h1>
          <motion.p
            className="mt-1.5 flex items-center justify-center gap-1.5 text-sm font-medium text-text-secondary"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            transition={{ delay: 0.25, duration: 0.4 }}
          >
            {alerts > 0 ? (
              <>
                <AlertTriangle className="h-4 w-4 text-warn" strokeWidth={1.75} />
                {statusLine}
              </>
            ) : (
              <>
                <CheckCircle2 className="h-4 w-4 text-ok" strokeWidth={1.75} />
                {statusLine}
              </>
            )}
          </motion.p>
        </div>

        {/* Donut de salud, grande y centrado */}
        <div className="flex flex-col items-center gap-2">
          <motion.div layoutId="health-ring">
            <HealthRing
              value={healthScore.score}
              size={220}
              stroke={14}
              ariaLabel={t('home.healthAria', {
                caption: t('common.healthCaption'),
                score: healthScore.score,
                label: healthLabel(healthScore.label),
              })}
              center={
                <div className="flex flex-col items-center">
                  <span className="kpi-value text-display-sm font-bold text-text-primary md:text-display">
                    <CountUp value={healthScore.score} duration={1.2} nonce={refreshKey} />
                  </span>
                  <span className="font-mono text-mono-sm text-text-muted">/100</span>
                </div>
              }
            />
          </motion.div>
          <StatusPill tone="ok" label={healthLabel(healthScore.label)} />
          <span className="text-caption text-text-muted">{t('common.healthCaption')}</span>

          {/* Subscore bars (#334) */}
          {healthScore.subscores && healthScore.subscores.length > 0 && (
            <div className="mt-2 grid w-full max-w-xs grid-cols-2 gap-x-4 gap-y-1.5">
              {healthScore.subscores.map((s) => (
                <div key={s.key} className="flex items-center gap-2">
                  <span className="w-12 text-right text-[10px] font-medium uppercase tracking-wider text-text-muted">
                    {s.label}
                  </span>
                  <div className="relative h-1.5 flex-1 overflow-hidden rounded-full bg-border">
                    <div
                      className={cn(
                        'h-full rounded-full transition-all duration-500',
                        s.score >= 90 ? 'bg-ok' : s.score >= 70 ? 'bg-warn' : 'bg-danger',
                      )}
                      style={{ width: `${s.score}%` }}
                    />
                  </div>
                  <span className="w-6 text-right font-mono text-[10px] text-text-secondary">
                    {s.score}
                  </span>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Desglose del health score (#23): barras de penalizacion */}
        {healthScore.breakdown.length > 0 && (
          <div className="w-full max-w-sm space-y-1.5">
            {healthScore.breakdown.map((b, i) => {
              const pct = Math.min(100, Math.abs(b.delta) * 10)
              const color = Math.abs(b.delta) >= 8 ? 'bg-danger' : Math.abs(b.delta) >= 4 ? 'bg-warn' : 'bg-info'
              return (
                <motion.div
                  key={b.label}
                  initial={reduce ? false : { opacity: 0, x: -8 }}
                  animate={{ opacity: 1, x: 0 }}
                  transition={{ duration: 0.3, delay: 0.4 + i * 0.08 }}
                  className="flex items-center gap-2"
                >
                  <span className="w-28 shrink-0 truncate text-right text-[11px] text-text-muted">{b.label}</span>
                  <div className="relative h-2 flex-1 overflow-hidden rounded-full bg-border/50">
                    <motion.div
                      className={cn('absolute left-0 top-0 h-full rounded-full', color)}
                      initial={{ width: 0 }}
                      animate={{ width: `${pct}%` }}
                      transition={{ duration: 0.6, delay: 0.5 + i * 0.08, ease: 'easeOut' }}
                    />
                  </div>
                  <span className={cn('w-8 shrink-0 text-right font-mono text-[11px] font-semibold',
                    Math.abs(b.delta) >= 8 ? 'text-danger' : Math.abs(b.delta) >= 4 ? 'text-warn' : 'text-info',
                  )}>
                    {b.delta}
                  </span>
                </motion.div>
              )
            })}
          </div>
        )}

        {/* Stats: latencia + dispositivos en fila, centrados */}
        <div className="mt-1 flex w-full items-center justify-center gap-8 md:gap-12">
          <MiniStat icon={Gauge} label={t('home.latency')} colorClass="text-ok" index={0}>
            <CountUp value={wan.latencyMs} nonce={refreshKey} /> ms
          </MiniStat>
          <MiniStat icon={MonitorSmartphone} label={t('home.devices')} colorClass="text-text-primary" index={1}>
            <CountUp value={deviceTotals.total} nonce={refreshKey} />
          </MiniStat>
        </div>
      </div>
    </section>
  )
}
