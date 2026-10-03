// rackFaceplates.ts - catálogo declarativo de faceplates del rack canvas.
// Dominio puro, SIN dependencias de runtime: importable desde el canvas, el
// picker y scripts node (type stripping). Las plantillas se expresan en
// coordenadas de unidad 0..1 del faceplate: el puerto mantiene su sitio a
// cualquier zoom y con cualquier altura de U.
//
// Bandas no solapadas por construcción (el renderer las dibuja en este
// orden): LED de estado fijo a la izquierda, labelBox (nombre, clipped),
// artwork del equipo y, a la derecha, los puertos.

export type RackPortKind = 'rj45' | 'sfp' | 'sfp+'

export interface FaceplatePort {
  id: string
  kind: RackPortKind
  x: number
  y: number
}

export interface PortRow {
  kind: RackPortKind
  count: number
}

/** Zona horizontal de las bocas RJ45 (0..1); los SFP van en columna a su
 * derecha. La disposición concreta (filas de hasta 24, reparto uniforme)
 * la calcula layoutPortZone: sirve para 4 bocas como para 48+4. */
export interface PortZone {
  xStart: number
  xEnd: number
}

export interface FaceplateTemplate {
  id: string
  /** clave i18n del nombre en el picker (rack.plates.<id>) */
  nameKey: string
  group: 'device' | 'accessory'
  /** artwork que dibuja el renderer (server, switch, router, panel, pdu...) */
  artwork: string
  uHeight: number
  /** 12 = ancho completo, 6 = mitad, 4 = tercio, 3 = cuarto */
  colSpan: number
  /** LED de estado: centro en fracciones 0..1; r queda como referencia
   * vertical (el renderer dibuja un punto de tamaño fijo) */
  led: { x: number; y: number; r: number }
  labelBox: { x: number; y: number; w: number; h: number }
  rows: PortRow[]
  portZone?: PortZone
  /** los puertos de un patch panel son pass-through: admite 2 cables */
  passThrough?: boolean
}

const fullWidth = 12

