import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Loader2 } from 'lucide-react'
import { useNetPulse } from '@/data/DataProvider'
import { useAuth } from '@/data/AuthContext'
import type { AgentInfo, AgentUpgradeProgress } from '@/data/types'
import { cn } from '@/lib/utils'

interface AgentUpgradeButtonProps {
  agent: AgentInfo | undefined
  className?: string
}

type UpgradeState = 'idle' | 'busy' | 'sent' | 'queued' | 'not_connected' | 'fail'

type StepT = (key: string, opts?: Record<string, unknown>) => string

/** Ventana que considera "vivo" un reporte de progreso (segundos, #284). */
export const UPGRADE_LIVE_WINDOW_S = 120

/** Upgrade en marcha: paso no terminal con reporte fresco (#284). */
export function activeUpgrade(agent: AgentInfo | undefined, nowSec: number): AgentUpgradeProgress | undefined {
  const u = agent?.upgrade
  if (!u || u.step === 'failed' || u.step === 'done') return undefined
  if (u.step === 'queued') return u
  if (nowSec - u.ts > UPGRADE_LIVE_WINDOW_S) return undefined
  return u
}

/** Texto del paso para el botón y el panel de flota (#284). */
export function upgradeStepText(u: { step: AgentUpgradeProgress['step']; pct?: number }, t: StepT): string {
  switch (u.step) {
    case 'requested':
      return t('routers.agent.stepRequested')
    case 'downloading':
      return t('routers.agent.stepDownloading', { pct: u.pct ?? 0 })
    case 'verifying':
      return t('routers.agent.stepVerifying')
    case 'swapping':
      return t('routers.agent.stepSwapping')
    case 'restarting':
      return t('routers.agent.stepRestarting')
    case 'done':
      return t('routers.agent.upgraded')
    case 'queued':
      return t('routers.agent.stepQueued')
    default:
      return t('routers.agent.upgrading')
  }
}

/** Sufijo de segundos transcurridos desde el último reporte. */
export function upgradeElapsed(u: AgentUpgradeProgress, nowSec: number): string {
  return `${Math.max(0, nowSec - u.ts)}s`
}

/**
 * Botón «Actualizar agente» (Fase 6.3, issue #243): solo aparece si el agente
 * reporta una versión distinta de la del binario embebido (updateAvailable) y
 * la sesión es admin (la API exige admin). POST /api/agents/{slug}/upgrade
 * ordena al agente descargar el binario nuevo, intercambiarlo y reiniciarse.
 * Refleja el resultado real del envío:
 *   sent          → 202, comando enviado por SSE (el agente se reinicia solo)
 *   not_connected → 409, el agente no está conectado por SSE
 *   fail          → petición fallida (red, 4xx/5xx)
 * El estado sent/not_connected/fail dura 6 s y vuelve a idle.
 *
 * #284: mientras el agente reporta pasos (downloading con porcentaje,
 * swapping, restarting) el botón muestra el paso en vivo y los segundos
 * transcurridos, en vez de quedarse congelado en "Enviando". El flash
 * final "Actualizado" aparece cuando updateAvailable pasa a false.
 */
export function AgentUpgradeButton({ agent, className }: AgentUpgradeButtonProps) {
  const { t } = useTranslation()
  const { upgradeAgent, refreshAgents } = useNetPulse()
  const auth = useAuth()
  const [state, setState] = useState<UpgradeState>('idle')
  const [nowSec, setNowSec] = useState(() => Math.floor(Date.now() / 1000))
  const [justDone, setJustDone] = useState(false)
  const wasAvailable = useRef(false)

  const live = agent ? activeUpgrade(agent, nowSec) : undefined
  const liveActive = live !== undefined
  const liveFailed = agent?.upgrade?.step === 'failed' && nowSec - (agent.upgrade?.ts ?? 0) < 60

  // Tick de 1 s mientras hay progreso en vivo (para los segundos transcurridos).
  useEffect(() => {
    if (!liveActive) return
    const timer = window.setInterval(() => setNowSec(Math.floor(Date.now() / 1000)), 1000)
    return () => window.clearInterval(timer)
  }, [liveActive])

  // Flash final: updateAvailable true → false significa binario nuevo en
  // marcha. El effect es idempotente (solo actúa en la transición, guardado
  // por wasAvailable) para tolerar los re-renders del poll de agentes.
  useEffect(() => {
    if (!agent || agent.updateAvailable) {
      if (agent?.updateAvailable) wasAvailable.current = true
      return
    }
    if (!wasAvailable.current) return
    wasAvailable.current = false
    setState('idle')
    setJustDone(true)
  }, [agent, agent?.updateAvailable])

  // El timer del flash vive en su propio effect (clave: el booleano), para
  // que los re-renders del poll no lo cancelen a medias.
  useEffect(() => {
    if (!justDone) return
    const timer = window.setTimeout(() => setJustDone(false), 4000)
    return () => window.clearTimeout(timer)
  }, [justDone])

  if (auth?.role !== 'admin') return null
  if (!agent) return null
  if (!agent.updateAvailable && !justDone) return null

  const label =
    state === 'busy' ? t('routers.agent.upgrading')
    : live ? `${upgradeStepText(live, t)} · ${upgradeElapsed(live, nowSec)}`
    : justDone ? t('routers.agent.upgraded')
    : state === 'sent' ? t('routers.agent.upgradeSent')
    : state === 'queued' ? t('routers.agent.upgradeQueued')
    : state === 'not_connected' ? t('routers.agent.upgradeNotConnected')
    : state === 'fail' || liveFailed ? t('routers.agent.upgradeFail')
    : t('routers.agent.upgrade')

  const onClick = async () => {
    if (state === 'busy' || live) return
    setState('busy')
    const res = await upgradeAgent(agent.slug)
    const next: UpgradeState =
      res === 'sent' ? 'sent' : res === 'queued' ? 'queued' : res === 'not_connected' ? 'not_connected' : 'fail'
    setState(next)
    // Traer el paso "requested"/"queued" cuanto antes: encadena el sondeo rápido.
    if (next === 'sent' || next === 'queued') void refreshAgents()
    window.setTimeout(() => setState('idle'), 6000)
  }

  return (
    <button
      type="button"
      onClick={onClick}
      disabled={state === 'busy' || live !== undefined}
      title={liveFailed ? agent.upgrade?.error || t('routers.agent.upgradeFail') : live?.step === 'queued' ? t('routers.agent.upgradeQueuedTip') : t('routers.agent.upgradeTip', { version: agent.version ?? '?' })}
      className={cn(
        'inline-flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-[11px] font-semibold transition-opacity hover:opacity-90 disabled:opacity-50',
        state === 'sent' || justDone ? 'bg-ok text-canvas'
        : state === 'queued' ? 'bg-accent-soft text-accent'
        : state === 'fail' || state === 'not_connected' || liveFailed ? 'bg-danger text-canvas'
        : 'bg-accent text-canvas',
        className,
      )}
    >
      {(state === 'busy' || live !== undefined) && <Loader2 className="h-3 w-3 animate-spin" />}
      {label}
    </button>
  )
}
