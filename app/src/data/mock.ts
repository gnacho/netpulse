/**
 * NetPulse — Datos mock canónicos (design.md §11).
 * CONTRATO: todas las páginas importan de `@/data/mock`.
 * No modificar valores sin actualizar design.md.
 */

// ---------------------------------------------------------------------------
// Tipos — viven en `@/data/types` (forma exacta del contrato API);
// aquí solo se re-exportan para no romper imports existentes.
// ---------------------------------------------------------------------------

import { numLocale } from '@/i18n'
import type {
  AdGuardStats,
  AlertEvent,
  Device,
  DistributionNode,
  HealthScore,
  Router,
  TimeRange,
  TrafficPoint,
  WanInfo,
  WireGuardStats,
} from './types'

export type {
  AdGuardStats,
  AlertEvent,
  AlertSeverity,
  Band,
  Device,
  DeviceTotals,
  DeviceType,
  DistributionNode,
  HealthScore,
  LldpInfo,
  PeerType,
  Router,
  Status,
  TimeRange,
  TrafficPoint,
  WanInfo,
  WGPeer,
  WireGuardStats,
} from './types'

// ---------------------------------------------------------------------------
// Paleta de gráficos (design.md §3)
// ---------------------------------------------------------------------------

export const CHART_COLORS = ['#22D3EE', '#A78BFA', '#34D399', '#FBBF24', '#FB7185', '#60A5FA'] as const

// ---------------------------------------------------------------------------
// Routers (canon §11)
// ---------------------------------------------------------------------------

export const routers: Router[] = [
  {
    id: 'flint2',
    name: 'Gateway',
    model: 'GL.iNet Flint 2 (GL-MT6000)',
    modelShort: 'GL.iNet Flint 2',
    role: 'Main gateway',
    roleBadge: 'Principal',
    ip: '192.168.8.1',
    backhaul: 'cable',
    status: 'online',
    health: 98,
    cpu: 23,
    ram: 41,
    temp: 54,
    uptime: '32d 14h',
    // D5: recuento real de clientes online atribuidos (enumeración del dataset)
    clients: 26,
    sparkline: [8, 6, 5, 5, 6, 9, 18, 32, 41, 38, 35, 44, 52, 48, 45, 55, 68, 84, 96, 120, 150, 110, 84, 40],
  },
  {
    id: 'living',
    name: 'Living Room',
    model: 'OpenWrt AP (Xiaomi AX3000T)',
    modelShort: 'Xiaomi AX3000T',
    role: 'Access point',
    roleBadge: 'AP',
    ip: '192.168.8.2',
    backhaul: 'cable',
    lldp: { chassis: 'AX3000T', caps: 'Bridge, Wlan', portDesc: 'eth0' },
    status: 'online',
    health: 95,
    cpu: 12,
    ram: 38,
    temp: 47,
    uptime: '32d 14h',
    clients: 20,
    sparkline: [4, 3, 3, 2, 3, 5, 10, 22, 28, 26, 24, 30, 38, 35, 33, 42, 55, 72, 88, 105, 132, 92, 61, 28],
  },
  {
    id: 'estudio',
    name: 'Study',
    model: 'OpenWrt (NanoPi R4S)',
    modelShort: 'NanoPi R4S',
    role: 'AP + switch',
    roleBadge: 'AP',
    ip: '192.168.8.3',
    backhaul: 'cable',
    lldp: { chassis: 'NanoPi-R4S', caps: 'Bridge, Router', portDesc: 'eth0' },
    status: 'online',
    health: 92,
    cpu: 18,
    ram: 44,
    temp: 51,
    uptime: '11d 3h',
    clients: 8,
    sparkline: [2, 2, 1, 1, 2, 4, 8, 15, 22, 25, 24, 22, 26, 24, 21, 24, 28, 31, 29, 24, 18, 12, 8, 4],
  },
  {
    id: 'patio',
    name: 'Patio',
    model: 'OpenWrt (TP-Link EAP225)',
    modelShort: 'TP-Link EAP225',
    role: 'Outdoor AP',
    roleBadge: 'AP',
    ip: '192.168.8.4',
    backhaul: 'wifi',
    status: 'warn',
    health: 68,
    cpu: 31,
    ram: 57,
    temp: 71,
    uptime: '4d 2h',
    clients: 5,
    hotMetric: 'temp',
    sparkline: [1, 1, 1, 1, 1, 2, 3, 5, 7, 8, 8, 9, 10, 9, 8, 9, 11, 12, 13, 12, 9, 6, 4, 2],
  },
]