export const FACEPLATES: FaceplateTemplate[] = [
  {
    id: 'server-1u',
    nameKey: 'rack.plates.server-1u',
    group: 'device',
    artwork: 'server',
    uHeight: 1,
    colSpan: fullWidth,
    led: { x: 0.035, y: 0.5, r: 0.16 },
    labelBox: { x: 0.07, y: 0.28, w: 0.5, h: 0.44 },
    portZone: { xStart: 0.52, xEnd: 0.94 },
    rows: [
      { kind: 'rj45', count: 2 },
      { kind: 'sfp+', count: 2 },
    ],
  },
  {
    id: 'server-2u',
    nameKey: 'rack.plates.server-2u',
    group: 'device',
    artwork: 'server',
    uHeight: 2,
    colSpan: fullWidth,
    led: { x: 0.035, y: 0.25, r: 0.08 },
    labelBox: { x: 0.07, y: 0.14, w: 0.45, h: 0.22 },
    portZone: { xStart: 0.5, xEnd: 0.94 },
    rows: [
      { kind: 'rj45', count: 4 },
      { kind: 'sfp+', count: 2 },
      { kind: 'rj45', count: 2 },
    ],
  },
  {
    id: 'switch',
    nameKey: 'rack.plates.switch',
    group: 'device',
    artwork: 'switch',
    uHeight: 1,
    colSpan: fullWidth,
    led: { x: 0.03, y: 0.5, r: 0.16 },
    labelBox: { x: 0.06, y: 0.3, w: 0.16, h: 0.4 },
    portZone: { xStart: 0.24, xEnd: 0.8 },
    rows: [
      { kind: 'rj45', count: 12 },
      { kind: 'rj45', count: 12 },
      { kind: 'sfp+', count: 4 },
    ],
  },
  {
    id: 'router',
    nameKey: 'rack.plates.router',
    group: 'device',
    artwork: 'router',
    uHeight: 1,
    colSpan: fullWidth,
    led: { x: 0.035, y: 0.5, r: 0.16 },
    labelBox: { x: 0.07, y: 0.3, w: 0.3, h: 0.4 },
    portZone: { xStart: 0.44, xEnd: 0.88 },
    rows: [
      { kind: 'rj45', count: 5 },
      { kind: 'sfp+', count: 1 },
      { kind: 'rj45', count: 1 },
    ],
  },
  {
    id: 'nas-tower',
    nameKey: 'rack.plates.nas-tower',
    group: 'device',
    artwork: 'nas',
    uHeight: 4,
    colSpan: 6,
    led: { x: 0.07, y: 0.06, r: 0.02 },
    labelBox: { x: 0.14, y: 0.04, w: 0.6, h: 0.05 },
    portZone: { xStart: 0.4, xEnd: 0.94 },
    rows: [{ kind: 'rj45', count: 2 }],
  },
  {
    id: 'patch-panel-1u',
    nameKey: 'rack.plates.patch-panel-1u',
    group: 'accessory',
    artwork: 'panel',
    uHeight: 1,
    colSpan: fullWidth,
    led: { x: 0.03, y: 0.5, r: 0.16 },
    labelBox: { x: 0.06, y: 0.3, w: 0.12, h: 0.4 },
    portZone: { xStart: 0.2, xEnd: 0.96 },
    rows: [
      { kind: 'rj45', count: 12 },
      { kind: 'rj45', count: 12 },
    ],
    passThrough: true,
  },
  {
    id: 'patch-panel-12p',
    nameKey: 'rack.plates.patch-panel-12p',
    group: 'accessory',
    artwork: 'panel',
    uHeight: 1,
    colSpan: 12,
    led: { x: 0.03, y: 0.5, r: 0.16 },
    labelBox: { x: 0.06, y: 0.3, w: 0.12, h: 0.4 },
    portZone: { xStart: 0.2, xEnd: 0.96 },
    rows: [{ kind: 'rj45', count: 12 }],
    passThrough: true,
  },
  {
    id: 'patch-panel-48p',
    nameKey: 'rack.plates.patch-panel-48p',
    group: 'accessory',
    artwork: 'panel',
    uHeight: 2,
    colSpan: 12,
    led: { x: 0.03, y: 0.25, r: 0.08 },
    labelBox: { x: 0.06, y: 0.14, w: 0.14, h: 0.22 },
    portZone: { xStart: 0.2, xEnd: 0.96 },
    rows: [
      { kind: 'rj45', count: 24 },
      { kind: 'rj45', count: 24 },
    ],
    passThrough: true,
  },
  {
    id: 'pdu-1u',
    nameKey: 'rack.plates.pdu-1u',
    group: 'accessory',
    artwork: 'pdu',
    uHeight: 1,
    colSpan: fullWidth,
    led: { x: 0.03, y: 0.5, r: 0.16 },
    labelBox: { x: 0.06, y: 0.3, w: 0.5, h: 0.4 },
    rows: [],
  },
  {
    id: 'ups-4u',
    nameKey: 'rack.plates.ups-4u',
    group: 'device',
    artwork: 'ups',
    uHeight: 4,
    colSpan: fullWidth,
    led: { x: 0.03, y: 0.08, r: 0.04 },
    labelBox: { x: 0.06, y: 0.05, w: 0.5, h: 0.07 },
    portZone: { xStart: 0.7, xEnd: 0.94 },
    rows: [{ kind: 'rj45', count: 1 }],
  },
  {
    id: 'shelf-1u',
    nameKey: 'rack.plates.shelf-1u',
    group: 'accessory',
    artwork: 'shelf',
    uHeight: 1,
    colSpan: fullWidth,
    led: { x: 0.03, y: 0.5, r: 0.16 },
    labelBox: { x: 0.06, y: 0.3, w: 0.6, h: 0.4 },
    rows: [],
  },
  {
    id: 'blank-1u',
    nameKey: 'rack.plates.blank-1u',
    group: 'accessory',
    artwork: 'blank',
    uHeight: 1,
    colSpan: fullWidth,
    led: { x: 0.03, y: 0.5, r: 0.16 },
    labelBox: { x: 0.06, y: 0.3, w: 0.88, h: 0.4 },
    rows: [],
  },
  {
    id: 'blank-half-1u',
    nameKey: 'rack.plates.blank-half-1u',
    group: 'accessory',
    artwork: 'blank',
    uHeight: 1,
    colSpan: 6,
    led: { x: 0.06, y: 0.5, r: 0.16 },
    labelBox: { x: 0.12, y: 0.3, w: 0.76, h: 0.4 },
    rows: [],
  },
]

const byId = new Map(FACEPLATES.map((p) => [p.id, p]))

export function getFaceplate(id: string): FaceplateTemplate | undefined {
  return byId.get(id)
}

/**
 * seedPorts expande las filas declarativas en puertos con IDs estables
 * ("p01", "p02", ...) y posiciones 0..1. Aplicar una plantilla siembra los
 * puertos; el usuario los edita después.
 */
export function seedPorts(plate: FaceplateTemplate): FaceplatePort[] {
  const counts: Partial<Record<RackPortKind, number>> = {}
  for (const row of plate.rows) counts[row.kind] = (counts[row.kind] ?? 0) + row.count
  return layoutPortZone(counts, plate.portZone ?? { xStart: 0.24, xEnd: 0.94 })
}

