/**
 * UpdateDialog — asistente de actualización multi-paso con barra de progreso
 * (issue #280): confirmación → pasos (fetch → download → verify → install →
 * restart) → reinicio → recarga. Patrón tipo Pulse: estado por SSE
 * (/api/update/stream) con fallback a polling, y recarga solo cuando responde
 * un proceso DISTINTO (uptimeSec menor que el baseline previo al apply).
 */
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import {
  AlertTriangle,
  ArrowRight,
  Check,
  Circle,
  DownloadCloud,
  Loader2,
} from 'lucide-react'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Progress } from '@/components/ui/progress'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { ReadinessPanel, type UpdateReadiness } from '@/components/UpdateReadiness'

export interface UpdateStatusInfo {
  current: string
  latest: string | null
  latestMsg: string | null
  /** Cuerpo del commit (rolling) o notas del release (estable): changelog. */
  latestBody?: string | null
  updateAvailable: boolean
  canApply: boolean
  repo: string
  updating: false | { step: string; progress?: number }
  error?: string | null
  readiness?: UpdateReadiness | null
}

/** Pasos visibles del asistente (el "done" se refleja solo en la barra). */
const STEP_ORDER = ['fetch', 'download', 'verify', 'install', 'restart'] as const

/** Mapa legado: pasos de scripts viejos → paso visible equivalente. */
const STEP_ALIAS: Record<string, string> = {
  start: 'fetch',
  binary: 'download', // legado: download+install juntos
  done: 'restart',
}

type Phase = 'confirm' | 'progress' | 'restarting' | 'error'

interface UpdateDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** Estado inicial (del banner/Ajustes); si no, se consulta al abrir. */
  initialStatus?: UpdateStatusInfo | null
}