// ---------------------------------------------------------------------------
// WAN (vía Flint 2)
// ---------------------------------------------------------------------------

export const wan: WanInfo = {
  plan: '600/600 Mbps',
  downMbps: 84.2,
  upMbps: 12.6,
  latencyMs: 8,
  lossPct: 0,
  publicIp: '84.122.x.x',
  isp: 'Digi',
  peakTodayMbps: 412,
  peakTodayTime: '21:14',
  avgDownMbps: 61,
  total24h: '1.32 TB',
}

// ---------------------------------------------------------------------------
// Tráfico WAN — series por rango (valle nocturno 5–15, pico tarde 300–412,
// pico de subida 21:14 = backup NAS 96 Mbps)
// ---------------------------------------------------------------------------

export const trafficByRange: Record<TimeRange, TrafficPoint[]> = {
  '1h': [
    { t: '21:30', down: 68, up: 9 }, { t: '21:33', down: 74, up: 10 },
    { t: '21:36', down: 81, up: 11 }, { t: '21:39', down: 92, up: 12 },
    { t: '21:42', down: 88, up: 10 }, { t: '21:45', down: 96, up: 13 },
    { t: '21:48', down: 104, up: 14 }, { t: '21:51', down: 98, up: 12 },
    { t: '21:54', down: 90, up: 11 }, { t: '21:57', down: 86, up: 10 },
    { t: '22:00', down: 82, up: 11 }, { t: '22:03', down: 78, up: 12 },
    { t: '22:06', down: 84, up: 13 }, { t: '22:09', down: 91, up: 14 },
    { t: '22:12', down: 88, up: 12 }, { t: '22:15', down: 84, up: 11 },
    { t: '22:18', down: 80, up: 10 }, { t: '22:21', down: 83, up: 12 },
    { t: '22:24', down: 86, up: 13 }, { t: '22:27', down: 84, up: 12.6 },
  ],
  '24h': [
    { t: '00', down: 42, up: 8 }, { t: '01', down: 28, up: 6 },
    { t: '02', down: 15, up: 5 }, { t: '03', down: 9, up: 4 },
    { t: '04', down: 7, up: 4 }, { t: '05', down: 8, up: 5 },
    { t: '06', down: 14, up: 6 }, { t: '07', down: 32, up: 9 },
    { t: '08', down: 58, up: 14 }, { t: '09', down: 74, up: 18 },
    { t: '10', down: 88, up: 21 }, { t: '11', down: 96, up: 24 },
    { t: '12', down: 112, up: 26 }, { t: '13', down: 124, up: 28 },
    { t: '14', down: 118, up: 25 }, { t: '15', down: 132, up: 27 },
    { t: '16', down: 148, up: 30 }, { t: '17', down: 176, up: 34 },
    { t: '18', down: 224, up: 42 }, { t: '19', down: 298, up: 55 },
    { t: '20', down: 356, up: 71 }, { t: '21', down: 412, up: 96 },
    { t: '22', down: 284, up: 48 }, { t: '23', down: 122, up: 21 },
  ],
  '7d': [
    { t: 'Mon', down: 61, up: 12 }, { t: 'Tue', down: 58, up: 11 },
    { t: 'Wed', down: 64, up: 13 }, { t: 'Thu', down: 71, up: 15 },
    { t: 'Fri', down: 88, up: 19 }, { t: 'Sat', down: 96, up: 22 },
    { t: 'Sun', down: 84, up: 18 },
  ],
  '30d': [
    { t: '1', down: 54, up: 10 }, { t: '4', down: 58, up: 11 },
    { t: '7', down: 62, up: 12 }, { t: '10', down: 57, up: 11 },
    { t: '13', down: 66, up: 13 }, { t: '16', down: 72, up: 15 },
    { t: '19', down: 68, up: 14 }, { t: '22', down: 75, up: 16 },
    { t: '25', down: 81, up: 18 }, { t: '28', down: 78, up: 17 },
    { t: '30', down: 84, up: 19 },
  ],
}

// ---------------------------------------------------------------------------
// Health Score global
// ---------------------------------------------------------------------------

export const healthScore: HealthScore = {
  score: 92,
  label: 'Excelente',
  caption: 'Network health score',
  note: 'Penalized by the Patio temperature.',
  breakdown: [
    { label: 'Patio temp.', delta: -4 },
    { label: 'Patio coverage', delta: -2 },
    { label: 'congested 2.4 GHz channel', delta: -2 },
  ],
}

