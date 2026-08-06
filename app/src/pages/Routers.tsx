import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { RefreshCw } from 'lucide-react'
import { motion, useReducedMotion } from 'framer-motion'
import { useNetPulse } from '@/data/DataProvider'
import { FleetCard } from '@/components/routers/FleetCard'
import { FleetTable } from '@/components/routers/FleetTable'
import { cn } from '@/lib/utils'

/** Página `/routers` — vista de flota (routers.md) */
export default function Routers() {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const { routers } = useNetPulse()
  const [refreshKey, setRefreshKey] = useState(0)
  const [spinning, setSpinning] = useState(false)

  function handleRefresh() {
    setRefreshKey((k) => k + 1)
    if (reduce) return
    setSpinning(true)
    window.setTimeout(() => setSpinning(false), 650)
  }

  const online = routers.filter((r) => r.status === 'online').length
  const warned = routers.filter((r) => r.status === 'warn').length

  return (
    <div className="space-y-4 md:space-y-5">
      {/* ① Page header */}
      <header>
        <motion.nav
          initial={reduce ? false : { opacity: 0, y: 12 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.25, ease: 'easeOut' }}
          aria-label={t('common.breadcrumb')}
          className="font-mono text-caption text-text-muted"
        >
          <Link to="/" className="transition-colors hover:text-accent">{t('common.home')}</Link>
          <span className="mx-1.5">/</span>
          <span className="text-text-secondary">{t('nav.routers')}</span>
        </motion.nav>
        <div className="mt-1.5 flex flex-wrap items-end justify-between gap-x-4 gap-y-3">
          <motion.div
            initial={reduce ? false : { opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.25, ease: 'easeOut', delay: 0.06 }}
          >
            <h1 className="font-display text-h1 text-text-primary">{t('nav.routers')}</h1>
            <p className="text-caption text-text-muted">
              {t('routers.summary', { total: routers.length, online, warnings: warned })}
            </p>
          </motion.div>
          <motion.div
            initial={reduce ? false : { opacity: 0, y: 12 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.25, ease: 'easeOut', delay: 0.12 }}
            className="flex items-center gap-3"
          >
            {/* Estado agregado: 3 online + 1 aviso */}
            <span className="inline-flex items-center gap-1.5 rounded-full border border-border bg-surface px-3 py-1.5" aria-label={t('routers.statusAria', { online, warnings: warned })}>
              {routers.map((r, i) => (
                <motion.span
                  key={r.id}
                  initial={reduce ? false : { scale: 0 }}
                  animate={{ scale: 1 }}
                  transition={{ duration: 0.2, delay: 0.2 + i * 0.1 }}
                  className="relative flex h-2 w-2"
                >
                  {r.status === 'warn' && (
                    <span className="absolute inline-flex h-full w-full rounded-full bg-warn opacity-75 animate-ping-soft" />
                  )}
                  <span className={cn('relative inline-flex h-2 w-2 rounded-full', r.status === 'online' ? 'bg-ok' : r.status === 'warn' ? 'bg-warn' : 'bg-danger')} />
                </motion.span>
              ))}
              <span className="ml-1 text-caption font-medium text-text-secondary">{online}/{routers.length} online</span>
            </span>
            <button
              onClick={handleRefresh}
              className="inline-flex h-9 items-center gap-2 rounded-lg border border-border bg-surface px-3 text-sm font-medium text-text-secondary transition-colors hover:border-accent/40 hover:text-accent"
            >
              <RefreshCw className={cn('h-4 w-4 transition-transform duration-500', spinning && 'rotate-[360deg]')} strokeWidth={1.75} />
              {t('common.refresh')}
            </button>
            <span className="hidden text-caption text-text-muted sm:inline">{t('common.updatedAgo')}</span>
          </motion.div>
        </div>
      </header>

      {/* ② Fleet cards 2×2 */}
      <div className="grid grid-cols-1 gap-4 md:gap-5 lg:grid-cols-2">
        {routers.map((r, i) => (
          <FleetCard key={r.id} router={r} index={i} refreshKey={refreshKey} />
        ))}
      </div>

      {/* ③ Tabla comparativa */}
      <FleetTable refreshKey={refreshKey} />
    </div>
  )
}
