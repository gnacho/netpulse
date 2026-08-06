import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from 'react-router'
import { motion } from 'framer-motion'
import { RefreshCw } from 'lucide-react'
import { AlertItem } from '@/components/AlertItem'
import { SectionHeader } from '@/components/SectionHeader'
import { useNetPulse } from '@/data/DataProvider'
import { cn } from '@/lib/utils'

/** ⑥ Alertas recientes (home.md §⑥) */
export function RecentAlerts() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { alerts, unreadAlerts } = useNetPulse()
  const [readIds, setReadIds] = useState<Set<string>>(new Set())
  const [spinning, setSpinning] = useState(false)
  const recent = alerts.slice(0, 4)
  // El badge muestra el total de no leídas (como la campana), no solo de las 4 visibles
  const unread = Math.max(0, unreadAlerts - readIds.size)

  const markReadAndGo = (id: string) => {
    setReadIds((prev) => new Set(prev).add(id))
    navigate('/alerts')
  }

  return (
    <section className="flex h-full flex-col rounded-2xl border border-border bg-surface p-5">
      <SectionHeader title={t('nav.alerts')} linkTo="/alerts" linkLabel={t('common.viewAll')} className="mb-3">
        {unread > 0 && (
          <span className="flex h-5 min-w-5 items-center justify-center rounded-full bg-warn px-1.5 font-mono text-[11px] font-semibold text-canvas">
            {unread}
          </span>
        )}
      </SectionHeader>
      <div className="-mx-3 flex-1 space-y-1">
        {recent.map((a, i) => (
          <motion.div
            key={a.id}
            initial={{ opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.3, ease: 'easeOut', delay: 0.15 + i * 0.07 }}
          >
            <AlertItem
              alert={{ ...a, read: a.read || readIds.has(a.id) }}
              onClick={() => markReadAndGo(a.id)}
            />
          </motion.div>
        ))}
      </div>
      <div className="mt-3 flex items-center justify-between border-t border-border pt-3">
        <span className="text-caption text-text-muted">{t('home.recentAlerts.lastCheck')}</span>
        <button
          type="button"
          aria-label={t('home.recentAlerts.recheck')}
          onClick={() => {
            setSpinning(true)
            window.setTimeout(() => setSpinning(false), 450)
          }}
          className="flex h-7 w-7 items-center justify-center rounded-lg text-text-muted transition-colors hover:bg-hover hover:text-accent"
        >
          <RefreshCw className={cn('h-3.5 w-3.5 transition-transform duration-500', spinning && 'rotate-[360deg]')} strokeWidth={1.75} />
        </button>
      </div>
    </section>
  )
}
