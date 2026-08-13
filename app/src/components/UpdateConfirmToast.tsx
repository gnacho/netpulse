/**
 * UpdateConfirmToast — confirmación post-update (issue #161): tras aplicar y
 * reiniciar, el backend expone `pendingApply` en /api/update/status solo si
 * el commit actual cambió respecto al marcador pre-update. Este componente
 * lo lee al cargar (admin), muestra un toast "Actualizado a <sha>" y hace el
 * ack (POST /api/update/pending-confirm) para que sea una sola vez.
 */
import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { motion, AnimatePresence, useReducedMotion } from 'framer-motion'
import { CheckCircle2, X } from 'lucide-react'
import { useAuth } from '@/data/AuthContext'

const AUTO_DISMISS_MS = 8000

interface PendingApply {
  from: string
  to: string
}

export function UpdateConfirmToast() {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const auth = useAuth()
  const [pending, setPending] = useState<PendingApply | null>(null)
  const ackedRef = useRef(false)

  // Leer la confirmación al cargar (solo admin).
  useEffect(() => {
    if (auth?.role !== 'admin') return
    let cancelled = false
    void (async () => {
      try {
        const res = await fetch('/api/update/status')
        if (!res.ok) return
        const json = (await res.json()) as { pendingApply?: PendingApply | null }
        if (!cancelled && json.pendingApply) setPending(json.pendingApply)
      } catch {
        /* sin status: sin confirmación */
      }
    })()
    return () => {
      cancelled = true
    }
  }, [auth?.role])

  // Ack tras mostrar (una sola vez): el backend descarta la confirmación.
  useEffect(() => {
    if (!pending || ackedRef.current) return
    ackedRef.current = true
    void fetch('/api/update/pending-confirm', { method: 'POST' }).catch(() => {
      /* si el ack falla, el toast reaparece en la próxima carga (backend la
         mantiene hasta confirmar) */
    })
  }, [pending])

  // Auto-cierre.
  useEffect(() => {
    if (!pending) return
    const id = window.setTimeout(() => setPending(null), AUTO_DISMISS_MS)
    return () => window.clearTimeout(id)
  }, [pending])

  if (!pending) return null

  return (
    <div className="pointer-events-none fixed inset-x-0 bottom-24 z-50 flex justify-center px-4 md:bottom-8" aria-live="polite">
      <AnimatePresence>
        <motion.div
          key={pending.to}
          initial={reduce ? false : { opacity: 0, y: 16, scale: 0.95 }}
          animate={{ opacity: 1, y: 0, scale: 1 }}
          exit={reduce ? undefined : { opacity: 0, y: 8, scale: 0.95 }}
          transition={{ duration: 0.25, ease: 'easeOut' }}
          role="status"
          className="flex items-center gap-2 rounded-xl border border-border-strong bg-elevated py-2 pl-3 pr-2 text-sm font-medium text-text-primary shadow-soft"
        >
          <CheckCircle2 className="h-4 w-4 shrink-0 text-ok" strokeWidth={1.75} />
          <span>{t('update.confirmed', { sha: pending.to })}</span>
          <button
            type="button"
            onClick={() => setPending(null)}
            aria-label={t('update.dismiss')}
            className="flex h-6 w-6 shrink-0 items-center justify-center rounded-lg text-text-muted transition-colors hover:text-text-primary"
          >
            <X className="h-4 w-4" strokeWidth={1.75} />
          </button>
        </motion.div>
      </AnimatePresence>
    </div>
  )
}
