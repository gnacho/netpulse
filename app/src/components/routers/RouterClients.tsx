import { useMemo, useState } from 'react'
import { motion, useReducedMotion } from 'framer-motion'
import { ArrowDown, ArrowUp, ArrowUpDown } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { Router } from '@/data/mock'
import { fmtEs } from '@/data/mock'
import { useNetPulse } from '@/data/DataProvider'
import { DEVICE_ICONS, DeviceRow, SignalIcon } from '@/components/DeviceRow'
import { SectionHeader } from '@/components/SectionHeader'
import { cn } from '@/lib/utils'

const VISIBLE_COUNT = 6

type SortKey = 'name' | 'ip' | 'type' | 'band' | 'signal' | 'traffic'

function ipNum(ip: string): number {
  return ip.split('.').reduce((acc, o) => acc * 256 + (parseInt(o, 10) || 0), 0)
}

/** ⑦ Clientes de este router (router-detail.md §⑦). */
export function RouterClients({ router }: { router: Router }) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)
  const [sort, setSort] = useState<{ key: SortKey; dir: 1 | -1 }>({ key: 'name', dir: 1 })
  const reduce = useReducedMotion()
  const { devices } = useNetPulse()

  const clients = useMemo(() => {
    const list = devices.filter((d) => d.routerId === router.id && d.online)
    const dir = sort.dir
    return [...list].sort((a, b) => {
      switch (sort.key) {
        case 'name':
          return dir * a.name.localeCompare(b.name)
        case 'ip':
          return dir * (ipNum(a.ip) - ipNum(b.ip))
        case 'type':
          return dir * a.type.localeCompare(b.type)
        case 'band':
          return dir * a.band.localeCompare(b.band)
        case 'signal':
          return dir * ((a.signalDbm ?? -999) - (b.signalDbm ?? -999))
        case 'traffic':
          return dir * (a.trafficMbps - b.trafficMbps)
      }
    })
  }, [devices, router.id, sort])
  const visible = expanded ? clients : clients.slice(0, VISIBLE_COUNT)
  const hiddenCount = clients.length - VISIBLE_COUNT

  const toggleSort = (key: SortKey) =>
    setSort((prev) => (prev.key === key ? { key, dir: prev.dir === 1 ? -1 : 1 } : { key, dir: 1 }))

  const Th = ({ label, k, right }: { label: string; k: SortKey; right?: boolean }) => {
    const active = sort.key === k
    const Icon = active ? (sort.dir === 1 ? ArrowUp : ArrowDown) : ArrowUpDown
    return (
      <th className={cn('pb-2.5 pr-3 text-label font-medium uppercase text-text-muted', right && 'text-right')}>
        <button
          type="button"
          onClick={() => toggleSort(k)}
          className={cn('inline-flex items-center gap-1 uppercase transition-colors', active ? 'text-accent' : 'hover:text-text-secondary')}
        >
          {label}
          <Icon className="h-3 w-3" strokeWidth={2} />
        </button>
      </th>
    )
  }

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
              <Th label={t('devices.colDevice')} k="name" />
              <Th label="IP" k="ip" />
              <Th label={t('devices.colType')} k="type" />
              <Th label={t('devices.colBand')} k="band" />
              <Th label={t('devices.colSignal')} k="signal" />
              <Th label={t('devices.colTraffic')} k="traffic" right />
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
