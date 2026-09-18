/**
 * Matching an agent to its router.
 *
 * An agent registers under its own slug, which is not the router's id: the
 * slug comes from the board (its hostname), the id from the fleet table, and
 * they only coincide when both were named the same. The server already knows
 * the association and sends it as `routerId` (#282), with the hostname as a
 * third key for older setups whose overview id differs from the table id
 * (#483).
 *
 * Every screen that asks "does this router have an agent?" has to use all
 * three keys, or it shows "agent not installed" next to a router that is
 * being polled by its agent right now.
 */
import type { AgentInfo } from '@/data/types'

/** The three keys the matching needs; anything agent-shaped can be passed. */
export type AgentKeys = Pick<AgentInfo, 'slug' | 'routerId' | 'hostname'>

/** True when this agent belongs to the router with that id. */
export function agentMatchesRouter(a: AgentKeys, routerId: string): boolean {
  return (
    a.routerId === routerId ||
    a.slug === routerId ||
    (a.hostname !== undefined && a.hostname.toLowerCase() === routerId.toLowerCase())
  )
}

/**
 * The agent of a router, preferring the association the server states over
 * the two name-based fallbacks, so a coincidence of names never wins over it.
 */
export function findAgentFor<T extends AgentKeys>(agents: T[], routerId: string): T | undefined {
  return (
    agents.find((a) => a.routerId === routerId) ??
    agents.find((a) => a.slug === routerId) ??
    agents.find((a) => a.hostname !== undefined && a.hostname.toLowerCase() === routerId.toLowerCase())
  )
}

/** The router an agent belongs to: the inverse lookup, same three keys. */
export function findRouterFor<T extends { id: string }>(routers: T[], agent: AgentKeys): T | undefined {
  return (
    routers.find((r) => r.id === agent.routerId) ??
    routers.find((r) => r.id === agent.slug) ??
    routers.find((r) => agent.hostname !== undefined && r.id.toLowerCase() === agent.hostname.toLowerCase())
  )
}
