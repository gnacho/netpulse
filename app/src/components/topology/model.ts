/**
 * NetPulse — Modelo del mapa de topología (topology.md §②).
 * `buildTopologyModel` deriva todo del bundle del DataProvider (mock en demo,
 * API en live). Las posiciones del lienzo son fijas por rol: gateway al
 * centro y hasta 3 APs en las coordenadas canónicas.
 */
import type { Band, Device, Router, WanInfo, WGPeer, WireGuardStats } from '@/data/mock'

/** viewBox del lienzo SVG */
export const VB_W = 1000
export const VB_H = 680

export const MAX_CLUSTER_DOTS = 12

// ---------------------------------------------------------------------------
// Tipos
// ---------------------------------------------------------------------------

export interface RouterNode {
  kind: 'router'
  id: string // router id
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

export interface ClientDot {
  id: string
  x: number
  y: number
  band: Band
  weak: boolean
  device?: Device
}

export interface Cluster {
  id: string // `cluster-${routerId}`
  routerId: string
  cx: number
  cy: number
  dots: ClientDot[]
  /** clientes no representados (badge "+N") */
  extra: number
}

export type TopoLinkKind = 'wan' | 'uplink' | 'cluster' | 'wg'

export interface TopoLink {
  id: string
  kind: TopoLinkKind
  d: string
  /** posición de la etiqueta del enlace */
  lx: number
  ly: number
  label: string
  /** grosor ∝ tráfico (2px base, hasta ~5px) */
  width: number
  /** nº de paquetes animados simultáneos (guardrail ≤14 total) */
  packets: number
  /** duración del viaje del paquete (s); menor = más tráfico */
  packetDur: number
  /** nodos extremos (para coordinación hover) */
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
}

export interface TopologyModel {
  gatewayNode: RouterNode | null
  apNodes: RouterNode[]
  routerNodes: RouterNode[]
  internetNode: { id: 'internet'; x: number; y: number }
  peerNodes: PeerNode[]
  clusters: Cluster[]
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

// ---------------------------------------------------------------------------
// Coordenadas canónicas del lienzo (gateway centrado + 3 APs + 2 peers VPN)
// ---------------------------------------------------------------------------

const GATEWAY_COORD = { x: 500, y: 246, r: 40, label: { x: 556, y: 240, anchor: 'start' as const } }
const AP_COORDS = [
  { x: 210, y: 452, r: 32, label: { x: 158, y: 446, anchor: 'end' as const } },
  { x: 500, y: 478, r: 32, label: { x: 546, y: 472, anchor: 'start' as const } },
  { x: 790, y: 452, r: 32, label: { x: 836, y: 446, anchor: 'start' as const } },
]
const CLUSTER_COORDS = [
  { cx: 210, cy: 580 },
  { cx: 500, cy: 606 },
  { cx: 790, cy: 580 },
]
const GATEWAY_CLUSTER = { cx: 210, cy: 246 }
const PEER_COORDS = [
  { x: 96, y: 120 },
  { x: 240, y: 56 },
]
const UPLINK_PATHS = [
  { d: 'M 468 276 C 400 330, 300 380, 224 420', lx: 322, ly: 340, width: 4.5, packets: 3, packetDur: 2.8 },
  { d: 'M 500 288 C 500 350, 500 390, 500 434', lx: 514, ly: 360, width: 2.5, packets: 2, packetDur: 3.4 },
  { d: 'M 532 276 C 600 330, 700 380, 776 420', lx: 668, ly: 340, width: 2, packets: 1, packetDur: 4 },
]
const CLUSTER_PATHS = [
  'M 210 492 C 210 516, 210 528, 210 550',
  'M 500 518 C 500 542, 500 554, 500 576',
  'M 790 492 C 790 516, 790 528, 790 550',
]
const WG_PATHS = [
  { d: 'M 120 134 C 240 210, 360 220, 458 236', lx: 268, ly: 200, packetDur: 3 },
  { d: 'M 264 70 C 360 110, 430 160, 468 210', lx: 356, ly: 122, packetDur: 3.4 },
]

// ---------------------------------------------------------------------------
// Builder
// ---------------------------------------------------------------------------

export function buildTopologyModel({ routers, devices, wan, wireguard }: TopologyInput): TopologyModel {
  const gateway = routers.find((r) => r.roleBadge === 'Principal') ?? routers[0]
  const aps = routers.filter((r) => r.id !== gateway?.id).slice(0, AP_COORDS.length)

  const gatewayNode: RouterNode | null = gateway
    ? { kind: 'router', id: gateway.id, router: gateway, x: GATEWAY_COORD.x, y: GATEWAY_COORD.y, r: GATEWAY_COORD.r, label: GATEWAY_COORD.label }
    : null
  const apNodes: RouterNode[] = aps.map((router, i) => ({
    kind: 'router',
    id: router.id,
    router,
    x: AP_COORDS[i].x,
    y: AP_COORDS[i].y,
    r: AP_COORDS[i].r,
    label: AP_COORDS[i].label,
  }))
  const routerNodes: RouterNode[] = gatewayNode ? [gatewayNode, ...apNodes] : apNodes

  const internetNode = { id: 'internet' as const, x: 500, y: 108 }

  const activePeers = wireguard.peers.filter((p) => p.active)
  const peerNodes: PeerNode[] = activePeers.slice(0, PEER_COORDS.length).map((peer, i) => ({
    kind: 'peer',
    id: `peer-${peer.id}`,
    peer,
    x: PEER_COORDS[i].x,
    y: PEER_COORDS[i].y,
  }))

  // Clusters de clientes (máx 12 dots visibles, resto = badge "+N")
  const mkCluster = (node: RouterNode, cx: number, cy: number): Cluster => {
    const named = devices.filter((d) => d.routerId === node.id && d.online)
    const count = Math.min(named.length, MAX_CLUSTER_DOTS)
    const dots: ClientDot[] = []
    for (let j = 0; j < count; j++) {
      const inner = j >= 8
      const idx = inner ? j - 8 : j
      const n = inner ? count - 8 : Math.min(count, 8)
      const radius = inner ? 22 : 46
      const angle = (-90 + (360 / n) * idx + (inner ? 22 : 0)) * (Math.PI / 180)
      const device = named[j]
      const weak = device?.signalDbm != null ? device.signalDbm <= -65 : false
      dots.push({
        id: `${node.id}-dot-${j}`,
        x: Math.round((cx + radius * Math.cos(angle)) * 10) / 10,
        y: Math.round((cy + radius * Math.sin(angle)) * 10) / 10,
        band: device?.band ?? 'cable',
        weak,
        device,
      })
    }
    return {
      id: `cluster-${node.id}`,
      routerId: node.id,
      cx,
      cy,
      dots,
      extra: Math.max(0, named.length - MAX_CLUSTER_DOTS),
    }
  }

  const clusters: Cluster[] = apNodes.map((node, i) => {
    const { cx, cy } = CLUSTER_COORDS[i]
    return mkCluster(node, cx, cy)
  })
  // El gateway también tiene clientes (cable + wifi)
  if (gatewayNode) {
    clusters.unshift(mkCluster(gatewayNode, GATEWAY_CLUSTER.cx, GATEWAY_CLUSTER.cy))
  }

  // Enlaces
  const links: TopoLink[] = []
  if (gatewayNode) {
    links.push({
      id: 'wan', kind: 'wan',
      d: 'M 500 140 C 500 162, 500 182, 500 204',
      lx: 518, ly: 162, label: `Fibra ${wan.plan} · ${wan.latencyMs} ms`,
      width: 3, packets: 3, packetDur: 2.4,
      from: 'internet', to: gatewayNode.id,
    })
    // Enlace gateway → su cluster de clientes
    links.push({
      id: `cluster-${gatewayNode.id}`, kind: 'cluster',
      d: `M ${GATEWAY_COORD.x - GATEWAY_COORD.r} ${GATEWAY_COORD.y} L ${GATEWAY_CLUSTER.cx + 46} ${GATEWAY_CLUSTER.cy}`,
      lx: 0, ly: 0, label: '',
      width: 1.5, packets: 0, packetDur: 0,
      from: gatewayNode.id, to: `cluster-${gatewayNode.id}`,
    })
  }
  apNodes.forEach((node, i) => {
    const p = UPLINK_PATHS[i]
    const isWifi = node.id === 'patio'
    links.push({
      id: `uplink-${node.id}`, kind: 'uplink',
      d: p.d, lx: p.lx, ly: p.ly,
      label: isWifi ? 'WiFi uplink −58 dBm' : 'Cable 1G',
      width: p.width, packets: p.packets, packetDur: p.packetDur,
      from: gatewayNode?.id ?? 'internet', to: node.id,
    })
    links.push({
      id: `cluster-${node.id}`, kind: 'cluster',
      d: CLUSTER_PATHS[i], lx: 0, ly: 0, label: '',
      width: 1.5, packets: 0, packetDur: 0,
      from: node.id, to: `cluster-${node.id}`,
    })
  })
  peerNodes.forEach((node, i) => {
    const p = WG_PATHS[i]
    links.push({
      id: `wg-${node.peer.id}`, kind: 'wg',
      d: p.d, lx: p.lx, ly: p.ly, label: 'VPN',
      width: 2, packets: 1, packetDur: p.packetDur,
      from: node.id, to: gatewayNode?.id ?? 'internet',
    })
  })

  /** Guardrail de rendimiento: máx ~14 dots de paquetes simultáneos */
  const totalPackets = links.reduce((acc, l) => acc + l.packets, 0)

  /** Enlaces activos de la red (WAN + uplinks), sin túneles */
  const activeLinkCount = links.filter((l) => l.kind === 'wan' || l.kind === 'uplink').length

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
    clusters,
    links,
    totalPackets,
    activeLinkCount,
    backhauls,
    activePeerCount,
    wan,
    relatedTo,
  }
}
