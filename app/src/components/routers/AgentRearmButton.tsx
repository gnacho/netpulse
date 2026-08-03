import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Loader2 } from 'lucide-react'
import { useNetPulse } from '@/data/DataProvider'
import type { AgentInfo } from '@/data/types'
import { cn } from '@/lib/utils'

interface AgentRearmButtonProps {
  agent: AgentInfo | undefined
  className?: string
}

type RearmState = 'idle' | 'busy' | 'ok' | 'pending' | 'fail'

/**
 * Botón «Rearmar» (Fase 6.1, Plan B): solo aparece con un agente registrado
 * y NO fresh (caído). Reinicia el servicio procd en el router vía
 * POST /api/agents/{slug}/rearm y refleja el resultado real:
 *   ok      → el agente volvió a empujar (recuperado)
 *   pending → reiniciado pero sin push en 30 s
 *   fail    → petición fallida (SSH, cooldown, sin servidor…)
 * El estado ok/pending/fail dura 6 s y vuelve a idle.
 */
export function AgentRearmButton({ agent, className }: AgentRearmButtonProps) {
  const { t } = useTranslation()
  const { rearmAgent } = useNetPulse()
  const [state, setState] = useState<RearmState>('idle')

  if (!agent || agent.fresh) return null

  const label =
    state === 'busy' ? t('routers.agent.rearming')
    : state === 'ok' ? t('routers.agent.rearmOk')
    : state === 'pending' ? t('routers.agent.rearmPending')
    : state === 'fail' ? t('routers.agent.rearmFail')
    : t('routers.agent.rearm')

  const onClick = async () => {
    if (state === 'busy') return
    setState('busy')
    const res = await rearmAgent(agent.slug)
    const next: RearmState = res === null ? 'fail' : res.recovered ? 'ok' : 'pending'
    setState(next)
    window.setTimeout(() => setState('idle'), 6000)
  }

  return (
    <button
      type="button"
      onClick={onClick}
      disabled={state === 'busy'}
      title={t('routers.agent.staleTip')}
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
