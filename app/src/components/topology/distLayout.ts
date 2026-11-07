/**
 * Separación inicial de los distnodes (switches inferidos/gestionados).
 *
 * El resolver de colisiones general trata routers y distnodes como fijos y
 * solo empuja los chips, así que un switch inferido cuyo abanico cae encima de
 * otro nodo se quedaba solapado hasta que el admin lo arrastraba (#1957). Este
 * pase mueve SOLO los distnodes, antes de aplicar las posiciones pineadas del
 * usuario, para que el primer layout automático ya salga sin solapes.
 *
 * Módulo puro y determinista: sin imports del modelo ni del DOM.
 */

export interface RelaxNode {
  id: string
  x: number
  y: number
  r: number
}

export interface RelaxObstacle {
  x: number
  y: number
  r: number
  margin?: number
}

export interface RelaxBounds {
  width: number
  height: number
}

/** Margen mínimo entre dos distnodes y entre un distnode y un obstáculo. */
const NODE_GAP = 26
const OBSTACLE_GAP = 18
/** Muelle suave hacia la posición que el abanico eligió, para no dispersar. */
const SPRING = 0.03

export function relaxDistNodes(
  nodes: RelaxNode[],
  obstacles: RelaxObstacle[],
  bounds: RelaxBounds,
  iterations = 90,
): Map<string, { x: number; y: number }> {
  const pts = nodes.map((n) => ({ x: n.x, y: n.y, homeX: n.x, homeY: n.y, r: n.r }))
  const clamp = (p: { x: number; y: number }, r: number) => {
    const pad = r + 16
    p.x = Math.min(Math.max(p.x, pad), Math.max(pad, bounds.width - pad))
    p.y = Math.min(Math.max(p.y, pad), Math.max(pad, bounds.height - pad))
  }
  for (const p of pts) clamp(p, p.r)

  for (let iter = 0; iter < iterations; iter++) {
    let maxMove = 0
    for (let i = 0; i < pts.length; i++) {
      const a = pts[i]!
      let dx = 0
      let dy = 0
      // Empuje determinista cuando dos centros coinciden exactamente.
      const push = (bx: number, by: number, minDist: number, weight: number) => {
        let vx = a.x - bx
        let vy = a.y - by
        let dist = Math.hypot(vx, vy)
        if (dist >= minDist) return
        if (dist < 0.001) {
          const ang = ((i * 137.508 + iter * 15) * Math.PI) / 180
          vx = Math.cos(ang)
          vy = Math.sin(ang)
          dist = 1
        }
        const overlap = ((minDist - dist) / dist) * weight
        dx += vx * overlap
        dy += vy * overlap
      }
      for (const ob of obstacles) {
        push(ob.x, ob.y, a.r + ob.r + (ob.margin ?? OBSTACLE_GAP), 1)
      }
      for (let j = 0; j < pts.length; j++) {
        if (j === i) continue
        const b = pts[j]!
        push(b.x, b.y, a.r + b.r + NODE_GAP, 0.5)
      }
      // Muelle hacia casa solo en la primera fase: si se mantiene hasta el
      // final, el equilibrio queda un pelín por debajo de la distancia
      // mínima y el solape no llega a resolverse. Las últimas iteraciones
      // separan sin muelle.
      const spring = iter < Math.floor(iterations * 0.6) ? SPRING : 0
      dx += (a.homeX - a.x) * spring
      dy += (a.homeY - a.y) * spring
      a.x += dx
      a.y += dy
      clamp(a, a.r)
      maxMove = Math.max(maxMove, Math.abs(dx), Math.abs(dy))
    }
    if (iter >= Math.floor(iterations * 0.6) && maxMove < 0.05) break
  }

  const out = new Map<string, { x: number; y: number }>()
  nodes.forEach((n, i) => {
    const p = pts[i]!
    out.set(n.id, { x: Math.round(p.x * 10) / 10, y: Math.round(p.y * 10) / 10 })
  })
  return out
}
