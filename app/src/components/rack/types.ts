// types.ts - estado compartido entre la página y los nodos del canvas.
import type { Footprint } from '@/lib/rackGeometry'

/** Ghost de drop durante el drag de un montaje. */
export interface GhostState extends Footprint {
  valid: boolean
}

export type CableVisibility = 'hover' | 'always' | 'hidden'
