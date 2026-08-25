import { Link } from 'react-router'
import { Router as RouterIcon } from 'lucide-react'
import { motion, useReducedMotion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import { roleLabel } from '@/i18n'
import type { Router } from '@/data/mock'
import { HealthRing } from '@/components/HealthRing'
import { StatusPill } from '@/components/StatusPill'
import { AgentBadge } from '@/components/routers/AgentBadge'
import { AgentRearmButton } from '@/components/routers/AgentRearmButton'
import { AgentUpgradeButton } from '@/components/routers/AgentUpgradeButton'
import { useAgentFor } from '@/hooks/useAgentFor'
import { cn } from '@/lib/utils'

/** ① Detail header (router-detail.md §①) — identidad + StatusRing 96px. */
export function RouterDetailHeader({ router }: { router: Router }) {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const isGateway = router.roleBadge === 'Principal'
  const agent = useAgentFor(router.id)
  const pills = [
    { label: roleLabel(router.roleBadge), tone: 'accent' as const, pulse: false },
    {
      label: t(`common.status.${router.status}`),
      tone: (router.status === 'online' ? 'ok' : router.status === 'warn' ? 'warn' : 'danger') as 'ok' | 'warn' | 'danger',
      pulse: router.status !== 'online',
    },
    ...(isGateway
      ? [
          { label: 'AdGuard', tone: 'ok' as const, pulse: false },
          { label: 'WireGuard', tone: 'tunnel' as const, pulse: false },
        ]
      : []),
  ]

  return (
    <section className="relative overflow-hidden rounded-2xl border border-border bg-surface p-5 md:p-6">
      {/* Hairline superior cyan→violet (gateway) */}
      {isGateway && (
        <div
          aria-hidden="true"
          className="absolute inset-x-0 top-0 h-0.5 opacity-40"
          style={{ background: 'linear-gradient(90deg, #22D3EE, #A78BFA)' }}
        />
      )}
      <div className="flex flex-wrap items-center gap-3">
        <nav aria-label={t('common.breadcrumb')} className="font-mono text-caption text-text-muted">
          <Link to="/" className="transition-colors hover:text-accent">{t('common.home')}</Link>
          <span className="mx-1.5">/</span>
          <Link to="/routers" className="transition-colors hover:text-accent">{t('nav.routers')}</Link>
          <span className="mx-1.5">/</span>
          <span className="text-text-secondary">{router.name}</span>
        </nav>
      </div>

      <div className="mt-3 flex flex-wrap items-center justify-between gap-x-6 gap-y-4">
        <div className="flex min-w-0 items-center gap-4">
          <motion.div
            layoutId={`router-tile-${router.id}`}
            className={cn(
              'flex h-14 w-14 shrink-0 items-center justify-center rounded-xl',
              isGateway ? 'bg-gradient-to-br from-accent to-tunnel text-canvas' : 'bg-accent-soft text-accent',
            )}
          >
            <RouterIcon className="h-7 w-7" strokeWidth={1.75} />
          </motion.div>
          <div className="min-w-0">
            <h1 className="font-display text-h1 text-text-primary">{router.name}</h1>
            <p className="text-caption text-text-muted">
              {isGateway ? 'GL.iNet Flint 2 · GL-MT6000' : `OpenWrt · ${router.modelShort}`}
            </p>
            <div className="mt-2 flex flex-wrap items-center gap-1.5">
              {pills.map((p, i) => (
                <motion.span
                  key={p.label}
                  initial={reduce ? false : { opacity: 0, scale: 0.9 }}
                  animate={{ opacity: 1, scale: 1 }}
                  transition={{ duration: 0.2, delay: 0.15 + i * 0.06 }}
                >
                  <StatusPill tone={p.tone} label={p.label} pulse={p.pulse} />
                </motion.span>
              ))}
              {agent && (
                <motion.span
                  initial={reduce ? false : { opacity: 0, scale: 0.9 }}
                  animate={{ opacity: 1, scale: 1 }}
                  transition={{ duration: 0.2, delay: 0.15 + pills.length * 0.06 }}
                >
                  <AgentBadge agent={agent} agentOnly={router.agentOnly} deviceType={router.type} />
                </motion.span>
              )}
              {agent && !agent.fresh && (
                <motion.span
                  initial={reduce ? false : { opacity: 0, scale: 0.9 }}
                  animate={{ opacity: 1, scale: 1 }}
                  transition={{ duration: 0.2, delay: 0.15 + (pills.length + 1) * 0.06 }}
                >
                  <AgentRearmButton agent={agent} />
                </motion.span>
              )}
              {agent && agent.updateAvailable && (
                <motion.span
                  initial={reduce ? false : { opacity: 0, scale: 0.9 }}
                  animate={{ opacity: 1, scale: 1 }}
                  transition={{ duration: 0.2, delay: 0.15 + (pills.length + 2) * 0.06 }}
                >
                  <AgentUpgradeButton agent={agent} />
                </motion.span>
              )}
            </div>
          </div>
        </div>

        <div className="flex flex-col items-center gap-1">
          <motion.div layoutId={`router-ring-${router.id}`}>
            <HealthRing
              value={router.health}
              size={96}
              stroke={8}
              variant="status"
              status={router.status}
              ariaLabel={t('common.healthOf', { name: router.name, health: router.health })}
              center={
                <span className="font-display text-display-sm text-text-primary">{router.health}</span>
              }
            />
          </motion.div>
          <span className="text-caption uppercase tracking-[0.06em] text-text-muted">{t('common.healthLabel')}</span>
        </div>
      </div>
    </section>
  )
}
