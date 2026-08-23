/**
 * Datos suplementarios de presentación por router (routers.md / router-detail.md).
 * Las cifras canónicas (CPU, RAM, temp, salud, clientes, AdGuard, WireGuard,
 * dispositivos) viven en `@/data/mock`; aquí solo hay metadatos de detalle
 * (MAC, firmware, bandas, backhaul, radios, puertos) y series temporales
 * derivadas que NO contradicen el mock: toda serie termina en el valor actual
 * canónico del router.
 */
import i18n, { dayAbbr } from '@/i18n'
import type { Router } from '@/data/mock'
import { adguard, routers } from '@/data/mock'

// ---------------------------------------------------------------------------
// Extras por router
// ---------------------------------------------------------------------------

export interface BandSplit {
  band24: number
  band5: number
  cable: number
}

export interface RadioInfo {
  name: string // "2.4 GHz" | "5 GHz"
  channel: number
  widthMhz: number
  powerDbm: number
  clients: number
  congested?: boolean
}

export interface PortInfo {
  name: string
  up: boolean
  speed: string // "1 Gbps" | "—"
  role: string
}

/** Boca física RJ45 del chasis (panel visual de puertos). */
export interface EthPort {
  id: string
  label: string // "WAN" | "LAN 1"
  up: boolean
  speed?: string // "2.5 Gbps" | "1 Gbps"
  connectedTo?: string // "NAS Synology" | "Salón · AX3000T"
  deviceMac?: string // MAC del dispositivo conectado (para enlazar a /devices)
  detail?: string // "192.168.8.10 · full duplex"
}

export interface BackhaulInfo {
  kind: 'wireless' | 'cable'
  /** Etiqueta principal: "−58 dBm · 866 Mbps PHY" / "Cable · 1 Gbps · full duplex" */
  headline: string
  latencyMs: number
}

export interface RouterExtras {
  mac: string
  firmware: string
  firmwareBase?: string
  firmwareUpdated: boolean
  /** Caption de changelog si hay update disponible ("Disponible: 24.10.1") */
  firmwareAvailable?: string
  lastReboot: string
  soc: string
  flash: string
  ramMb: number
  bandSplit: BandSplit
  /** Tráfico actual ↓ Mbps (routers.md §③) */
  trafficNow: number
  /** Latencia al gateway en ms (solo APs; gateway = latencia WAN vía mock) */
  gatewayLatencyMs?: number
  gatewayLatencySpark: number[]
  backhaul?: BackhaulInfo
  backhaulSignal: number[] // dBm 24 puntos (wireless) o throughput relativo (cable)
  radios: RadioInfo[]
  ports: PortInfo[]
  /** Bocas RJ45 físicas, en orden de chasis (izquierda → derecha) */
  ethPorts: EthPort[]
}