// ---------------------------------------------------------------------------
// Dispositivos (totales + destacados)
// ---------------------------------------------------------------------------

// D5: totales derivados de la enumeración real del dataset reconciliado
// (65 IDs únicos: 59 online · 6 conocidos offline · 3 nuevos hoy).
export const deviceTotals = {
  total: 65,
  online: 59,
  knownOffline: 6,
  newToday: 3,
}

export const devices: Device[] = [
  {
    id: 'imac-salon', name: 'iMac Living Room', type: 'ordenador', manufacturer: 'Apple',
    ip: '192.168.8.21', mac: 'A4:83:E7:21:0B:3C', routerId: 'living', band: '5 GHz',
    signalDbm: -48, trafficMbps: 32.4, online: true,
    sparkline: [12, 15, 18, 22, 26, 30, 34, 32, 28, 30, 32, 31],
  },
  {
    id: 'tv-samsung', name: 'TV Samsung', type: 'tv', manufacturer: 'Samsung',
    ip: '192.168.8.34', mac: '8C:EA:48:5D:2F:91', routerId: 'living', band: '5 GHz',
    signalDbm: -52, trafficMbps: 18.1, online: true,
    sparkline: [8, 10, 14, 16, 18, 20, 19, 18, 17, 18, 18, 18],
  },
  {
    id: 'pixel-8-pro', name: 'Pixel 8 Pro', type: 'movil', manufacturer: 'Google',
    ip: '192.168.8.45', mac: 'F2:6D:19:A8:44:C2', routerId: 'flint2', band: '5 GHz',
    signalDbm: -41, trafficMbps: 6.2, online: true,
    sparkline: [3, 4, 5, 7, 6, 8, 6, 5, 7, 6, 6, 6],
  },
  {
    id: 'macbook-air', name: 'MacBook Air', type: 'portatil', manufacturer: 'Apple',
    ip: '192.168.8.23', mac: '3C:22:FB:71:9E:05', routerId: 'estudio', band: '5 GHz',
    signalDbm: -45, trafficMbps: 4.8, online: true,
    sparkline: [2, 3, 4, 5, 5, 6, 5, 4, 5, 5, 5, 5],
  },
  {
    id: 'ps5', name: 'PS5', type: 'consola', manufacturer: 'Sony',
    ip: '192.168.8.31', mac: '78:C8:81:0A:6B:D4', routerId: 'living', band: 'cable',
    signalDbm: null, trafficMbps: 12.7, online: true, port: 'lan1',
    sparkline: [4, 6, 8, 10, 12, 14, 13, 12, 13, 12, 13, 13],
  },
  {
    id: 'robot-aspirador', name: 'Robot vacuum', type: 'iot', manufacturer: 'Roborock',
    ip: '192.168.8.61', mac: 'B0:4A:39:2E:77:10', routerId: 'patio', band: '2.4 GHz',
    signalDbm: -67, trafficMbps: 0.02, online: true,
    sparkline: [0.01, 0.02, 0.02, 0.03, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02, 0.02],
  },
  {
    id: 'camara-porche', name: 'Porch camera', type: 'camara', manufacturer: 'Reolink',
    ip: '192.168.8.71', mac: 'EC:71:DB:44:12:8A', routerId: 'patio', band: '2.4 GHz',
    signalDbm: -72, trafficMbps: 1.1, online: true,
    sparkline: [1, 1.1, 1.1, 1.2, 1.1, 1, 1.1, 1.1, 1.2, 1.1, 1.1, 1.1],
  },
  {
    id: 'nest-mini', name: 'Nest Mini', type: 'altavoz', manufacturer: 'Google',
    ip: '192.168.8.52', mac: '1A:2B:3C:4D:5E:6F', routerId: 'estudio', band: '2.4 GHz',
    signalDbm: -55, trafficMbps: 0.4, online: true,
    sparkline: [0.3, 0.4, 0.4, 0.5, 0.4, 0.4, 0.4, 0.4, 0.4, 0.4, 0.4, 0.4],
  },
  {
    id: 'nas-synology', name: 'NAS Synology', type: 'servidor', manufacturer: 'Synology',
    ip: '192.168.8.10', mac: '00:11:32:9C:51:B7', routerId: 'flint2', band: 'cable',
    signalDbm: null, trafficMbps: 2.3, online: true, port: 'lan4',
    sparkline: [1, 1.5, 2, 2.5, 3, 2.8, 2.4, 2.2, 2.3, 2.3, 2.3, 2.3],
  },
  {
    id: 'bombillas-ikea', name: 'Ikea bulbs ×6', type: 'iot', manufacturer: 'Ikea',
    ip: '192.168.8.8x', mac: '—', routerId: 'living', band: '2.4 GHz',
    signalDbm: -60, trafficMbps: 0, online: true,
    sparkline: [0, 0, 0.01, 0, 0, 0.01, 0, 0, 0, 0.01, 0, 0],
  },
  {
    id: 'galaxy-tab-s9', name: 'Galaxy Tab S9', type: 'tablet', manufacturer: 'Samsung',
    ip: '192.168.8.48', mac: 'D6:91:2F:07:B3:55', routerId: 'living', band: '5 GHz',
    signalDbm: -50, trafficMbps: 1.8, online: true, isNew: true,
    sparkline: [0, 0, 0, 0, 0, 0, 0, 0, 0.5, 1.2, 1.6, 1.8],
  },

  // -- Fixtures de topología v5 (mockup aprobado: switch gestionado LLDP, --
  // -- hipervisor con CTs anidados, switch/bridge inferido vía FDB) ----------

  // Switch gestionado Netgear GS308E (Salón, uplink lan3): D1 — existe como
  // Device (badge "Switch gestionado" en /devices) Y como DistributionNode
  // kind='managed' (dist-living-lan3, misma chassis-MAC); el mapa nunca
  // renderiza su chip (filtro por MAC en model.ts).
  {
    id: 'gs308e', name: 'GS308E', type: 'switch', manufacturer: 'Netgear',
    ip: '192.168.8.13', mac: 'B0:B9:8A:13:08:0E', routerId: 'living', band: 'cable',
    signalDbm: null, trafficMbps: 0.4, online: true, attachTo: 'living', port: 'lan3',
    lldp: { chassis: 'GS308E', mgmt: '192.168.8.13', caps: 'Bridge', portDesc: 'ge5' },
    sparkline: [0.3, 0.4, 0.4, 0.5, 0.4, 0.4, 0.4, 0.4, 0.4, 0.4, 0.4, 0.4],
  },

  // 3 clientes tras el switch gestionado GS308E (Salón, lan3) — cuelgan del
  // DistributionNode kind='managed' (identificado vía LLDP).
  {
    id: 'xbox-series-s', name: 'Xbox Series S', type: 'consola', manufacturer: 'Microsoft',
    ip: '192.168.8.35', mac: '7C:ED:8D:4A:11:22', routerId: 'living', band: 'cable',
    signalDbm: null, trafficMbps: 9.8, online: true, attachTo: 'dist-living-lan3',
    sparkline: [3, 5, 7, 9, 11, 12, 10, 9, 10, 9, 10, 9.8],
  },
  {
    id: 'apple-tv-4k', name: 'Apple TV 4K', type: 'tv', manufacturer: 'Apple',
    ip: '192.168.8.36', mac: 'F0:18:98:2B:33:44', routerId: 'living', band: 'cable',
    signalDbm: null, trafficMbps: 15.2, online: true, attachTo: 'dist-living-lan3',
    sparkline: [6, 8, 10, 12, 14, 16, 15, 14, 15, 15, 15, 15.2],
  },
  {
    id: 'receptor-denon', name: 'Receptor Denon', type: 'altavoz', manufacturer: 'Denon',
    ip: '192.168.8.37', mac: '00:05:CD:55:66:77', routerId: 'living', band: 'cable',
    signalDbm: null, trafficMbps: 0.6, online: true, attachTo: 'dist-living-lan3',
    sparkline: [0.4, 0.5, 0.5, 0.6, 0.6, 0.7, 0.6, 0.6, 0.6, 0.6, 0.6, 0.6],
  },

  // Hipervisor Proxmox (gateway lan5, 2.5G) + 10 CTs anidados (MACs OUI BC:24:11)
  {
    id: 'pve', name: 'Proxmox pve', type: 'servidor', manufacturer: 'Supermicro',
    ip: '192.168.8.5', mac: '3C:52:82:10:20:30', routerId: 'flint2', band: 'cable',
    signalDbm: null, trafficMbps: 12.3, online: true, port: 'lan5',
    sparkline: [8, 9, 10, 11, 12, 13, 12, 12, 12, 12, 12, 12.3],
  },
  ...([
    ['ct-pihole', 'Pi-hole', 'servidor', 8.41, 6.1],
    ['ct-home-assistant', 'Home Assistant', 'iot', 8.42, 4.2],
    ['ct-nextcloud', 'Nextcloud', 'servidor', 8.43, 7.8],
    ['ct-jellyfin', 'Jellyfin', 'servidor', 8.44, 9.4],
    ['ct-immich', 'Immich', 'servidor', 8.45, 3.3],
    ['ct-gitea', 'Gitea', 'servidor', 8.46, 1.2],
    ['ct-uptime-kuma', 'Uptime Kuma', 'iot', 8.47, 0.4],
    ['ct-adguard-sync', 'AdGuard sync', 'servidor', 8.48, 0.8],
    ['ct-postgres', 'Postgres', 'servidor', 8.49, 2.1],
    ['ct-redis', 'Redis', 'servidor', 8.50, 0.9],
  ] as const).map(([id, name, type, ipSuffix, mbps], i): Device => ({
    id, name, type: type as Device['type'], manufacturer: 'Proxmox VE (CT)',
    ip: `192.168.${ipSuffix}`, mac: `BC:24:11:00:2${i}:${(0x10 + i).toString(16).toUpperCase().padStart(2, '0')}`,
    routerId: 'flint2', band: 'cable', signalDbm: null, trafficMbps: mbps, online: true,
    attachTo: 'pve',
    sparkline: Array.from({ length: 12 }, (_, j) => Math.max(0.1, mbps - 2 + ((i + j) % 5))),
  })),

  // Tras el switch/bridge inferido (gateway lan3, OUI heterogéneo, sin IP).
  // D3: pc-sobremesa, raspberry-pi e impresora-hp ya existen como clientes
  // preexistentes (enriquecidos con attachTo en devices-data.ts, igual que en
  // el dataset Go) — aquí solo los 5 IDs nuevos; cada ID aparece 1 sola vez.
  ...([
    ['tv-salon-cable', 'TV Living (wired)', 'tv', 'Samsung', '8C:EA:48:AA:02:02', 8.61, 24.4],
    ['xbox-one', 'Xbox One', 'consola', 'Microsoft', '7C:ED:8D:AA:05:05', 8.64, 4.2],
    ['receptor-av', 'AV Receiver', 'altavoz', 'Denon', '00:05:CD:AA:06:06', 8.65, 0.3],
    ['deco-orange', 'Orange STB', 'tv', 'Sagemcom', '48:83:B4:AA:07:07', 8.66, 1.1],
    ['pc-invitado', 'Guest PC', 'ordenador', '—', 'A2:F4:11:AA:08:08', 8.67, 0.8],
  ] as const).map(([id, name, type, man, mac, ipSuffix, mbps], i): Device => ({
    id, name, type: type as Device['type'], manufacturer: man,
    ip: `192.168.${ipSuffix}`, mac, routerId: 'flint2', band: 'cable',
    signalDbm: null, trafficMbps: mbps, online: true, attachTo: 'dist-flint2-lan3',
    sparkline: Array.from({ length: 12 }, (_, j) => Math.max(0.05, mbps - 1.5 + ((i + j) % 4) * 0.5)),
  })),
]

