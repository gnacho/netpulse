import { Link } from 'react-router'
import { ChevronRight, Cpu, MemoryStick, Router as RouterIcon, Thermometer, Users } from 'lucide-react'
import { motion } from 'framer-motion'
import type { LucideIcon } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { roleLabel } from '@/i18n'
import type { Router } from '@/data/mock'
import { HealthRing, STATUS_COLORS } from '@/components/HealthRing'
import { Sparkline } from '@/components/Sparkline'
import { StatusPill } from '@/components/StatusPill'
import { AgentBadge } from '@/components/routers/AgentBadge'
import { useAgentFor } from '@/hooks/useAgentFor'
import { cn } from '@/lib/utils'

function MiniMetric({
  icon: Icon,
  value,
  label,
  hot = false,
}: {
  icon: LucideIcon
  value: string
  label: string
  hot?: boolean
}) {
  return (
    <div className="flex items-center gap-1.5" title={label}>
      <Icon className={cn('h-3.5 w-3.5', hot ? 'text-warn' : 'text-text-muted')} strokeWidth={1.75} />
      <span className={cn('font-mono text-mono-sm', hot ? 'text-warn' : 'text-text-secondary')}>{value}</span>
    </div>
  )
}

interface RouterCardProps {
  router: Router
  index?: number
  className?: string
}

/** Tarjeta de router (design.md §10.2): StatusRing, mini-métricas, sparkline, chevron. */
export function RouterCard({ router, index = 0, className }: RouterCardProps) {
  const { t } = useTranslation()
  const agent = useAgentFor(router.id)
  const warn = router.status === 'warn'
  const offline = router.status === 'offline'
  return (
    <motion.div
      initial={{ opacity: 0, y: 24 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ type: 'spring', stiffness: 260, damping: 24, delay: index * 0.08 }}
      className={className}
    >
      <Link
        to={`/routers/${router.id}`}
        aria-label={t('common.viewDetail', { name: router.name })}
        className={cn(
          'group flex h-full flex-col rounded-2xl border bg-surface p-5 transition-all duration-150',
          'hover:-translate-y-0.5 hover:border-accent/40',
          warn ? 'border-t-2 border-t-warn border-x-border border-b-border shadow-glow-warn' : 'border-border',
          offline && 'border-danger opacity-60 saturate-50',
        )}
      >
        <div className="flex items-start justify-between gap-3">
          <div className="flex min-w-0 items-center gap-3">
            <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-elevated text-accent">
              <RouterIcon className="h-5 w-5" strokeWidth={1.75} />
            </div>
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                <span className="truncate font-display text-h2 text-text-primary">{router.name}</span>
                <span className="shrink-0 rounded-full bg-accent-soft px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-accent">
                  {roleLabel(router.roleBadge)}
                </span>
                <AgentBadge agent={agent} agentOnly={router.agentOnly} deviceType={router.type} className="shrink-0" />
              </div>
              <div className="truncate text-caption text-text-muted">{router.modelShort}</div>
            </div>
          </div>
          <HealthRing
            value={router.health}
            size={56}
            stroke={5}
            variant="status"
            status={router.status}
            delay={0.2 + index * 0.08}
            ariaLabel={t('common.healthOf', { name: router.name, health: router.health })}
            center={
              <span className="font-mono text-mono-sm font-semibold text-text-primary">{router.health}</span>
            }
          />
        </div>

        <div className="mt-4 grid grid-cols-2 gap-x-3 gap-y-2">
          <MiniMetric icon={Cpu} value={`${router.cpu} %`} label="CPU" hot={router.hotMetric === 'cpu'} />
          <MiniMetric icon={MemoryStick} value={`${router.ram} %`} label="RAM" hot={router.hotMetric === 'ram'} />
          <MiniMetric icon={Thermometer} value={`${router.temp} °C`} label={t('common.temperature')} hot={router.hotMetric === 'temp'} />
          <MiniMetric icon={Users} value={String(router.clients)} label={t('common.clients')} />
        </div>

        <div className="mt-4 flex items-center justify-between gap-3 border-t border-border pt-3">
          <Sparkline data={router.sparkline} width={100} height={28} color={STATUS_COLORS[router.status] === '#34D399' ? '#22D3EE' : STATUS_COLORS[router.status]} />
          <div className="flex items-center gap-2">
            <span className="font-mono text-caption text-text-muted">{router.uptime}</span>
            <StatusPill
              tone={router.status === 'online' ? 'ok' : router.status === 'warn' ? 'warn' : 'danger'}
              label={t(`common.status.${router.status}`)}
              pulse={router.status !== 'online'}
              className="hidden xl:inline-flex"
            />
            <ChevronRight
              className="h-4 w-4 text-text-muted transition-transform duration-150 group-hover:translate-x-1 group-hover:text-accent"
              strokeWidth={1.75}
            />
          </div>
        </div>
      </Link>
    </motion.div>
  )
}
