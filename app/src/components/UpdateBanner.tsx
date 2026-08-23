/**
 * UpdateBanner — franja horizontal que avisa cuando hay versión nueva en
 * GitHub (solo admin). Comprueba al montar y cada 30 min; al pulsar
 * "Actualizar" sigue el progreso (fetch → build → restart) y recarga la app
 * cuando el backend vuelve.
 */
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { motion, AnimatePresence, useReducedMotion } from 'framer-motion'
import { DownloadCloud, ExternalLink, Loader2, X } from 'lucide-react'
import { useAuth } from '@/data/AuthContext'
import { ReadinessPanel, type UpdateReadiness } from '@/components/UpdateReadiness'

function normalizeVersion(s: string) {
  return s.trim().toLowerCase().replace(/^v/, '')
}

interface UpdateStatus {
  current: string
  latest: string | null
  latestMsg: string | null
  updateAvailable: boolean
  canApply: boolean
  mode: string
  repo: string
  updating: false | { step: string }
  hasToken: boolean
  readiness?: UpdateReadiness | null
}

const POLL_MS = 60 * 60 * 1000
const WEEK_MS = 7 * 24 * 60 * 60 * 1000
const CHECK_KEY = 'netpulse-last-update-check'
const APPLY_POLL_MS = 2500
const APPLY_TIMEOUT_MS = 90_000

export function UpdateBanner() {
  const { t } = useTranslation()
  const reduce = useReducedMotion()
  const auth = useAuth()
  const [status, setStatus] = useState<UpdateStatus | null>(null)
  const [dismissed, setDismissed] = useState<string | null>(null)
  const applyingRef = useRef(false)
  // Id del poll de progreso del apply: en un ref para limpiarlo en unmount
  // (#226).
  const applyPollRef = useRef<number | null>(null)
  const clearApplyPoll = () => {
    if (applyPollRef.current !== null) {
      window.clearInterval(applyPollRef.current)
      applyPollRef.current = null
    }
  }
  useEffect(() => clearApplyPoll, [])

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
        return !last || Date.now() - last > WEEK_MS
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
    return () => window.clearInterval(id)
  }, [auth?.role, fetchStatus])

  // Tras aplicar: espera a que el backend reinicie y recarga la app.
  // BUG pantalla negra: STEP:done salta al tocar el flag de restart, pero el
  // proceso VIEJO sigue respondiendo unos segundos. Recargar con el primer
  // health OK hacía que la recarga compitiera con el reinicio y la página
  // moría a medias (#root vacío → pantalla negra hasta refrescar a mano).
  // Fix: solo recargamos cuando responde un proceso DISTINTO (uptimeSec menor
  // que el que había antes de aplicar). Tope de 90 s por si el restart se atasca.
  const waitAndReload = useCallback((uptimeBefore: number) => {
    const deadline = Date.now() + 90_000
    const id = window.setInterval(async () => {
      try {
        const res = await fetch('/api/health', { signal: AbortSignal.timeout(2000) })
        if (res.ok) {
          const j = (await res.json().catch(() => null)) as { uptimeSec?: number } | null
          const freshProcess = j != null && typeof j.uptimeSec === 'number' && j.uptimeSec < uptimeBefore
          if (freshProcess || Date.now() > deadline) {
            window.clearInterval(id)
            window.location.reload()
          }
        }
      } catch {
        /* reiniciando… */
      }
    }, 1500)
  }, [])

  const apply = async () => {
    if (applyingRef.current) return
    applyingRef.current = true
    // Uptime base del proceso actual: waitAndReload exige un proceso nuevo.
    // Sin baseline (fetch falló) se acepta el primer OK (comportamiento legacy).
    let uptimeBefore = Number.MAX_SAFE_INTEGER
    try {
      const res = await fetch('/api/health', { signal: AbortSignal.timeout(2000) })
      if (res.ok) {
        const j = (await res.json()) as { uptimeSec?: number }
        if (typeof j.uptimeSec === 'number') uptimeBefore = j.uptimeSec
      }
    } catch {
      /* sin baseline: primer OK recarga */
    }
    try {
      await fetch('/api/update/apply', { method: 'POST' })
      // Seguimiento del progreso hasta que acabe. Tope de 90 s: si el backend
      // se queda en 'updating' sin llegar a 'done', se abandona el poll en vez
      // de seguir sondeando para siempre (#226). El id vive en un ref para
      // limpiarlo si el banner se desmonta.
      const deadline = Date.now() + APPLY_TIMEOUT_MS
      applyPollRef.current = window.setInterval(async () => {
        const s = await fetchStatus()
        if (s && s.updating && s.updating.step === 'done') {
          clearApplyPoll()
          waitAndReload(uptimeBefore)
        } else if ((s && !s.updating) || Date.now() > deadline) {
          clearApplyPoll()
          applyingRef.current = false
        }
      }, APPLY_POLL_MS)
    } catch {
      applyingRef.current = false
    }
  }

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
                  onClick={() => void apply()}
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
      </motion.div>
    </AnimatePresence>
  )
}