// ---------------------------------------------------------------------------
// Nodos de distribución (topología v5): inferidos del FDB en live; en demo,
// fixtures equivalentes al mockup aprobado (switch lan3 + hipervisor lan5).
// ---------------------------------------------------------------------------

export const distributionNodes: DistributionNode[] = [
  { id: 'dist-flint2-lan3', kind: 'inferred', routerId: 'flint2', port: 'lan3', macCount: 8 },
  { id: 'dist-pve', kind: 'hypervisor', routerId: 'flint2', port: 'lan5', macCount: 11, hostDeviceId: 'pve', name: 'Proxmox pve' },
  // Switch gestionado Netgear GS308E en el AP Salón (uplink lan3), identificado
  // vía LLDP (chasis + IP de mgmt + caps + puerto remoto). 4 MACs: switch + 3
  // equipos. D1: `mac` = chassis-MAC del Device 'gs308e' (excluye su chip).
  {
    id: 'dist-living-lan3', kind: 'managed', routerId: 'living', port: 'lan3', macCount: 4,
    name: 'GS308E', ip: '192.168.8.13', mac: 'B0:B9:8A:13:08:0E',
    lldp: { chassis: 'GS308E', mgmt: '192.168.8.13', caps: 'Bridge', portDesc: 'ge5' },
  },
]

