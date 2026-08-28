import { useNetPulse } from '@/data/DataProvider'
import type { AgentInfo } from '@/data/types'

/** Agente nativo registrado en un router. El backend resuelve la asociación
 *  por slug/hostname/MAC, así que preferimos `routerId` y caemos a `slug`
 *  para compatibilidad con configuraciones antiguas (#282). */
export function useAgentFor(routerId: string): AgentInfo | undefined {
  const { agents } = useNetPulse()
  return agents.find((a) => a.routerId === routerId || a.slug === routerId)
}
