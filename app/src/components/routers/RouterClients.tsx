import { useEffect, useMemo, useState } from 'react'
import { motion, useReducedMotion } from 'framer-motion'
import { ArrowDown, ArrowUp, ArrowUpDown, SquarePen } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { manufacturerLabel } from '@/i18n'
import type { Router } from '@/data/mock'
import { fmtEs } from '@/data/mock'
import { useNetPulse } from '@/data/DataProvider'
import { DEVICE_ICONS, DeviceRow, SignalIcon } from '@/components/DeviceRow'
import { DeviceEditSheet } from '@/components/DeviceEditSheet'
import { SectionHeader } from '@/components/SectionHeader'
import { buildClientDevices } from '@/pages/devices-data'
import type { ClientDevice } from '@/pages/devices-data'
import { cn, fetchJson } from '@/lib/utils'

const VISIBLE_COUNT = 6

type SortKey = 'name' | 'ip' | 'type' | 'band' | 'signal' | 'traffic'

function ipNum(ip: string): number {
  return ip.split('.').reduce((acc, o) => acc * 256 + (parseInt(o, 10) || 0), 0)
}

interface LocalOverride {
  icon?: string
  name?: string
  type?: string
}

/** ⑦ Clientes de este router (router-detail.md §⑦). */
export function RouterClients({ router }: { router: Router }) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)
  const [sort, setSort] = useState<{ key: SortKey; dir: 1 | -1 }>({ key: 'name', dir: 1 })
  const reduce = useReducedMotion()
  const { devices, isDemo, refresh } = useNetPulse()
  // #827: edición inline desde la lista. En demo el override es local (el
  // sheet lo indica); en live se persiste en el server y se refresca.
  const [overrides, setOverrides] = useState<Record<string, LocalOverride>>({})
  const [editingId, setEditingId] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [toast, setToast] = useState<string | null>(null)

  useEffect(() => {
    if (!toast) return
    const id = window.setTimeout(() => setToast(null), 2200)
    return () => window.clearTimeout(id)
  }, [toast])

  const clients = useMemo(() => {
    const list = buildClientDevices(devices, isDemo)
      .filter((d) => d.routerId === router.id && d.online)
      .map((d) => {
        const ov = overrides[d.id]
        if (!ov) return d
        return {
          ...d,
          iconOverride: ov.icon || d.iconOverride,
          name: ov.name || d.name,
          type: (ov.type || d.type) as ClientDevice['type'],
          nameOverride: ov.name || d.nameOverride,
          typeOverride: ov.type || d.typeOverride,
        }
      })
    // #827: los sin identificar (nombre = MAC) son los accionables: se
    // muestran SIEMPRE, fijos en los primeros puestos de la sección.
    const unknown = list.filter((d) => d.name === d.mac)
    const known = list.filter((d) => d.name !== d.mac)
    const dir = sort.dir
    known.sort((a, b) => {
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
    return [...unknown, ...known]
  }, [devices, router.id, sort, isDemo, overrides])
  const visible = expanded ? clients : clients.slice(0, VISIBLE_COUNT)
  const hiddenCount = clients.length - VISIBLE_COUNT

  const toggleSort = (key: SortKey) =>
    setSort((prev) => (prev.key === key ? { key, dir: prev.dir === 1 ? -1 : 1 } : { key, dir: 1 }))

  const handleEditSave = async (device: ClientDevice, patch: { icon: string; name: string; type: string }) => {
    if (isDemo) {
      setOverrides((prev) => ({ ...prev, [device.id]: { icon: patch.icon, name: patch.name, type: patch.type } }))
      setEditingId(null)
      setToast(t('devices.edit.saved'))
      return
    }
    setSaving(true)
    const res = await fetchJson(`/api/devices/${encodeURIComponent(device.mac)}/override`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ icon: patch.icon, name: patch.name, type: patch.type }),
    })
    setSaving(false)
    if (!res.ok) {
      setToast(t('devices.edit.saveError'))
      return
    }
    setEditingId(null)
    setToast(t('devices.edit.saved'))
    refresh()
  }

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
        linkLabel={t('routerDetail.clients.viewAllClients')}
      />

      {/* Desktop: tabla compacta */}
      <div className="mt-4 hidden overflow-x-auto md:block">
        <table className="w-full text-left text-sm">
          <thead>
            <tr className="border-b border-border">
              <Th label={t('routerDetail.clients.colClient')} k="name" />
              <Th label="IP" k="ip" />
              <Th label={t('devices.colType')} k="type" />
              <Th label={t('devices.colBand')} k="band" />
              <Th label={t('devices.colSignal')} k="signal" />
              <Th label={t('devices.colTraffic')} k="traffic" right />
              {/* #827: columna de acción (editar inline) */}
              <th className="w-8 pb-2.5" aria-label={t('devices.edit.action')} />
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
                        <div className="font-medium text-text-primary" translate="no">{d.name}</div>
                        <div className="text-caption text-text-muted">{manufacturerLabel(d.manufacturer)}</div>
                      </div>
                    </div>
                  </td>
                  <td className="py-3 pr-3 font-mono text-mono-sm text-text-secondary" translate="no">{d.ip}</td>
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
                  <td className="py-3 text-right">
                    <button
                      type="button"
                      onClick={() => setEditingId(d.id)}
                      aria-label={t('devices.edit.action')}
                      title={t('devices.edit.action')}
                      className="rounded-md p-1.5 text-text-muted transition-colors hover:bg-hover hover:text-accent"
                    >
                      <SquarePen className="h-4 w-4" strokeWidth={1.75} />
                    </button>
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

      <DeviceEditSheet
        open={editingId !== null}
        device={clients.find((d) => d.id === editingId) ?? null}
        isDemo={isDemo}
        saving={saving}
        onClose={() => setEditingId(null)}
        onSave={handleEditSave}
      />

      {toast && (
        <div className="fixed bottom-6 left-1/2 z-50 -translate-x-1/2 rounded-full border border-border-strong bg-elevated px-4 py-2 text-sm font-medium text-text-primary shadow-lg" role="status">
          {toast}
        </div>
      )}
    </section>
  )
}
