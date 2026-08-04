import { useTranslation } from 'react-i18next'
import type { AgentInfo } from '@/data/types'
import { StatusPill } from '@/components/StatusPill'
import { cn } from '@/lib/utils'

interface AgentBadgeProps {
  /** Agente del router; undefined/null → no se renderiza nada (demo, sin agentes) */
  agent: AgentInfo | undefined
  className?: string
}

/**
 * Badge "Agente" (Fase 3 piloto):
 * - `fresh` → tono info discreto, tooltip con versión y cadencia de push.
 * - caído  → tono warn pulsante, tooltip "sondeando por SSH" (las alertas de
 *   categoría system ya narran la caída/recuperación; aquí solo el estado).
 */
export function AgentBadge({ agent, className }: AgentBadgeProps) {
  const { t } = useTranslation()
  if (!agent) return null
  const title = agent.fresh
    ? agent.version
      ? t('routers.agent.freshTip', { version: agent.version })
      : t('routers.agent.freshTipUnknown')
    : t('routers.agent.staleTip')
  return (
    <span title={title} className={cn('inline-flex', className)}>
      <StatusPill
        tone={agent.fresh ? 'info' : 'warn'}
        label={agent.fresh ? t('routers.agent.badge') : t('routers.agent.stale')}
        pulse={!agent.fresh}
      />
    </span>
  )
}
