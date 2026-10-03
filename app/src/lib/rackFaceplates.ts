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
  /** fracción horizontal 0..1 del primer puerto (centro) */
  xStart: number
  /** fracción horizontal 0..1 del último puerto (centro) */
  xEnd: number
  /** fracción vertical 0..1 de la fila (centro) */
  y: number
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
  /** LED de estado: centro en fracciones 0..1; r = semi-extensión vertical en fracción del alto del faceplate (el renderer ajusta el ancho al aspecto) */
  led: { x: number; y: number; r: number }
  labelBox: { x: number; y: number; w: number; h: number }
  rows: PortRow[]
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
    rows: [
      { kind: 'rj45', count: 2, xStart: 0.62, xEnd: 0.72, y: 0.5 },
      { kind: 'sfp+', count: 2, xStart: 0.8, xEnd: 0.9, y: 0.5 },
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
    rows: [
      { kind: 'rj45', count: 4, xStart: 0.6, xEnd: 0.92, y: 0.25 },
      { kind: 'sfp+', count: 2, xStart: 0.66, xEnd: 0.78, y: 0.62 },
      { kind: 'rj45', count: 2, xStart: 0.84, xEnd: 0.92, y: 0.62 },
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
    rows: [
      { kind: 'rj45', count: 12, xStart: 0.24, xEnd: 0.64, y: 0.3 },
      { kind: 'rj45', count: 12, xStart: 0.24, xEnd: 0.64, y: 0.7 },
      { kind: 'sfp+', count: 4, xStart: 0.72, xEnd: 0.92, y: 0.5 },
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
    rows: [
      { kind: 'rj45', count: 5, xStart: 0.38, xEnd: 0.62, y: 0.5 },
      { kind: 'sfp+', count: 1, xStart: 0.7, xEnd: 0.7, y: 0.5 },
      { kind: 'rj45', count: 1, xStart: 0.78, xEnd: 0.78, y: 0.5 },
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
    rows: [{ kind: 'rj45', count: 2, xStart: 0.6, xEnd: 0.8, y: 0.94 }],
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
    rows: [
      { kind: 'rj45', count: 12, xStart: 0.24, xEnd: 0.64, y: 0.3 },
      { kind: 'rj45', count: 12, xStart: 0.24, xEnd: 0.64, y: 0.7 },
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
    rows: [{ kind: 'rj45', count: 12, xStart: 0.26, xEnd: 0.66, y: 0.5 }],
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
    rows: [
      { kind: 'rj45', count: 24, xStart: 0.24, xEnd: 0.64, y: 0.55 },
      { kind: 'rj45', count: 24, xStart: 0.24, xEnd: 0.64, y: 0.8 },
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
    rows: [{ kind: 'rj45', count: 1, xStart: 0.9, xEnd: 0.9, y: 0.08 }],
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
  const out: FaceplatePort[] = []
  let n = 0
  for (const row of plate.rows) {
    for (let i = 0; i < row.count; i++) {
      const t = row.count === 1 ? 0 : i / (row.count - 1)
      n++
      out.push({
        id: 'p' + String(n).padStart(2, '0'),
        kind: row.kind,
        x: row.xStart + (row.xEnd - row.xStart) * t,
        y: row.y,
      })
    }
  }
  return out
}

/**
 * layoutPhysicalPorts dispone puertos físicos reales (id = ifName) en el
 * faceplate: rj45 en filas de hasta 12 a la izquierda, sfp/sfp+ en columna a
 * la derecha. Determinista; ids preservados para que el cable casé con la
 * interfaz real.
 */
export function layoutPhysicalPorts(ports: { id: string; kind: RackPortKind }[]): FaceplatePort[] {
  const rj45 = ports.filter((p) => p.kind === 'rj45')
  const sfps = ports.filter((p) => p.kind !== 'rj45')
  const out: FaceplatePort[] = []
  const rjRows = Math.max(1, Math.ceil(rj45.length / 12))
  rj45.forEach((p, i) => {
    const row = Math.floor(i / 12)
    const inRow = Math.min(12, rj45.length - row * 12)
    const idx = i % 12
    out.push({
      id: p.id,
      kind: p.kind,
      x: inRow === 1 ? 0.44 : 0.24 + (0.64 - 0.24) * (idx / (inRow - 1)),
      y: rjRows === 1 ? 0.5 : 0.28 + (0.72 - 0.28) * (row / (rjRows - 1)),
    })
  })
  sfps.forEach((p, i) => {
    out.push({
      id: p.id,
      kind: p.kind,
      x: 0.82,
      y: sfps.length === 1 ? 0.5 : 0.2 + 0.6 * (i / (sfps.length - 1)),
    })
  })
  return out
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
