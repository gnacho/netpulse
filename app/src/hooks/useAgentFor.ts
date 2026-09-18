import { useNetPulse } from '@/data/DataProvider'
import type { AgentInfo } from '@/data/types'
import { findAgentFor } from '@/lib/agentMatch'

/** Agente nativo registrado en un router. El backend resuelve la asociación
 *  por slug/hostname/MAC, así que preferimos `routerId` y caemos a `slug`
 *  para compatibilidad con configuraciones antiguas (#282). */
export function useAgentFor(routerId: string): AgentInfo | undefined {
  const { agents } = useNetPulse()
  return findAgentFor(agents, routerId)
}
