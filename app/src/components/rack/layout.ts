// layout.ts - constantes y conversiones grid <-> píxeles del rack canvas.
// U 1-based desde el rail inferior (RF y crece hacia abajo: la U 1 va abajo
// del todo). Columnas 0-based.

export const U_PX = 48
/** Proporción real de un rack 19": ancho útil entre raíles (482,6 mm) /
 * alto de 1U (44,45 mm) ≈ 10,86. El interior mide 10,86 U de ancho. */
export const RACK19_RATIO = 10.86
/** Ancho de columna derivado: interior = 12 columnas = U_PX * RACK19_RATIO. */
export const COL_PX = (U_PX * RACK19_RATIO) / 12
export const RAIL_PX = 18
export const RACK_HEADER_PX = 24

/** Ancho interior útil de un rack de ancho completo. */
export function interiorWidthPx(): number {
  return 12 * COL_PX
}

/** Alto total del nodo rack. */
export function rackHeightPx(uHeight: number): number {
  return RACK_HEADER_PX + uHeight * U_PX + 4
}

/** Ancho total del nodo rack. */
export function rackWidthPx(): number {
  return RAIL_PX * 2 + interiorWidthPx()
}

/**
 * Posición RELATIVA al nodo rack de un montaje (RF nested nodes). Interior
 * empieza tras el rail izquierdo y bajo la cabecera.
 */
export function mountRelPos(
  rackUHeight: number,
  uStart: number,
  uHeight: number,
  colStart: number,
): { x: number; y: number } {
  return {
    x: RAIL_PX + colStart * COL_PX,
    y: RACK_HEADER_PX + (rackUHeight - uStart - uHeight + 1) * U_PX,
  }
}

/**
 * Celda de grid correspondiente a una posición relativa suelta (inversa de
 * mountRelPos, redondeando al hueco más cercano).
 */
export function relPosToCell(
  rackUHeight: number,
  x: number,
  y: number,
  uHeight: number,
): { uStart: number; colStart: number } {
  const colStart = Math.round((x - RAIL_PX) / COL_PX)
  const uTop = Math.round((y - RACK_HEADER_PX) / U_PX)
  return { uStart: rackUHeight - uTop - uHeight + 1, colStart }
}
