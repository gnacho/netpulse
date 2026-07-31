import { motion, useReducedMotion } from 'framer-motion'
import { Fragment } from 'react'
import { ArrowDown, ArrowUp, Gauge, MonitorSmartphone } from 'lucide-react'
import { useTranslation } from 'react-i18next'
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
      transition={{ duration: 0.4, ease: 'easeOut', delay: 0.3 + index * 0.1 }}
      className="flex items-center gap-3 md:flex-col md:items-start md:gap-1.5 md:border-l md:border-border md:pl-4"
    >
      <span className="flex items-center gap-1.5 text-label uppercase text-text-muted">
        <Icon className={`h-3.5 w-3.5 ${colorClass}`} strokeWidth={1.75} />
        {label}
      </span>
      <span className={`font-mono text-lg font-semibold md:text-xl ${colorClass}`}>{children}</span>
    </motion.div>
  )
}

/** ① Hero strip — saludo + HealthRing + WAN quick stats (home.md §①) */
export function HeroStrip() {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const { refreshKey } = useDashboard()
  const { deviceTotals, health: healthScore, wan } = useNetPulse()
  const auth = useAuth()
  const greeting = t(greetingKey()) + (auth?.user ? t('home.greetingName', { name: auth.user }) : '')
  const words = greeting.split(' ')

  return (
    <section className="mesh-bg relative h-full overflow-hidden rounded-2xl border border-border bg-surface p-5 md:p-6">
      {/* Halo radial cyan que respira */}
      {!reduce && (
        <motion.div
          className="pointer-events-none absolute -right-24 -top-24 h-72 w-72 rounded-full bg-accent/[0.08] blur-3xl"
          animate={{ scale: [1, 1.06, 1] }}
          transition={{ duration: 6, repeat: Infinity, ease: 'easeInOut' }}
        />
      )}
      <div className="relative flex flex-col items-center gap-6 md:flex-row md:justify-between md:gap-8">
        {/* Saludo */}
        <div className="text-center md:text-left">
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
            className="mt-1.5 max-w-md text-sm text-text-secondary"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            transition={{ delay: 0.25, duration: 0.4 }}
          >
            {healthScore.note || t('home.statusPhrase')}
          </motion.p>
        </div>

        {/* HealthRing */}
        <div className="flex flex-col items-center gap-2">
          <motion.div layoutId="health-ring">
            <HealthRing
              value={healthScore.score}
              size={160}
              stroke={12}
              ariaLabel={t('home.healthAria', {
                caption: t('common.healthCaption'),
                score: healthScore.score,
                label: healthLabel(healthScore.label),
              })}
              center={
                <div className="flex flex-col items-center">
                  <span className="font-display text-display-sm font-bold text-text-primary md:text-display">
                    <CountUp value={healthScore.score} duration={1.2} nonce={refreshKey} />
                  </span>
                  <span className="font-mono text-mono-sm text-text-muted">/100</span>
                </div>
              }
            />
          </motion.div>
          <StatusPill tone="ok" label={healthLabel(healthScore.label)} />
          <span className="text-caption text-text-muted">{t('common.healthCaption')}</span>
        </div>

        {/* Quick stats WAN: columna desktop / grid 2×2 móvil */}
        <div className="grid w-full grid-cols-2 gap-4 md:w-auto md:grid-cols-1 md:gap-3">
          <MiniStat icon={ArrowDown} label={t('home.download')} colorClass="text-accent" index={0}>
            ↓ <CountUp value={wan.downMbps} decimals={1} nonce={refreshKey} /> Mbps
          </MiniStat>
          <MiniStat icon={ArrowUp} label={t('home.upload')} colorClass="text-tunnel" index={1}>
            ↑ <CountUp value={wan.upMbps} decimals={1} nonce={refreshKey} /> Mbps
          </MiniStat>
          <MiniStat icon={Gauge} label={t('home.latency')} colorClass="text-ok" index={2}>
            <CountUp value={wan.latencyMs} nonce={refreshKey} /> ms
          </MiniStat>
          <div className="md:hidden">
            <MiniStat icon={MonitorSmartphone} label={t('home.devices')} colorClass="text-text-primary" index={3}>
              <CountUp value={deviceTotals.total} nonce={refreshKey} />
            </MiniStat>
          </div>
        </div>
      </div>
    </section>
  )
}
