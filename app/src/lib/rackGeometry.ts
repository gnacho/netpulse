// rackGeometry.ts - port del dominio Go (internal/rack/geometry.go), misma
// semántica: U 1-based desde el rail inferior, 12 columnas por U, snap que
// busca primero la U de destino y luego camina hacia fuera, con la columna
// deseada primero y barrido izquierda-derecha. Módulo puro (node-testeable).

export const RACK_COLUMNS = 12

export interface Footprint {
  uStart: number
  uHeight: number
  colStart: number
  colSpan: number
}

export function overlaps(a: Footprint, b: Footprint): boolean {
  return a.uStart < b.uStart + b.uHeight && b.uStart < a.uStart + a.uHeight &&
    a.colStart < b.colStart + b.colSpan && b.colStart < a.colStart + a.colSpan
}

export function fitsRack(rackUHeight: number, f: Footprint): boolean {
  return f.uStart >= 1 && f.uHeight >= 1 && f.uStart + f.uHeight - 1 <= rackUHeight &&
    f.colStart >= 0 && f.colSpan >= 1 && f.colStart + f.colSpan <= RACK_COLUMNS
}

/** existing debe EXCLUIR la huella del montaje que se está moviendo. */
export function canPlace(rackUHeight: number, existing: Footprint[], cand: Footprint): boolean {
  if (!fitsRack(rackUHeight, cand)) return false
  return !existing.some((e) => overlaps(e, cand))
}

function tryAt(rackUHeight: number, existing: Footprint[], u: number, c: number, uHeight: number, colSpan: number): Footprint | null {
  const f: Footprint = { uStart: u, uHeight, colStart: c, colSpan }
  return canPlace(rackUHeight, existing, f) ? f : null
}

/** Snap del drop: U destino primero, luego hacia fuera; columna deseada
 * (clampada) primero, barrido izq→der. null = drop imposible (preview rojo,
 * nunca colisión silenciosa). */
export function findSlot(rackUHeight: number, existing: Footprint[], dropU: number, dropCol: number, uHeight: number, colSpan: number): Footprint | null {
  if (uHeight < 1 || colSpan < 1) return null
  const maxU = rackUHeight - uHeight + 1
  const maxCol = RACK_COLUMNS - colSpan
  if (maxU < 1 || maxCol < 0) return null
  const col = Math.min(Math.max(dropCol, 0), maxCol)
  const targetU = Math.min(Math.max(dropU, 1), maxU)
  for (let offset = 0; offset < rackUHeight; offset++) {
    const us = offset === 0 ? [targetU] : [targetU - offset, targetU + offset]
    for (const u of us) {
      if (u < 1 || u > maxU) continue
      const first = tryAt(rackUHeight, existing, u, col, uHeight, colSpan)
      if (first) return first
      for (let c = 0; c <= maxCol; c++) {
        if (c === col) continue
        const f = tryAt(rackUHeight, existing, u, c, uHeight, colSpan)
        if (f) return f
      }
    }
  }
  return null
}
