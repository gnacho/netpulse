import { useMemo, useState } from 'react'
import { motion, useReducedMotion } from 'framer-motion'
import { useTranslation } from 'react-i18next'
import type { Router } from '@/data/mock'
import { fmtEs } from '@/data/mock'
import { useNetPulse } from '@/data/DataProvider'
import { DEVICE_ICONS, DeviceRow, SignalIcon } from '@/components/DeviceRow'
import { SectionHeader } from '@/components/SectionHeader'
import { cn } from '@/lib/utils'

const VISIBLE_COUNT = 6

/** ⑦ Clientes de este router (router-detail.md §⑦). */
export function RouterClients({ router }: { router: Router }) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)
  const reduce = useReducedMotion()
  const { devices } = useNetPulse()

  const clients = useMemo(
    () => devices.filter((d) => d.routerId === router.id && d.online),
    [devices, router.id],
  )
  const visible = expanded ? clients : clients.slice(0, VISIBLE_COUNT)
  const hiddenCount = clients.length - VISIBLE_COUNT

  return (
    <section className="rounded-2xl border border-border bg-surface p-5 md:p-6 lg:col-span-12">
      <SectionHeader
        title={t('routerDetail.clients.title', { count: router.clients })}
        linkTo="/devices"
        linkLabel={t('routerDetail.clients.viewAllDevices')}
      />

      {/* Desktop: tabla compacta */}
      <div className="mt-4 hidden overflow-x-auto md:block">
        <table className="w-full text-left text-sm">
          <thead>
            <tr className="border-b border-border">
              {[t('devices.colDevice'), 'IP', t('devices.colType'), t('devices.colBand'), t('devices.colSignal'), t('devices.colTraffic')].map((h, i) => (
                <th key={h} className={cn('pb-2.5 pr-3 text-label font-medium uppercase text-text-muted', i === 5 && 'text-right')}>
                  {h}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {visible.map((d, i) => {
              const Icon = DEVICE_ICONS[d.type]
              return (
                <motion.tr
                  key={d.id}
                  initial={reduce ? false : { opacity: 0, y: 8 }}
                  animate={{ opacity: 1, y: 0 }}
                  transition={{ duration: 0.25, delay: i * 0.04 }}
                  className="border-b border-border/60 last:border-0 hover:bg-hover"
                >
                  <td className="py-3 pr-3">
                    <div className="flex items-center gap-2.5">
                      <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-elevated text-accent">
                        <Icon className="h-4 w-4" strokeWidth={1.75} />
                      </span>
                      <div>
                        <div className="font-medium text-text-primary">{d.name}</div>
                        <div className="text-caption text-text-muted">{d.manufacturer}</div>
                      </div>
                    </div>
                  </td>
                  <td className="py-3 pr-3 font-mono text-mono-sm text-text-secondary">{d.ip}</td>
                  <td className="py-3 pr-3 capitalize text-text-secondary">{t(`devices.types.${d.type}`)}</td>
                  <td className="py-3 pr-3">
                    <span className="rounded-full bg-elevated px-2 py-0.5 font-mono text-caption text-text-secondary">{d.band}</span>
                  </td>
                  <td className="py-3 pr-3">
                    <span className="inline-flex items-center gap-1.5">
                      <SignalIcon dbm={d.signalDbm} />
                      {d.signalDbm !== null && (
                        <span className="font-mono text-mono-sm text-text-muted">{d.signalDbm} dBm</span>
                      )}
                    </span>
                  </td>
                  <td className="py-3 text-right font-mono text-mono-sm text-accent">
                    {d.trafficMbps >= 1 ? fmtEs(d.trafficMbps, 1) : fmtEs(d.trafficMbps, 2)} Mbps
                  </td>
                </motion.tr>
              )
            })}
          </tbody>
        </table>
      </div>

      {/* Móvil: DeviceRows */}
      <div className="mt-3 divide-y divide-border/60 md:hidden">
        {visible.map((d) => (
          <DeviceRow key={d.id} device={d} variant="compact" />
        ))}
      </div>

      {clients.length === 0 && (
        <p className="py-6 text-center text-sm text-text-secondary">
          {t('routerDetail.clients.empty')}
        </p>
      )}

      {hiddenCount > 0 && (
        <button
          onClick={() => setExpanded((v) => !v)}
          className="mt-3 w-full rounded-lg border border-border py-2 text-caption font-semibold text-text-secondary transition-colors hover:border-accent/40 hover:text-accent"
        >
          {expanded ? t('routerDetail.clients.showLess') : t('routerDetail.clients.showAll', { count: router.clients })}
        </button>
      )}
    </section>
  )
}
