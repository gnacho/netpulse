import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Archive, Loader2 } from 'lucide-react'
import { useNetPulse } from '@/data/DataProvider'
import { useAuth } from '@/data/AuthContext'
import type { AgentInfo } from '@/data/types'
import { cn } from '@/lib/utils'

interface AgentBackupButtonProps {
  agent: AgentInfo | undefined
  /** id del router: el resultado se confirma viendo aparecer la entrada en
   *  GET /api/firmware-backups/{routerId}. */
  routerId?: string
  className?: string
}

type BackupState = 'idle' | 'busy' | 'waiting' | 'done' | 'timeout' | 'not_connected' | 'fail'

/** Espera máxima del snapshot: sysupgrade -b + subida del tar.gz (#1462). */
const BACKUP_WAIT_S = 45

/**
 * Botón «Copia de config» (#1462): snapshot standalone de la configuración,
 * independiente del flujo de firmware. POST /api/agents/{slug}/backup envía
 * el evento SSE "config_backup"; el agente hace sysupgrade -b y sube el
 * tar.gz con el mismo contrato que el backup del flujo de upgrade (#1414),
 * así la entrada aparece donde la UI ya lista backups.
 *
 * Estados:
 *   waiting       → comando enviado, sondeando la lista de backups hasta que
 *                   aparezca una entrada con ts posterior al clic
 *   done          → la entrada apareció (flash verde)
 *   timeout       → 45 s sin entrada nueva (el agente falló en silencio)
 *   not_connected → 409, agente sin SSE (a diferencia del upgrade no se encola)
 *   fail          → petición fallida (red, 4xx/5xx)
 */
export function AgentBackupButton({ agent, routerId, className }: AgentBackupButtonProps) {
  const { t } = useTranslation()
  const { backupAgent } = useNetPulse()
  const auth = useAuth()
  const [state, setState] = useState<BackupState>('idle')
  const [clickTs, setClickTs] = useState(0)
  const timerRef = useRef<number | null>(null)

  const toIdle = (ms: number) => {
    if (timerRef.current) window.clearTimeout(timerRef.current)
    timerRef.current = window.setTimeout(() => setState('idle'), ms)
  }

  // Limpieza del timer al desmontar.
  useEffect(() => () => {
    if (timerRef.current) window.clearTimeout(timerRef.current)
  }, [])

  // Sondeo de la lista de backups mientras esperamos la entrada nueva.
  useEffect(() => {
    if (state !== 'waiting' || !routerId) return
    const started = Date.now()
    const poll = window.setInterval(async () => {
      if (Date.now() - started > BACKUP_WAIT_S * 1000) {
        window.clearInterval(poll)
        setState('timeout')
        toIdle(6000)
        return
      }
      try {
        const res = await fetch(`/api/firmware-backups/${encodeURIComponent(routerId)}`, {
          signal: AbortSignal.timeout(10_000),
        })
        if (res.status === 401) return
        if (!res.ok) return
        const body = (await res.json()) as { ts: number }[]
        if (body.some((b) => b.ts * 1000 >= clickTs)) {
          window.clearInterval(poll)
          setState('done')
          toIdle(4000)
        }
      } catch {
        /* reintento en el siguiente tick */
      }
    }, 3000)
    return () => window.clearInterval(poll)
  }, [state, routerId, clickTs])

  if (auth?.role !== 'admin') return null
  if (!agent || !routerId) return null

  const label =
    state === 'busy' ? t('routers.agent.backupSent')
    : state === 'waiting' ? t('routers.agent.backupWaiting')
    : state === 'done' ? t('routers.agent.backupDone')
    : state === 'timeout' ? t('routers.agent.backupTimeout')
    : state === 'not_connected' ? t('routers.agent.backupNotConnected')
    : state === 'fail' ? t('routers.agent.backupFail')
    : t('routers.agent.backup')

  return (
    <button
      type="button"
      onClick={async () => {
        if (state === 'busy' || state === 'waiting') return
        setState('busy')
        const res = await backupAgent(agent.slug)
        if (res === 'sent') {
          setClickTs(Date.now())
          setState('waiting')
        } else if (res === 'not_connected') {
          setState('not_connected')
          toIdle(6000)
        } else {
          setState('fail')
          toIdle(6000)
        }
      }}
      disabled={state === 'busy' || state === 'waiting'}
      title={t('routers.agent.backupTip')}
      className={cn(
        'inline-flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-[11px] font-semibold transition-opacity hover:opacity-90 disabled:opacity-50',
        state === 'done' ? 'bg-ok text-canvas'
        : state === 'timeout' || state === 'not_connected' || state === 'fail' ? 'bg-danger text-canvas'
        : 'bg-accent-soft text-accent',
        className,
      )}
    >
      {(state === 'busy' || state === 'waiting') && <Loader2 className="h-3 w-3 animate-spin" />}
      {state === 'idle' && <Archive className="h-3 w-3" />}
      {label}
    </button>
  )
}