// ---------------------------------------------------------------------------
// AdGuard Home (en Flint 2, :3000)
// ---------------------------------------------------------------------------

export const adguard: AdGuardStats = {
  host: '192.168.8.1',
  port: 3000,
  status: 'active',
  queries24h: 84312,
  blocked24h: 15687,
  blockedPct: 18.6,
  trackersBlocked: 9204,
  dnsLatencyMs: 14,
  clientsUsing: 60,
  clientsTotal: 65,
  topBlocked: [
    { domain: 'graph.facebook.com', count: 1204 },
    { domain: 'adservice.google.com', count: 986 },
    { domain: 'metrics.icloud.com', count: 731 },
    { domain: 'telemetry.nvidia.com', count: 512 },
    { domain: 'ads.tiktok.com', count: 448 },
  ],
  filterLists: 6,
  rules: 218442,
}

// ---------------------------------------------------------------------------
// WireGuard (servidor en Flint 2, wg0 · 10.0.0.1/24)
// ---------------------------------------------------------------------------

export const wireguard: WireGuardStats = {
  interface: 'wg0',
  subnet: '10.0.0.1/24',
  status: 'active',
  peers: [
    { id: 'pixel-8-pro', name: 'Pixel 8 Pro', type: 'movil', tunnelIp: '10.0.0.2', active: true, lastHandshake: '38s ago', rx: '1.2 GB', tx: '214 MB' },
    { id: 'macbook-air', name: 'MacBook Air', type: 'portatil', tunnelIp: '10.0.0.3', active: true, lastHandshake: '1 min ago', rx: '640 MB', tx: '88 MB' },
    { id: 'ipad-air', name: 'iPad Air', type: 'tablet', tunnelIp: '10.0.0.4', active: false, lastHandshake: '2 days ago', rx: '3.1 GB', tx: '402 MB' },
    { id: 'portatil-trabajo', name: 'Work laptop', type: 'portatil', tunnelIp: '10.0.0.5', active: false, lastHandshake: '6h ago', rx: '812 MB', tx: '121 MB' },
    { id: 'casa-familia', name: 'Family home', type: 'sitio', tunnelIp: '10.0.0.6', active: false, lastHandshake: '9 days ago', rx: '12 GB', tx: '4.2 GB' },
  ],
}

