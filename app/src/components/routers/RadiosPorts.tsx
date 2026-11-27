import { Wifi } from 'lucide-react'
import { motion, useReducedMotion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import type { Router } from '@/data/mock'
import { SectionHeader } from '@/components/SectionHeader'
import { StatusPill } from '@/components/StatusPill'
import { PortPanel } from '@/components/routers/PortPanel'
import { getRouterExtras } from '@/components/routers/routerExtras'
import type { RouterExtras } from '@/components/routers/routerExtras'
import { EMPTY_EXTRAS, useNetPulse } from '@/data/DataProvider'
import { cn } from '@/lib/utils'

/** ⑤⑥ variante OpenWrt — Radios WiFi + Puertos (5/12, o 12/12 con chasis de 5+ bocas, #2180) (router-detail.md §Variante). */
export function RadiosPorts({ router, extras, snmpStats }: {
  router: Router
  extras?: RouterExtras
  snmpStats?: { ok?: number; fail?: number; lastErr?: string }
}) {
  const { t } = useTranslation()
  const { isDemo } = useNetPulse()
  const ex = extras ?? (isDemo ? getRouterExtras(router.id) : EMPTY_EXTRAS)
  const reduce = useReducedMotion()

  // Equipos cableados (switches gestionados tipo RTLPlayground/KP-9000):
  // sin radios no se muestra la sección WiFi ni estados vacíos (#291).
  const hasRadios = ex.radios.length > 0
  // #2180: cada boca del chasis ocupa 84 px más separación, así que con 5 o
  // más bocas el panel no cabe en 5/12 del ancho y aparecía scroll
  // horizontal. Se le da la fila completa (y a los radios también, para no
  // dejar un hueco de 5 columnas).
  const wideChassis = ex.ethPorts.filter((p) => !p.family).length > 4
  const radiosSpan = wideChassis ? 'lg:col-span-12' : 'lg:col-span-7'
  const portsSpan = hasRadios && !wideChassis ? 'lg:col-span-5' : 'lg:col-span-12'

  return (
    <>
      {/* Radios WiFi (solo si el equipo tiene: switches cableados fuera) */}
      {hasRadios && (
      <section className={cn('rounded-2xl border border-border bg-surface p-5 md:p-6', radiosSpan)}>
        <SectionHeader title={t('routerDetail.radios.title')} />
        <div className="mt-4 grid grid-cols-1 gap-3 sm:grid-cols-2">
          {ex.radios.map((radio, i) => (
            <motion.div
              key={radio.name}
              initial={reduce ? false : { opacity: 0, y: 12 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ duration: 0.3, ease: 'easeOut', delay: i * 0.08 }}
              className={cn(
                'rounded-xl border bg-elevated/60 p-4',
                radio.congested ? 'border-warn/40' : 'border-border',
              )}
            >
              <div className="flex items-center justify-between gap-2">
                <div className="flex items-center gap-2">
                  <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-accent-soft text-accent">
                    <Wifi className="h-4 w-4" strokeWidth={1.75} />
                  </span>
                  <span className="font-display text-sm font-semibold text-text-primary">{radio.name}</span>
                </div>
                {radio.congested ? (
                  <StatusPill tone="warn" label={t('routerDetail.radios.congested')} pulse />
                ) : (
                  <StatusPill tone="ok" label={t('routerDetail.radios.clear')} />
                )}
              </div>
              <dl className="mt-3 grid grid-cols-2 gap-x-3 gap-y-2">
                {[
                  [t('routerDetail.radios.channel'), String(radio.channel)],
                  [t('routerDetail.radios.width'), `${radio.widthMhz} MHz`],
                  [t('routerDetail.radios.power'), `${radio.powerDbm} dBm`],
                  [t('routers.colClients'), String(radio.clients)],
                  // #1213: el ruido solo se muestra si el driver lo reporta
                  ...(radio.noiseDbm != null ? [[t('routerDetail.radios.noise'), `${radio.noiseDbm} dBm`]] : []),
                  // #1206: el BSSID de la interfaz (la MAC que antes asomaba como cliente)
                  ...(radio.bssid ? [[t('routerDetail.radios.bssid'), radio.bssid]] : []),
                ].map(([k, v]) => (
                  <div key={k}>
                    <dt className="text-[10px] font-medium uppercase tracking-[0.06em] text-text-muted">{k}</dt>
                    <dd className="font-mono text-mono-sm text-text-primary">{v}</dd>
                  </div>
                ))}
              </dl>
            </motion.div>
          ))}
        </div>
      </section>
      )}

      {/* Puertos Ethernet (panel visual de bocas RJ45) */}
      <PortPanel router={router} extras={extras} snmpStats={snmpStats} className={portsSpan} />
    </>
  )
}
