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
import type { Band, Device, DistributionNode, Router, WanInfo, WGPeer, WireGuardStats } from '@/data/mock'

/** viewBox del lienzo SVG */
export const VB_W = 1000
export const VB_H = 680

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
}

export interface TopologyModel {
  gatewayNode: RouterNode | null
  apNodes: RouterNode[]
  routerNodes: RouterNode[]
  internetNode: { id: 'internet'; x: number; y: number }
  peerNodes: PeerNode[]
  chips: ChipNode[]
  distNodes: DistNodeView[]
  /** CTs/VMs anidados por host hipervisor (hostId → chips en grid) */
  ctsByHost: Map<string, ChipNode[]>
  /** nº de CTs por host (badge +N) */
  ctCountByHost: Map<string, number>
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
const AP_COORDS = [
  { x: 195, y: 470, r: 32, label: { x: 124, y: 464, anchor: 'end' as const } },
  { x: 500, y: 505, r: 32, label: { x: 554, y: 500, anchor: 'start' as const } },
  { x: 805, y: 470, r: 32, label: { x: 858, y: 464, anchor: 'start' as const } },
]
const INTERNET_COORD = { x: 500, y: 58 }
const PEER_COORDS = [
  { x: 265, y: 26 },
  { x: 735, y: 26 },
  { x: 140, y: 26 },
  { x: 860, y: 26 },
]
/** Uplinks canónicos gateway→AP (cúbicas QA-das en el mockup) */
const UPLINK_PATHS = [
  { d: 'M 464 282 C 390 340, 290 400, 224 440', lx: 292, ly: 366 },
  { d: 'M 500 292 C 500 360, 500 420, 500 468', lx: 514, ly: 418 },
  { d: 'M 536 282 C 610 340, 710 400, 776 440', lx: 664, ly: 366 },
]
/** Túneles WG canónicos peer→Internet */
const WG_PATHS = [
  { d: 'M 272 44 C 330 82, 410 94, 474 80', dur: 3 },
  { d: 'M 728 44 C 670 82, 590 94, 526 80', dur: 3.4 },
]
/** Anillos wifi (v5: separados de los nodos; chips no "tocan" el router) */
const GATEWAY_RINGS = [
  { r: 88, cap: 5 },
  { r: 118, cap: 8 },
]
const AP_RINGS = [
  { r: 74, cap: 7 },
  { r: 108, cap: 13 },
]
/** Abanicos cableados: gateway tiene sector este (switch inferido) y oeste */
const GW_EAST_FAN: [number, number] = [336, 24] // wrap (a1 < a0)
const GW_WEST_FAN: [number, number] = [150, 210]
const AP_WIRED_FAN: [number, number] = [50, 130]
const DIST_FAN_RADIUS = 82
const HUB_FAN_RADIUS = 56
const ROUTER_FAN_RADIUS = 134
/** Hosts hipervisores del gateway: lejos (su grid de CTs necesita espacio) */
const HYPERVISOR_FAN_RADIUS = 300
/** Grid de CTs bajo el host hipervisor */
const CT_COLS = 5
const CT_DX = 42
const CT_DY = 38
const CT_OFFSET_Y = 56

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
function ringLayout<T extends { x: number; y: number }>(
  items: T[],
  node: { x: number; y: number },
  rings: { r: number; cap: number }[],
  excludes: [number, number][],
): void {
  const free = freeArcs(excludes)
  const total = free.reduce((a, [s, e]) => a + (e - s), 0)
  if (total <= 0) return
  let idx = 0
  for (const ring of rings) {
    if (idx >= items.length) break
    const n = Math.min(ring.cap, items.length - idx)
    let placed = 0
    for (let ai = 0; ai < free.length && placed < n; ai++) {
      const [s, e] = free[ai]
      let k = Math.min(Math.round((n * (e - s)) / total), n - placed)
      if (ai === free.length - 1) k = n - placed
      for (let i = 0; i < k; i++) {
        const a = s + ((e - s) * (i + 0.5)) / k
        const p = pos(node.x, node.y, a, ring.r)
        Object.assign(items[idx++], p)
        placed++
      }
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

/** flujo de paquetes ∝ tráfico (mockup: umbrales 35/15/2 Mbps, guardrail 60) */
function flowFor(mbps: number): { packets: number; packetDur: number } {
  const packets = mbps >= 35 ? 3 : mbps >= 15 ? 2 : mbps >= 2 ? 1 : 0
  return { packets, packetDur: Math.max(1.6, 5 - mbps / 12) }
}

// ---------------------------------------------------------------------------
// Builder
// ---------------------------------------------------------------------------

export function buildTopologyModel({ routers, devices, wan, wireguard, distributionNodes = [] }: TopologyInput): TopologyModel {
  const gateway = routers.find((r) => r.roleBadge === 'Principal') ?? routers[0]
  const aps = routers.filter((r) => r.id !== gateway?.id).slice(0, AP_COORDS.length)

  const gatewayNode: RouterNode | null = gateway
    ? { kind: 'router', id: gateway.id, router: gateway, ...GATEWAY_COORD }
    : null
  const apNodes: RouterNode[] = aps.map((router, i) => ({ kind: 'router', id: router.id, router, ...AP_COORDS[i] }))
  const routerNodes: RouterNode[] = gatewayNode ? [gatewayNode, ...apNodes] : apNodes
  const routerById = new Map(routerNodes.map((n) => [n.id, n]))

  const internetNode = { id: 'internet' as const, ...INTERNET_COORD }

  const online = devices.filter((d) => d.online)
  const deviceById = new Map(online.map((d) => [d.id, d]))
  const distById = new Map(distributionNodes.map((n) => [n.id, n]))
  /** hub al que cuelga un dispositivo: attachTo (si existe y resuelve) o su router */
  const hubOf = (d: Device): string => {
    if (d.attachTo && (routerById.has(d.attachTo) || distById.has(d.attachTo) || deviceById.has(d.attachTo))) {
      return d.attachTo
    }
    return d.routerId
  }
  const childrenOf = (hubId: string) => online.filter((d) => hubOf(d) === hubId)
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
    // Solo los distnodes inferidos son anchors propios (círculo dashed); el
    // hipervisor se posiciona vía su device host (hub), el distnode solo
    // aporta metadatos (puerto, macCount).
    const dists = distributionNodes.filter((n) => n.routerId === node.id && n.kind === 'inferred')
    const directWired = childrenOf(node.id).filter(isWired)
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
      fanLayout(eastItems, node, ROUTER_FAN_RADIUS, GW_EAST_FAN[0], GW_EAST_FAN[1])
      fanLayout(farWestItems, node, HYPERVISOR_FAN_RADIUS, 160, 200)
      fanLayout(westItems, node, ROUTER_FAN_RADIUS, GW_WEST_FAN[0], GW_WEST_FAN[1])
      placed.push(...eastItems, ...farWestItems, ...westItems)
      fanArcByRouter.set(node.id, [...arcAround(0, 26), [GW_WEST_FAN[0] - 8, GW_WEST_FAN[1] + 8]])
    } else {
      const items = anchors.map((a) => ({ ...a, x: 0, y: 0 }))
      fanLayout(items, node, ROUTER_FAN_RADIUS, AP_WIRED_FAN[0], AP_WIRED_FAN[1])
      placed.push(...items)
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
    size: isWired(d) ? 26 : 24,
    wired: isWired(d),
    band: d.band,
    weak: d.signalDbm != null && d.signalDbm <= -65,
    hubId,
    isCt,
  })

  // wifi: anillos con arcos prohibidos (uplink, etiqueta, WAN, fan cableado)
  for (const node of routerNodes) {
    const isGw = node.id === gatewayNode?.id
    const wifi = childrenOf(node.id).filter((d) => !isWired(d)).map((d) => mkChip(d, node.id))
    if (wifi.length === 0) continue
    const excludes: [number, number][] = []
    if (isGw) {
      excludes.push(...arcAround(270, 14)) // WAN hacia Internet
      if (gatewayNode) excludes.push(...arcAround(angleTo(gatewayNode.x, gatewayNode.y, gatewayNode.label.x, gatewayNode.label.y), 22))
      for (const ap of apNodes) excludes.push(...arcAround(angleTo(node.x, node.y, ap.x, ap.y), 15))
    } else {
      if (gatewayNode) excludes.push(...arcAround(angleTo(node.x, node.y, gatewayNode.x, gatewayNode.y), 27))
      excludes.push(...arcAround(angleTo(node.x, node.y, node.label.x, node.label.y), 22))
    }
    excludes.push(...(fanArcByRouter.get(node.id) ?? []))
    const baseRings = isGw ? GATEWAY_RINGS : AP_RINGS
    // anillos extra si el wifi supera el aforo de los dos primeros
    const rings = [...baseRings]
    let cap = rings.reduce((a, r) => a + r.cap, 0)
    while (cap < wifi.length) {
      const last = rings[rings.length - 1]
      rings.push({ r: last.r + 30, cap: last.cap + 5 })
      cap = rings.reduce((a, r) => a + r.cap, 0)
    }
    ringLayout(wifi, node, rings, excludes)
    // chips algo menores en anillos externos
    wifi.forEach((c, i) => {
      if (i >= baseRings[0].cap) c.size = 22
    })
    chips.push(...wifi)
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
    const kids = childrenOf(hubId).map((d) => mkChip(d, hubId))
    const center = angleTo(routerById.get(parentHub)?.x ?? p.x, routerById.get(parentHub)?.y ?? p.y, p.x, p.y)
    fanLayout(kids, p, HUB_FAN_RADIUS, center - 45, center + 45)
    chips.push(...kids)
  }
  chips.push(...hubChips)

  // hijos de distnodes inferidos: abanico alrededor del círculo dashed
  for (const dv of distNodes) {
    const kids = childrenOf(dv.id).map((d) => mkChip(d, dv.id))
    const rn = routerById.get(dv.node.routerId)
    const center = rn ? angleTo(rn.x, rn.y, dv.x, dv.y) : 0
    fanLayout(kids, dv, DIST_FAN_RADIUS, center - 68, center + 68)
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

  // -- peers WireGuard (arriba; túnel trazado vía Internet) -------------------
  const activePeers = wireguard.peers.filter((p) => p.active)
  const peerNodes: PeerNode[] = activePeers.slice(0, PEER_COORDS.length).map((peer, i) => ({
    kind: 'peer',
    id: `peer-${peer.id}`,
    peer,
    ...PEER_COORDS[i],
  }))

  // -- tráfico agregado de un sub-árbol (para el flujo ∝ Mbps) ----------------
  const subtreeTraffic = (hubId: string, seen = new Set<string>()): number => {
    if (seen.has(hubId)) return 0
    seen.add(hubId)
    let sum = 0
    for (const d of childrenOf(hubId)) {
      sum += d.trafficMbps + subtreeTraffic(d.id, seen)
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
    links.push({
      id: 'wan', kind: 'wan',
      d: 'M 500 92 C 500 122, 500 162, 500 208',
      lx: 518, ly: 162, label: `Fibra ${wan.plan} · ${wan.latencyMs} ms`,
      width: 3, ...flowFor(600),
      from: 'internet', to: gatewayNode.id,
    })
  }
  apNodes.forEach((node, i) => {
    const p = UPLINK_PATHS[i]
    const isWifi = node.id === 'patio'
    const traffic = subtreeTraffic(node.id)
    links.push({
      id: `uplink-${node.id}`, kind: 'uplink', wifi: isWifi,
      d: p.d, lx: p.lx, ly: p.ly,
      label: isWifi ? 'WiFi uplink −58 dBm' : 'Cable 1G',
      width: isWifi ? 2 : 3, ...flowFor(Math.max(traffic, isWifi ? 40 : 120)),
      from: gatewayNode?.id ?? 'internet', to: node.id,
    })
  })
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
      width: 2.5, ...flowFor(subtreeTraffic(dv.id)),
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
      width: 1.4, ...flowFor(mbps),
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
        width: 1.2, ...flowFor(ct.device.trafficMbps),
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

  /** Enlaces activos de la red (WAN + uplinks + distribución), sin túneles */
  const activeLinkCount = links.filter((l) => l.kind === 'wan' || l.kind === 'uplink' || l.kind === 'dist').length

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
    const isWifi = node.id === 'patio'
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

  return {
    gatewayNode,
    apNodes,
    routerNodes,
    internetNode,
    peerNodes,
    chips,
    distNodes,
    ctsByHost,
    ctCountByHost,
    links,
    totalPackets,
    activeLinkCount,
    backhauls,
    activePeerCount,
    wan,
    relatedTo,
  }
}