export const routerExtras: Record<string, RouterExtras> = {
  flint2: {
    mac: '94:83:C4:2A:7F:10',
    firmware: 'GL 4.7.0',
    firmwareBase: 'OpenWrt 21.02',
    firmwareUpdated: true,
    lastReboot: 'Nov 12, 03:12 (maintenance)',
    soc: 'MediaTek MT7986A',
    flash: '8 GB eMMC',
    ramMb: 512,
    bandSplit: { band24: 4, band5: 10, cable: 3 },
    trafficNow: 84.2,
    gatewayLatencySpark: [],
    backhaulSignal: [],
    radios: [],
    ports: [],
    // GL-MT6000: 1× WAN 2.5G + 4× LAN 1G + 1× LAN 2.5G — canon D4:
    // lan1 = uplink Salón (LLDP), lan2 = uplink Estudio (LLDP), lan3 = switch
    // inferido, lan4 = NAS, lan5 = Proxmox pve (2.5G).
    ethPorts: [
      { id: 'wan', label: 'WAN', up: true, speed: '2.5 Gbps', connectedTo: 'Fiber ONT · Digi', detail: '84.122.x.x · full duplex' },
      { id: 'lan1', label: 'LAN 1', up: true, speed: '1 Gbps', connectedTo: 'Living Room · AX3000T', detail: 'Living Room AP uplink (192.168.8.2) · LLDP' },
      { id: 'lan2', label: 'LAN 2', up: true, speed: '1 Gbps', connectedTo: 'Study · NanoPi R4S', detail: 'Study AP uplink (192.168.8.3) · LLDP' },
      { id: 'lan3', label: 'LAN 3', up: true, speed: '1 Gbps', connectedTo: 'Switch sin gestión (inferido)', detail: '8 MACs · sin IP (FDB)' },
      { id: 'lan4', label: 'LAN 4', up: true, speed: '1 Gbps', connectedTo: 'NAS Synology', detail: '192.168.8.10 · cable' },
      { id: 'lan5', label: 'LAN 5', up: true, speed: '2.5 Gbps', connectedTo: 'Proxmox pve', detail: '192.168.8.5 · 2.5G · 10 CT' },
    ],
  },
  living: {
    mac: '50:D2:F5:11:8C:2E',
    firmware: 'OpenWrt 23.05.5',
    firmwareUpdated: true,
    lastReboot: 'Nov 12, 03:12 (maintenance)',
    soc: 'MediaTek MT7981B',
    flash: '128 MB NAND',
    ramMb: 256,
    bandSplit: { band24: 6, band5: 10, cable: 2 },
    trafficNow: 51.7,
    gatewayLatencyMs: 1,
    gatewayLatencySpark: [1, 1, 2, 1, 1, 1, 2, 1, 1, 1, 1, 2, 1, 1, 1, 2, 1, 2, 2, 1, 1, 1, 1, 1],
    backhaul: { kind: 'cable', headline: 'Cable · 1 Gbps · full duplex', latencyMs: 1 },
    backhaulSignal: [58, 59, 60, 59, 58, 58, 60, 61, 60, 59, 60, 60, 61, 60, 59, 60, 61, 62, 61, 60, 60, 59, 60, 60],
    radios: [
      { name: '2.4 GHz', channel: 6, widthMhz: 20, powerDbm: 20, clients: 6 },
      { name: '5 GHz', channel: 36, widthMhz: 80, powerDbm: 23, clients: 10 },
    ],
    ports: [
      { name: 'br-lan', up: true, speed: '—', role: 'Bridge LAN' },
      { name: 'eth0', up: true, speed: '1 Gbps', role: 'Uplink to Gateway' },
      { name: 'lan1', up: true, speed: '1 Gbps', role: 'PS5' },
      { name: 'lan3', up: true, speed: '1 Gbps', role: 'Switch GS308E (LLDP)' },
      { name: 'phy0-ap0', up: true, speed: '—', role: 'Radio 2.4 GHz' },
      { name: 'phy1-ap0', up: true, speed: '—', role: 'Radio 5 GHz' },
    ],
    // AX3000T: 1× WAN + 3× LAN — canon D4: lan3 = uplink del GS308E (up).
    ethPorts: [
      { id: 'wan', label: 'WAN', up: true, speed: '1 Gbps', connectedTo: 'Uplink → Gateway', detail: 'Gateway · LAN 1 (192.168.8.1)' },
      { id: 'lan1', label: 'LAN 1', up: true, speed: '1 Gbps', connectedTo: 'PS5', detail: '192.168.8.31 · cable' },
      { id: 'lan2', label: 'LAN 2', up: false },
      { id: 'lan3', label: 'LAN 3', up: true, speed: '1 Gbps', connectedTo: 'Switch GS308E', detail: '192.168.8.13 · LLDP · uplink ge5' },
    ],
  },
  estudio: {
    mac: '7A:1B:9C:03:F4:62',
    firmware: 'OpenWrt 23.05.5',
    firmwareUpdated: false,
    firmwareAvailable: '24.10.1',
    lastReboot: 'Dec 2, 21:40 (update)',
    soc: 'Rockchip RK3399',
    flash: '32 GB microSD',
    ramMb: 1024,
    bandSplit: { band24: 4, band5: 4, cable: 1 },
    trafficNow: 9.4,
    gatewayLatencyMs: 1,
    gatewayLatencySpark: [1, 1, 1, 2, 1, 1, 1, 1, 2, 1, 1, 1, 1, 1, 2, 1, 1, 1, 1, 2, 1, 1, 1, 1],
    backhaul: { kind: 'cable', headline: 'Cable · 1 Gbps · full duplex', latencyMs: 1 },
    backhaulSignal: [55, 56, 56, 55, 55, 56, 57, 56, 55, 56, 56, 57, 56, 56, 55, 56, 57, 57, 56, 56, 56, 55, 56, 56],
    radios: [
      { name: '2.4 GHz', channel: 11, widthMhz: 20, powerDbm: 18, clients: 4 },
      { name: '5 GHz', channel: 44, widthMhz: 80, powerDbm: 21, clients: 4 },
    ],
    ports: [
      { name: 'br-lan', up: true, speed: '—', role: 'Bridge LAN' },
      { name: 'eth0', up: true, speed: '1 Gbps', role: 'Uplink to Gateway' },
      { name: 'eth1', up: true, speed: '1 Gbps', role: 'Unmanaged switch' },
      { name: 'phy0-ap0', up: true, speed: '—', role: 'Radio 2.4 GHz' },
      { name: 'phy1-ap0', up: true, speed: '—', role: 'Radio 5 GHz' },
    ],
    // NanoPi R4S: 2× 1G (WAN + LAN) — canon D4: lan1 = switch sin gestión.
    // Solo panel: la topología no lo muestra (regla "inferred solo en gateway").
    ethPorts: [
      { id: 'wan', label: 'WAN', up: true, speed: '1 Gbps', connectedTo: 'Uplink → Gateway', detail: 'Gateway · LAN 2 (192.168.8.1)' },
      { id: 'lan1', label: 'LAN 1', up: true, speed: '1 Gbps', connectedTo: 'Switch sin gestión', detail: '8 puertos · 4 en uso · la topología no lo muestra' },
    ],
  },
  patio: {
    mac: 'C0:4A:00:9B:51:8D',
    firmware: 'OpenWrt 23.05.5',
    firmwareUpdated: false,
    firmwareAvailable: '24.10.1',
    lastReboot: 'Dec 9, 14:05 (power outage)',
    soc: 'Qualcomm QCA9563',
    flash: '16 MB SPI',
    ramMb: 128,
    bandSplit: { band24: 5, band5: 1, cable: 0 },
    trafficNow: 1.8,
    gatewayLatencyMs: 2,
    gatewayLatencySpark: [2, 2, 3, 2, 2, 2, 3, 2, 2, 3, 2, 2, 3, 3, 2, 2, 3, 4, 3, 2, 2, 2, 2, 2],
    backhaul: { kind: 'wireless', headline: '−58 dBm · 866 Mbps PHY', latencyMs: 2 },
    backhaulSignal: [-60, -61, -61, -62, -61, -60, -59, -60, -61, -60, -59, -58, -59, -60, -60, -59, -58, -57, -58, -59, -60, -59, -58, -58],
    radios: [
      { name: '2.4 GHz', channel: 1, widthMhz: 20, powerDbm: 20, clients: 5, congested: true },
      { name: '5 GHz', channel: 149, widthMhz: 80, powerDbm: 22, clients: 1 },
    ],
    ports: [
      { name: 'br-lan', up: true, speed: '—', role: 'Bridge LAN' },
      { name: 'eth0', up: false, speed: '—', role: 'LAN port (unused)' },
      { name: 'phy0-ap0', up: true, speed: '—', role: 'Radio 2.4 GHz' },
      { name: 'phy1-sta0', up: true, speed: '866 Mbps', role: 'Wireless uplink to Gateway' },
    ],
    // EAP225: 1× 1G — sin uso (uplink inalámbrico mesh, ver backhaul)
    ethPorts: [
      { id: 'lan1', label: 'LAN', up: false, detail: 'Uplink over WiFi mesh' },
    ],
  },
}