// ---------------------------------------------------------------------------
// Alertas (canon §11)
// ---------------------------------------------------------------------------

export const alerts: AlertEvent[] = [
  {
    id: 'alert-temp-patio', severity: 'warn', title: 'High temperature on Patio',
    description: '71 °C, above the threshold (65 °C)', time: '12 min ago', read: false, routerId: 'patio',
  },
  {
    id: 'alert-firmware-estudio', severity: 'warn', title: 'Firmware available',
    description: 'OpenWrt 24.10.1 for Study', time: '3h ago', read: false, routerId: 'estudio',
  },
  {
    id: 'alert-nuevo-tab', severity: 'info', title: 'New device',
    description: "'Galaxy Tab S9' joined Living Room", time: '26 min ago', read: true, routerId: 'living',
  },
  {
    id: 'alert-handshake-wg', severity: 'info', title: 'WireGuard handshake',
    description: 'Pixel 8 Pro connected from 5.224.x.x', time: '38s ago', read: true, routerId: 'flint2',
  },
  {
    id: 'alert-backup-adguard', severity: 'ok', title: 'AdGuard backup completed',
    description: 'Configuration and lists backed up to the NAS', time: '1 day ago', read: true, routerId: 'flint2',
  },
]

export const unreadAlerts = alerts.filter((a) => !a.read).length

// ---------------------------------------------------------------------------
// Helpers compartidos
// ---------------------------------------------------------------------------

export function getRouter(id: string): Router | undefined {
  return routers.find((r) => r.id === id)
}

export function routerName(id: string): string {
  return getRouter(id)?.name ?? id
}

/** Formato con el locale activo: 84.2 → "84,2" (es) / "84.2" (en) */
export function fmtEs(n: number, decimals = 1): string {
  return n.toLocaleString(numLocale(), { minimumFractionDigits: decimals, maximumFractionDigits: decimals })
}

/** Entero con separador de miles según el locale activo */
export function fmtInt(n: number): string {
  return n.toLocaleString(numLocale())
}

/** Icono de señal según dBm (umbrales domésticos) */
export function signalLevel(dbm: number | null): 'high' | 'medium' | 'low' | 'zero' | 'cable' {
  if (dbm === null) return 'cable'
  if (dbm >= -55) return 'high'
  if (dbm >= -65) return 'medium'
  if (dbm >= -75) return 'low'
  return 'zero'
}
