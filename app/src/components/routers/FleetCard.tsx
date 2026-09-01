import { Link } from 'react-router'
import { AlertTriangle, Cable, ChevronRight, Cpu, MemoryStick, Router as RouterIcon, Thermometer, Users, Wifi } from 'lucide-react'
import type { LucideIcon } from 'lucide-react'
import { motion, useReducedMotion } from 'framer-motion'
import { Area, AreaChart, ResponsiveContainer } from 'recharts'
import { useTranslation } from 'react-i18next'
import { fmtUptime } from '@/i18n'
import type { Router } from '@/data/mock'
import { HealthRing } from '@/components/HealthRing'
import { MetricBar } from '@/components/MetricBar'
import { Sparkline } from '@/components/Sparkline'
import { StatusPill } from '@/components/StatusPill'
import { AgentBadge } from '@/components/routers/AgentBadge'
import { useAgentFor } from '@/hooks/useAgentFor'
import { getRouterExtras } from '@/components/routers/routerExtras'
import { EMPTY_EXTRAS, useNetPulse } from '@/data/DataProvider'
import { cn } from '@/lib/utils'

function MetricRow({
  icon: Icon,
  label,
  value,
  pct,
  hot = false,
  title,
}: {
  icon: LucideIcon
  label: string
  value: string
  pct: number
  hot?: boolean
  title?: string
}) {
  return (
    <div
      className={cn('flex items-center gap-3', hot && 'rounded-lg bg-warn/10 px-2 py-1.5 -mx-2')}
      title={title}
    >
      <Icon className={cn('h-4 w-4 shrink-0', hot ? 'text-warn' : 'text-text-muted')} strokeWidth={1.75} />
      <span className={cn('w-28 shrink-0 whitespace-nowrap text-caption font-medium uppercase tracking-[0.04em]', hot ? 'text-warn' : 'text-text-muted')}>
        {label}
      </span>
      <MetricBar value={pct} className="min-w-0 flex-1" />
      <span className={cn('w-16 shrink-0 whitespace-nowrap text-right font-mono text-mono-sm', hot ? 'text-warn' : 'text-text-primary')}>
        {value}
      </span>
    </div>
  )
}

interface FleetCardProps {
  router: Router
  index?: number
  /** Cambia al pulsar "Actualizar": re-anima barras */
  refreshKey?: number
}

