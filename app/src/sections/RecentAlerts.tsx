import { useEffect, useRef, useState } from 'react'
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
  const { alerts, unreadAlerts, dismissAlert } = useNetPulse()
  const [readIds, setReadIds] = useState<Set<string>>(new Set())
  const [spinning, setSpinning] = useState(false)
  // Timer del spin del botón de recheck: en un ref para limpiarlo en unmount
  // (#227).
  const spinTimer = useRef<number | null>(null)
  useEffect(() => () => {
    if (spinTimer.current !== null) window.clearTimeout(spinTimer.current)
  }, [])
  const recent = alerts.slice(0, 4)
  // #916: toggle "solo no leídas" para mantener la tarjeta limpia en
  // operación normal. Persistente en localStorage (preferencia de vista,
  // no de negocio).
  const [unreadOnly, setUnreadOnly] = useState(() => {
    try {
      return localStorage.getItem('netpulse-recentalerts-unread-only') === '1'
    } catch {
      return false
    }
  })
  const toggleUnreadOnly = () => {
    setUnreadOnly((prev) => {
      const next = !prev
      try {
        localStorage.setItem('netpulse-recentalerts-unread-only', next ? '1' : '0')
      } catch {
        /* modo privado */
      }
      return next
    })
  }
  const visible = (unreadOnly ? alerts.filter((a) => !a.read && !readIds.has(a.id)) : recent).slice(0, 4)
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
        <button
          type="button"
          onClick={toggleUnreadOnly}
          aria-pressed={unreadOnly}
          className={cn(
            'rounded-lg border px-2 py-0.5 text-[11px] font-medium transition-colors',
            unreadOnly
              ? 'border-accent/40 bg-accent/15 text-accent'
              : 'border-border text-text-muted hover:text-text-secondary',
          )}
        >
          {t('home.recentAlerts.unreadOnly')}
        </button>
      </SectionHeader>
      <div className="-mx-3 flex-1 space-y-1">
        {visible.length === 0 && (
          <p className="px-3 py-6 text-center text-caption text-text-muted">{t('home.recentAlerts.allRead')}</p>
        )}
        {visible.map((a, i) => (
          <motion.div
            key={a.id}
            initial={{ opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.3, ease: 'easeOut', delay: 0.15 + i * 0.07 }}
          >
            <AlertItem
              alert={{ ...a, read: a.read || readIds.has(a.id) }}
              onClick={() => markReadAndGo(a.id)}
              onDismiss={dismissAlert}
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
            if (spinTimer.current !== null) window.clearTimeout(spinTimer.current)
            spinTimer.current = window.setTimeout(() => {
              spinTimer.current = null
              setSpinning(false)
            }, 450)
          }}
          className="flex h-7 w-7 items-center justify-center rounded-lg text-text-muted transition-colors hover:bg-hover hover:text-accent"
        >
          <RefreshCw className={cn('h-3.5 w-3.5 transition-transform duration-500', spinning && 'rotate-[360deg]')} strokeWidth={1.75} />
        </button>
      </div>
    </section>
  )
}
