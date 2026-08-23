import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Loader2 } from 'lucide-react'
import { useNetPulse } from '@/data/DataProvider'
import { useAuth } from '@/data/AuthContext'
import type { AgentInfo } from '@/data/types'
import { cn } from '@/lib/utils'

interface AgentUpgradeButtonProps {
  agent: AgentInfo | undefined
  className?: string
}

type UpgradeState = 'idle' | 'busy' | 'sent' | 'not_connected' | 'fail'

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
 */
export function AgentUpgradeButton({ agent, className }: AgentUpgradeButtonProps) {
  const { t } = useTranslation()
  const { upgradeAgent } = useNetPulse()
  const auth = useAuth()
  const [state, setState] = useState<UpgradeState>('idle')

  if (!agent || !agent.updateAvailable) return null
  if (auth?.role !== 'admin') return null

  const label =
    state === 'busy' ? t('routers.agent.upgrading')
    : state === 'sent' ? t('routers.agent.upgradeSent')
    : state === 'not_connected' ? t('routers.agent.upgradeNotConnected')
    : state === 'fail' ? t('routers.agent.upgradeFail')
    : t('routers.agent.upgrade')

  const onClick = async () => {
    if (state === 'busy') return
    setState('busy')
    const res = await upgradeAgent(agent.slug)
    const next: UpgradeState = res === 'sent' ? 'sent' : res === 'not_connected' ? 'not_connected' : 'fail'
    setState(next)
    window.setTimeout(() => setState('idle'), 6000)
  }

  return (
    <button
      type="button"
      onClick={onClick}
      disabled={state === 'busy'}
      title={t('routers.agent.upgradeTip', { version: agent.version ?? '?' })}
      className={cn(
        'inline-flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-[11px] font-semibold transition-opacity hover:opacity-90 disabled:opacity-50',
        state === 'sent' ? 'bg-ok text-canvas'
        : state === 'fail' || state === 'not_connected' ? 'bg-danger text-canvas'
        : 'bg-accent text-canvas',
        className,
      )}
    >
      {state === 'busy' && <Loader2 className="h-3 w-3 animate-spin" />}
      {label}
    </button>
  )
}