/** FleetCard grande de /routers (routers.md §②) */
export function FleetCard({ router, index = 0, refreshKey = 0 }: FleetCardProps) {
  const { t } = useTranslation()
  const { isDemo } = useNetPulse()
  const agent = useAgentFor(router.id)
  const extras = isDemo ? getRouterExtras(router.id) : EMPTY_EXTRAS
  const warn = router.status === 'warn'
  const isOpenWrt = router.type === undefined || router.type === '' || router.type === 'glinet' || router.type === 'openwrt'
  const hasMetrics = isOpenWrt
  const agentDown = isOpenWrt && agent !== undefined && !agent.fresh
  const agentMissing = isOpenWrt && agent === undefined && router.agentOnly
  const reduce = useReducedMotion()
  const isGateway = router.id === 'flint2'
  const traffic = router.sparkline.map((v, i) => ({ i, v }))

  return (
    <motion.article
      initial={reduce ? false : { opacity: 0, y: 24 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ type: 'spring', stiffness: 240, damping: 26, delay: index * 0.1 }}
      className="h-full"
    >
      <Link
        to={`/routers/${router.id}`}
        aria-label={t('common.viewDetail', { name: router.name })}
        className={cn(
          'group relative flex h-full flex-col overflow-hidden rounded-2xl border bg-surface p-6 transition-all duration-150',
          'hover:-translate-y-[3px] hover:border-accent/40',
          warn ? 'border-warn/50 shadow-glow-warn' : 'border-border',
        )}
      >
        {/* Banner interno de aviso (Patio) */}
        {warn && (
          <div className="mb-4 -mx-6 -mt-6 flex items-center gap-2 border-b border-warn/30 bg-warn/10 px-6 py-2.5">
            <AlertTriangle className="h-4 w-4 shrink-0 text-warn" strokeWidth={1.75} />
            <span className="text-caption font-semibold text-warn">
              {t('routers.highTemp', { temp: router.temp })}
            </span>
          </div>
        )}

        {/* Banner de agente caído (el router tiene agente registrado pero no responde) */}
        {agentDown && (
          <div className="mb-4 -mx-6 -mt-6 flex items-center gap-2 border-b border-warn/30 bg-warn/10 px-6 py-2.5">
            <AlertTriangle className="h-4 w-4 shrink-0 text-warn" strokeWidth={1.75} />
            <span className="text-caption font-semibold text-warn">{t('routers.agent.downBanner')}</span>
          </div>
        )}

        {/* Banner de agente no instalado (router agent-only sin agente registrado) */}
        {agentMissing && (
          <div className="mb-4 -mx-6 -mt-6 flex items-center gap-2 border-b border-danger/30 bg-danger/10 px-6 py-2.5">
            <AlertTriangle className="h-4 w-4 shrink-0 text-danger" strokeWidth={1.75} />
            <span className="text-caption font-semibold text-danger">{t('routers.agent.notInstalledBanner')}</span>
          </div>
        )}

        {/* Fila superior */}
        <div className="flex items-start justify-between gap-4">
          <div className="flex min-w-0 items-center gap-3.5">
            <motion.div
              layoutId={`router-tile-${router.id}`}
              className={cn(
                'flex h-11 w-11 shrink-0 items-center justify-center rounded-xl',
                isGateway
                  ? 'bg-gradient-to-br from-accent to-tunnel text-canvas'
                  : 'bg-accent-soft text-accent',
              )}
            >
              <RouterIcon className="h-[22px] w-[22px]" strokeWidth={1.75} />
            </motion.div>
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
                <h3 className="truncate font-display text-h2 text-text-primary">{router.name}</h3>
                <AgentBadge agent={agent} agentOnly={router.agentOnly} deviceType={router.type} />
              </div>
              <div className="truncate text-caption text-text-muted">
                {isGateway
                  ? 'GL.iNet Flint 2 · GL-MT6000'
                  : hasMetrics
                    ? `OpenWrt · ${router.modelShort}`
                    : router.modelShort}
              </div>
              {isGateway && (
                <div className="mt-1.5 inline-flex items-center gap-1.5 rounded-full bg-elevated px-2 py-0.5">
                  <span className="h-1.5 w-1.5 rounded-full bg-ok" />
                  <span className="text-[10px] font-semibold uppercase tracking-wide text-text-secondary">AdGuard</span>
                  <span className="h-1.5 w-1.5 rounded-full bg-tunnel" />
                  <span className="text-[10px] font-semibold uppercase tracking-wide text-text-secondary">WireGuard</span>
                </div>
              )}
            </div>
          </div>
          <div className="flex shrink-0 flex-col items-center gap-1.5">
            <motion.div layoutId={`router-ring-${router.id}`}>
              <HealthRing
                value={router.health}
                size={72}
                stroke={6}
                variant="status"
                status={router.status}
                delay={0.2 + index * 0.1}
                ariaLabel={t('common.healthOf', { name: router.name, health: router.health })}
                center={
                  <span className="kpi-value text-lg font-bold text-text-primary">{router.health}</span>
                }
              />
            </motion.div>
            <StatusPill
              tone={router.status === 'online' ? 'ok' : router.status === 'warn' ? 'warn' : 'danger'}
              label={t(`common.status.${router.status}`)}
              pulse={router.status !== 'online'}
            />
          </div>
        </div>

        {/* Fila identidad */}
        <div className="mt-4 grid grid-cols-2 gap-x-4 gap-y-1.5 rounded-xl bg-elevated/60 px-3.5 py-3">
          {[
            ['IP', router.ip],
            ['MAC', router.mac ?? extras.mac],
            ['Firmware', router.firmware ?? (extras.firmwareBase ? `${extras.firmware} / ${extras.firmwareBase}` : extras.firmware)],
            ['Uptime', fmtUptime(router.uptime)],
          ].map(([k, v]) => (
            <div key={k} className="min-w-0">
              <span className="text-[10px] font-medium uppercase tracking-[0.06em] text-text-muted">{k}</span>
              <div
                className="truncate font-mono text-mono-sm text-text-secondary"
                title={k === 'Firmware' && extras.firmwareAvailable ? t('routers.firmwareAvailable', { version: extras.firmwareAvailable }) : undefined}
              >
                {v}
                {k === 'Firmware' && router.firmwareOutdated && (
                  <span
                    className="ml-1.5 rounded-full bg-warn/10 px-1.5 py-0.5 text-[10px] font-semibold text-warn"
                    title={router.firmwareTarget ? t('routers.firmwareTargetHint', { target: router.firmwareTarget }) : undefined}
                  >
                    {t('routers.firmwareOutdated')}
                  </span>
                )}
                {k === 'Firmware' && extras.firmwareAvailable && (
                  <span className="ml-1.5 rounded-full bg-warn/10 px-1.5 py-0.5 text-[10px] font-semibold text-warn">
                    {extras.firmwareAvailable}
                  </span>
                )}
              </div>
            </div>
          ))}
        </div>

        {/* Métricas con barras */}
        {hasMetrics && (
          <div key={refreshKey} className="mt-4 space-y-2.5">
            <MetricRow icon={Cpu} label="CPU" value={`${router.cpu} %`} pct={router.cpu} hot={router.hotMetric === 'cpu'} />
            <MetricRow icon={MemoryStick} label={t('common.memory')} value={`${router.ram} %`} pct={router.ram} hot={router.hotMetric === 'ram'} />
            <MetricRow
              icon={Thermometer}
              label={t('common.temperature')}
              value={`${router.temp} °C`}
              pct={Math.min(100, (router.temp / 90) * 100)}
              hot={router.hotMetric === 'temp'}
              title={router.hotMetric === 'temp' ? t('routers.tempThreshold') : undefined}
            />
          </div>
        )}

        {/* Mini-gráfico de tráfico 24h */}
        <div className="mt-4">
          <div className="mb-1 text-[10px] font-medium uppercase tracking-[0.06em] text-text-muted">{t('routers.traffic24h')}</div>
          <div className="h-16" role="img" aria-label={t('routers.trafficAria', { name: router.name })}>
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={traffic} margin={{ top: 2, right: 0, bottom: 0, left: 0 }}>
                <defs>
                  <linearGradient id={`fleet-grad-${router.id}`} x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="#22D3EE" stopOpacity={0.25} />
                    <stop offset="100%" stopColor="#22D3EE" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <Area
                  type="monotone"
                  dataKey="v"
                  stroke="#22D3EE"
                  strokeWidth={1.75}
                  fill={`url(#fleet-grad-${router.id})`}
                  dot={false}
                  animationDuration={800}
                  animationEasing="ease-out"
                />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </div>

        {/* Fila clientes */}
        <div className="mt-4 flex flex-wrap items-center gap-2">
          <span className="inline-flex items-center gap-1.5 text-caption font-medium text-text-secondary">
            <Users className="h-3.5 w-3.5 text-text-muted" strokeWidth={1.75} />
            {t('common.clientsCount', { count: router.clients })}
          </span>
          {hasMetrics && (
            <>
              <span className="inline-flex items-center gap-1 rounded-full bg-elevated px-2 py-0.5 text-caption text-text-secondary">
                <Wifi className="h-3 w-3 text-text-muted" strokeWidth={1.75} /> 2.4 GHz · {extras.bandSplit.band24}
              </span>
              <span className="inline-flex items-center gap-1 rounded-full bg-elevated px-2 py-0.5 text-caption text-text-secondary">
                <Wifi className="h-3 w-3 text-text-muted" strokeWidth={1.75} /> 5 GHz · {extras.bandSplit.band5}
              </span>
            </>
          )}
          <span className="inline-flex items-center gap-1 rounded-full bg-elevated px-2 py-0.5 text-caption text-text-secondary">
            <Cable className="h-3 w-3 text-text-muted" strokeWidth={1.75} /> {t('common.cable')} · {extras.bandSplit.cable}
          </span>
        </div>

        {/* Footer */}
        <div className="mt-4 flex items-center justify-between gap-3 border-t border-border pt-3.5">
          {extras.gatewayLatencyMs !== undefined ? (
            <span className="flex items-center gap-2">
              <Sparkline data={extras.gatewayLatencySpark} width={72} height={20} color="#34D399" />
              <span className="font-mono text-caption text-text-muted" title={t('routers.gatewayLatency')}>
                {t('routers.msToGateway', { ms: extras.gatewayLatencyMs })}
              </span>
            </span>
          ) : (
            <span className="font-mono text-caption text-text-muted">{t('routers.mainGateway')}</span>
          )}
          <span className="inline-flex items-center gap-1 text-caption font-semibold text-accent">
            {t('common.viewDetailShort')}
            <ChevronRight className="h-3.5 w-3.5 transition-transform duration-150 group-hover:translate-x-0.5" strokeWidth={1.75} />
          </span>
        </div>
      </Link>
    </motion.article>
  )
}