export function getRouterExtras(id: string): RouterExtras {
  // flint2 es el default canónico y siempre está en el literal de arriba.
  return routerExtras[id] ?? routerExtras.flint2!
}

// ---------------------------------------------------------------------------
// Series de rendimiento (CPU / RAM / Temp) — deterministas, terminan en el
// valor actual canónico. Picos documentados: Gateway CPU 61 % (21:10), temp
// máx 58 °C. El resto se deriva del nivel actual de cada router.
// ---------------------------------------------------------------------------

export interface PerfPoint {
  t: string
  cpu: number
  ram: number
  temp: number
}

/** Ruido determinista 0..1 a partir del índice y una semilla por router. */
function noise(i: number, seed: number): number {
  const x = Math.sin(i * 127.1 + seed * 311.7) * 43758.5453
  return x - Math.floor(x)
}

function hourLabels(): string[] {
  return Array.from({ length: 24 }, (_, i) => `${String(i).padStart(2, '0')}:00`)
}

/** Etiquetas para 1h: puntos cada 3 minutos terminando "ahora". */
function hourRangeLabels(n = 20): string[] {
  const labels: string[] = []
  const now = new Date()
  for (let i = n - 1; i >= 0; i--) {
    const d = new Date(now.getTime() - i * 3 * 60_000)
    labels.push(`${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`)
  }
  return labels
}

