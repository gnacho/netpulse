/**
 * NetPulse — Modelo del mapa de topología (v5, mockup aprobado 2-Ago-2026).
 *
 * `buildTopologyModel` deriva todo del bundle del DataProvider (mock en demo,
 * API en live). Vocabulario visual:
 *   - Chip por dispositivo: 26px cable (con puerto FDB) / 24px wifi (badge de
 *     banda; ámbar si señal débil). Badge "LLDP" si el vecino se anuncia.
 *   - Verde sólido = cable físico (WAN, uplinks cableados, cableados, CTs);
 *     discontinuo = inalámbrico (uplink wifi ámbar) o túnel (WG violeta).
 *   - DistributionNode: puerto con varias MACs en el FDB → OUI heterogéneo =
 *     "Switch o bridge (inferido)" (círculo dashed); OUI de hipervisor =
 *     host con badge +N y sus CTs/VMs anidados en grid.
 *   - Túneles WG trazados peer → Internet (el camino físico real es la nube).
 * Layout: gateway/APs en coordenadas canónicas; wifi en anillos concéntricos
 * con arcos prohibidos (uplink, etiqueta, WAN, abanico cableado); cableados
 * en abanicos por hub; CTs en grid bajo el host.
 */
import type { Band, Device, DistributionNode, Router, TopoSemantics, TopoSemLink, WanInfo, WGPeer, WireGuardStats } from '@/data/mock'

/** viewBox del lienzo SVG */
export const VB_W = 1000
export const VB_H = 850

// ---------------------------------------------------------------------------
// Tipos
// ---------------------------------------------------------------------------

export interface RouterNode {
  kind: 'router'
  id: string
  router: Router
  x: number
  y: number
  /** radio del círculo interior */
  r: number
  label: { x: number; y: number; anchor: 'start' | 'middle' | 'end' }
}

export interface PeerNode {
  kind: 'peer'
  id: string // `peer-${peer.id}`
  peer: WGPeer
  x: number
  y: number
}

/** Chip de un dispositivo cliente (wifi o cableado). */
export interface ChipNode {
  kind: 'chip'
  id: string // device id
  device: Device
  x: number
  y: number
  /** lado del chip en px (26 cable, 24/22 wifi según anillo) */
  size: number
  wired: boolean
  band: Band
  weak: boolean
  /** hub del que cuelga: router id | distnode id | device id (host/switch) */
  hubId: string
  /** CT/VM anidado bajo un hipervisor (grid, sin enlace propio al router) */
  isCt: boolean
}

/** Vista de un DistributionNode kind=inferred (círculo dashed, sin IP). */
export interface DistNodeView {
  kind: 'dist'
  id: string
  node: DistributionNode
  x: number
  y: number
  r: number
}

export type TopoLinkKind = 'wan' | 'uplink' | 'wired' | 'dist' | 'wg'

export interface TopoLink {
  id: string
  kind: TopoLinkKind
  d: string
  /** uplink inalámbrico (backhaul wifi): dashed ámbar */
  wifi?: boolean
  /** posición de la etiqueta del enlace (sin etiqueta si label='') */
  lx: number
  ly: number
  label: string
  width: number
  /** nº de paquetes animados simultáneos (∝ Mbps; guardrail global ≤60) */
  packets: number
  /** duración del viaje del paquete (s); menor = más tráfico */
  packetDur: number
  from: string
  to: string
}

export interface BackhaulRow {
  id: string // coincide con TopoLink.id
  a: string
  b: string
  /** clave i18n de `b` cuando el nombre del extremo B se traduce (p. ej. "Switch inferido · lan3") */
  bKey?: string
  bVars?: Record<string, string | number>
  kind: TopoLinkKind
  type: string
  speed: string
  signal: string
  tone: 'ok' | 'warn' | 'tunnel'
  statusLabel: string
  note?: string
  spark: number[]
  sparkColor: string
}

export interface TopologyInput {
  routers: Router[]
  devices: Device[]
  wan: WanInfo
  wireguard: WireGuardStats
  distributionNodes?: DistributionNode[]
  /**
   * Semántica de topología precalculada server-side (SPEC-65 D65-3/D65-9 B2):
   * anillos (miembros y orden), enlaces (from/to/kind) y "+N" de peers
   * ocultos. La GEOMETRÍA (coordenadas, radios, paths) sigue 100% aquí.
   * Ausente (servidor viejo o demo local) → cálculo local exacto de siempre.
   */
  topology?: TopoSemantics
  /** Versión del view-model (SPEC-65 D65-4): la semántica aplica si vm >= 1. */
  vm?: number
}

/** Chip "+N" de un anillo desbordado (posición ya calculada, geometría local).
 *  `devices` = clientes ocultos del anillo (los que el "+N" resume), para que
 *  el chip sea interactivo y los liste (issue #117). */
export interface RingOverflowChip {
  routerId: string
  count: number
  devices: Device[]
  x: number
  y: number
}

export interface TopologyModel {
  gatewayNode: RouterNode | null
  apNodes: RouterNode[]
  routerNodes: RouterNode[]
  internetNode: { id: 'internet'; x: number; y: number }
  peerNodes: PeerNode[]
  /** Peers activos que exceden las coordenadas canónicas (chip "+N") */
  hiddenPeers: WGPeer[]
  chips: ChipNode[]
  distNodes: DistNodeView[]
  /** CTs/VMs anidados por host hipervisor (hostId → chips en grid) */
  ctsByHost: Map<string, ChipNode[]>
  /** nº de CTs por host (badge +N) */
  ctCountByHost: Map<string, number>
  /** radios de los anillos wifi realmente usados por router (guías punteadas) */
  ringRadii: Map<string, number[]>
  /**
   * Chips "+N" por anillo desbordado (SPEC-65 D65-3 hiddenPeers, solo con
   * semántica server-side): clientes del anillo que exceden el aforo visible.
   */
  ringOverflowChips: RingOverflowChip[]
  links: TopoLink[]
  totalPackets: number
  activeLinkCount: number
  backhauls: BackhaulRow[]
  activePeerCount: number
  wan: WanInfo
  relatedTo: (nodeId: string) => { nodes: Set<string>; links: Set<string> }
}

// ---------------------------------------------------------------------------
// Colores (hex para SVG; alineados con design.md §3)
// ---------------------------------------------------------------------------

export const COLOR = {
  accent: '#22D3EE',
  tunnel: '#A78BFA',
  ok: '#34D399',
  warn: '#FBBF24',
  danger: '#F87171',
  info: '#60A5FA',
} as const

export function bandColor(band: Band, weak: boolean): string {
  if (weak) return COLOR.warn
  if (band === '5 GHz') return COLOR.accent
  if (band === '2.4 GHz') return COLOR.info
  return COLOR.ok
}

export function statusColor(status: Router['status']): string {
  if (status === 'warn') return COLOR.warn
  if (status === 'offline') return COLOR.danger
  return COLOR.ok
}

/** Color del enlace según tipo (verde = cable físico; ámbar = uplink wifi). */
export function linkColor(link: Pick<TopoLink, 'kind' | 'wifi'>): string {
  if (link.kind === 'wg') return COLOR.tunnel
  if (link.kind === 'uplink' && link.wifi) return COLOR.warn
  return COLOR.ok
}

// ---------------------------------------------------------------------------
// Coordenadas canónicas del lienzo (mockup v5: QA de colisiones hecha)
// ---------------------------------------------------------------------------

const GATEWAY_COORD = { x: 500, y: 250, r: 40, label: { x: 446, y: 312, anchor: 'end' as const } }
const AP_RADIUS = 32
const AP_ARC_CENTER = 90
const AP_ARC_HALF = 70
const AP_BASE_Y = 545

function apCoords(n: number, total: number): { x: number; y: number; r: number; label: { x: number; y: number; anchor: 'end' | 'start' } } {
  let angle: number
  if (total === 1) {
    angle = AP_ARC_CENTER
  } else {
    angle = AP_ARC_CENTER - AP_ARC_HALF + (2 * AP_ARC_HALF * n) / (total - 1)
  }
  const x = Math.round(GATEWAY_COORD.x + 280 * Math.cos(rad(angle)))
  const y = Math.round(AP_BASE_Y + 25 * Math.sin(rad(angle - AP_ARC_CENTER)))
  const isLeft = x < GATEWAY_COORD.x
  return {
    x, y, r: AP_RADIUS,
    label: { x: isLeft ? x - AP_RADIUS - 6 : x + AP_RADIUS + 6, y: y - 6, anchor: isLeft ? 'end' as const : 'start' as const },
  }
}
const INTERNET_COORD = { x: 500, y: 26 }
const PEER_COORDS = [
  { x: 265, y: 26 },
  { x: 735, y: 26 },
  { x: 140, y: 26 },
  { x: 860, y: 26 },
]
/** Túneles WG canónicos peer→Internet */
const WG_PATHS = [
  { d: 'M 272 44 C 330 82, 410 94, 474 80', dur: 3 },
  { d: 'M 728 44 C 670 82, 590 94, 526 80', dur: 3.4 },
]
/** Anillos wifi: radios ajustados para caber muchos chips (el resolver de
 *  colisiones mantiene 0 solapes). Suma de caps = topoGatewayRingCap (60) /
 *  topoAPRingCap (40) del server — deben coincidir para que GW_RING_VISIBLE
 *  no corte antes de tiempo. */