/**
 * layoutPortZone: disposición uniforme de bocas en la zona, pensada para el
 * peor caso real (48 RJ45 + 4 SFP+ en 1U): RJ45 en filas de hasta 24 (reparto
 * equitativo con la zona entera: 4 bocas la ocupan completa con hueco
 * garantizado entre ellas, igual que 24) y SFP/SFP+ en columna a la derecha
 * de la zona. Mismo criterio para el KP-9000 (9) que para rt2 (4).
 */
export function layoutPortZone(
  counts: Partial<Record<RackPortKind, number>>,
  zone: PortZone,
): FaceplatePort[] {
  const out: FaceplatePort[] = []
  const rj45 = counts.rj45 ?? 0
  const sfp = counts.sfp ?? 0
  const sfpPlus = counts['sfp+'] ?? 0
  const sfps = sfp + sfpPlus
  const rjEnd = sfps > 0 ? Math.max(zone.xStart + 0.1, zone.xEnd - 0.08) : zone.xEnd - 0.02
  if (rj45 > 0) {
    const rows = Math.max(1, Math.ceil(rj45 / 24))
    const perRow = Math.ceil(rj45 / rows)
    let n = 0
    for (let row = 0; row < rows; row++) {
      const inRow = Math.min(perRow, rj45 - row * perRow)
      const y = rows === 1 ? 0.5 : 0.26 + (0.74 - 0.26) * (row / (rows - 1))
      for (let i = 0; i < inRow; i++) {
        n++
        out.push({
          id: 'p' + String(n).padStart(2, '0'),
          kind: 'rj45',
          x: inRow === 1 ? (zone.xStart + rjEnd) / 2 : zone.xStart + (rjEnd - zone.xStart) * (i / (inRow - 1)),
          y,
        })
      }
    }
  }
  for (let i = 0; i < sfps; i++) {
    out.push({
      id: 'p' + String(out.length + 1).padStart(2, '0'),
      kind: i < sfpPlus ? 'sfp+' : 'sfp',
      x: zone.xEnd - 0.02,
      y: sfps === 1 ? 0.5 : 0.26 + (0.74 - 0.26) * (i / (sfps - 1)),
    })
  }
  return out
}

/**
 * layoutPhysicalPorts dispone puertos físicos reales (id = ifName) con el
 * mismo algoritmo de zona, preservando los ids en el orden de disposición.
 */
export function layoutPhysicalPorts(
  ports: { id: string; kind: RackPortKind }[],
  zone?: { xStart: number; xEnd: number },
): FaceplatePort[] {
  const counts: Partial<Record<RackPortKind, number>> = {}
  for (const p of ports) counts[p.kind] = (counts[p.kind] ?? 0) + 1
  const laid = layoutPortZone(counts, zone ?? { xStart: 0.24, xEnd: 0.94 })
  const byKind: Record<string, string[]> = {}
  for (const p of ports) {
    ;(byKind[p.kind] = byKind[p.kind] ?? []).push(p.id)
  }
  const used: Record<string, number> = {}
  return laid.map((slot) => {
    const ids = byKind[slot.kind] ?? []
    const idx = Math.min(used[slot.kind] ?? 0, ids.length - 1)
    used[slot.kind] = (used[slot.kind] ?? 0) + 1
    return { ...slot, id: ids[idx] ?? slot.id }
  })
}

/**
 * suggestFaceplate sugiere plantilla desde el tipo detectado por NetPulse
 * (clasificador interno, valores canónicos en español con alias en inglés).
 * Tipos sin faceplate propio devuelven undefined y el caller decide.
 */
export function suggestFaceplate(deviceType: string): FaceplateTemplate | undefined {
  switch (deviceType.toLowerCase().trim()) {
    case 'router':
    case 'ap': // los AP van montados como cualquier equipo de red pequeño
    case 'punto de acceso':
      return byId.get('router')
    case 'switch':
      return byId.get('switch')
    case 'servidor':
    case 'ordenador':
    case 'server':
    case 'desktop':
      return byId.get('server-1u')
    case 'nas':
      return byId.get('nas-tower')
    case 'ups':
    case 'sai':
      return byId.get('ups-4u')
    case 'pdu':
      return byId.get('pdu-1u')
    default:
      return undefined
  }
}

/** applyFaceplate combina suggest + seed para un tipo detectado. */
export function applyFaceplate(deviceType: string): { plate: FaceplateTemplate; ports: FaceplatePort[] } | undefined {
  const plate = suggestFaceplate(deviceType)
  if (!plate) return undefined
  return { plate, ports: seedPorts(plate) }
}
