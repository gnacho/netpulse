/**
 * UpdateBanner — franja horizontal que avisa cuando hay versión nueva en
 * GitHub (solo admin). Comprueba al montar y con cadencia semanal; al pulsar
 * "Actualizar" abre el asistente multi-paso (UpdateDialog, issue #280) que
 * sigue el progreso por pasos y recarga la app cuando el backend vuelve.
 */
import { useCallback, useEffect, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { motion, AnimatePresence, useReducedMotion } from 'framer-motion'
import { DownloadCloud, ExternalLink, Loader2, X } from 'lucide-react'
import { useAuth } from '@/data/AuthContext'
import { ReadinessPanel, type UpdateReadiness } from '@/components/UpdateReadiness'
import { UpdateDialog } from '@/components/UpdateDialog'

function normalizeVersion(s: string) {
  return s.trim().toLowerCase().replace(/^v/, '')
}

interface UpdateStatus {
  current: string
  latest: string | null
  latestMsg: string | null
  latestBody?: string | null
  commits?: { sha: string; subject: string }[] | null
  compareUrl?: string | null
  updateAvailable: boolean
  canApply: boolean
  mode: string
  repo: string
  updating: false | { step: string; progress?: number }
  hasToken: boolean
  readiness?: UpdateReadiness | null
}

import { CHECK_INTERVAL, CHECK_KEY, onBannerSignal } from '@/lib/update-check'

const POLL_MS = 60 * 60 * 1000

export function UpdateBanner() {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const auth = useAuth()
  const [status, setStatus] = useState<UpdateStatus | null>(null)
  const [dismissed, setDismissed] = useState<string | null>(null)
  const [dialogOpen, setDialogOpen] = useState(false)

  const fetchStatus = useCallback(async () => {
    try {
      const res = await fetch('/api/update/status')
      if (!res.ok) return
      const json = (await res.json()) as UpdateStatus
      setStatus(json)
      return json
    } catch {
      /* sin status: sin banner */
    }
  }, [])

  // Chequeo inicial + periódico (solo admin). Cadencia: 1 vez por semana por
  // navegador (localStorage), patrón del resto de apps del stack. El interval
  // de 1 h solo re-evalúa si toca volver a consultar el status.
  useEffect(() => {
    if (auth?.role !== 'admin') return
    const shouldCheck = () => {
      try {
        const last = Number(localStorage.getItem(CHECK_KEY) || 0)
        return !last || Date.now() - last > CHECK_INTERVAL
      } catch {
        return true
      }
    }
    const run = () => {
      if (!shouldCheck()) return
      void fetchStatus()
      try {
        localStorage.setItem(CHECK_KEY, String(Date.now()))
      } catch {
        /* sin localStorage: sigue consultando */
      }
    }
    run()
    const id = window.setInterval(run, POLL_MS)
    const off = onBannerSignal(() => void fetchStatus())
    return () => { window.clearInterval(id); off() }
  }, [auth?.role, fetchStatus])

  // Al cerrar el asistente: refrescar (si la actualización sigue en curso,
  // el banner pasa a mostrar "Actualizando…" con su spinner).
  const handleDialogChange = useCallback(
    (open: boolean) => {
      setDialogOpen(open)
      if (!open) void fetchStatus()
    },
    [fetchStatus],
  )

  if (auth?.role !== 'admin') return null
  if (!status || !status.updateAvailable || !status.latest || dismissed === status.latest) return null

  const updating = Boolean(status.updating)
  const step = status.updating ? status.updating.step : 'start'
  const ready = status.readiness ? status.readiness.ready : true

  const latest = status.latest
  const latestMsg = status.latestMsg ?? ''
  const showMsg = latestMsg && normalizeVersion(latestMsg) !== normalizeVersion(latest)
  const message = updating
    ? t('update.updating', { step: t(`update.step.${step}`) })
    : showMsg
      ? t('update.availableWithMsg', { version: latest, msg: latestMsg })
      : t('update.available', { version: latest })

  return (
    <AnimatePresence>
      <motion.div
        initial={reduce ? false : { opacity: 0, y: -8 }}
        animate={{ opacity: 1, y: 0 }}
        exit={{ opacity: 0, y: -8 }}
        transition={{ duration: 0.25, ease: 'easeOut' }}
        role="status"
        className="mb-4 flex flex-col gap-2"
      >
        <div className="flex items-center gap-3 rounded-xl border border-accent/40 bg-accent-soft px-4 py-2.5">
          <DownloadCloud className="h-4 w-4 shrink-0 text-accent" strokeWidth={1.75} />
          <div className="min-w-0 flex-1">
            <p className="truncate text-sm text-text-primary">{message}</p>
            {!updating && !status.canApply && (
              <p className="text-xs text-text-muted">{t('update.stableHint')}</p>
            )}
          </div>
          {!updating ? (
            <>
              {status.canApply ? (
                <button
                  type="button"
                  onClick={() => setDialogOpen(true)}
                  disabled={!ready}
                  className="flex shrink-0 items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  <DownloadCloud className="h-3.5 w-3.5" strokeWidth={2} />
                  {t('update.button')}
                </button>
              ) : (
                <a
                  href={`https://github.com/${status.repo || 'gnacho/netpulse'}/releases`}
                  target="_blank"
                  rel="noreferrer"
                  title={t('update.stableHint')}
                  className="flex shrink-0 items-center gap-1.5 rounded-lg border border-border px-3 py-1.5 text-xs font-semibold text-text-primary transition-colors hover:bg-surface-2"
                >
                  <ExternalLink className="h-3.5 w-3.5" strokeWidth={2} />
                  {t('update.getRelease')}
                </a>
              )}
              <button
                type="button"
                onClick={() => status.latest && setDismissed(status.latest)}
                aria-label={t('update.dismiss')}
                className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg text-text-muted transition-colors hover:text-text-primary"
              >
                <X className="h-4 w-4" strokeWidth={1.75} />
              </button>
            </>
          ) : (
            <Loader2 className="h-4 w-4 shrink-0 animate-spin text-accent" strokeWidth={1.75} />
          )}
        </div>
        {/* Pre-flight checks (issue #160): se muestran antes de permitir aplicar
            y deshabilitan el botón si alguno falla. */}
        {!updating && status.canApply && status.readiness && (
          <ReadinessPanel readiness={status.readiness} compact className="mx-0.5" />
        )}
        <UpdateDialog open={dialogOpen} onOpenChange={handleDialogChange} initialStatus={status} />
      </motion.div>
    </AnimatePresence>
  )
}