const GATEWAY_RINGS = [
  { r: 84, cap: 8 },
  { r: 116, cap: 14 },
  { r: 148, cap: 18 },
  { r: 180, cap: 20 },
]
const AP_RINGS = [
  { r: 78, cap: 8 },
  { r: 112, cap: 14 },
  { r: 148, cap: 18 },
]
/** Abanicos cableados: gateway tiene sector este (switch inferido) y oeste */
const GW_EAST_FAN: [number, number] = [336, 24] // wrap (a1 < a0)
const GW_WEST_FAN: [number, number] = [150, 210]
const AP_WIRED_FAN: [number, number] = [150, 210]
const DIST_FAN_RADIUS = 112
/** distnodes: hasta DIST_FAN_MAX hijos en abanico de 136°; con más, el arco
 *  de radio fijo los amontona (issue #5 bug 2: 8 bocas tras un switch en el
 *  mismo puerto) → anillos concéntricos alrededor del círculo dashed. */
const DIST_FAN_MAX = 5
const DIST_RINGS = [
  { r: 66, cap: 8 },
  { r: 110, cap: 14 },
]
const HUB_FAN_RADIUS = 96
/** device-hubs (switch gestionado): con más de HUB_FAN_MAX hijos, el abanico
 *  fijo de 90° a r=HUB_FAN_RADIUS los apiña (muchos puertos en el mismo
 *  switch) → anillos concéntricos, igual que los distnodes. */
const HUB_FAN_MAX = 8
const HUB_RINGS = [
  { r: 58, cap: 6 },
  { r: 94, cap: 12 },
]
const ROUTER_FAN_RADIUS = 176
/** Hosts hipervisores del gateway: lejos (su grid de CTs necesita espacio) */
const HYPERVISOR_FAN_RADIUS = 244
/** Host hipervisor de un switch/AP (no-gateway): radio desde su switch/AP.
 *  > ROUTER_FAN_RADIUS para quedar fuera del abanico de cableados directos;
 *  el resolver de colisiones lo acomoda si algún vecino está demasiado cerca. */
const HUB_HOST_RADIUS = 224
/** Grid de CTs bajo el host hipervisor */
const CT_COLS = 5
const CT_DX = 46
const CT_DY = 44
const CT_OFFSET_Y = 64
/** Grid de cableados directos del gateway (a partir de N, el abanico oeste
 *  de 60° los apiña: se pasa a filas×columnas al oeste, decisión 2-Ago-2026) */
const GW_GRID_MIN = 6
const GW_GRID_ROWS = 4
const GW_GRID_DX = 56
const GW_GRID_DY = 50
const GW_GRID_R0 = 160

// ---------------------------------------------------------------------------
// Utilidades de ángulos y layout (portadas del mockup aprobado)
// ---------------------------------------------------------------------------

const rad = (d: number) => (d * Math.PI) / 180
const norm = (a: number) => ((a % 360) + 360) % 360
const angleTo = (x: number, y: number, tx: number, ty: number) => norm((Math.atan2(ty - y, tx - x) * 180) / Math.PI)
const pos = (cx: number, cy: number, degA: number, r: number) => ({
  x: Math.round((cx + r * Math.cos(rad(degA))) * 10) / 10,
  y: Math.round((cy + r * Math.sin(rad(degA))) * 10) / 10,
})

/** Arco [±half] alrededor de `center`, como hasta 2 intervalos sin wrap. */
function arcAround(center: number, half: number): [number, number][] {
  const s = norm(center - half)
  const e = norm(center + half)
  return s <= e ? [[s, e]] : [[s, 360], [0, e]]
}

/** Hueco angular más grande cuya posición a `radius` de `origin` quede DENTRO
 *  del viewport (margen `m`). Si el hueco más grande apunta fuera del lienzo
 *  (p. ej. un switch en la esquina), se prueba el siguiente. Portable. */
function widestGapCenterInView(
  origin: { x: number; y: number },
  points: { x: number; y: number }[],
  radius: number,
  m = 30,
): number {
  if (points.length === 0) return 0
  const angles = points.map((p) => angleTo(origin.x, origin.y, p.x, p.y)).sort((a, b) => a - b)
  const gaps: { start: number; gap: number }[] = []
  for (let i = 0; i < angles.length; i++) {
    const start = angles[i]!
    const next = angles[(i + 1) % angles.length]! + (i === angles.length - 1 ? 360 : 0)
    gaps.push({ start, gap: next - start })
  }
  gaps.sort((a, b) => b.gap - a.gap)
  const inView = (a: number) => {
    const p = pos(origin.x, origin.y, a, radius)
    return p.x >= m && p.x <= VB_W - m && p.y >= m && p.y <= VB_H - m
  }
  const fallback = gaps[0] ? norm(gaps[0].start + gaps[0].gap / 2) : 0
  for (const g of gaps) {
    const center = norm(g.start + g.gap / 2)
    if (inView(center)) return center
  }
  // ningún hueco está dentro: probar ángulos cardinales, y si nada, el primero
  for (const a of [90, 180, 270, 0, 135, 225]) {
    if (inView(a)) return a
  }
  return fallback
}

/** Hueco angular más grande entre `points` cuya posición a `radius` del origen
 *  caiga DENTRO de uno de los arcos permitidos `allowed` y del viewport.
 *  El host hipervisor de un AP/switch usa esta variante: el cable que sale del
 *  AP hacia el host no debe cruzar sus clientes wifi, así que el host se
 *  restringe a los arcos que el AP ya excluye del wifi (hacia el gateway y el
 *  fan cableado). Si ningún hueco cae en un arco permitido, se prueba el centro
 *  de cada arco y, en última instancia, el hueco más grande (issue #141). */
function widestGapCenterInArcs(
  origin: { x: number; y: number },
  points: { x: number; y: number }[],
  radius: number,
  allowed: [number, number][],
  m = 30,
): number {
  if (points.length === 0) return 0
  const angles = points.map((p) => angleTo(origin.x, origin.y, p.x, p.y)).sort((a, b) => a - b)
  const gaps: { start: number; gap: number }[] = []
  for (let i = 0; i < angles.length; i++) {
    const start = angles[i]!
    const next = angles[(i + 1) % angles.length]! + (i === angles.length - 1 ? 360 : 0)
    gaps.push({ start, gap: next - start })
  }
  gaps.sort((a, b) => b.gap - a.gap)
  const inView = (a: number) => {
    const p = pos(origin.x, origin.y, a, radius)
    return p.x >= m && p.x <= VB_W - m && p.y >= m && p.y <= VB_H - m
  }
  const inAllowed = (a: number) =>
    allowed.some(([s, e]) => {
      const a2 = norm(a)
      return s <= e ? a2 >= s && a2 <= e : a2 >= s || a2 <= e
    })
  for (const g of gaps) {
    const center = norm(g.start + g.gap / 2)
    if (inAllowed(center) && inView(center)) return center
  }
  for (const [s, e] of allowed) {
    const span = e >= s ? e - s : e + 360 - s
    const center = norm(s + span / 2)
    if (inView(center)) return center
  }
  for (const a of [90, 180, 270, 0, 135, 225]) {
    if (inAllowed(a) && inView(a)) return a
  }
  return gaps[0] ? norm(gaps[0].start + gaps[0].gap / 2) : 0
}

/** arcos libres = [0,360) menos exclusiones (intervalos sin wrap) */
function freeArcs(excludes: [number, number][]): [number, number][] {
  const ex = excludes
    .map(([s, e]): [number, number] => [Math.max(0, s), Math.min(360, e)])
    .filter(([s, e]) => e > s)
    .sort((a, b) => a[0] - b[0])
  const free: [number, number][] = []
  let cur = 0
  for (const [s, e] of ex) {
    if (s > cur) free.push([cur, s])
    cur = Math.max(cur, e)
  }
  if (cur < 360) free.push([cur, 360])
  return free
}

/** reparte items en anillos concéntricos evitando arcos prohibidos */
const CHIP_GAP = 6
function ringLayoutCount<T extends { x: number; y: number; size?: number }>(
  items: T[],
  node: { x: number; y: number },
  rings: { r: number; cap: number }[],
  excludes: [number, number][],
): number {
  const free = freeArcs(excludes)
  const totalFree = free.reduce((a, [s, e]) => a + (e - s), 0)
  if (totalFree <= 0) return 0
  let idx = 0
  for (const ring of rings) {
    if (idx >= items.length) break
    const chipSize = items[idx]?.size ?? 24
    const minArcDeg = ((chipSize + CHIP_GAP) * 180) / (Math.PI * ring.r)
    let ringPlaced = 0
    for (let ai = 0; ai < free.length && ringPlaced < ring.cap && idx < items.length; ai++) {
      const [s, e] = free[ai]!
      const arcLen = e - s
      const maxFit = Math.max(1, Math.floor(arcLen / minArcDeg))
      const remaining = items.length - idx
      const k = Math.min(maxFit, ring.cap - ringPlaced, remaining)
      for (let i = 0; i < k; i++) {
        const a = s + (arcLen * (i + 0.5)) / k
        const p = pos(node.x, node.y, a, ring.r)
        Object.assign(items[idx++]!, p)
        ringPlaced++
      }
    }
  }
  return idx
}