export function UpdateDialog({ open, onOpenChange, initialStatus }: UpdateDialogProps) {
  const { t } = useTranslation()
  const [phase, setPhase] = useState<Phase>('confirm')
  const [status, setStatus] = useState<UpdateStatusInfo | null>(initialStatus ?? null)
  const [step, setStep] = useState<string>('start')
  const [pct, setPct] = useState<number>(0)
  const [errorCode, setErrorCode] = useState<string | null>(null)
  // Confirmación explícita de la caída del servicio (patrón Pulse): sin
  // marcarla, el botón de actualizar no se habilita.
  const [ackDowntime, setAckDowntime] = useState(false)

  const esRef = useRef<EventSource | null>(null)
  const pollRef = useRef<number | null>(null)
  const uptimeRef = useRef<number>(Number.MAX_SAFE_INTEGER)
  const phaseRef = useRef<Phase>('confirm')
  const fetchFailRef = useRef(0)

  const setPhaseBoth = (p: Phase) => {
    phaseRef.current = p
    setPhase(p)
  }

  const closeStream = useCallback(() => {
    if (esRef.current) {
      esRef.current.close()
      esRef.current = null
    }
    if (pollRef.current !== null) {
      window.clearInterval(pollRef.current)
      pollRef.current = null
    }
  }, [])

  useEffect(() => closeStream, [closeStream])

  // Recarga cuando responde un proceso distinto (uptimeSec menor). Tope de
  // 90 s por si el restart se atasca (misma regla que el banner, #226/#241).
  const waitAndReload = useCallback(() => {
    if (phaseRef.current === 'restarting') return
    setPhaseBoth('restarting')
    const deadline = Date.now() + 90_000
    const id = window.setInterval(async () => {
      try {
        const res = await fetch('/api/health', { signal: AbortSignal.timeout(2000) })
        if (res.ok) {
          const j = (await res.json().catch(() => null)) as { uptimeSec?: number } | null
          const fresh = j != null && typeof j.uptimeSec === 'number' && j.uptimeSec < uptimeRef.current
          if (fresh || Date.now() > deadline) {
            window.clearInterval(id)
            window.location.reload()
          }
        }
      } catch {
        /* reiniciando… */
      }
    }, 1500)
  }, [])

  // Reacción central al estado que llega por SSE o polling.
  const handleStatus = useCallback(
    (st: UpdateStatusInfo) => {
      setStatus(st)
      fetchFailRef.current = 0
      if (st.error) {
        setErrorCode(st.error)
        setPhaseBoth('error')
        closeStream()
        return
      }
      if (st.updating) {
        // Adoptar una actualización ya en curso (otra pestaña/admin): el
        // asistente pasa directamente a la fase de progreso.
        if (phaseRef.current === 'confirm') setPhaseBoth('progress')
        setStep(st.updating.step)
        setPct(st.updating.progress ?? 0)
        if (st.updating.step === 'done') {
          // El script terminó: el reinicio real tarda unos segundos (el
          // proceso viejo sigue respondiendo un rato).
          closeStream()
          waitAndReload()
        }
      } else if (phaseRef.current === 'progress') {
        // updating=false tras haber arrancado: el apply terminó en error
        // (el updater limpia el paso) o ya reinició sin ver el done.
        closeStream()
        waitAndReload()
      }
    },
    [closeStream, waitAndReload],
  )

  // Fallback a polling si el SSE no está disponible (proxy, etc.).
  const startPolling = useCallback(() => {
    if (pollRef.current !== null) return
    const poll = async () => {
      try {
        const res = await fetch('/api/update/status', { signal: AbortSignal.timeout(3000) })
        if (!res.ok) throw new Error(String(res.status))
        handleStatus((await res.json()) as UpdateStatusInfo)
      } catch {
        // Sin conexión durante el update: casi seguro reiniciando.
        fetchFailRef.current += 1
        if (fetchFailRef.current >= 2 && phaseRef.current === 'progress') {
          closeStream()
          waitAndReload()
        }
      }
    }
    void poll()
    pollRef.current = window.setInterval(poll, 2000)
  }, [closeStream, handleStatus, waitAndReload])

  const startStream = useCallback(() => {
    try {
      const es = new EventSource('/api/update/stream')
      esRef.current = es
      es.addEventListener('update', (ev) => {
        try {
          handleStatus(JSON.parse((ev as MessageEvent).data) as UpdateStatusInfo)
        } catch {
          /* payload corrupto: sigue esperando el próximo */
        }
      })
      es.onerror = () => {
        // El stream MUERE con el proceso durante el reinicio final.
        if (phaseRef.current === 'restarting') return
        if (step === 'restart' || step === 'done') {
          closeStream()
          waitAndReload()
          return
        }
        closeStream()
        startPolling()
      }
    } catch {
      startPolling()
    }
  }, [closeStream, handleStatus, startPolling, waitAndReload, step])

  // Al abrir: reset + estado fresco; al cerrar: cortar stream/poll.
  useEffect(() => {
    if (open) {
      setPhaseBoth('confirm')
      setStep('start')
      setPct(0)
      setErrorCode(null)
      setAckDowntime(false)
      fetchFailRef.current = 0
      if (!initialStatus) {
        void fetch('/api/update/status')
          .then((r) => (r.ok ? r.json() : null))
          .then((j) => {
            if (!j) return
            setStatus(j as UpdateStatusInfo)
            // Ya hay un update en curso: adoptarlo (fase progreso + stream).
            if ((j as UpdateStatusInfo).updating) {
              setPhaseBoth('progress')
              startStream()
            }
          })
          .catch(() => undefined)
      } else {
        setStatus(initialStatus)
        if (initialStatus.updating) {
          setPhaseBoth('progress')
          startStream()
        }
      }
    } else {
      closeStream()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const apply = async () => {
    // Baseline de uptime ANTES de aplicar: la recarga exige proceso nuevo.
    try {
      const res = await fetch('/api/health', { signal: AbortSignal.timeout(2000) })
      if (res.ok) {
        const j = (await res.json()) as { uptimeSec?: number }
        if (typeof j.uptimeSec === 'number') uptimeRef.current = j.uptimeSec
      }
    } catch {
      /* sin baseline: el primer OK tras el corte recarga */
    }
    try {
      const res = await fetch('/api/update/apply', { method: 'POST' })
      if (!res.ok) {
        setErrorCode(res.status === 409 ? 'already_updating' : 'update_failed')
        setPhaseBoth('error')
        return
      }
      setPhaseBoth('progress')
      startStream()
    } catch {
      setErrorCode('network')
      setPhaseBoth('error')
    }
  }

  const visibleStep = STEP_ALIAS[step] ?? step
  const activeIdx = STEP_ORDER.indexOf(visibleStep as (typeof STEP_ORDER)[number])
  const busy = phase === 'progress' || phase === 'restarting'

  return (
    <Dialog open={open} onOpenChange={(o) => !busy && onOpenChange(o)}>
      <DialogContent className="max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <DownloadCloud className="h-5 w-5 text-accent" strokeWidth={1.75} aria-hidden="true" />
            {t('update.dialog.title')}
          </DialogTitle>
          {phase === 'confirm' && <DialogDescription>{t('update.dialog.desc')}</DialogDescription>}
        </DialogHeader>

        {phase === 'confirm' && (
          <div className="flex flex-col gap-4">
            <div className="flex items-center justify-center gap-3 rounded-xl border border-border bg-surface px-4 py-3">
              <span className="font-mono text-sm text-text-secondary">{status?.current ?? '—'}</span>
              <ArrowRight className="h-4 w-4 text-text-muted" strokeWidth={1.75} aria-hidden="true" />
              <span className="font-mono text-sm font-semibold text-accent">
                {status?.latest ?? '—'}
              </span>
            </div>
            {status?.latestMsg && (
              <p className="text-center text-caption text-text-muted">{status.latestMsg}</p>
            )}
            {/* Changelog (issue #280): cuerpo del commit (rolling) o notas
                del release (estable), línea a línea con scroll. */}
            {!!status?.latestBody?.trim() && (
              <div className="flex flex-col gap-1.5">
                <p className="text-xs font-semibold uppercase tracking-wider text-text-muted">
                  {t('update.dialog.changelogTitle')}
                </p>
                <div className="max-h-44 overflow-y-auto rounded-xl border border-border bg-surface px-3.5 py-2.5">
                  <ul className="flex flex-col gap-1">
                    {status.latestBody
                      .split('\n')
                      .map((l) => l.trim())
                      .filter(Boolean)
                      .map((l, i) => (
                        <li key={i} className="flex items-start gap-2 text-caption leading-snug text-text-secondary">
                          <span className="mt-1 h-1 w-1 shrink-0 rounded-full bg-text-muted/60" aria-hidden="true" />
                          {l.replace(/^[-*]\s+/, '')}
                        </li>
                      ))}
                  </ul>
                </div>
              </div>
            )}
            {status?.readiness && <ReadinessPanel readiness={status.readiness} compact />}
            <label className="flex cursor-pointer items-start gap-2.5 rounded-xl bg-warn/10 px-3.5 py-2.5 text-caption leading-snug text-warn">
              <Checkbox
                checked={ackDowntime}
                onCheckedChange={(v) => setAckDowntime(v === true)}
                className="mt-0.5"
                aria-label={t('update.dialog.downNotice')}
              />
              <span>{t('update.dialog.downNotice')}</span>
            </label>
            <DialogFooter>
              <Button variant="outline" onClick={() => onOpenChange(false)}>
                {t('update.dialog.cancel')}
              </Button>
              <Button
                onClick={() => void apply()}
                disabled={
                  !ackDowntime ||
                  !status?.canApply ||
                  (status?.readiness ? !status.readiness.ready : false)
                }
              >
                <DownloadCloud className="h-4 w-4" strokeWidth={1.75} aria-hidden="true" />
                {t('update.dialog.start')}
              </Button>
            </DialogFooter>
          </div>
        )}

        {phase === 'progress' && (
          <div className="flex flex-col gap-4" role="status">
            <ul className="flex flex-col gap-2.5">
              {STEP_ORDER.map((s, i) => {
                const done = activeIdx > i || step === 'done'
                const active = activeIdx === i && step !== 'done'
                return (
                  <li key={s} className="flex items-center gap-2.5 text-sm">
                    {done ? (
                      <Check className="h-4 w-4 shrink-0 text-ok" strokeWidth={2} aria-hidden="true" />
                    ) : active ? (
                      <Loader2
                        className="h-4 w-4 shrink-0 animate-spin text-accent"
                        strokeWidth={2}
                        aria-hidden="true"
                      />
                    ) : (
                      <Circle className="h-4 w-4 shrink-0 text-text-muted/40" strokeWidth={2} aria-hidden="true" />
                    )}
                    <span className={done ? 'text-text-secondary' : active ? 'text-text-primary font-medium' : 'text-text-muted'}>
                      {t(`update.step.${s}`)}
                    </span>
                  </li>
                )
              })}
            </ul>
            <div className="flex flex-col gap-1.5">
              <Progress value={pct} aria-label={t('update.dialog.progressLabel')} />
              <div className="flex items-center justify-between text-caption text-text-muted">
                <span>{t(`update.step.${visibleStep}`)}…</span>
                <span className="font-mono">{pct}%</span>
              </div>
            </div>
            <p className="text-center text-caption text-text-muted">{t('update.dialog.hideHint')}</p>
          </div>
        )}

        {phase === 'restarting' && (
          <div className="flex flex-col items-center gap-3 py-4" role="status">
            <Loader2 className="h-10 w-10 animate-spin text-accent" strokeWidth={1.5} aria-hidden="true" />
            <p className="text-sm font-medium text-text-primary">{t('update.dialog.restarting')}</p>
            <p className="text-caption text-text-muted">{t('update.dialog.reloadSoon')}</p>
          </div>
        )}

        {phase === 'error' && (
          <div className="flex flex-col gap-4">
            <div className="flex items-start gap-3 rounded-xl bg-danger/10 px-4 py-3">
              <AlertTriangle className="mt-0.5 h-5 w-5 shrink-0 text-danger" strokeWidth={1.75} aria-hidden="true" />
              <div className="min-w-0">
                <p className="text-sm font-medium text-danger">{t('update.dialog.failed')}</p>
                {errorCode && <p className="mt-0.5 font-mono text-caption text-text-muted">{errorCode}</p>}
              </div>
            </div>
            <DialogFooter>
              <Button variant="outline" onClick={() => onOpenChange(false)}>
                {t('update.dialog.close')}
              </Button>
            </DialogFooter>
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}
