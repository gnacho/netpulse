/**
 * UpdateBanner — franja horizontal que avisa cuando hay versión nueva en
 * GitHub (solo admin). Comprueba al montar y cada 30 min; al pulsar
 * "Actualizar" sigue el progreso (fetch → build → restart) y recarga la app
 * cuando el backend vuelve.
 */
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { motion, AnimatePresence, useReducedMotion } from 'framer-motion'
import { DownloadCloud, Loader2, X } from 'lucide-react'
import { useAuth } from '@/data/AuthContext'

interface UpdateStatus {
  current: string
  latest: string | null
  latestMsg: string | null
  updateAvailable: boolean
  updating: false | { step: string }
  hasToken: boolean
}

const POLL_MS = 30 * 60 * 1000
const APPLY_POLL_MS = 2500

export function UpdateBanner() {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const auth = useAuth()
  const [status, setStatus] = useState<UpdateStatus | null>(null)
  const [dismissed, setDismissed] = useState<string | null>(null)
  const applyingRef = useRef(false)

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

  // Chequeo inicial + periódico (solo admin)
  useEffect(() => {
    if (auth?.role !== 'admin') return
    void fetchStatus()
    const id = window.setInterval(() => void fetchStatus(), POLL_MS)
    return () => window.clearInterval(id)
  }, [auth?.role, fetchStatus])

  // Tras aplicar: espera a que el backend reinicie y recarga la app
  const waitAndReload = useCallback(() => {
    const id = window.setInterval(async () => {
      try {
        const res = await fetch('/api/health', { signal: AbortSignal.timeout(2000) })
        if (res.ok) {
          window.clearInterval(id)
          window.location.reload()
        }
      } catch {
        /* reiniciando… */
      }
    }, 3000)
  }, [])

  const apply = async () => {
    if (applyingRef.current) return
    applyingRef.current = true
    try {
      await fetch('/api/update/apply', { method: 'POST' })
      // Seguimiento del progreso hasta que acabe
      const id = window.setInterval(async () => {
        const s = await fetchStatus()
        if (s && s.updating && s.updating.step === 'done') {
          window.clearInterval(id)
          waitAndReload()
        } else if (s && !s.updating) {
          window.clearInterval(id)
          applyingRef.current = false
        }
      }, APPLY_POLL_MS)
    } catch {
      applyingRef.current = false
    }
  }

  if (auth?.role !== 'admin') return null
  if (!status.updateAvailable || !status.latest || dismissed === status.latest) return null

  const updating = Boolean(status.updating)
  const step = status.updating ? status.updating.step : 'start'

  return (
    <AnimatePresence>
      <motion.div
        initial={reduce ? false : { opacity: 0, y: -8 }}
        animate={{ opacity: 1, y: 0 }}
        exit={{ opacity: 0, y: -8 }}
        transition={{ duration: 0.25, ease: 'easeOut' }}
        role="status"
        className="mb-4 flex items-center gap-3 rounded-xl border border-accent/40 bg-accent-soft px-4 py-2.5"
      >
        <DownloadCloud className="h-4 w-4 shrink-0 text-accent" strokeWidth={1.75} />
        <p className="min-w-0 flex-1 truncate text-sm text-text-primary">
          {updating
            ? t('update.updating', { step: t(`update.step.${step}`) })
            : t('update.available', { version: status.latest, msg: status.latestMsg ?? '' })}
        </p>
        {!updating ? (
          <>
            <button
              type="button"
              onClick={() => void apply()}
              className="flex shrink-0 items-center gap-1.5 rounded-lg bg-accent px-3 py-1.5 text-xs font-semibold text-canvas transition-opacity hover:opacity-90"
            >
              <DownloadCloud className="h-3.5 w-3.5" strokeWidth={2} />
              {t('update.button')}
            </button>
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
      </motion.div>
    </AnimatePresence>
  )
}
