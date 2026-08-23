import { useTranslation } from 'react-i18next'
import type { AgentInfo } from '@/data/types'
import { StatusPill } from '@/components/StatusPill'
import { cn } from '@/lib/utils'

interface AgentBadgeProps {
  /** Agente del router; undefined/null → no se renderiza nada (demo, sin agentes) */
  agent: AgentInfo | undefined
  /**
   * Router configurado para funcionar solo con agente (sin SSH). Con esto hay
   * certeza: si `agent` es undefined y `agentOnly`, el agente NO está instalado
   * (rojo); sin agentOnly, simplemente no hay agente (nada que avisar).
   */
  agentOnly?: boolean
  className?: string
}

/**
 * Badge "Agente" (Fase 3 piloto):
 * - `fresh` → tono info discreto, tooltip con versión y cadencia de push.
 * - caído  → rojo, tooltip "agente registrado pero no responde" (las alertas
 *   de categoría system ya narran la caída/recuperación; aquí solo el estado).
 * - no instalado (agentOnly sin agente) → rojo, tooltip "reinstalar desde el
 *   detalle".
 */
export function AgentBadge({ agent, agentOnly, className }: AgentBadgeProps) {
  const { t } = useTranslation()
  if (!agent && !agentOnly) return null
  if (!agent) {
    return (
      <span title={t('routers.agent.notInstalledTip')} className={cn('inline-flex', className)}>
        <StatusPill tone="danger" label={t('routers.agent.notInstalled')} pulse />
      </span>
    )
  }
  const title = agent.fresh
    ? agent.version
      ? t('routers.agent.freshTip', { version: agent.version })
      : t('routers.agent.freshTipUnknown')
    : t('routers.agent.staleTip')
  return (
    <span title={title} className={cn('inline-flex', className)}>
      <StatusPill
        tone={agent.fresh ? 'info' : 'danger'}
        label={agent.fresh ? t('routers.agent.badge') : t('routers.agent.stale')}
        pulse={!agent.fresh}
      />
    </span>
  )
}
