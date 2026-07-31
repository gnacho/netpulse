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

/** ⑤⑥ variante OpenWrt — Radios WiFi (7) + Puertos (5) (router-detail.md §Variante). */
export function RadiosPorts({ router, extras }: { router: Router; extras?: RouterExtras }) {
  const { t } = useTranslation()
  const { isDemo } = useNetPulse()
  const ex = extras ?? (isDemo ? getRouterExtras(router.id) : EMPTY_EXTRAS)
  const reduce = useReducedMotion()

  return (
    <>
      {/* Radios WiFi */}
      <section className="rounded-2xl border border-border bg-surface p-5 md:p-6 lg:col-span-7">
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

      {/* Puertos Ethernet (panel visual de bocas RJ45) */}
      <PortPanel router={router} extras={extras} className="lg:col-span-5" />
    </>
  )
}
