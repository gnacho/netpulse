import { Fragment, useState } from 'react'
import { ChevronDown, Network } from 'lucide-react'
import { motion, AnimatePresence, useReducedMotion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import type { Device, NetworkInfo } from '@/data/types'
import { SectionHeader } from '@/components/SectionHeader'
import { cn } from '@/lib/utils'

/** fmtBps: rate en humano ("12.4 Mbps" · "320 kbps" · "0"). */
function fmtBps(bps?: number): string {
  if (bps === undefined) return '-'
  if (bps >= 1e6) return `${(bps / 1e6).toFixed(1)} Mbps`
  if (bps >= 1e3) return `${(bps / 1e3).toFixed(0)} kbps`
  return `${bps.toFixed(0)} bps`
}

/**
 * NetworksPanel (#1889): la unidad tal como está configurada, una fila por red
 * de OpenWrt con su device, su subred, sus miembros y su tráfico, en lugar de
 * leerla solo como un chasis con bocas. Los dos modelos caben en la misma
 * tabla: bridge con VLAN filtering (br_switch.999) y subinterfaces (br-iot
 * alimentado por eth0.999).
 *
 * El tráfico es el del device de la red, no la suma de sus puertos, y una
 * unidad sin contadores muestra "-" en lugar de una cifra inventada. Los
 * clientes son los que tiene VLAN conocida; sin ese dato no se adivina.
 *
 * #1942: la atribucion la hace el server (puerto FDB, VLAN observada o
 * wifi-iface declarada en UCI) y llega como `clientMacs`. El contador se
 * despliega para listar nombre, IP y MAC de cada cliente de la red.
 */
export function NetworksPanel({ networks, clients }: { networks: NetworkInfo[]; clients: Device[] }) {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const [open, setOpen] = useState(true)
  const [expandedNet, setExpandedNet] = useState<string | null>(null)

  if (!networks || networks.length === 0) return null

  return (
    <section className="rounded-2xl border border-border bg-surface p-5 md:p-6 lg:col-span-12" data-testid="networks-panel">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center justify-between gap-3"
        aria-expanded={open}
        aria-label={t('routerDetail.networks.toggleAria', { count: networks.length })}
      >
        <SectionHeader title={t('routerDetail.networks.title')} />
        <div className="flex items-center gap-2">
          <span className="rounded-full bg-accent/10 px-2 py-0.5 font-mono text-caption font-semibold text-accent">
            {networks.length}
          </span>
          <ChevronDown
            className={cn('h-4 w-4 text-text-muted transition-transform duration-200', open && 'rotate-180')}
            strokeWidth={1.75}
          />
        </div>
      </button>

      <AnimatePresence initial={false}>
        {open && (
          <motion.div
            key="networks-body"
            initial={{ height: reduce ? 'auto' : 0, opacity: reduce ? 1 : 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={{ height: reduce ? 'auto' : 0, opacity: reduce ? 1 : 0 }}
            transition={{ duration: 0.2, ease: 'easeInOut' }}
            className="overflow-hidden"
          >
            <div className="mt-4 overflow-x-auto">
              <table className="w-full text-left text-sm">
                <thead>
                  <tr className="border-b border-border/60 text-[10px] font-semibold uppercase tracking-[0.06em] text-text-muted">
                    <th className="pb-2 pr-4">{t('routerDetail.networks.network')}</th>
                    <th className="pb-2 pr-4">{t('routerDetail.networks.device')}</th>
                    <th className="pb-2 pr-4">{t('routerDetail.networks.subnet')}</th>
                    <th className="pb-2 pr-4">{t('routerDetail.networks.members')}</th>
                    <th className="pb-2 pr-4">{t('routerDetail.networks.clients')}</th>
                    <th className="pb-2">{t('routerDetail.networks.traffic')}</th>
                  </tr>
                </thead>
                <tbody>
                  {networks.map((n) => {
                    const macs = n.clientMacs
                    const byMac = new Map(clients.map((c) => [c.mac.toUpperCase(), c]))
                    const expanded = expandedNet === n.name
                    const hasRate = n.rxBps !== undefined || n.txBps !== undefined
                    return (
                      <Fragment key={n.name}>
                      <tr className="border-b border-border/30 last:border-0" data-vlan-id={n.vlan || undefined}>
                        <td className="py-2 pr-4">
                          <span className="flex items-center gap-1.5 font-semibold text-text-primary">
                            <Network className="h-3.5 w-3.5 text-text-muted" strokeWidth={1.75} />
                            {n.name}
                          </span>
                          {n.vlan ? (
                            <span className="ml-[22px] font-mono text-[10px] text-text-muted">
                              {t('routerDetail.networks.vlan', { id: n.vlan })}
                            </span>
                          ) : null}
                        </td>
                        <td className="py-2 pr-4 font-mono text-mono-sm text-text-secondary">{n.device ?? '-'}</td>
                        <td className="py-2 pr-4 font-mono text-mono-sm text-text-secondary">{n.subnet ?? '-'}</td>
                        <td className="py-2 pr-4 font-mono text-mono-sm text-text-secondary">
                          {n.ports && n.ports.length > 0 ? n.ports.join(' ') : '-'}
                        </td>
                        <td className="py-2 pr-4" title={t('routerDetail.networks.clientsTip')}>
                          {macs && macs.length > 0 ? (
                            <button
                              type="button"
                              onClick={() => setExpandedNet(expanded ? null : n.name)}
                              aria-expanded={expanded}
                              aria-label={t(expanded ? 'routerDetail.networks.hideClients' : 'routerDetail.networks.showClients')}
                              className="flex items-center gap-1 font-mono text-mono-sm text-text-primary hover:text-accent"
                            >
                              {macs.length}
                              <ChevronDown className={cn('h-3 w-3 transition-transform duration-200', expanded && 'rotate-180')} strokeWidth={1.75} />
                            </button>
                          ) : (
                            <span className="text-text-muted">-</span>
                          )}
                        </td>
                        <td className="py-2" title={hasRate ? undefined : t('routerDetail.networks.noCounters')}>
                          {hasRate ? (
                            <span className="whitespace-nowrap font-mono text-mono-sm text-text-secondary">
                              ↓{fmtBps(n.rxBps)} ↑{fmtBps(n.txBps)}
                            </span>
                          ) : (
                            <span className="text-text-muted">-</span>
                          )}
                        </td>
                      </tr>
                      {expanded && macs ? (
                        <tr className="border-b border-border/30 last:border-0">
                          <td colSpan={6} className="pb-3 pl-[22px] pr-4">
                            <ul className="flex flex-wrap gap-x-5 gap-y-1.5">
                              {macs.map((mac) => {
                                const d = byMac.get(mac.toUpperCase())
                                return (
                                  <li key={mac} className="flex items-baseline gap-2 text-sm">
                                    <span className="font-medium text-text-primary">{d?.name || mac}</span>
                                    {d?.ip ? <span className="font-mono text-mono-sm text-text-secondary">{d.ip}</span> : null}
                                    <span className="font-mono text-[10px] text-text-muted">{mac}</span>
                                  </li>
                                )
                              })}
                            </ul>
                          </td>
                        </tr>
                      ) : null}
                      </Fragment>
                    )
                  })}
                </tbody>
              </table>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </section>
  )
}
