import { motion, useReducedMotion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import { useState } from 'react'
import { Link } from 'react-router'
import type { EthPort, PortHealth } from '@/components/routers/routerExtras'
import { SectionHeader } from '@/components/SectionHeader'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'
import { ChevronDown, ChevronUp } from 'lucide-react'

function fmtPortBps(bps?: number): string {
  if (bps === undefined || Number.isNaN(bps)) return '-'
  if (bps >= 1e9) return `${(bps / 1e9).toFixed(1)} Gbps`
  if (bps >= 1e6) return `${(bps / 1e6).toFixed(1)} Mbps`
  if (bps >= 1e3) return `${Math.round(bps / 1e3)} kbps`
  return `${Math.round(bps)} bps`
}

function healthColor(health?: PortHealth, up?: boolean): string {
  if (!up) return 'bg-border-strong/40'
  if (!health) return 'bg-ok'
  if (health.score >= 85) return 'bg-ok'
  if (health.score >= 60) return 'bg-warn'
  return 'bg-error'
}

function healthGlow(health?: PortHealth, up?: boolean): string {
  if (!up) return ''
  if (!health) return 'shadow-[0_0_4px_theme(colors.ok/0.4)]'
  if (health.score >= 85) return 'shadow-[0_0_4px_theme(colors.ok/0.4)]'
  if (health.score >= 60) return 'shadow-[0_0_4px_theme(colors.warn/0.4)]'
  return 'shadow-[0_0_6px_theme(colors.error/0.5)]'
}

function speedLabel(speed?: string): string {
  if (!speed) return ''
  const s = speed.toLowerCase()
  if (s.includes('10 g') || s.includes('10g')) return '10G'
  if (s.includes('2.5 g') || s.includes('2.5g')) return '2.5G'
  if (s.includes('1 g') || s.includes('1g')) return '1G'
  if (s.includes('100 m') || s.includes('100m')) return '100M'
  if (s.includes('10 m') || s.includes('10m')) return '10M'
  return ''
}

function PortLed({ port, index }: { port: EthPort; index: number }) {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const ledColor = healthColor(port.health, port.up)
  const glow = healthGlow(port.health, port.up)
  const spd = speedLabel(port.speed)

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <motion.div
          initial={reduce ? false : { opacity: 0, scale: 0.8 }}
          animate={{ opacity: 1, scale: 1 }}
          transition={{ duration: 0.2, delay: index * 0.02 }}
          className="group flex w-[44px] shrink-0 flex-col items-center gap-0.5"
          role="img"
          aria-label={
            port.up
              ? t('switchPanel.ariaUsed', { label: port.label, device: port.connectedTo ?? t('switchPanel.unknownDevice') })
              : t('switchPanel.ariaFree', { label: port.label })
          }
        >
          <div className={cn('h-2.5 w-2.5 rounded-full transition-all duration-300', ledColor, glow)} />
          <div className="font-mono text-[9px] font-medium leading-none text-text-secondary">{port.label.replace(/\s+/g, '')}</div>
          {spd && <div className="font-mono text-[8px] leading-none text-text-muted">{spd}</div>}
        </motion.div>
      </TooltipTrigger>
      <TooltipContent
        side="top"
        align="center"
        sideOffset={8}
        className="w-60 border border-border-strong bg-elevated p-3 text-text-primary shadow-xl"
      >
        {port.up ? (
          <div>
            <div className="flex items-center justify-between gap-2">
              <span className="font-display text-sm font-semibold">{port.label}</span>
              {port.health && <HealthBadge health={port.health} />}
            </div>
            {port.connectedTo && (
              <div className="mt-1.5 text-caption font-medium text-text-primary">
                {port.connectedTo === port.label && port.deviceMac ? (
                  <span className="font-mono text-[10px] text-text-muted">{port.deviceMac}</span>
                ) : port.deviceMac ? (
                  <Link
                    to={`/devices?q=${encodeURIComponent(port.deviceMac)}`}
                    className="transition-colors hover:text-accent hover:underline"
                    onClick={(e) => e.stopPropagation()}
                  >
                    {port.connectedTo}
                  </Link>
                ) : (
                  port.connectedTo
                )}
              </div>
            )}
            <div className="mt-2 grid grid-cols-2 gap-1.5">
              {port.speed && (
                <div className="rounded-lg bg-canvas/60 px-2 py-1">
                  <div className="text-caption uppercase tracking-wider text-text-muted">{t('switchPanel.speed')}</div>
                  <div className="font-mono text-mono-sm text-text-primary">{port.speed}</div>
                </div>
              )}
              {port.deviceMac && (
                <div className="rounded-lg bg-canvas/60 px-2 py-1">
                  <div className="text-caption uppercase tracking-wider text-text-muted">MAC</div>
                  <div className="truncate font-mono text-mono-sm text-text-primary">{port.deviceMac}</div>
                </div>
              )}
              {(port.rxBps !== undefined || port.txBps !== undefined) && (
                <div className="col-span-2 rounded-lg bg-canvas/60 px-2 py-1">
                  <div className="text-caption uppercase tracking-wider text-text-muted">{t('switchPanel.traffic')}</div>
                  <div className="font-mono text-mono-sm text-text-primary">
                    ↓ {fmtPortBps(port.rxBps)} / ↑ {fmtPortBps(port.txBps)}
                  </div>
                </div>
              )}
              {(port.rxErrors ?? 0) + (port.txErrors ?? 0) > 0 && (
                <div className="col-span-2 rounded-lg bg-canvas/60 px-2 py-1">
                  <div className="text-caption uppercase tracking-wider text-text-muted">{t('switchPanel.errors')}</div>
                  <div className="font-mono text-mono-sm text-warn">
                    RX {port.rxErrors ?? 0} / TX {port.txErrors ?? 0}
                  </div>
                </div>
              )}
            </div>
            {port.health && port.health.breakdown.length > 0 && (
              <div className="mt-2 border-t border-border/40 pt-2">
                <div className="text-caption uppercase tracking-wider text-text-muted">{t('switchPanel.healthBreakdown')}</div>
                <div className="mt-1 flex flex-wrap gap-1">
                  {port.health.breakdown.map((item) => (
                    <span
                      key={item.signal}
                      className={cn(
                        'rounded px-1.5 py-0.5 text-[10px] font-medium',
                        item.status === 'ok' && 'bg-ok/15 text-ok',
                        item.status === 'warn' && 'bg-warn/15 text-warn',
                        item.status === 'crit' && 'bg-error/15 text-error',
                      )}
                    >
                      {t(`switchPanel.signals.${item.signal}`, item.signal)}
                    </span>
                  ))}
                </div>
              </div>
            )}
          </div>
        ) : (
          <div className="flex items-center justify-between">
            <span className="font-display text-sm font-semibold">{port.label}</span>
            <span className="text-caption text-text-muted">{t('switchPanel.free')}</span>
          </div>
        )}
      </TooltipContent>
    </Tooltip>
  )
}