function ringLayout<T extends { x: number; y: number; size?: number }>(
  items: T[],
  node: { x: number; y: number },
  rings: { r: number; cap: number }[],
  excludes: [number, number][],
): void {
  ringLayoutCount(items, node, rings, excludes)
  // Fallback anti (0,0): los que no cupieron se colocan en el primer arco libre
  // del anillo más externo (mejor que quedar apilados en el origen).
  const free = freeArcs(excludes)
  const outer = rings[rings.length - 1]
  const freeArr = free.filter(([s, e]) => e - s > 0)
  if (!outer || freeArr.length === 0) return
  const [s0, e0] = freeArr[0]!
  let placed = 0
  for (const item of items) {
    if (item.x === 0 && item.y === 0) {
      const n = placed
      const a = s0 + ((e0 - s0) * (n + 0.5)) / Math.max(1, items.filter((i) => i.x === 0 && i.y === 0).length)
      Object.assign(item, pos(node.x, node.y, a, outer.r))
      placed++
    }
  }
}

/** abanico de items en un arco (soporta wrap: a1 < a0) */
function fanLayout<T extends { x: number; y: number }>(
  items: T[],
  node: { x: number; y: number },
  r: number,
  a0: number,
  a1: number,
): void {
  if (a1 < a0) a1 += 360
  const n = items.length
  items.forEach((d, i) => {
    const a = a0 + ((a1 - a0) * (n === 1 ? 0.5 : i / (n - 1)))
    Object.assign(d, pos(node.x, node.y, norm(a), r))
  })
}

/** grid de filas × columnas al oeste del gateway (muchas bocas cableadas
 *  directas: el abanico de 60° las apiñaría). Llena por columnas hacia el
 *  oeste; la última columna parcial queda centrada verticalmente. */
function gridLayoutWest<T extends { x: number; y: number }>(items: T[], node: { x: number; y: number }): void {
  items.forEach((d, i) => {
    const col = Math.floor(i / GW_GRID_ROWS)
    const row = i % GW_GRID_ROWS
    const inThisCol = Math.min(GW_GRID_ROWS, items.length - col * GW_GRID_ROWS)
    d.x = node.x - GW_GRID_R0 - col * GW_GRID_DX
    d.y = node.y + (row - (inThisCol - 1) / 2) * GW_GRID_DY
  })
}

/** Resolución de colisiones por empuje iterativo + fuerza centrípeta.
 *  Cada chip tiene opcionalmente un hubId que lo atrae suavemente hacia su
 *  padre. Los nodos fijos (routers, distnodes) no se mueven.
 *  La fuerza centrípeta usa un RADIO OBJETIVO (restDist = la distancia que el
 *  layout inicial le asignó al chip respecto a su hub): atrae si el chip se
 *  ha separado (colisión lo empujó lejos) y empuja si se ha amontonado sobre
 *  el hub. Así los chips "respiran" alrededor de su posición canónica sin
 *  dispersarse ni amontonarse.
 *
 *  NORMA DE SEPARACIÓN (9-Ago-2026): los items fijos (routers, Internet,
 *  distnodes, switches) llevan un `margin` extra además de su radio — un chip
 *  no puede quedar a menos de `r_nodo + r_chip + margin + padding`. Antes solo
 *  evitaba que quedaran DENTRO del radio, así que un chip podía quedar pegado
 *  visualmente al borde de un router (p. ej. el host del switch a 85px del
 *  gateway parecía "pegado"). */
function resolveCollisions(
  items: { x: number; y: number; r: number; fixed?: boolean; margin?: number; hubX?: number; hubY?: number; restDist?: number; id?: string }[],
  iterations = 80,
  padding = 14,
  segments: { x1: number; y1: number; x2: number; y2: number; ownerId?: string }[] = [],
  cableMargin = 6,
): void {
  for (let iter = 0; iter < iterations; iter++) {
    let moved = false
    for (let i = 0; i < items.length; i++) {
      for (let j = i + 1; j < items.length; j++) {
        const a = items[i]!
        const b = items[j]!
        const dx = b.x - a.x
        const dy = b.y - a.y
        const dist = Math.sqrt(dx * dx + dy * dy) || 0.001
        // El margen de un nodo fijo SOLO aplica a chips de OTRO hub (no a los
        // propios de su anillo — si no, empuja todo el anillo del router hacia
        // fuera y el layout se expande). isOwn = el chip no-fijo tiene este
        // nodo como hub (hubX/hubY coinciden con las coords del fijo).
        const ownOfA = (b.hubX !== undefined && b.hubY !== undefined && Math.abs(b.hubX - a.x) < 0.5 && Math.abs(b.hubY - a.y) < 0.5)
        const ownOfB = (a.hubX !== undefined && a.hubY !== undefined && Math.abs(a.hubX - b.x) < 0.5 && Math.abs(a.hubY - b.y) < 0.5)
        const aSpace = a.r + (a.fixed && !ownOfB ? (a.margin ?? 0) : 0)
        const bSpace = b.r + (b.fixed && !ownOfA ? (b.margin ?? 0) : 0)
        const minDist = aSpace + bSpace + padding
        if (dist < minDist) {
          const overlap = (minDist - dist) * 0.45
          const nx = dx / dist
          const ny = dy / dist
          if (!a.fixed) { a.x -= nx * overlap; a.y -= ny * overlap }
          if (!b.fixed) { b.x += nx * overlap; b.y += ny * overlap }
          moved = true
        }
      }
    }
    // repulsión de segmentos (cables ajenos): empuje perpendicular al cable
    for (let i = 0; i < items.length; i++) {
      const it = items[i]!
      if (it.fixed || it.id === undefined) continue
      for (const seg of segments) {
        if (seg.ownerId === it.id) continue // el propio cable no se auto-repele
        const px = it.x, py = it.y
        const d = distToSegment(px, py, seg.x1, seg.y1, seg.x2, seg.y2)
        if (d >= it.r + cableMargin) continue
        const vx = seg.x2 - seg.x1
        const vy = seg.y2 - seg.y1
        const len = Math.hypot(vx, vy) || 1
        const t = clamp(((px - seg.x1) * vx + (py - seg.y1) * vy) / (len * len), 0, 1)
        const projX = seg.x1 + t * vx
        const projY = seg.y1 + t * vy
        const nx = (px - projX) / (d || 0.001)
        const ny = (py - projY) / (d || 0.001)
        const push = (it.r + cableMargin - d) * 0.4
        it.x += nx * push
        it.y += ny * push
        moved = true
      }
    }
    // fuerza centrípeta hacia el radio objetivo: proporcional a la desviación
    for (const item of items) {
      if (item.fixed || item.hubX === undefined || item.restDist === undefined) continue
      const dx = item.hubX! - item.x
      const dy = item.hubY! - item.y
      const dist = Math.sqrt(dx * dx + dy * dy) || 0.001
      const pull = (dist - item.restDist) * 0.04
      if (Math.abs(pull) > 0.02) {
        item.x += (dx / dist) * pull
        item.y += (dy / dist) * pull
        moved = true
      }
    }
    if (!moved) break
  }
}

/** distancia mínima del punto (px,py) al segmento (x1,y1)-(x2,y2) */
function distToSegment(px: number, py: number, x1: number, y1: number, x2: number, y2: number): number {
  const vx = x2 - x1, vy = y2 - y1
  const len2 = vx * vx + vy * vy || 1
  const t = clamp(((px - x1) * vx + (py - y1) * vy) / len2, 0, 1)
  const qx = x1 + t * vx, qy = y1 + t * vy
  return Math.hypot(px - qx, py - qy)
}

function clamp(v: number, lo: number, hi: number): number {
  return v < lo ? lo : v > hi ? hi : v
}

/** flujo de paquetes ∝ tráfico (mockup: umbrales 35/15/2 Mbps, guardrail 60).
 *  `alive`: enlace físico activo sin medición de tráfico (live no mide Mbps
 *  por cliente) → 1 paquete lento para que el cable "respire" (5s). */
function flowFor(mbps: number, alive = false): { packets: number; packetDur: number } {
  const packets = mbps >= 35 ? 3 : mbps >= 15 ? 2 : mbps >= 2 ? 1 : alive ? 1 : 0
  return { packets, packetDur: Math.max(1.6, 5 - mbps / 12) }
}

// ---------------------------------------------------------------------------
// Builder
// ---------------------------------------------------------------------------

