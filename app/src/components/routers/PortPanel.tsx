import { motion, useReducedMotion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import type { Router } from '@/data/mock'
import { SectionHeader } from '@/components/SectionHeader'
import { getRouterExtras } from '@/components/routers/routerExtras'
import type { EthPort, RouterExtras } from '@/components/routers/routerExtras'
import { EMPTY_EXTRAS, useNetPulse } from '@/data/DataProvider'
import { cn } from '@/lib/utils'

/**
 * Panel visual de puertos Ethernet — dibuja las bocas RJ45 del chasis con
 * LEDs de link/actividad, ocupación y qué dispositivo hay en cada boca.
 * Variante compacta (APs, col-span-5) y ancha (gateway, col-span-12).
 */

function Jack({ port, index }: { port: EthPort; index: number }) {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const isWan = port.id === 'wan'

  const aria = port.up
    ? t('routerDetail.ports.ariaUsed', { label: port.label, connectedTo: port.connectedTo ?? t('routerDetail.ports.unknownDevice'), speed: port.speed ? `, ${port.speed}` : '' })
    : t('routerDetail.ports.ariaFree', { label: port.label })

  return (
    <motion.div
      initial={reduce ? false : { opacity: 0, y: 10 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.3, ease: 'easeOut', delay: 0.08 + index * 0.06 }}
      className="group flex w-[84px] flex-col items-center"
      role="img"
      aria-label={aria}
      title={port.up ? `${port.connectedTo ?? ''}${port.detail ? ` — ${port.detail}` : ''}` : t('routerDetail.ports.freeLabel', { label: port.label })}
    >
      {/* LEDs link / act */}
      <div className="mb-1 flex w-9 items-center justify-between">
        <span className={cn('h-1.5 w-1.5 rounded-full', port.up ? 'bg-ok' : 'bg-border-strong/50')} />
        <span
          className={cn(
            'h-1.5 w-1.5 rounded-full',
            port.up ? 'bg-ok' : 'bg-border-strong/50',
            port.up && !reduce && 'animate-pulse',
          )}
        />
      </div>

      {/* Cuerpo del conector */}
      <div
        className={cn(
          'relative h-12 w-12 rounded-md border-2 transition-all duration-150 group-hover:-translate-y-0.5',
          port.up && isWan && 'border-accent/60 bg-canvas shadow-glow-accent',
          port.up && !isWan && 'border-ok/50 bg-canvas',
          !port.up && 'border-border bg-canvas/50',
        )}
      >
        {/* Pines dorados */}
        <div className="absolute inset-x-[7px] top-[5px] flex justify-between">
          {Array.from({ length: 8 }).map((_, i) => (
            <span
              key={i}
              className={cn(
                'h-2.5 w-[2px] rounded-full',
                port.up ? 'bg-warn/80' : 'bg-border-strong/50',
              )}
            />
          ))}
        </div>
        {/* Apertura */}
        <div
          className={cn(
            'absolute inset-x-[6px] bottom-[5px] h-[18px] rounded-[3px] border',
            port.up ? 'border-border/80 bg-black/50' : 'border-border/50 bg-black/25',
          )}
        />
      </div>

      {/* Etiquetas */}
      <div className="mt-1.5 w-full text-center">
        <div className={cn('font-mono text-[10px] font-semibold tracking-wide', isWan ? 'text-accent' : 'text-text-secondary')}>
          {port.label}
        </div>
        {port.up ? (
          <>
            <div className="mt-0.5 w-full truncate text-caption font-medium text-text-primary">
              {port.connectedTo && port.deviceMac ? (
                <Link
                  to={`/devices?q=${encodeURIComponent(port.deviceMac)}`}
                  className="transition-colors hover:text-accent hover:underline"
                  onClick={(e) => e.stopPropagation()}
                >
                  {port.connectedTo}
                </Link>
              ) : port.connectedTo ? (
                port.connectedTo
              ) : (
                t('routerDetail.ports.inUse')
              )}
            </div>
            {port.speed && <div className="font-mono text-[10px] text-text-muted">{port.speed}</div>}
          </>
        ) : (
          <div className="mt-0.5 text-caption text-text-muted">{t('routerDetail.ports.free')}</div>
        )}
      </div>
    </motion.div>
  )
}

export function PortPanel({ router, extras, className }: { router: Router; extras?: RouterExtras; className?: string }) {
  const { t } = useTranslation()
  const { isDemo } = useNetPulse()
  const ex = extras ?? (isDemo ? getRouterExtras(router.id) : EMPTY_EXTRAS)
  const ports = ex.ethPorts
  const used = ports.filter((p) => p.up).length
  const wirelessUplink = ex.backhaul?.kind === 'wireless'

  // Separador visual tras el puerto WAN
  const wanPorts = ports.filter((p) => p.id === 'wan')
  const lanPorts = ports.filter((p) => p.id !== 'wan')

  return (
    <section className={cn('rounded-2xl border border-border bg-surface p-5 md:p-6', className)}>
      <SectionHeader title={t('routerDetail.ports.title')} />
      <p className="mt-1 text-caption text-text-muted">
        {t('routerDetail.ports.usedOf', { used, total: ports.length, count: ports.length })}
        {wirelessUplink && t('routerDetail.ports.wirelessUplink')}
      </p>

      {/* Chasis */}
      <div className="mt-4 flex flex-wrap items-start gap-x-4 gap-y-5 rounded-xl border border-border/70 bg-elevated/40 px-4 py-4">
        {wanPorts.map((p, i) => (
          <Jack key={p.id} port={p} index={i} />
        ))}
        {wanPorts.length > 0 && lanPorts.length > 0 && (
          <div className="mx-1 hidden h-[104px] w-px self-center bg-border/70 sm:block" aria-hidden="true" />
        )}
        {lanPorts.map((p, i) => (
          <Jack key={p.id} port={p} index={wanPorts.length + i} />
        ))}
      </div>

      {/* Leyenda */}
      <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-caption text-text-muted">
        <span className="inline-flex items-center gap-1.5">
          <span className="h-1.5 w-1.5 rounded-full bg-ok" /> {t('routerDetail.ports.inUse')}
        </span>
        <span className="inline-flex items-center gap-1.5">
          <span className="h-1.5 w-1.5 rounded-full bg-border-strong/50" /> {t('routerDetail.ports.free')}
        </span>
        {ports.some((p) => p.id === 'wan' && p.up) && (
          <span className="inline-flex items-center gap-1.5">
            <span className="font-mono text-[10px] font-semibold text-accent">WAN</span> {t('routerDetail.ports.internetLink')}
          </span>
        )}
      </div>
    </section>
  )
}