function HealthBadge({ health }: { health: PortHealth }) {
  const color =
    health.score >= 85
      ? 'bg-ok/15 text-ok'
      : health.score >= 60
        ? 'bg-warn/15 text-warn'
        : 'bg-error/15 text-error'
  return (
    <span className={cn('rounded-full px-2 py-0.5 font-mono text-[10px] font-bold', color)}>
      {health.score}
    </span>
  )
}

export function SwitchFrontPanel({
  ports,
  className,
}: {
  ports: EthPort[]
  className?: string
}) {
  const { t } = useTranslation()
  const [collapsed, setCollapsed] = useState(false)
  const lanPorts = ports.filter((p) => p.id !== 'wan')
  const portCount = lanPorts.length
  const used = lanPorts.filter((p) => p.up).length
  const healthy = lanPorts.filter((p) => p.up && p.health && p.health.score >= 85).length
  const degraded = lanPorts.filter((p) => p.up && p.health && p.health.score < 85 && p.health.score >= 60).length
  const critical = lanPorts.filter((p) => p.up && p.health && p.health.score < 60).length

  if (portCount < 8) return null

  return (
    <section className={cn('rounded-2xl border border-border bg-surface p-5 md:p-6', className)}>
      <div className="flex items-center justify-between">
        <div>
          <SectionHeader title={t('switchPanel.title')} />
          <p className="mt-1 text-caption text-text-muted">
            {t('switchPanel.summary', { used, total: portCount })}
            {healthy > 0 && ` · ${t('switchPanel.healthy', { count: healthy })}`}
            {degraded > 0 && ` · ${t('switchPanel.degraded', { count: degraded })}`}
            {critical > 0 && ` · ${t('switchPanel.critical', { count: critical })}`}
          </p>
        </div>
        <button
          type="button"
          onClick={() => setCollapsed((v) => !v)}
          className="rounded-lg p-2 text-text-muted transition-colors hover:bg-elevated hover:text-text-primary md:hidden"
          aria-label={collapsed ? t('switchPanel.expand') : t('switchPanel.collapse')}
        >
          {collapsed ? <ChevronDown className="h-4 w-4" /> : <ChevronUp className="h-4 w-4" />}
        </button>
      </div>

      {!collapsed && (
        <div className="mt-4 rounded-xl border border-border/70 bg-elevated/40 px-3 py-3">
          <div className="flex flex-wrap items-end gap-x-1 gap-y-2">
            {lanPorts.map((p, i) => (
              <PortLed key={p.id} port={p} index={i} />
            ))}
          </div>
        </div>
      )}

      {!collapsed && (
        <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-caption text-text-muted">
          <span className="inline-flex items-center gap-1.5">
            <span className="h-2 w-2 rounded-full bg-ok" /> {t('switchPanel.legendHealthy')}
          </span>
          <span className="inline-flex items-center gap-1.5">
            <span className="h-2 w-2 rounded-full bg-warn" /> {t('switchPanel.legendDegraded')}
          </span>
          <span className="inline-flex items-center gap-1.5">
            <span className="h-2 w-2 rounded-full bg-error" /> {t('switchPanel.legendCritical')}
          </span>
          <span className="inline-flex items-center gap-1.5">
            <span className="h-2 w-2 rounded-full bg-border-strong/40" /> {t('switchPanel.legendFree')}
          </span>
        </div>
      )}
    </section>
  )
}
