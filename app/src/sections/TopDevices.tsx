import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'
import { motion } from 'framer-motion'
import { DeviceRow } from '@/components/DeviceRow'
import { SectionHeader } from '@/components/SectionHeader'
import { useNetPulse } from '@/data/DataProvider'

const TOP_IDS = ['imac-salon', 'tv-samsung', 'ps5', 'pixel-8-pro', 'macbook-air', 'nas-synology']

/** ⑤ Top dispositivos (home.md §⑤) */
export function TopDevices() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { devices, deviceTotals, topDevices } = useNetPulse()
  const top = useMemo(() => {
    const known = TOP_IDS.map((id) => devices.find((d) => d.id === id)).filter((d) => d !== undefined)
    // Con backend live los ids del canon pueden no existir: cae al top del bundle
    const list = known.length > 0 ? known : topDevices
    return [...list].sort((a, b) => b.trafficMbps - a.trafficMbps)
  }, [devices, topDevices])
  const newDevice = devices.find((d) => d.isNew)

  return (
    <section className="rounded-2xl border border-border bg-surface p-5">
      <SectionHeader title={t('home.topDevices.title')} linkTo="/devices" linkLabel={t('common.viewAll')} className="mb-3">
        <span className="rounded-full bg-elevated px-2.5 py-1 text-caption font-medium text-text-secondary">
          {t('home.topDevices.summary', { total: deviceTotals.total, newToday: deviceTotals.newToday })}
        </span>
      </SectionHeader>
      <div className="-mx-3">
        {top.map((d, i) => (
          <motion.div
            key={d.id}
            initial={{ opacity: 0, x: -12 }}
            animate={{ opacity: 1, x: 0 }}
            transition={{ duration: 0.3, ease: 'easeOut', delay: 0.15 + i * 0.05 }}
          >
            <DeviceRow device={d} onClick={() => navigate('/devices')} />
          </motion.div>
        ))}
        {newDevice && (
          <motion.div
            initial={{ opacity: 0, scale: 0.98 }}
            animate={{ opacity: 1, scale: 1 }}
            transition={{ type: 'spring', stiffness: 300, damping: 22, delay: 0.15 + top.length * 0.05 }}
            className="mt-1 rounded-xl border border-accent/20 bg-accent-soft/40"
          >
            <DeviceRow device={newDevice} onClick={() => navigate('/devices')} />
          </motion.div>
        )}
      </div>
    </section>
  )
}
