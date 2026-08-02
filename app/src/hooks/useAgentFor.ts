import { useNetPulse } from '@/data/DataProvider'
import type { AgentInfo } from '@/data/types'

/** Agente nativo registrado en un router: casa `slug` con `router.id` (Fase 6). */
export function useAgentFor(routerId: string): AgentInfo | undefined {
  const { agents } = useNetPulse()
  return agents.find((a) => a.slug === routerId)
}