export function buildTopologyModel({ routers, devices, wan, wireguard, distributionNodes = [], topology, vm }: TopologyInput): TopologyModel {
  // SPEC-65 D65-3/D65-9 B2: la semántica server-side aplica con vm >= 1 y
  // `topology` presente; sin ella, fallback EXACTO al cálculo local.
  const sem = topology && (vm ?? 1) >= 1 ? topology : undefined
  const gateway = routers.find((r) => r.roleBadge === 'Principal') ?? routers[0]
  const aps = routers.filter((r) => r.id !== gateway?.id && r.roleBadge !== 'SW')
  const switches = routers.filter((r) => r.roleBadge === 'SW')

  const gatewayNode: RouterNode | null = gateway
    ? { kind: 'router', id: gateway.id, router: gateway, ...GATEWAY_COORD }
    : null
  const internetNode = { id: 'internet' as const, ...INTERNET_COORD, r: 42 }
  const apNodes: RouterNode[] = aps.map((router, i) => ({ kind: 'router', id: router.id, router, ...apCoords(i, aps.length) }))
  // Switches gestionados: REFACTOR PORTABLE (9-Ago-2026).
  // Se colocan en el HUECO ANGULAR MÁS GRANDE alrededor del gateway (lejos de
  // los APs y del Internet), apilados radialmente. Antes se fijaban en una
  // esquina (190,120) — coordenadas específicas de la red del autor que no
  // funcionan con otras topologías (más/menos APs, otro switch...).
  const switchAnchor = gatewayNode
    ? (() => {
        const others = [...apNodes, internetNode].map((p) => ({ x: p.x, y: p.y }))
        const r = GATEWAY_RINGS[GATEWAY_RINGS.length - 1]!.r + 100
        const a = widestGapCenterInView(gatewayNode, others, r)
        return { x: Math.round(gatewayNode.x + r * Math.cos(rad(a))), y: Math.round(gatewayNode.y + r * Math.sin(rad(a))), angle: a }
      })()
    : { x: 190, y: 120, angle: 180 }
  const switchNodes: RouterNode[] = switches.slice(0, 3).map((router, i) => {
    const off = i * 55
    const x = Math.round(switchAnchor.x - off * Math.cos(rad(switchAnchor.angle)))
    const y = Math.round(switchAnchor.y - off * Math.sin(rad(switchAnchor.angle)))
    const isLeft = x < (gatewayNode?.x ?? 500)
    return {
      kind: 'router' as const,
      id: router.id,
      router,
      x,
      y,
      r: 28,
      label: { x: isLeft ? x - 34 : x + 34, y: y - 6, anchor: isLeft ? 'end' as const : 'start' as const },
    }
  })
  const routerNodes: RouterNode[] = gatewayNode ? [gatewayNode, ...apNodes, ...switchNodes] : [...apNodes, ...switchNodes]
  const routerById = new Map(routerNodes.map((n) => [n.id, n]))

  // D1: el switch gestionado existe como Device Y como distnode managed, pero
  // en el mapa se representa SOLO como nodo managed: se excluye de los chips
  // cualquier Device cuya MAC coincida con la chassis-MAC de un distnode.
  const managedMacs = new Set(
    distributionNodes.filter((n) => n.kind === 'managed' && n.mac).map((n) => n.mac!.toUpperCase()),
  )
  const online = devices.filter((d) => d.online && !managedMacs.has(d.mac.toUpperCase()))
  const deviceById = new Map(online.map((d) => [d.id, d]))
  const distById = new Map(distributionNodes.map((n) => [n.id, n]))
  /** hub al que cuelga un dispositivo: attachTo (si existe y resuelve) o su router.
   *  Regla de anclaje (2-Ago-2026, decisión usuario): un cableado SIN evidencia
   *  (ni attachTo ni puerto FDB) se ancla al GATEWAY — sin evidencia no se
   *  afirma que cuelgue de un AP. */
  const hubOf = (d: Device): string => {
    if (d.attachTo && (routerById.has(d.attachTo) || distById.has(d.attachTo) || deviceById.has(d.attachTo))) {
      return d.attachTo
    }
    if (isWired(d) && !d.port && gatewayNode) return gatewayNode.id
    return d.routerId
  }
  // SPEC-65 D65-3/D65-9 B2: con semántica server-side, los anillos llegan
  // calculados (miembros y orden: cableados primero, luego 5/2.4 GHz) junto
  // con el "+N" (hiddenPeers = ring − aforo visible de los anillos canónicos:
  // gateway 5+8=13, AP 7+13=20). Sin semántica, fallback al cálculo local.
  const GW_RING_VISIBLE = GATEWAY_RINGS.reduce((a, r) => a + r.cap, 0)
  const AP_RING_VISIBLE = AP_RINGS.reduce((a, r) => a + r.cap, 0)
  const ringOrder = new Map<string, string[]>()
  const ringVisible = new Map<string, Set<string>>()
  const ringOverflow = new Map<string, number>()
  if (sem) {
    for (const node of routerNodes) {
      const ring = sem.rings[node.id] ?? []
      ringOrder.set(node.id, ring)
      const cap = node.id === gatewayNode?.id ? GW_RING_VISIBLE : AP_RING_VISIBLE
      ringVisible.set(node.id, new Set(ring.slice(0, cap)))
      const hidden = sem.hiddenPeers?.[node.id] ?? 0
      if (hidden > 0) ringOverflow.set(node.id, hidden)
    }
  }
  const childrenOf = (hubId: string): Device[] => {
    const ring = ringOrder.get(hubId)
    if (ring) return ring.map((id) => deviceById.get(id)).filter((d): d is Device => d !== undefined)
    return online.filter((d) => hubOf(d) === hubId)
  }
  /** Chips visibles del anillo: con semántica, los ocultos ("+N") no se pintan. */
  const isVisibleInRing = (d: Device): boolean => {
    if (!sem) return true
    const vis = ringVisible.get(hubOf(d))
    return vis ? vis.has(d.id) : true
  }
  const isWired = (d: Device) => d.band === 'cable'

  /** Hubs que son dispositivos con hijos propios (switch gestionado, hipervisor) */
  const deviceHubs = new Set<string>()
  for (const d of online) {
    const h = d.attachTo
    if (h && deviceById.has(h)) deviceHubs.add(h)
  }

  // -- posicionamiento de hubs cableados (distnodes + device-hubs) y anchors --
  const distNodes: DistNodeView[] = []
  const anchorPos = new Map<string, { x: number; y: number }>() // hubId → pos
  const fanArcByRouter = new Map<string, [number, number][]>() // arcos ocupados por cableados (prohibidos para wifi)
  /** hosts hipervisores (distnode kind=hypervisor con hostDeviceId) */
  const hypervisorHosts = new Set(
    distributionNodes.filter((n) => n.kind === 'hypervisor' && n.hostDeviceId).map((n) => n.hostDeviceId!),
  )

  for (const node of routerNodes) {
    const isGw = node.id === gatewayNode?.id
    // Los distnodes inferidos y gestionados (LLDP) son anchors propios; el
    // hipervisor se posiciona vía su device host (hub), el distnode solo
    // aporta metadatos (puerto, macCount).
    const dists = distributionNodes.filter(
      (n) => n.routerId === node.id && (n.kind === 'inferred' || n.kind === 'managed'),
    )
    const directWired = childrenOf(node.id).filter((d) => isWired(d) && isVisibleInRing(d))
    const hubs = directWired.filter((d) => deviceHubs.has(d.id))
    const plain = directWired.filter((d) => !deviceHubs.has(d.id))

    // Anchors: todo cuelga de abanicos; gateway separa este/oeste
    const anchors: { id: string; kind: 'dist' | 'hub' | 'dev'; ref: DistributionNode | Device }[] = [
      ...dists.map((n) => ({ id: n.id, kind: 'dist' as const, ref: n })),
      ...hubs.map((d) => ({ id: d.id, kind: 'hub' as const, ref: d })),
      ...plain.map((d) => ({ id: d.id, kind: 'dev' as const, ref: d })),
    ]
    if (anchors.length === 0) continue

    const placed: { id: string; kind: 'dist' | 'hub' | 'dev'; ref: DistributionNode | Device; x: number; y: number }[] = []
    if (isGw) {
      // distnodes (switch inferido) al ESTE r134; hosts hipervisores al OESTE
      // r300 (su grid de CTs necesita espacio, mockup: pve a ~310px); resto OESTE r134
      const east = anchors.filter((a) => a.kind === 'dist')
      const farWest = anchors.filter((a) => a.kind === 'hub' && hypervisorHosts.has(a.id))
      const west = anchors.filter((a) => a.kind !== 'dist' && !farWest.includes(a))
      const eastItems = east.map((a) => ({ ...a, x: 0, y: 0 }))
      const farWestItems = farWest.map((a) => ({ ...a, x: 0, y: 0 }))
      const westItems = west.map((a) => ({ ...a, x: 0, y: 0 }))
      // distnodes del gateway (switch inferido al ESTE): fuera del círculo
      // virtual wifi (radio base máximo + margen) para no invadir la zona de
      // clientes wifi del Flint.
      fanLayout(eastItems, node, GATEWAY_RINGS[GATEWAY_RINGS.length - 1]!.r + 50, GW_EAST_FAN[0], GW_EAST_FAN[1])
      fanLayout(farWestItems, node, HYPERVISOR_FAN_RADIUS, 160, 200)
      if (westItems.length >= GW_GRID_MIN) {
        gridLayoutWest(westItems, node)
      } else {
        fanLayout(westItems, node, ROUTER_FAN_RADIUS, GW_WEST_FAN[0], GW_WEST_FAN[1])
      }
      placed.push(...eastItems, ...farWestItems, ...westItems)
      fanArcByRouter.set(node.id, [...arcAround(0, 26), [GW_WEST_FAN[0] - 8, GW_WEST_FAN[1] + 8]])
    } else {
      // No-gateway (AP o switch): separar los hosts hipervisores (necesitan
      // espacio para su grid de CTs) del resto de cableados directos.
      // REFACTOR PORTABLE (9-Ago-2026): el host se coloca en el CENTRO del
      // hueco angular más grande entre los vecinos (gateway, distnodes de este
      // router y otros routers) — regla de máxima distancia, sin arcos
      // hardcodeados por red. El radio se ajusta para que quede fuera del
      // círculo de sus propios cableados y dentro del viewport.
      const farWest = anchors.filter((a) => a.kind === 'hub' && hypervisorHosts.has(a.id))
      const rest = anchors.filter((a) => !farWest.includes(a))
      const farWestItems = farWest.map((a) => ({ ...a, x: 0, y: 0 }))
      const restItems = rest.map((a) => ({ ...a, x: 0, y: 0 }))
      fanLayout(restItems, node, ROUTER_FAN_RADIUS, AP_WIRED_FAN[0], AP_WIRED_FAN[1])
      // Vecinos desde los que alejarse: gateway, los distnodes del switch/AP y
      // los demás routers (APs/switches) — el host se abre paso en el hueco.
      const neighbors: { x: number; y: number }[] = []
      if (gatewayNode) neighbors.push(gatewayNode)
      for (const other of routerNodes) {
        if (other.id !== node.id) neighbors.push(other)
      }
      for (const dv of distNodes) neighbors.push(dv)
      // El cable del host no debe cruzar los clientes wifi del propio AP/switch:
      // el host se restringe a los arcos que el wifi ya excluye (hacia el
      // gateway y el fan cableado), donde este router no tiene clientes (issue
      // #141). En su interior se elige el hueco angular más grande.
      const hostArcs: [number, number][] = []
      if (gatewayNode) {
        hostArcs.push(...arcAround(angleTo(node.x, node.y, gatewayNode.x, gatewayNode.y), 27))
      }
      hostArcs.push([AP_WIRED_FAN[0] - 10, AP_WIRED_FAN[1] + 10])
      const gapAngle = widestGapCenterInArcs(node, neighbors, HUB_HOST_RADIUS, hostArcs)
      fanLayout(farWestItems, node, HUB_HOST_RADIUS, gapAngle - 20, gapAngle + 20)
      placed.push(...restItems, ...farWestItems)
      fanArcByRouter.set(node.id, [[AP_WIRED_FAN[0] - 8, AP_WIRED_FAN[1] + 8]])
    }

    for (const p of placed) {
      anchorPos.set(p.id, { x: p.x, y: p.y })
      if (p.kind === 'dist') {
        const dn = p.ref as DistributionNode
        distNodes.push({ kind: 'dist', id: dn.id, node: dn, x: p.x, y: p.y, r: 20 })
      }
    }
  }

  // -- chips de dispositivos --------------------------------------------------
  const chips: ChipNode[] = []
  const mkChip = (d: Device, hubId: string, isCt = false): ChipNode => ({
    kind: 'chip',
    id: d.id,
    device: d,
    x: 0,
    y: 0,
    size: isWired(d) ? 22 : 20,
    wired: isWired(d),
    band: d.band,
    weak: d.signalDbm != null && d.signalDbm <= -65,
    hubId,
    isCt,
  })

  // wifi: anillos con arcos prohibidos (uplink, etiqueta, WAN, fan cableado)
  const ringRadii = new Map<string, number[]>()
  for (const node of routerNodes) {
    const isGw = node.id === gatewayNode?.id
    const wifi = childrenOf(node.id)
      .filter((d) => !isWired(d) && isVisibleInRing(d))
      .map((d) => mkChip(d, node.id))
    if (wifi.length === 0) continue
    const excludes: [number, number][] = []
    if (isGw) {
      // WAN hacia Internet: arco amplio (el icono de Internet está sobre el
      // gateway y su halo ocupa ±35°; sin esto un chip del anillo exterior
      // quedaría a ~80px del icono).
      excludes.push(...arcAround(270, 35))
      if (gatewayNode) excludes.push(...arcAround(angleTo(gatewayNode.x, gatewayNode.y, gatewayNode.label.x, gatewayNode.label.y), 22))
      for (const ap of apNodes) excludes.push(...arcAround(angleTo(node.x, node.y, ap.x, ap.y), 15))
      // switches gestionados: ahora al NW del gateway; sin este exclude los
      // chips wifi del anillo externo (r=190) caerían encima del icono del switch.
      for (const sw of switchNodes) excludes.push(...arcAround(angleTo(node.x, node.y, sw.x, sw.y), 20))
    } else {
      if (gatewayNode) excludes.push(...arcAround(angleTo(node.x, node.y, gatewayNode.x, gatewayNode.y), 27))
      excludes.push(...arcAround(angleTo(node.x, node.y, node.label.x, node.label.y), 22))
    }
    excludes.push(...(fanArcByRouter.get(node.id) ?? []))
    const baseRings = isGw ? GATEWAY_RINGS : AP_RINGS
    const rings = [...baseRings]
    // Anillos extra: la capacidad REAL por anillo depende del arco libre (los
    // excludes —WAN, APs, switches, label, fan cableado— restan grados). El cap
    // nominal (360°) sobrestima y deja chips en (0,0); por eso se añaden anillos
    // hasta que ringLayout coloque TODOS los chips (devuelve cuántos colocó).
    const extraCap = (r: number) => Math.max(10, Math.floor(360 / ((20 + CHIP_GAP) * 180) * (Math.PI * r)))
    let placed = 0
    for (let guard = 0; guard < 8 && placed < wifi.length; guard++) {
      const last = rings[rings.length - 1]!
      const cap = rings[rings.length - 1]!.cap + 2
      rings.push({ r: last.r + 34, cap: Math.max(extraCap(last.r + 34), cap) })
      // re-colocar desde el principio (las posiciones parciales se recalcular)
      wifi.forEach((c) => { c.x = 0; c.y = 0 })
      placed = ringLayoutCount(wifi, node, rings, excludes)
    }
    ringRadii.set(node.id, rings.map((r) => r.r))
    ringLayout(wifi, node, rings, excludes)
    wifi.forEach((c, i) => {
      if (i >= baseRings[0]!.cap) c.size = 20
    })
    chips.push(...wifi)

    // -- CÍRCULO VIRTUAL del gateway (9-Ago-2026) ---------------------------
    // El gateway se procesa primero (routerNodes = [gateway, ...APs, ...switches]).
    // Conocido su radio wifi real, se reposicionan los APs y switches a partir
    // del BORDE del círculo (radio + margen), de modo que ningún AP/switch
    // invada la zona de los clientes wifi del gateway. Internet también se aleja.
    if (isGw && gatewayNode) {
      // El círculo virtual usa el radio del anillo wifi BASE del gateway (el
      // anillo exterior canónico), no el radio real con anillos extra — los
      // anillos extra solo crecen en arcos libres parciales, no alrededor de
      // los APs. Así un AP/switch nunca invade la zona de clientes wifi sin
      // ser empujado absurdamente lejos.
      const ringRadiiGw = ringRadii.get(node.id)
      const baseMax = GATEWAY_RINGS[GATEWAY_RINGS.length - 1]!.r
      const gwRingRadius = ringRadiiGw?.[ringRadiiGw.length - 1] ?? baseMax
      const clearDist = Math.max(baseMax, Math.min(gwRingRadius, baseMax + 80)) + 36
      // Internet: sobre el gateway (misma x), empujado hacia arriba del viewport.
      const targetY = Math.max(24, gatewayNode.y - clearDist)
      internetNode.y = targetY
      // APs y switches: a lo largo del vector desde el gateway hasta su posición
      // canónica, empujados hasta quedar a >= clearDist (fuera del círculo).
      const pushOut = (rn: RouterNode) => {
        const dx = rn.x - gatewayNode.x
        const dy = rn.y - gatewayNode.y
        const d = Math.sqrt(dx * dx + dy * dy) || 1
        if (d >= clearDist) return
        const k = clearDist / d
        rn.x = Math.round(gatewayNode.x + dx * k)
        rn.y = Math.round(gatewayNode.y + dy * k)
        const isLeft = rn.x < gatewayNode.x
        rn.label.x = isLeft ? rn.x - rn.r - 6 : rn.x + rn.r + 6
        rn.label.y = rn.y - 6
        rn.label.anchor = isLeft ? 'end' as const : 'start' as const
      }
      for (const ap of apNodes) pushOut(ap)
      for (const sw of switchNodes) pushOut(sw)
    }
  }

  // cableados directos del router: ya tienen anchorPos
  for (const node of routerNodes) {
    for (const d of childrenOf(node.id).filter(isWired)) {
      if (deviceHubs.has(d.id)) continue // el hub se renderiza como chip con hijos
      const p = anchorPos.get(d.id)
      if (!p) continue
      const c = mkChip(d, node.id)
      Object.assign(c, p)
      chips.push(c)
    }
  }

  // hijos de device-hubs (switch gestionado): abanico alrededor del hub
  const hubChips: ChipNode[] = []
  for (const hubId of deviceHubs) {
    const hubDev = deviceById.get(hubId)
    if (!hubDev) continue
    const parentHub = hubOf(hubDev)
    const p = anchorPos.get(hubId)
    if (!p) continue
    const hc = mkChip(hubDev, parentHub)
    Object.assign(hc, p)
    hubChips.push(hc)
    // Los CTs de un hipervisor NO van en el abanico: los renderiza el loop
    // dedicado (grid bajo el host, isCt=true). Meterlos aquí también duplicaba
    // chips (key ct-*) y enlaces (key wired-ct-*).
    const kids = hypervisorHosts.has(hubId) ? [] : childrenOf(hubId).map((d) => mkChip(d, hubId))
    const center = angleTo(routerById.get(parentHub)?.x ?? p.x, routerById.get(parentHub)?.y ?? p.y, p.x, p.y)
    if (kids.length > HUB_FAN_MAX) {
      // Muchos hijos cableados (switch con varios puertos): anillos concéntricos
      // alrededor del hub, excluyendo el sector por donde entra el enlace desde
      // el router padre (para que las líneas no crucen los chips).
      const excludes = arcAround(center, 25)
      const rings = [...HUB_RINGS]
      let cap = rings.reduce((a, r) => a + r.cap, 0)
      while (cap < kids.length) {
        const last = rings[rings.length - 1]!
        rings.push({ r: last.r + 34, cap: last.cap + 5 })
        cap = rings.reduce((a, r) => a + r.cap, 0)
      }
      ringLayout(kids, p, rings, excludes)
    } else {
      fanLayout(kids, p, HUB_FAN_RADIUS, center - 45, center + 45)
    }
    chips.push(...kids)
  }
  chips.push(...hubChips)

  // hijos de distnodes inferidos: hasta DIST_FAN_MAX en abanico de 136°; con
  // más, anillos concéntricos (el arco único los solapa — issue #5 bug 2).
  for (const dv of distNodes) {
    const kids = childrenOf(dv.id).map((d) => mkChip(d, dv.id))
    const rn = routerById.get(dv.node.routerId)
    const center = rn ? angleTo(rn.x, rn.y, dv.x, dv.y) : 0
    if (kids.length > DIST_FAN_MAX) {
      // anillos alrededor del círculo dashed: se excluye el sector hacia el
      // router (por donde entra el enlace dist-*) para que las líneas no
      // crucen los chips
      const excludes = rn ? arcAround(angleTo(dv.x, dv.y, rn.x, rn.y), 20) : []
      const rings = [...DIST_RINGS]
      let cap = rings.reduce((a, r) => a + r.cap, 0)
      while (cap < kids.length) {
        const last = rings[rings.length - 1]!
        rings.push({ r: last.r + 34, cap: last.cap + 5 })
        cap = rings.reduce((a, r) => a + r.cap, 0)
      }
      ringLayout(kids, dv, rings, excludes)
    } else {
      fanLayout(kids, dv, DIST_FAN_RADIUS, center - 68, center + 68)
    }
    chips.push(...kids)
  }

  // CTs de hipervisores: grid bajo el host (badge +N en el chip del host)
  const ctsByHost = new Map<string, ChipNode[]>()
  const ctCountByHost = new Map<string, number>()
  for (const dn of distributionNodes.filter((n) => n.kind === 'hypervisor' && n.hostDeviceId)) {
    const hostId = dn.hostDeviceId!
    const hostChip = chips.find((c) => c.id === hostId)
    const kids = childrenOf(hostId).map((d) => mkChip(d, hostId, true))
    if (!hostChip || kids.length === 0) continue
    kids.forEach((c, i) => {
      c.x = hostChip.x - ((Math.min(kids.length, CT_COLS) - 1) * CT_DX) / 2 + (i % CT_COLS) * CT_DX
      c.y = hostChip.y + CT_OFFSET_Y + Math.floor(i / CT_COLS) * CT_DY
      c.size = 22
    })
    ctsByHost.set(hostId, kids)
    ctCountByHost.set(hostId, kids.length)
    chips.push(...kids)
  }

  // Chips "+N" de anillos desbordados (solo con semántica server-side):
  // posición en el anillo externo — geometría 100% local (ángulo libre:
  // gateway 60° entre el fan WAN y el uplink patio; AP 250° bajo el router).
  const ringOverflowChips: RingOverflowChip[] = []
  for (const [routerId, count] of ringOverflow) {
    const node = routerById.get(routerId)
    if (!node) continue
    const radii = ringRadii.get(routerId)
    const isGw = routerId === gatewayNode?.id
    const r = radii?.[radii.length - 1] ?? (isGw ? GATEWAY_RINGS : AP_RINGS)[(isGw ? GATEWAY_RINGS : AP_RINGS).length - 1]!.r
    // Clientes ocultos = miembros del anillo que exceden el aforo visible
    // (issue #117: el chip "+N" los lista al hacer clic, como peers-overflow).
    const hidden = online.filter((d) => hubOf(d) === routerId && !isVisibleInRing(d))
    ringOverflowChips.push({ routerId, count, devices: hidden, ...pos(node.x, node.y, isGw ? 60 : 250, r) })
  }

  // -- peers WireGuard (arriba; túnel trazado vía Internet) -------------------
  // Hay 4 coordenadas canónicas: los activos que exceden se agrupan en el
  // chip "+N" (antes se descartaban silenciosamente).
  const activePeers = wireguard.peers.filter((p) => p.active)
  const peerNodes: PeerNode[] = activePeers.slice(0, PEER_COORDS.length).map((peer, i) => ({
    kind: 'peer',
    id: `peer-${peer.id}`,
    peer,
    ...PEER_COORDS[i]!,
  }))
  const hiddenPeers = activePeers.slice(PEER_COORDS.length)

  // -- tráfico agregado de un sub-árbol (para el flujo ∝ Mbps) ----------------
  const subtreeTraffic = (hubId: string, seen = new Set<string>()): number => {
    if (seen.has(hubId)) return 0
    seen.add(hubId)
    let sum = 0
    for (const d of childrenOf(hubId)) {
      sum += d.trafficMbps + subtreeTraffic(d.id, seen)
    }
    // distnodes que cuelgan de este hub: su sub-árbol también viaja por el
    // enlace del hub (el hipervisor ya cuenta vía su device host).
    for (const dn of distributionNodes) {
      if (dn.routerId !== hubId || dn.kind === 'hypervisor') continue
      sum += subtreeTraffic(dn.id, seen)
    }
    const dn = distById.get(hubId)
    if (dn?.kind === 'hypervisor' && dn.hostDeviceId) {
      const host = deviceById.get(dn.hostDeviceId)
      if (host) sum += host.trafficMbps
    }
    return sum
  }

  // -- enlaces -----------------------------------------------------------------
  const links: TopoLink[] = []
  if (gatewayNode) {
    // WAN Internet→gateway: path DINÁMICO desde el borde del icono de Internet
    // hasta el borde del gateway (antes estaba hardcodeado a la posición vieja
    // de Internet y quedaba colgando al moverlo).
    const wanEdge = pos(internetNode.x, internetNode.y, 90, internetNode.r + 4)
    const gwTop = { x: gatewayNode.x, y: gatewayNode.y - gatewayNode.r }
    const d = `M ${wanEdge.x} ${wanEdge.y} C ${wanEdge.x} ${(wanEdge.y + gwTop.y) / 2}, ${gwTop.x} ${(wanEdge.y + gwTop.y) / 2}, ${gwTop.x} ${gwTop.y}`
    links.push({
      id: 'wan', kind: 'wan',
      d,
      lx: 518, ly: Math.round((wanEdge.y + gwTop.y) / 2), label: `Fibra ${wan.plan} · ${wan.latencyMs} ms`,
      width: 3, ...flowFor(600),
      from: 'internet', to: gatewayNode.id,
    })
  }
  const makeUplink = (node: RouterNode) => {
    const isWifi = node.router.backhaul === 'wifi'
    const isSwitch = node.router.roleBadge === 'SW'
    const traffic = subtreeTraffic(node.id)
    const label = isWifi
      ? 'WiFi uplink'
      : isSwitch
        ? `Switch ${node.router.name}${node.router.lldp ? ' · LLDP' : ''}`
        : `Cable 1G${node.router.lldp ? ' · LLDP' : ''}`
    const from = gatewayNode ?? null
    let d = ''
    let lx = 0, ly = 0
    if (from) {
      const edge = pos(from.x, from.y, angleTo(from.x, from.y, node.x, node.y), from.r + 2)
      const dx = node.x - edge.x
      const dy = node.y - edge.y
      d = `M ${edge.x} ${edge.y} C ${edge.x + dx * 0.35} ${edge.y + dy * 0.15}, ${node.x - dx * 0.15} ${node.y - dy * 0.35}, ${node.x} ${node.y}`
      lx = Math.round((edge.x + node.x) / 2)
      ly = Math.round((edge.y + node.y) / 2)
    }
    links.push({
      id: `uplink-${node.id}`, kind: 'uplink', wifi: isWifi,
      d, lx, ly,
      label,
      width: isSwitch ? 2 : isWifi ? 2 : 3, ...flowFor(Math.max(traffic, isWifi ? 40 : isSwitch ? 60 : 120)),
      from: from?.id ?? 'internet', to: node.id,
    })
  }
  apNodes.forEach(makeUplink)
  switchNodes.forEach(makeUplink)
  // enlaces router → distnode / device-hub / cableado directo
  for (const dv of distNodes) {
    const rn = routerById.get(dv.node.routerId)
    if (!rn) continue
    const edge = pos(rn.x, rn.y, angleTo(rn.x, rn.y, dv.x, dv.y), rn.r + 2)
    const mid = { x: (edge.x + dv.x) / 2, y: (edge.y + dv.y) / 2 }
    links.push({
      id: `dist-${dv.id}`, kind: 'dist',
      d: `M ${edge.x} ${edge.y} Q ${mid.x + 6} ${mid.y - 6}, ${dv.x} ${dv.y}`,
      lx: 0, ly: 0, label: '',
      width: 2.5, ...flowFor(subtreeTraffic(dv.id), true),
      from: dv.node.routerId, to: dv.id,
    })
  }
  const chipById = new Map(chips.map((c) => [c.id, c]))
  const hubPos = (hubId: string): { x: number; y: number; r: number } | null => {
    const rn = routerById.get(hubId)
    if (rn) return { x: rn.x, y: rn.y, r: rn.r }
    const dv = distNodes.find((n) => n.id === hubId)
    if (dv) return { x: dv.x, y: dv.y, r: dv.r }
    const c = chipById.get(hubId)
    if (c) return { x: c.x, y: c.y, r: c.size / 2 }
    return null
  }
  for (const chip of chips) {
    if (chip.isCt || !chip.wired) continue // wifi "va suelto" (sin línea); CTs cuelgan del host
    const hub = hubPos(chip.hubId)
    if (!hub) continue
    const a = angleTo(hub.x, hub.y, chip.x, chip.y)
    const edge = pos(hub.x, hub.y, a, hub.r + 2)
    const dx = chip.x - edge.x
    const dy = chip.y - edge.y
    const c1x = edge.x + dx * 0.5 - dy * 0.1
    const c1y = edge.y + dy * 0.5 + dx * 0.1
    const mbps = deviceHubs.has(chip.id) ? chip.device.trafficMbps + subtreeTraffic(chip.id) : chip.device.trafficMbps
    links.push({
      id: `wired-${chip.id}`, kind: 'wired',
      d: `M ${edge.x} ${edge.y} Q ${c1x} ${c1y}, ${chip.x} ${chip.y}`,
      lx: 0, ly: 0, label: '',
      width: 1.4, ...flowFor(mbps, true),
      from: chip.hubId, to: chip.id,
    })
  }
  // CTs: línea sólida desde el host (su tráfico viaja por el cable del host)
  for (const [hostId, cts] of ctsByHost) {
    const hostChip = chipById.get(hostId)
    if (!hostChip) continue
    for (const ct of cts) {
      links.push({
        id: `wired-${ct.id}`, kind: 'wired',
        d: `M ${hostChip.x} ${hostChip.y + hostChip.size / 2} L ${ct.x} ${ct.y - ct.size / 2}`,
        lx: 0, ly: 0, label: '',
        width: 1.2, ...flowFor(ct.device.trafficMbps, true),
        from: hostId, to: ct.id,
      })
    }
  }
  peerNodes.forEach((node, i) => {
    const p = WG_PATHS[i] ?? { d: '', dur: 3.2 }
    const d = p.d || `M ${node.x + 7} ${node.y + 18} C ${node.x + 60} ${node.y + 56}, ${internetNode.x - 40} ${internetNode.y + 36}, ${internetNode.x - 26} ${internetNode.y + 22}`
    links.push({
      id: `wg-${node.peer.id}`, kind: 'wg',
      d, lx: 0, ly: 0, label: '',
      width: 2, packets: 1, packetDur: p.dur,
      from: node.id, to: 'internet',
    })
  })

  // SPEC-65 D65-3/D65-9 B2: con semántica server-side, la LISTA de enlaces
  // (from/to/kind) la manda el servidor; aquí solo se asigna la geometría
  // (paths, etiquetas, flujo). En canon hay paridad 1:1 con los enlaces
  // derivados localmente (golden test Go), así que se reutiliza la geometría
  // ya calculada; para un enlace sin equivalente local (live con datos que
  // el cálculo local no reproduce) se genera con las mismas reglas visuales.
  if (sem) {
    /** Geometría de un enlace semántico sin equivalente derivado localmente. */
    const semLinkGeometry = (sl: TopoSemLink, wgIdx: number): TopoLink | null => {
      const curve = (a: { x: number; y: number; r: number }, b: { x: number; y: number }): string => {
        const edge = pos(a.x, a.y, angleTo(a.x, a.y, b.x, b.y), a.r + 2)
        const dx = b.x - edge.x
        const dy = b.y - edge.y
        return `M ${edge.x} ${edge.y} Q ${edge.x + dx * 0.5 - dy * 0.1} ${edge.y + dy * 0.5 + dx * 0.1}, ${b.x} ${b.y}`
      }
      switch (sl.kind) {
        case 'wan': {
          if (!gatewayNode || sl.to !== gatewayNode.id) return null
          return {
            id: 'wan', kind: 'wan',
            d: 'M 500 92 C 500 122, 500 162, 500 208',
            lx: 518, ly: 162, label: `Fibra ${wan.plan} · ${wan.latencyMs} ms`,
            width: 3, ...flowFor(600),
            from: sl.from, to: sl.to,
          }
        }
        case 'uplink': {
          const i = apNodes.findIndex((n) => n.id === sl.to)
          const node = i >= 0 ? apNodes[i] : routerById.get(sl.to)
          if (!node) return null
          const isWifi = node.router.backhaul === 'wifi'
          const label = isWifi ? 'WiFi uplink' : `Cable 1G${node.router.lldp ? ' · LLDP' : ''}`
          const from = gatewayNode ?? null
          const d = from ? curve(from, node) : ''
          return {
            id: `uplink-${node.id}`, kind: 'uplink', wifi: isWifi,
            d,
            lx: 0, ly: 0,
            label,
            width: isWifi ? 2 : 3, ...flowFor(Math.max(subtreeTraffic(node.id), isWifi ? 40 : 120)),
            from: sl.from, to: sl.to,
          }
        }
        case 'dist': {
          const dv = distNodes.find((n) => n.id === sl.to)
          const rn = routerById.get(sl.from)
          if (!dv || !rn) return null
          const edge = pos(rn.x, rn.y, angleTo(rn.x, rn.y, dv.x, dv.y), rn.r + 2)
          const mid = { x: (edge.x + dv.x) / 2, y: (edge.y + dv.y) / 2 }
          return {
            id: `dist-${dv.id}`, kind: 'dist',
            d: `M ${edge.x} ${edge.y} Q ${mid.x + 6} ${mid.y - 6}, ${dv.x} ${dv.y}`,
            lx: 0, ly: 0, label: '',
            width: 2.5, ...flowFor(subtreeTraffic(dv.id), true),
            from: sl.from, to: sl.to,
          }
        }
        case 'wired': {
          const chip = chipById.get(sl.to)
          const hub = hubPos(sl.from)
          if (!chip || !hub) return null
          const mbps = deviceHubs.has(chip.id) ? chip.device.trafficMbps + subtreeTraffic(chip.id) : chip.device.trafficMbps
          return {
            id: `wired-${chip.id}`, kind: 'wired',
            d: curve(hub, chip),
            lx: 0, ly: 0, label: '',
            width: chip.isCt ? 1.2 : 1.4, ...flowFor(mbps, true),
            from: sl.from, to: sl.to,
          }
        }
        case 'wg': {
          const pn = peerNodes.find((p) => p.id === sl.from)
          if (!pn) return null
          const p = WG_PATHS[wgIdx] ?? { d: '', dur: 3.2 }
          const d = p.d || `M ${pn.x + 7} ${pn.y + 18} C ${pn.x + 60} ${pn.y + 56}, ${internetNode.x - 40} ${internetNode.y + 36}, ${internetNode.x - 26} ${internetNode.y + 22}`
          return {
            id: `wg-${pn.peer.id}`, kind: 'wg',
            d, lx: 0, ly: 0, label: '',
            width: 2, packets: 1, packetDur: p.dur,
            from: sl.from, to: sl.to,
          }
        }
      }
    }
    const derivedByKey = new Map(links.map((l) => [`${l.kind}|${l.from}|${l.to}`, l] as const))
    const semLinks: TopoLink[] = []
    let wgIdx = 0
    for (const sl of sem.links) {
      const key = `${sl.kind}|${sl.from}|${sl.to}` as const
      const hit = derivedByKey.get(key)
      if (hit) {
        semLinks.push(hit)
        derivedByKey.delete(key)
        if (sl.kind === 'wg') wgIdx++
        continue
      }
      const gen = semLinkGeometry(sl, sl.kind === 'wg' ? wgIdx++ : 0)
      if (gen) semLinks.push(gen)
    }
    links.length = 0
    links.push(...semLinks)
  }

  /** Guardrail de rendimiento: máx ~60 paquetes simultáneos (mockup) */
  let totalPackets = links.reduce((acc, l) => acc + l.packets, 0)
  if (totalPackets > 60) {
    for (const l of [...links].sort((a, b) => b.packets - a.packets)) {
      if (totalPackets <= 60) break
      const cut = Math.min(l.packets - 1, totalPackets - 60)
      if (cut > 0) {
        l.packets -= cut
        totalPackets -= cut
      }
    }
  }

  /** Enlaces activos de la red: WAN + uplinks + distribución (dist-*) + el
   *  cable de cada host hipervisor (wired-<host>) — sin túneles WG. Cuadra
   *  con las filas físicas de la LinksTable (D7); la única fila no contada
   *  es el túnel WireGuard (se muestra aparte, tone 'tunnel'). */
  const activeLinkCount = links.filter(
    (l) =>
      l.kind === 'wan' ||
      l.kind === 'uplink' ||
      l.kind === 'dist' ||
      (l.kind === 'wired' && hypervisorHosts.has(l.to)),
  ).length

  // Tabla de backhauls (topology.md §④)
  const backhauls: BackhaulRow[] = []
  if (gatewayNode) {
    backhauls.push({
      id: 'wan', a: 'Gateway', b: 'Internet', kind: 'wan',
      type: 'topology.links.fiberWan', speed: wan.plan, signal: `${wan.latencyMs} ms`,
      tone: 'ok', statusLabel: 'common.status.online',
      spark: gatewayNode.router.sparkline, sparkColor: COLOR.accent,
    })
  }
  for (const node of apNodes) {
    const isWifi = node.router.backhaul === 'wifi'
    backhauls.push({
      id: `uplink-${node.id}`, a: 'Gateway', b: node.router.name, kind: 'uplink',
      type: isWifi ? 'topology.links.wifiUplink' : 'common.cable',
      speed: isWifi ? '866 Mbps PHY' : '1 Gbps',
      signal: isWifi ? '−58 dBm · 1 ms' : '<1 ms',
      tone: isWifi ? 'warn' : 'ok',
      statusLabel: isWifi ? 'common.status.warn' : 'common.status.online',
      note: isWifi ? 'topology.links.congestedChannel' : undefined,
      spark: node.router.sparkline,
      sparkColor: isWifi ? COLOR.warn : COLOR.accent,
    })
  }
  // D7: enlaces de distribución — el mapa ya los dibuja (dist-*) y
  // activeLinkCount los cuenta; la tabla también los lista.
  for (const dv of distNodes) {
    const rn = routerById.get(dv.node.routerId)
    if (!rn) continue
    if (dv.node.kind === 'managed') {
      backhauls.push({
        id: `dist-${dv.id}`, a: rn.router.name,
        b: [dv.node.name, dv.node.ip, 'LLDP', dv.node.portLabel ?? dv.node.port].filter(Boolean).join(' · '),
        kind: 'dist', type: 'topology.links.managedSwitch',
        speed: '1 Gbps', signal: '<1 ms',
        tone: 'ok', statusLabel: 'common.status.online',
        spark: rn.router.sparkline, sparkColor: COLOR.accent,
      })
    } else {
      backhauls.push({
        id: `dist-${dv.id}`, a: rn.router.name,
        b: '', bKey: 'topology.links.inferredSwitch', bVars: { port: dv.node.portLabel ?? dv.node.port },
        kind: 'dist', type: 'common.cable',
        speed: '1 Gbps', signal: '<1 ms',
        tone: 'ok', statusLabel: 'common.status.online',
        spark: rn.router.sparkline, sparkColor: COLOR.ok,
      })
    }
  }
  // D7: cable del hipervisor (host con sus CTs/VMs anidados).
  for (const dn of distributionNodes.filter((n) => n.kind === 'hypervisor' && n.hostDeviceId)) {
    const host = deviceById.get(dn.hostDeviceId!)
    const rn = routerById.get(dn.routerId)
    if (!host || !rn) continue
    backhauls.push({
      id: `wired-${host.id}`, a: rn.router.name,
      b: `${host.name} · ${dn.portLabel ?? dn.port} · ${ctCountByHost.get(host.id) ?? 0} CT`,
      kind: 'wired', type: 'topology.links.hypervisorCable',
      speed: '—', signal: '—',
      tone: 'ok', statusLabel: 'common.status.online',
      spark: host.sparkline, sparkColor: COLOR.ok,
    })
  }
  // La tabla canónica muestra un único túnel (el más reciente)
  for (const node of peerNodes.slice(0, 1)) {
    const device = devices.find((d) => d.id === node.peer.id)
    backhauls.push({
      id: `wg-${node.peer.id}`, a: 'Internet', b: node.peer.name, kind: 'wg',
      type: 'WireGuard', speed: '—', signal: '42 ms',
      tone: 'tunnel', statusLabel: 'common.active',
      spark: device?.sparkline ?? [0, 0], sparkColor: COLOR.tunnel,
    })
  }

  const activePeerCount = activePeers.length

  // Adyacencia (hover: atenuar nodos no relacionados)
  const adjacency = new Map<string, { nodes: Set<string>; links: Set<string> }>()
  const ensure = (id: string) => {
    let entry = adjacency.get(id)
    if (!entry) {
      entry = { nodes: new Set([id]), links: new Set() }
      adjacency.set(id, entry)
    }
    return entry
  }
  for (const link of links) {
    ensure(link.from).nodes.add(link.to)
    ensure(link.from).links.add(link.id)
    ensure(link.to).nodes.add(link.from)
    ensure(link.to).links.add(link.id)
  }
  const relatedTo = (nodeId: string) =>
    adjacency.get(nodeId) ?? { nodes: new Set([nodeId]), links: new Set<string>() }

  // --- NORMA DE SEPARACIÓN DE CABLES (9-Ago-2026) ---
  // Se recogen los segmentos hub→chip de todos los enlaces (uplinks, dist,
  // wired, CTs). El resolver empuja cada chip fuera de los cables AJENOS (los
  // que no le pertenecen) con un margen, para que ningún dispositivo quede
  // encima de un cable de otro.
  const repelSegments: { x1: number; y1: number; x2: number; y2: number; ownerId?: string }[] = []
  for (const c of chips) {
    if (!c.wired || c.isCt) continue
    const hub = routerNodes.find(rn => rn.id === c.hubId) ?? distNodes.find(dn => dn.id === c.hubId)
    if (!hub) continue
    repelSegments.push({ x1: hub.x, y1: hub.y, x2: c.x, y2: c.y, ownerId: c.id })
  }
  for (const [hostId, cts] of ctsByHost) {
    const hostChip = chips.find((c) => c.id === hostId)
    if (!hostChip) continue
    for (const ct of cts) repelSegments.push({ x1: hostChip.x, y1: hostChip.y, x2: ct.x, y2: ct.y, ownerId: ct.id })
  }
  for (const dn of distNodes) {
    const rn = routerById.get(dn.node.routerId)
    if (rn) repelSegments.push({ x1: rn.x, y1: rn.y, x2: dn.x, y2: dn.y })
  }
  for (const rn of routerNodes) {
    if (rn.id === gatewayNode?.id || !gatewayNode) continue
    repelSegments.push({ x1: gatewayNode.x, y1: gatewayNode.y, x2: rn.x, y2: rn.y })
  }

  // --- Resolución de colisiones ---
  // Routers fijos (no se mueven); chips se empujan y atraen hacia su hub padre.
  // restDist = distancia inicial hub→chip (la posición canónica del layout);
  // el resolver la usa como radio objetivo para que los chips no se dispersen
  // tras una colisión ni se amontonen sobre el hub.
  // Los nodos fijos llevan MARGEN (norma de separación): un chip no puede
  // quedar pegado al borde de un router/distnode/Internet — mínimo
  // r_nodo + r_chip + margin + padding. El gateway (r40) usa margen mayor.
  const collidables: { x: number; y: number; r: number; fixed?: boolean; margin?: number; hubX?: number; hubY?: number; restDist?: number; id?: string }[] = [
    ...routerNodes.map((rn) => ({ x: rn.x, y: rn.y, r: rn.r, fixed: true, margin: rn.id === gatewayNode?.id ? 40 : 26 })),
    { x: internetNode.x, y: internetNode.y, r: 30, fixed: true, margin: 30 },
  ]
  // chips: buscar coords del hub padre
  for (const c of chips) {
    const hub = routerNodes.find(rn => rn.id === c.hubId) ?? distNodes.find(dn => dn.id === c.hubId)
    const isHost = hypervisorHosts.has(c.id)
    if (hub) {
      const restDist = Math.sqrt((c.x - hub.x) ** 2 + (c.y - hub.y) ** 2)
      // Los hosts hipervisores son NODOS con hijos (CTs): fijos, como los
      // distnodes — no deben ser desplazados por los cables ajenos (si no, el
      // resolver los lanza a la esquina). Sus CTs sí se acomodan a su alrededor.
      collidables.push({
        id: c.id, x: c.x, y: c.y, r: c.size / 2,
        fixed: isHost, margin: isHost ? 16 : undefined,
        hubX: hub.x, hubY: hub.y, restDist,
      })
    } else {
      collidables.push({ id: c.id, x: c.x, y: c.y, r: c.size / 2, fixed: isHost, margin: isHost ? 16 : undefined })
    }
  }
  // distnodes fijos
  for (const dn of distNodes) {
    collidables.push({ x: dn.x, y: dn.y, r: dn.r, fixed: true, margin: 14 })
  }
  resolveCollisions(collidables, 80, 14, repelSegments)
  // Escribir posiciones de vuelta (solo chips; routers y distnodes son fixed)
  let ci = routerNodes.length + 1 // +1 = Internet (añadido tras los routers)
  for (const c of chips) { const ref = collidables[ci]!; c.x = ref.x; c.y = ref.y; ci++ }

  return {
    gatewayNode,
    apNodes,
    routerNodes,
    internetNode,
    peerNodes,
    hiddenPeers,
    chips,
    distNodes,
    ctsByHost,
    ctCountByHost,
    ringRadii,
    ringOverflowChips,
    links,
    totalPackets,
    activeLinkCount,
    backhauls,
    activePeerCount,
    wan,
    relatedTo,
  }
}
