import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Loader2 } from 'lucide-react'
import { useNetPulse } from '@/data/DataProvider'
import { useAuth } from '@/data/AuthContext'
import type { AgentInfo } from '@/data/types'
import { cn } from '@/lib/utils'

interface AgentRearmButtonProps {
  agent: AgentInfo | undefined
  className?: string
}

type RearmState = 'idle' | 'busy' | 'ok' | 'pending' | 'fail'

/**
 * #718: mapea el código de error del backend (envelope {error}) a la clave
 * i18n con el motivo real del fallo de rearme. Cualquier código desconocido
 * cae al mensaje genérico.
 */
const REARM_ERROR_KEYS: Record<string, string> = {
  not_found: 'routers.agent.rearmFailNoAgent',
  router_unknown: 'routers.agent.rearmFailNoRouter',
  not_openwrt: 'routers.agent.rearmFailNotOpenwrt',
  netgrip_managed: 'routers.agent.rearmFailNetgrip',
  ssh_unavailable: 'routers.agent.rearmFailNoSSH',
  cooldown: 'routers.agent.rearmFailCooldown',
  ssh_failed: 'routers.agent.rearmFailSSH',
}

/**
 * Botón «Rearmar» (Fase 5, Plan B): solo aparece con un agente registrado,
 * NO fresh (caído) y sesión con rol admin (la API exige admin en el rearme;
 * auditoría v2.4.0 §2, issue #7). Reinicia el servicio del agente en el
 * router vía POST /api/agents/{slug}/rearm y refleja el resultado real:
 *   ok      → el agente volvió a empujar (recuperado)
 *   pending → reiniciado pero sin push en 30 s
 *   fail    → petición fallida (SSH, cooldown, sin servidor…)
 * El estado ok/pending/fail dura 6 s y vuelve a idle.
 *
 * #569: para un agente NetGrip stale el botón dice «Reiniciar NetGrip» (el
 * backend reinicia el SERVICIO netgrip, que recarga el env del agente
 * embebido); para un agente nativo dice «Rearmar» (reinicia netpulse-agent).
 */
export function AgentRearmButton({ agent, className }: AgentRearmButtonProps) {
  const { t } = useTranslation()
  const { rearmAgent } = useNetPulse()
  const auth = useAuth()
  const [state, setState] = useState<RearmState>('idle')
  const [failCode, setFailCode] = useState<string | null>(null)

  if (!agent || agent.fresh) return null
  if (auth?.role !== 'admin') return null

  const netgrip = agent.kind === 'netgrip'
  const failKey = failCode ? (REARM_ERROR_KEYS[failCode] ?? 'routers.agent.rearmFail') : 'routers.agent.rearmFail'
  const label =
    state === 'busy' ? (netgrip ? t('routers.agent.netgripRestarting') : t('routers.agent.rearming'))
    : state === 'ok' ? t('routers.agent.rearmOk')
    : state === 'pending' ? t('routers.agent.rearmPending')
    : state === 'fail' ? t(failKey)
    : netgrip ? t('routers.agent.netgripRestart')
    : t('routers.agent.rearm')

  const onClick = async () => {
    if (state === 'busy') return
    setState('busy')
    const res = await rearmAgent(agent.slug)
    if (res === null) {
      setFailCode(null)
      setState('fail')
    } else if (res.error) {
      setFailCode(res.error)
      setState('fail')
    } else {
      setFailCode(null)
      setState(res.recovered ? 'ok' : 'pending')
    }
    window.setTimeout(() => setState('idle'), 6000)
  }

  return (
    <button
      type="button"
      onClick={onClick}
      disabled={state === 'busy'}
      title={netgrip ? t('routers.agent.netgripRestartTip') : t('routers.agent.staleTip')}
      className={cn(
        'inline-flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-[11px] font-semibold transition-opacity hover:opacity-90 disabled:opacity-50',
        state === 'ok' ? 'bg-ok text-canvas'
        : state === 'fail' ? 'bg-danger text-canvas'
        : 'bg-warn text-canvas',
        className,
      )}
    >
      {state === 'busy' && <Loader2 className="h-3 w-3 animate-spin" />}
      {label}
    </button>
  )
}