export function perfSeries(router: Router, range: '1h' | '24h' | '7d'): PerfPoint[] {
  const seed = routers.findIndex((r) => r.id === router.id) + 1
  const isGateway = router.id === 'flint2'
  const n = range === '1h' ? 20 : range === '24h' ? 24 : 7
  const labels = range === '1h' ? hourRangeLabels(n) : range === '24h' ? hourLabels() : Array.from({ length: 7 }, (_, i) => dayAbbr(i))

  // Índice del pico: 21:00 en 24h, penúltimo tramo en 1h, viernes en 7d
  const peakIdx = range === '24h' ? 21 : range === '1h' ? 15 : 4
  const cpuPeak = isGateway ? 61 : Math.min(88, router.cpu + 34)
  const tempPeak = isGateway ? 58 : router.temp + 4

  const points: PerfPoint[] = []
  for (let i = 0; i < n; i++) {
    // Forma diaria: valle de madrugada, subida por la tarde
    const dayShape =
      range === '7d'
        ? 0.7 + 0.3 * (i / (n - 1))
        : 0.55 + 0.45 * Math.sin(((i - 6) / n) * Math.PI * 2 - Math.PI / 2) ** 2
    const wob = noise(i, seed)
    let cpu = Math.max(2, Math.round(router.cpu * dayShape + wob * 6))
    const ram = Math.max(10, Math.round(router.ram - 6 + wob * 10 + (i / n) * 4))
    let temp = Math.max(30, Math.round(router.temp - 5 + wob * 4 + dayShape * 4))
    if (i === peakIdx) cpu = cpuPeak
    if (i === peakIdx) temp = tempPeak
    points.push({ t: labels[i]!, cpu, ram, temp })
  }
  // La serie termina SIEMPRE en el valor canónico actual
  points[n - 1] = { t: labels[n - 1]!, cpu: router.cpu, ram: router.ram, temp: router.temp }
  return points
}

export function perfCaptions(router: Router, data: PerfPoint[]): { cpu: string; ram: string; temp: string } {
  const extras = getRouterExtras(router.id)
  if (data.length === 0) throw new Error('perfCaptions: empty perf series')
  const cpuMax = data.reduce((m, p) => (p.cpu > m.cpu ? p : m), data[0]!)
  const tempMax = data.reduce((m, p) => (p.temp > m.temp ? p : m), data[0]!)
  const ramUsed = Math.round((router.ram / 100) * extras.ramMb)
  return {
    cpu: i18n.t('routerDetail.perf.peakCpu', { pct: cpuMax.cpu, t: cpuMax.t }),
    ram: i18n.t('routerDetail.perf.ramUsed', { used: ramUsed, total: extras.ramMb }),
    temp: i18n.t('routerDetail.perf.maxTemp', { temp: tempMax.temp }),
  }
}

// ---------------------------------------------------------------------------
// Latencia WAN 24h (Flint 2): base 8-12 ms, pico 34 ms a las 21:00 (canon).
// ---------------------------------------------------------------------------

