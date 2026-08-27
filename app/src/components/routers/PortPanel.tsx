import { motion, useReducedMotion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import type { Router } from '@/data/mock'
import type { WanInfo } from '@/data/types'
import { SectionHeader } from '@/components/SectionHeader'
import { getRouterExtras } from '@/components/routers/routerExtras'
import type { EthPort, RouterExtras } from '@/components/routers/routerExtras'
import { EMPTY_EXTRAS, useNetPulse } from '@/data/DataProvider'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { cn } from '@/lib/utils'

/**
 * Panel visual de puertos Ethernet — dibuja las bocas RJ45 del chasis con
 * LEDs de link/actividad, ocupación y qué dispositivo hay en cada boca.
 * Variante compacta (APs, col-span-5) y ancha (gateway, col-span-12).
 */

function Jack({ port, index, wan }: { port: EthPort; index: number; wan?: WanInfo }) {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const isWan = port.id === 'wan'

  const aria = port.up
    ? t('routerDetail.ports.ariaUsed', { label: port.label, connectedTo: port.connectedTo ?? t('routerDetail.ports.unknownDevice'), speed: port.speed ? `, ${port.speed}` : '' })
    : t('routerDetail.ports.ariaFree', { label: port.label })

  const hasWanInfo = isWan && wan && (wan.proto || wan.publicIp || wan.gateway || (wan.dns?.length ?? 0) > 0)

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <motion.div
          initial={reduce ? false : { opacity: 0, y: 10 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.3, ease: 'easeOut', delay: 0.08 + index * 0.06 }}
          className="group flex w-[84px] shrink-0 flex-col items-center"
          role="img"
          aria-label={aria}
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
                  {port.connectedTo && port.connectedTo === port.label ? (
                    <span className="font-mono text-[10px] text-text-muted">{port.deviceMac ?? ''}</span>
                  ) : port.connectedTo && port.deviceMac ? (
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
      </TooltipTrigger>
      <TooltipContent
        side="top"
        align="center"
        sideOffset={6}
        className="w-56 border border-border-strong bg-elevated text-text-primary shadow-xl"
      >
        {!port.up ? (
          <span className="text-caption text-text-muted">{t('routerDetail.ports.freeLabel', { label: port.label })}</span>
        ) : (
          <div>
            {/* Cabecera: etiqueta + estado */}
            <div className="flex items-center justify-between gap-2">
              <span className="font-display text-sm font-semibold text-text-primary">{port.label}</span>
              <span className="flex items-center gap-1.5 text-caption text-text-secondary">
                <span className="h-1.5 w-1.5 rounded-full bg-ok" aria-hidden="true" />
                {port.speed ?? t('routerDetail.ports.inUse')}
              </span>
            </div>

            {isWan && hasWanInfo ? (
              /* Boca WAN: conexión a Internet */
              <div className="mt-1 text-caption leading-snug text-text-secondary">
                {port.connectedTo && <div>{port.connectedTo}</div>}
                {wan!.proto && <div className="mt-0.5 font-mono text-caption text-accent">{wan!.proto.toUpperCase()}</div>}
              </div>
            ) : (
              /* Boca LAN: dispositivo conectado */
              port.connectedTo && (
                <div className="mt-1 text-caption font-medium text-text-primary">
                  {port.connectedTo}
                </div>
              )
            )}

            {isWan && hasWanInfo && wan ? (
              <div className="mt-2 grid grid-cols-2 gap-1.5">
                {wan.publicIp && <MiniStat label={t('routerDetail.ports.wanPublicIp')} value={wan.publicIp} />}
                {wan.gateway && <MiniStat label={t('routerDetail.ports.wanGateway')} value={wan.gateway} />}
                {wan.dns && wan.dns.length > 0 && (
                  <div className="col-span-2">
                    <MiniStat label={t('routerDetail.ports.wanDns')} value={wan.dns.join(' · ')} />
                  </div>
                )}
              </div>
            ) : (
              port.deviceMac && (
                <div className="mt-2 grid grid-cols-2 gap-1.5">
                  <MiniStat label="MAC" value={port.deviceMac} />
                  {port.detail && <MiniStat label={t('routerDetail.ports.deviceDetail')} value={port.detail} />}
                </div>
              )
            )}
          </div>
        )}
      </TooltipContent>
    </Tooltip>
  )
}

/** MiniStat: celda de dato del tooltip (mismo patrón que la topología). */
function MiniStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-lg bg-canvas/60 px-2 py-1.5">
      <div className="text-caption uppercase tracking-[0.06em] text-text-muted">{label}</div>
      <div className="truncate font-mono text-mono-sm text-text-primary" title={value}>
        {value}
      </div>
    </div>
  )
}

export function PortPanel({ router, extras, className }: { router: Router; extras?: RouterExtras; className?: string }) {
  const { t } = useTranslation()
  const { isDemo, wan } = useNetPulse()
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

      {/* Chasis: TODAS las bocas en una sola horizontal (sin wrap); si no
          caben (móvil, chasis largos), scroll horizontal en vez de apilar. */}
      <div className="mt-4 flex flex-nowrap items-start gap-x-4 overflow-x-auto rounded-xl border border-border/70 bg-elevated/40 px-4 py-4">
        {wanPorts.map((p, i) => (
          <Jack key={p.id} port={p} index={i} wan={wan} />
        ))}
        {wanPorts.length > 0 && lanPorts.length > 0 && (
          <div className="mx-1 h-[104px] w-px shrink-0 self-center bg-border/70" aria-hidden="true" />
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
