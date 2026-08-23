import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Link } from 'react-router'
import { RefreshCw, Rocket } from 'lucide-react'
import { motion, useReducedMotion } from 'framer-motion'
import { useNetPulse } from '@/data/DataProvider'
import { FleetCard } from '@/components/routers/FleetCard'
import { FleetTable } from '@/components/routers/FleetTable'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { cn } from '@/lib/utils'

/** Página `/routers` — vista de flota (routers.md) */
export default function Routers() {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const { routers, agents, upgradeAllAgents } = useNetPulse()
  const [refreshKey, setRefreshKey] = useState(0)
  const [spinning, setSpinning] = useState(false)
  const [upgrading, setUpgrading] = useState(false)
  const [confirmOpen, setConfirmOpen] = useState(false)
  const [upgradeMsg, setUpgradeMsg] = useState<string | null>(null)
  /** Slugs a los que se envió el upgrade de flota (para mostrar progreso). */
  const [upgradeTrack, setUpgradeTrack] = useState<string[]>([])

  function handleRefresh() {
    setRefreshKey((k) => k + 1)
    if (reduce) return
    setSpinning(true)
    window.setTimeout(() => setSpinning(false), 650)
  }

  const pendingAgents = agents.filter((a) => a.updateAvailable).length

  async function handleUpgradeAll() {
    if (upgrading) return
    setConfirmOpen(false)
    setUpgrading(true)
    setUpgradeMsg(null)
    const res = await upgradeAllAgents()
    setUpgrading(false)
    if (res) {
      setUpgradeMsg(res.message)
      // Seguimiento de progreso: los slugs a los que se envió el upgrade
      // (los "sent") quedan en seguimiento hasta que el poll de agentes los
      // marque como al día (updateAvailable=false).
      const sent = res.agents.filter((a) => a.status === 'sent').map((a) => a.slug)
      setUpgradeTrack(sent)
      if (sent.length === 0) window.setTimeout(() => setUpgradeMsg(null), 5000)
    } else {
      setUpgradeMsg(t('routers.upgradeAllFail'))
      window.setTimeout(() => setUpgradeMsg(null), 5000)
    }
  }

  // Progreso en vivo: estado por slug basado en el último poll de agentes.
  const upgradeProgress = upgradeTrack.map((slug) => {
    const agent = agents.find((a) => a.slug === slug)
    const done = agent ? !agent.updateAvailable : false
    return { slug, done, version: agent?.version ?? '…' }
  })
  const upgradeDone = upgradeProgress.filter((p) => p.done).length
  const trackActive = upgradeTrack.length > 0 && upgradeDone < upgradeTrack.length

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
            {pendingAgents > 0 && (
              <span className="inline-flex items-center gap-2 rounded-full border border-warn/40 bg-warn/10 px-3 py-1.5">
                <span className="inline-flex items-center gap-1.5 text-caption font-medium text-warn">
                  <Rocket className="h-3.5 w-3.5" strokeWidth={1.75} />
                  {t('routers.upgradeAllHint', { count: pendingAgents })}
                </span>
                <button
                  onClick={() => setConfirmOpen(true)}
                  disabled={upgrading}
                  className="inline-flex items-center gap-1 rounded-full bg-warn px-2.5 py-1 text-[11px] font-semibold text-canvas transition-opacity hover:opacity-90 disabled:opacity-50"
                >
                  {upgrading ? t('routers.upgradingAll') : t('routers.upgradeAllShort')}
                </button>
              </span>
            )}
            <span className="hidden text-caption text-text-muted sm:inline">{t('common.updatedAgo')}</span>
          </motion.div>
        </div>
        {upgradeMsg && <p className="mt-1 text-caption text-text-secondary">{upgradeMsg}</p>}
      </header>

      {/* Panel de progreso de actualización de flota */}
      {upgradeTrack.length > 0 && (
        <motion.section
          initial={reduce ? false : { opacity: 0, y: 12 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.25, ease: 'easeOut' }}
          className="rounded-2xl border border-accent/30 bg-accent/5 p-4 md:p-5"
          aria-label={t('routers.upgradeProgressAria')}
        >
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="flex items-center gap-2.5">
              <Rocket className={cn('h-4 w-4 text-accent', trackActive && 'animate-pulse')} strokeWidth={1.75} />
              <span className="text-sm font-semibold text-text-primary">
                {trackActive
                  ? t('routers.upgradeProgress', { done: upgradeDone, total: upgradeTrack.length })
                  : t('routers.upgradeProgressDone', { total: upgradeTrack.length })}
              </span>
            </div>
            <button
              onClick={() => setUpgradeTrack([])}
              className="inline-flex h-8 items-center gap-1.5 rounded-lg border border-border px-3 text-caption font-medium text-text-secondary transition-colors hover:border-accent/40 hover:text-accent"
            >
              {t('routers.upgradeClose')}
            </button>
          </div>
          <ul className="mt-3 space-y-2">
            {upgradeProgress.map((p) => (
              <li key={p.slug} className="flex items-center gap-2.5">
                <span
                  className={cn(
                    'h-2 w-2 shrink-0 rounded-full',
                    p.done ? 'bg-ok' : 'bg-accent animate-pulse',
                  )}
                />
                <span className="min-w-0 flex-1 truncate font-mono text-caption text-text-secondary">{p.slug}</span>
                <span className={cn('text-caption', p.done ? 'text-ok' : 'text-text-muted')}>
                  {p.done ? t('routers.upgradeUpdated') : t('routers.upgradeWaiting')}
                </span>
              </li>
            ))}
          </ul>
        </motion.section>
      )}

      {/* ② Fleet cards 2×2 */}
      <div className="grid grid-cols-1 gap-4 md:gap-5 lg:grid-cols-2">
        {routers.map((r, i) => (
          <FleetCard key={r.id} router={r} index={i} refreshKey={refreshKey} />
        ))}
      </div>

      {/* ③ Tabla comparativa */}
      <FleetTable refreshKey={refreshKey} />

      {/* Confirmación in-app del upgrade de flota */}
      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t('routers.upgradeAllTitle')}</AlertDialogTitle>
            <AlertDialogDescription>
              {t('routers.upgradeAllConfirm', { count: pendingAgents })}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={upgrading}>{t('routers.upgradeCancel')}</AlertDialogCancel>
            <AlertDialogAction
              onClick={(e) => {
                e.preventDefault()
                void handleUpgradeAll()
              }}
              disabled={upgrading}
            >
              <Rocket className="mr-1.5 h-4 w-4" strokeWidth={2} />
              {upgrading ? t('routers.upgradingAll') : t('routers.upgradeAllShort')}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