export const WAN_LATENCY_24H: number[] = [
  9, 8, 8, 7, 7, 8, 9, 10, 11, 12, 11, 12, 13, 12, 11, 12, 14, 16, 19, 24, 29, 34, 14, 8,
]

export const WAN_LATENCY_STATS = { avgMs: 11, jitterMs: 2.1, lossPct: 0 } as const

// ---------------------------------------------------------------------------
// AdGuard — serie horaria 24h: suma exacta del canon (84 312 / 15 687) con el
// punto documentado 21:00 → 5 412 consultas · 1 031 bloqueadas.
// ---------------------------------------------------------------------------

export interface AdGuardHour {
  t: string
  permitidas: number
  bloqueadas: number
}

/** Serie horaria 24h derivada de unas stats dadas (misma lógica que el canon). */
export function buildAdGuardSeries(stats: { queries24h: number; blocked24h: number }): AdGuardHour[] {
  return buildAdGuardSeriesFrom(stats)
}

function buildAdGuardSeriesFrom(stats: { queries24h: number; blocked24h: number }): AdGuardHour[] {
  // Pesos de actividad por hora (valle nocturno, pico 21h)
  const weights = [0.35, 0.22, 0.15, 0.12, 0.12, 0.18, 0.35, 0.6, 0.8, 0.9, 1, 1.05, 1.1, 1.05, 1, 1.05, 1.15, 1.3, 1.5, 1.7, 1.9, 2.1, 1.4, 0.7]
  const totalQ = stats.queries24h
  const totalB = stats.blocked24h
  const sumW = weights.reduce((a, b) => a + b, 0)
  const q = weights.map((w) => Math.round((w / sumW) * totalQ))
  // Fijar el punto canónico de las 21:00 solo si las stats son las canónicas
  const isCanon = totalQ === 84312 && totalB === 15687
  if (isCanon) q[21] = 5412
  const drift = totalQ - q.reduce((a, b) => a + b, 0)
  q[20] = (q[20] ?? 0) + drift // absorber el redondeo en la hora anterior
  const b = q.map((v) => Math.round(v * (totalQ > 0 ? totalB / totalQ : 0)))
  if (isCanon) b[21] = 1031
  const driftB = totalB - b.reduce((a, c) => a + c, 0)
  b[20] = (b[20] ?? 0) + driftB
  return q.map((v, i) => ({
    t: `${String(i).padStart(2, '0')}:00`,
    permitidas: v - (b[i] ?? 0),
    bloqueadas: b[i] ?? 0,
  }))
}

export const adguardSeries24h: AdGuardHour[] = buildAdGuardSeries(adguard)

// ---------------------------------------------------------------------------
// WireGuard — extras de peers (router-detail.md §Interactions: endpoint,
// allowed IPs, última IP de conexión). Solo lectura.
// ---------------------------------------------------------------------------

export interface PeerExtra {
  endpoint: string
  allowedIps: string
  lastIp: string
}

export const wgPeerExtras: Record<string, PeerExtra> = {
  'pixel-8-pro': { endpoint: '5.224.x.x:51820', allowedIps: '10.0.0.2/32', lastIp: '5.224.x.x' },
  'macbook-air': { endpoint: '92.58.x.x:51820', allowedIps: '10.0.0.3/32', lastIp: '92.58.x.x' },
  'ipad-air': { endpoint: '—', allowedIps: '10.0.0.4/32', lastIp: '81.34.x.x' },
  'portatil-trabajo': { endpoint: '—', allowedIps: '10.0.0.5/32', lastIp: '80.102.x.x' },
  'casa-familia': { endpoint: '—', allowedIps: '10.0.0.6/32', lastIp: '95.60.x.x' },
}

export const WG_TOTALS_30D = { rx: '17.9 GB', tx: '5.0 GB' } as const

// ---------------------------------------------------------------------------
// Utilidades
// ---------------------------------------------------------------------------

/** "32d 14h" → horas (para ordenar la tabla comparativa) */
export function uptimeHours(uptime: string): number {
  const d = /([0-9]+)d/.exec(uptime)?.[1] ?? '0'
  const h = /([0-9]+)h/.exec(uptime)?.[1] ?? '0'
  return Number(d) * 24 + Number(h)
}
