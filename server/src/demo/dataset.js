/**
 * NetPulse — Dataset demo canónico.
 * Port ESM (JS puro) de:
 *   - app/src/data/mock.ts                      (valores canónicos §11 design.md)
 *   - app/src/components/routers/routerExtras.ts (extras por router + series)
 *   - app/src/pages/devices-data.ts              (lista completa de 47 clientes)
 * Los valores deben coincidir EXACTAMENTE con el frontend (camelCase idéntico).
 * `iconOverride` (componente Lucide) se porta como string con el nombre del icono.
 */

// ---------------------------------------------------------------------------
// Routers (canon §11)
// ---------------------------------------------------------------------------

export const routers = [
  {
    id: 'flint2',
    name: 'Gateway',
    model: 'GL.iNet Flint 2 (GL-MT6000)',
    modelShort: 'GL.iNet Flint 2',
    role: 'Main gateway',
    roleBadge: 'Main',
    ip: '192.168.8.1',
    status: 'online',
    health: 98,
    cpu: 23,
    ram: 41,
    temp: 54,
    uptime: '32d 14h',
    clients: 14,
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
    status: 'online',
    health: 95,
    cpu: 12,
    ram: 38,
    temp: 47,
    uptime: '32d 14h',
    clients: 18,
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
    status: 'online',
    health: 92,
    cpu: 18,
    ram: 44,
    temp: 51,
    uptime: '11d 3h',
    clients: 9,
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
    status: 'warn',
    health: 68,
    cpu: 31,
    ram: 57,
    temp: 71,
    uptime: '4d 2h',
    clients: 6,
    hotMetric: 'temp',
    sparkline: [1, 1, 1, 1, 1, 2, 3, 5, 7, 8, 8, 9, 10, 9, 8, 9, 11, 12, 13, 12, 9, 6, 4, 2],
  },
]

// ---------------------------------------------------------------------------
// WAN (vía Flint 2)
// ---------------------------------------------------------------------------

export const wan = {
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
// Tráfico WAN — series por rango
// ---------------------------------------------------------------------------

export const trafficByRange = {
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

export const healthScore = {
  score: 92,
  label: 'Excellent',
  caption: 'Network health score',
  note: 'Penalized by the Patio temperature.',
  breakdown: [
    { label: 'Patio temp.', delta: -4 },
    { label: 'Patio coverage', delta: -2 },
    { label: 'congested 2.4 GHz channel', delta: -2 },
  ],
}

// ---------------------------------------------------------------------------
// Dispositivos (totales del canon)
// ---------------------------------------------------------------------------

export const deviceTotals = {
  total: 47,
  online: 39,
  knownOffline: 8,
  newToday: 3,
}

// ---------------------------------------------------------------------------
// Dispositivos canónicos destacados (mock.ts §Dispositivos)
// ---------------------------------------------------------------------------

const canonDevices = [
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
    signalDbm: null, trafficMbps: 12.7, online: true,
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
    signalDbm: null, trafficMbps: 2.3, online: true,
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
]

// ---------------------------------------------------------------------------
// AdGuard Home (en Flint 2, :3000)
// ---------------------------------------------------------------------------

export const adguard = {
  host: '192.168.8.1',
  port: 3000,
  status: 'active',
  queries24h: 84312,
  blocked24h: 15687,
  blockedPct: 18.6,
  trackersBlocked: 9204,
  dnsLatencyMs: 14,
  clientsUsing: 41,
  clientsTotal: 47,
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

export const wireguard = {
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

export const alerts = [
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
// Lista completa de clientes (port de app/src/pages/devices-data.ts):
// 10 canónicos (el agregado "Bombillas Ikea ×6" se expande en 6 bombillas)
// + 37 adicionales = 47 · 39 online · 8 offline
// (Gateway 14, Salón 18, Estudio 9, Patio 6)
// ---------------------------------------------------------------------------

const CANON_DETAILS = {
  'imac-salon': {
    hostname: 'imac-de-marc', dhcpLease: 'Static IP (reservation)', firstSeen: '320 days ago',
    traffic24hRx: '38 GB', traffic24hTx: '2.1 GB', adguard: true, group: 'ordenadores',
  },
  'tv-samsung': {
    hostname: 'samsung-tv-salon', dhcpLease: 'renews in 7h 48min', firstSeen: '290 days ago',
    traffic24hRx: '54 GB', traffic24hTx: '1.2 GB', adguard: true, group: 'tv',
  },
  'pixel-8-pro': {
    hostname: 'pixel-8-pro', dhcpLease: 'renews in 9h 12min', firstSeen: '214 days ago',
    traffic24hRx: '4.2 GB', traffic24hTx: '310 MB', adguard: true, group: 'moviles',
  },
  'macbook-air': {
    hostname: 'macbook-air-de-ana', dhcpLease: 'renews in 5h 31min', firstSeen: '260 days ago',
    traffic24hRx: '18 GB', traffic24hTx: '2.2 GB', adguard: true, group: 'ordenadores',
  },
  'ps5': {
    hostname: 'ps5-salon', dhcpLease: 'Static IP (reservation)', firstSeen: '300 days ago',
    traffic24hRx: '92 GB', traffic24hTx: '4.8 GB', adguard: true, group: 'tv',
  },
  'robot-aspirador': {
    hostname: 'roborock-s8', dhcpLease: 'renews in 10h 2min', firstSeen: '180 days ago',
    traffic24hRx: '340 MB', traffic24hTx: '28 MB', adguard: true, group: 'iot',
  },
  'camara-porche': {
    hostname: 'reolink-porche', dhcpLease: 'renews in 3h 57min', firstSeen: '150 days ago',
    traffic24hRx: '11 GB', traffic24hTx: '640 MB', adguard: true, group: 'iot',
  },
  'nest-mini': {
    hostname: 'nest-mini-estudio', dhcpLease: 'renews in 8h 20min', firstSeen: '240 days ago',
    traffic24hRx: '1.4 GB', traffic24hTx: '88 MB', adguard: true, group: 'iot',
  },
  'nas-synology': {
    hostname: 'diskstation', dhcpLease: 'Static IP (reservation)', firstSeen: '320 days ago',
    traffic24hRx: '96 GB', traffic24hTx: '1.1 TB', adguard: true, group: 'red',
  },
  'galaxy-tab-s9': {
    hostname: 'galaxy-tab-s9', dhcpLease: 'renews in 11h 5min', firstSeen: 'today',
    traffic24hRx: '640 MB', traffic24hTx: '48 MB', adguard: true, group: 'moviles', newThisWeek: true,
  },
}

/** Sparkline determinista (mismo PRNG que devices-data.ts) */
function spark(seed, base, spread) {
  let s = seed
  const out = []
  for (let i = 0; i < 12; i++) {
    s = (s * 9301 + 49297) % 233280
    const r = s / 233280
    out.push(Math.max(0, +(base + (r - 0.45) * spread).toFixed(2)))
  }
  out[out.length - 1] = base
  return out
}

function extra(spec) {
  const { seed = 1, spread, ...d } = spec
  return { ...d, sparkline: d.online ? spark(seed, d.trafficMbps, spread ?? d.trafficMbps * 0.8) : [] }
}

function bulb(n, name, ip, mac, dbm) {
  return {
    id: `bombilla-${n}`, name, type: 'iot', manufacturer: 'Ikea Trådfri',
    ip, mac, routerId: 'living', band: '2.4 GHz', signalDbm: dbm,
    trafficMbps: 0, online: true, sparkline: spark(40 + n, 0.005, 0.02),
    hostname: `tradfri-${n}`, dhcpLease: 'renews in 12h 0min', firstSeen: '280 days ago',
    traffic24hRx: '4 MB', traffic24hTx: '1 MB', adguard: true, group: 'iot',
  }
}

const ADDITIONAL = [
  // —— Gateway (flint2): 14 totales (canon: pixel-8-pro, nas-synology) ——
  {
    ...extra({ id: 'iphone-ana', name: "Ana's iPhone", type: 'movil', manufacturer: 'Apple',
      ip: '192.168.8.44', mac: 'F4:D4:88:19:C2:71', routerId: 'flint2', band: '5 GHz',
      signalDbm: -46, trafficMbps: 2.1, online: true, seed: 11 }),
    hostname: 'iphone-de-ana', dhcpLease: 'renews in 6h 40min', firstSeen: '190 days ago',
    traffic24hRx: '2.8 GB', traffic24hTx: '240 MB', adguard: true, group: 'moviles',
  },
  {
    ...extra({ id: 'macbook-pro', name: "Marc's MacBook Pro", type: 'portatil', manufacturer: 'Apple',
      ip: '192.168.8.26', mac: 'F0:18:98:5A:11:E9', routerId: 'flint2', band: '5 GHz',
      signalDbm: -44, trafficMbps: 8.6, online: true, seed: 12 }),
    hostname: 'macbook-pro-marc', dhcpLease: 'renews in 4h 16min', firstSeen: '230 days ago',
    traffic24hRx: '12.4 GB', traffic24hTx: '1.9 GB', adguard: true, group: 'ordenadores',
  },
  {
    ...extra({ id: 'pc-sobremesa', name: 'Desktop PC', type: 'ordenador', manufacturer: 'ASUSTeK',
      ip: '192.168.8.11', mac: '04:D4:C4:8B:30:A7', routerId: 'flint2', band: 'cable',
      signalDbm: null, trafficMbps: 21.3, online: true, seed: 13 }),
    hostname: 'desktop-8f2k1', dhcpLease: 'Static IP (reservation)', firstSeen: '310 days ago',
    traffic24hRx: '84 GB', traffic24hTx: '6.2 GB', adguard: true, group: 'ordenadores',
  },
  {
    ...extra({ id: 'raspberry-pi', name: 'Raspberry Pi 4', type: 'servidor', manufacturer: 'Raspberry Pi',
      ip: '192.168.8.12', mac: 'DC:A6:32:4F:77:02', routerId: 'flint2', band: 'cable',
      signalDbm: null, trafficMbps: 0.8, online: true, seed: 14 }),
    hostname: 'raspberrypi', dhcpLease: 'Static IP (reservation)', firstSeen: '320 days ago',
    traffic24hRx: '3.4 GB', traffic24hTx: '890 MB', adguard: true, group: 'red',
  },
  {
    ...extra({ id: 'switch-netgear', name: 'Netgear 8-port switch', type: 'servidor', manufacturer: 'Netgear',
      ip: '192.168.8.13', mac: '28:C6:8E:1D:90:44', routerId: 'flint2', band: 'cable',
      signalDbm: null, trafficMbps: 0.02, online: true, seed: 15, spread: 0.04 }),
    hostname: 'gs308e', dhcpLease: 'Static IP (reservation)', firstSeen: '300 days ago',
    traffic24hRx: '64 MB', traffic24hTx: '12 MB', adguard: false, group: 'red',
    iconOverride: 'Network',
  },
  {
    ...extra({ id: 'timbre-nest', name: 'Nest doorbell', type: 'camara', manufacturer: 'Google',
      ip: '192.168.8.72', mac: 'F4:F5:D8:66:01:B8', routerId: 'flint2', band: '2.4 GHz',
      signalDbm: -58, trafficMbps: 0.6, online: true, seed: 16 }),
    hostname: 'nest-doorbell', dhcpLease: 'renews in 9h 44min', firstSeen: '170 days ago',
    traffic24hRx: '1.2 GB', traffic24hTx: '140 MB', adguard: true, group: 'iot',
  },
  {
    ...extra({ id: 'enchufe-lavadora', name: 'Washing machine plug', type: 'iot', manufacturer: 'TP-Link',
      ip: '192.168.8.81', mac: '50:C7:BF:22:E1:9C', routerId: 'flint2', band: '2.4 GHz',
      signalDbm: -62, trafficMbps: 0.01, online: true, seed: 17, spread: 0.02 }),
    hostname: 'tapo-p110-lavadora', dhcpLease: 'renews in 12h 0min', firstSeen: '140 days ago',
    traffic24hRx: '8 MB', traffic24hTx: '2 MB', adguard: false, group: 'iot',
  },
  {
    ...extra({ id: 'pixel-7', name: 'Pixel 7', type: 'movil', manufacturer: 'Google',
      ip: '192.168.8.46', mac: '3C:5A:B4:08:D7:5E', routerId: 'flint2', band: '5 GHz',
      signalDbm: -49, trafficMbps: 1.4, online: true, seed: 18 }),
    hostname: 'pixel-7', dhcpLease: 'renews in 7h 3min', firstSeen: '205 days ago',
    traffic24hRx: '1.9 GB', traffic24hTx: '180 MB', adguard: true, group: 'moviles',
  },
  {
    ...extra({ id: 'impresora-hp', name: 'HP Printer', type: 'desconocido', manufacturer: 'HP',
      ip: '192.168.8.14', mac: '3C:52:82:AB:19:60', routerId: 'flint2', band: '2.4 GHz',
      signalDbm: -60, trafficMbps: 0, online: false }),
    hostname: 'hp-laserjet-m209', dhcpLease: 'Expired', firstSeen: '250 days ago',
    traffic24hRx: '0 MB', traffic24hTx: '0 MB', adguard: true, group: 'otros',
    iconOverride: 'Printer',
  },
  {
    ...extra({ id: 'ipad-air', name: 'iPad Air', type: 'tablet', manufacturer: 'Apple',
      ip: '192.168.8.47', mac: '8C:85:90:2F:B4:11', routerId: 'flint2', band: '5 GHz',
      signalDbm: -54, trafficMbps: 0, online: false }),
    hostname: 'ipad-air', dhcpLease: 'Expired', firstSeen: '260 days ago',
    traffic24hRx: '6.4 GB', traffic24hTx: '520 MB', adguard: true, group: 'moviles',
  },
  {
    ...extra({ id: 'portatil-trabajo', name: 'Work laptop', type: 'portatil', manufacturer: 'Lenovo',
      ip: '192.168.8.27', mac: '54:EE:75:9A:03:F1', routerId: 'flint2', band: '5 GHz',
      signalDbm: -50, trafficMbps: 0, online: false }),
    hostname: 'thinkpad-t14', dhcpLease: 'Expired', firstSeen: '160 days ago',
    traffic24hRx: '812 MB', traffic24hTx: '121 MB', adguard: true, group: 'ordenadores',
  },
  {
    ...extra({ id: 'kindle', name: 'Kindle Paperwhite', type: 'desconocido', manufacturer: 'Amazon',
      ip: '192.168.8.49', mac: '44:65:0D:71:28:C3', routerId: 'flint2', band: '2.4 GHz',
      signalDbm: -58, trafficMbps: 0, online: false }),
    hostname: 'kindle-paperwhite', dhcpLease: 'Expired', firstSeen: '220 days ago',
    traffic24hRx: '220 MB', traffic24hTx: '8 MB', adguard: true, group: 'otros',
    iconOverride: 'BookOpen',
  },
]

const LIVING = [
  // —— Salón (living): 18 totales (canon: imac, tv-samsung, ps5, galaxy-tab-s9) ——
  bulb(1, 'Living room bulb 1', '192.168.8.90', 'CC:86:EC:10:04:21', -58),
  bulb(2, 'Living room bulb 2', '192.168.8.91', 'CC:86:EC:10:04:22', -59),
  bulb(3, 'Floor lamp bulb', '192.168.8.92', 'CC:86:EC:10:04:23', -61),
  bulb(4, 'Entryway bulb', '192.168.8.93', 'CC:86:EC:10:04:24', -64),
  bulb(5, 'Hallway bulb', '192.168.8.94', 'CC:86:EC:10:04:25', -66),
  bulb(6, 'Kitchen bulb', '192.168.8.95', 'CC:86:EC:10:04:26', -60),
  {
    ...extra({ id: 'chromecast', name: 'Chromecast HD', type: 'tv', manufacturer: 'Google',
      ip: '192.168.8.36', mac: '54:60:09:E3:5B:0A', routerId: 'living', band: '5 GHz',
      signalDbm: -54, trafficMbps: 3.9, online: true, seed: 21 }),
    hostname: 'chromecast-hd', dhcpLease: 'renews in 6h 12min', firstSeen: '200 days ago',
    traffic24hRx: '21 GB', traffic24hTx: '380 MB', adguard: true, group: 'tv',
  },
  {
    ...extra({ id: 'homepod-mini', name: 'HomePod mini', type: 'altavoz', manufacturer: 'Apple',
      ip: '192.168.8.53', mac: 'F0:D1:A9:3E:77:5C', routerId: 'living', band: '5 GHz',
      signalDbm: -47, trafficMbps: 0.3, online: true, seed: 22 }),
    hostname: 'homepod-mini', dhcpLease: 'renews in 10h 41min', firstSeen: '190 days ago',
    traffic24hRx: '2.2 GB', traffic24hTx: '96 MB', adguard: true, group: 'iot',
  },
  {
    ...extra({ id: 'galaxy-s23', name: 'Galaxy S23', type: 'movil', manufacturer: 'Samsung',
      ip: '192.168.8.42', mac: '5C:0A:5B:88:1D:E4', routerId: 'living', band: '5 GHz',
      signalDbm: -51, trafficMbps: 0.9, online: true, seed: 23 }),
    hostname: 'galaxy-s23', dhcpLease: 'renews in 8h 55min', firstSeen: '175 days ago',
    traffic24hRx: '3.2 GB', traffic24hTx: '290 MB', adguard: true, group: 'moviles',
  },
  {
    ...extra({ id: 'echo-dot', name: 'Echo Dot', type: 'altavoz', manufacturer: 'Amazon',
      ip: '192.168.8.54', mac: '74:C2:46:19:F0:6B', routerId: 'living', band: '2.4 GHz',
      signalDbm: -56, trafficMbps: 0.2, online: true, seed: 24 }),
    hostname: 'echo-dot-cocina', dhcpLease: 'renews in 11h 18min', firstSeen: '210 days ago',
    traffic24hRx: '1.1 GB', traffic24hTx: '74 MB', adguard: true, group: 'iot',
  },
  {
    ...extra({ id: 'nintendo-switch', name: 'Nintendo Switch', type: 'consola', manufacturer: 'Nintendo',
      ip: '192.168.8.33', mac: '58:BD:A3:4C:E2:09', routerId: 'living', band: '5 GHz',
      signalDbm: -53, trafficMbps: 0.1, online: true, seed: 25, spread: 0.3 }),
    hostname: 'switch-oled', dhcpLease: 'renews in 5h 47min', firstSeen: '240 days ago',
    traffic24hRx: '8.6 GB', traffic24hTx: '310 MB', adguard: true, group: 'tv',
  },
  {
    ...extra({ id: 'portatil-invitado', name: 'Guest laptop', type: 'portatil', manufacturer: 'Desconocido',
      ip: '192.168.8.29', mac: 'A2:7E:9C:41:0B:6D', routerId: 'living', band: '5 GHz',
      signalDbm: -58, trafficMbps: 0.7, online: true, isNew: true, seed: 26 }),
    hostname: 'unknown-7f2a', dhcpLease: 'renews in 2h 9min', firstSeen: 'today',
    traffic24hRx: '480 MB', traffic24hTx: '62 MB', adguard: false, group: 'ordenadores',
    newThisWeek: true,
  },
  {
    ...extra({ id: 'xbox-series-s', name: 'Xbox Series S', type: 'consola', manufacturer: 'Microsoft',
      ip: '192.168.8.32', mac: '7C:1E:52:06:AA:3F', routerId: 'living', band: '5 GHz',
      signalDbm: -55, trafficMbps: 0, online: false }),
    hostname: 'xbox-series-s', dhcpLease: 'Expired', firstSeen: '230 days ago',
    traffic24hRx: '0 MB', traffic24hTx: '0 MB', adguard: true, group: 'tv',
  },
  {
    ...extra({ id: 'portatil-antiguo', name: 'Old laptop', type: 'portatil', manufacturer: 'HP',
      ip: '192.168.8.28', mac: '3C:52:82:5D:90:17', routerId: 'living', band: '2.4 GHz',
      signalDbm: -62, trafficMbps: 0, online: false }),
    hostname: 'hp-pavilion-15', dhcpLease: 'Expired', firstSeen: '300 days ago',
    traffic24hRx: '0 MB', traffic24hTx: '0 MB', adguard: true, group: 'ordenadores',
  },
]

const ESTUDIO = [
  // —— Estudio: 9 totales (canon: macbook-air, nest-mini) ——
  {
    ...extra({ id: 'mac-mini', name: 'Mac mini', type: 'ordenador', manufacturer: 'Apple',
      ip: '192.168.8.22', mac: 'A4:83:E7:66:2C:98', routerId: 'estudio', band: 'cable',
      signalDbm: null, trafficMbps: 1.9, online: true, seed: 31 }),
    hostname: 'mac-mini', dhcpLease: 'Static IP (reservation)', firstSeen: '280 days ago',
    traffic24hRx: '44 GB', traffic24hTx: '5.6 GB', adguard: true, group: 'ordenadores',
  },
  {
    ...extra({ id: 'enchufe-ventilador', name: 'Fan plug', type: 'iot', manufacturer: 'TP-Link',
      ip: '192.168.8.82', mac: '9C:53:22:B1:4E:70', routerId: 'estudio', band: '2.4 GHz',
      signalDbm: -59, trafficMbps: 0.01, online: true, seed: 32, spread: 0.02 }),
    hostname: 'tapo-p110-ventilador', dhcpLease: 'renews in 12h 0min', firstSeen: '120 days ago',
    traffic24hRx: '7 MB', traffic24hTx: '2 MB', adguard: true, group: 'iot',
  },
  {
    ...extra({ id: 'ipad-pro', name: 'iPad Pro', type: 'tablet', manufacturer: 'Apple',
      ip: '192.168.8.51', mac: 'F0:18:98:91:5A:2B', routerId: 'estudio', band: '5 GHz',
      signalDbm: -49, trafficMbps: 0.8, online: true, seed: 33 }),
    hostname: 'ipad-pro', dhcpLease: 'renews in 9h 26min', firstSeen: '160 days ago',
    traffic24hRx: '5.1 GB', traffic24hTx: '390 MB', adguard: true, group: 'moviles',
  },
  {
    ...extra({ id: 'hue-hub', name: 'Hub Philips Hue', type: 'iot', manufacturer: 'Signify',
      ip: '192.168.8.15', mac: '00:17:88:2A:91:CE', routerId: 'estudio', band: 'cable',
      signalDbm: null, trafficMbps: 0.02, online: true, seed: 34, spread: 0.04 }),
    hostname: 'philips-hue-bridge', dhcpLease: 'Static IP (reservation)', firstSeen: '280 days ago',
    traffic24hRx: '96 MB', traffic24hTx: '22 MB', adguard: false, group: 'iot',
  },
  {
    ...extra({ id: 'sonos-one', name: 'Sonos One', type: 'altavoz', manufacturer: 'Sonos',
      ip: '192.168.8.55', mac: '48:A6:B8:14:72:E0', routerId: 'estudio', band: '2.4 GHz',
      signalDbm: -57, trafficMbps: 0.5, online: true, seed: 35 }),
    hostname: 'sonos-one-estudio', dhcpLease: 'renews in 10h 4min', firstSeen: '195 days ago',
    traffic24hRx: '3.8 GB', traffic24hTx: '110 MB', adguard: true, group: 'iot',
  },
  {
    ...extra({ id: 'iphone-trabajo', name: 'Work iPhone', type: 'movil', manufacturer: 'Apple',
      ip: '192.168.8.50', mac: '8C:85:90:47:C1:93', routerId: 'estudio', band: '5 GHz',
      signalDbm: -47, trafficMbps: 0.3, online: true, isNew: true, seed: 36 }),
    hostname: 'iphone-15-pro-work', dhcpLease: 'renews in 3h 38min', firstSeen: 'today',
    traffic24hRx: '320 MB', traffic24hTx: '41 MB', adguard: true, group: 'moviles',
    newThisWeek: true,
  },
  {
    ...extra({ id: 'macbook-viejo', name: 'Old MacBook', type: 'portatil', manufacturer: 'Apple',
      ip: '192.168.8.25', mac: '3C:22:FB:0E:66:A1', routerId: 'estudio', band: '2.4 GHz',
      signalDbm: -60, trafficMbps: 0, online: false }),
    hostname: 'macbook-pro-2015', dhcpLease: 'Expired', firstSeen: '320 days ago',
    traffic24hRx: '0 MB', traffic24hTx: '0 MB', adguard: true, group: 'ordenadores',
  },
]

const PATIO = [
  // —— Patio: 6 totales (canon: robot-aspirador, camara-porche) ——
  {
    ...extra({ id: 'camara-jardin', name: 'Garden camera', type: 'camara', manufacturer: 'Reolink',
      ip: '192.168.8.73', mac: 'EC:71:DB:44:12:9B', routerId: 'patio', band: '2.4 GHz',
      signalDbm: -74, trafficMbps: 1.4, online: true, seed: 41 }),
    hostname: 'reolink-jardin', dhcpLease: 'renews in 8h 49min', firstSeen: '4 days ago',
    traffic24hRx: '14 GB', traffic24hTx: '820 MB', adguard: true, group: 'iot',
    newThisWeek: true,
  },
  {
    ...extra({ id: 'sensor-riego', name: 'Irrigation sensor', type: 'iot', manufacturer: 'Tuya',
      ip: '192.168.8.89', mac: 'D8:1F:12:5B:08:44', routerId: 'patio', band: '2.4 GHz',
      signalDbm: -71, trafficMbps: 0.01, online: true, seed: 42, spread: 0.02 }),
    hostname: 'tuya-riego-01', dhcpLease: 'renews in 12h 0min', firstSeen: '5 days ago',
    traffic24hRx: '2 MB', traffic24hTx: '1 MB', adguard: false, group: 'iot',
    newThisWeek: true,
  },
  {
    ...extra({ id: 'enchufe-calefactor', name: 'Heater plug', type: 'iot', manufacturer: 'TP-Link',
      ip: '192.168.8.80', mac: '50:C7:BF:31:7A:05', routerId: 'patio', band: '2.4 GHz',
      signalDbm: -75, trafficMbps: 0.02, online: true, seed: 43, spread: 0.03 }),
    hostname: 'tapo-p110-calefactor', dhcpLease: 'renews in 12h 0min', firstSeen: '96 days ago',
    traffic24hRx: '6 MB', traffic24hTx: '2 MB', adguard: true, group: 'iot',
  },
  {
    ...extra({ id: 'camara-garaje', name: 'Garage camera', type: 'camara', manufacturer: 'Reolink',
      ip: '192.168.8.74', mac: 'EC:71:DB:44:13:02', routerId: 'patio', band: '2.4 GHz',
      signalDbm: -78, trafficMbps: 0, online: false }),
    hostname: 'reolink-garaje', dhcpLease: 'Expired', firstSeen: '130 days ago',
    traffic24hRx: '0 MB', traffic24hTx: '0 MB', adguard: false, group: 'iot',
  },
]

// Lista completa: 47 clientes (los 10 del canon — salvo el agregado de
// bombillas, que se expande — + 37 adicionales)
const canonExpanded = canonDevices
  .filter((d) => d.id !== 'bombillas-ikea')
  .map((d) => ({ ...d, ...CANON_DETAILS[d.id] }))

export const allDevices = [...canonExpanded, ...ADDITIONAL, ...LIVING, ...ESTUDIO, ...PATIO]

// ---------------------------------------------------------------------------
// Extras por router (port de routerExtras.ts). Toda serie termina en el valor
// actual canónico del router.
// ---------------------------------------------------------------------------

export const routerExtras = {
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
    // GL-MT6000: 1× WAN 2.5G + 4× LAN 1G
    ethPorts: [
      { id: 'wan', label: 'WAN', up: true, speed: '2.5 Gbps', connectedTo: 'Fiber ONT · Digi', detail: '84.122.x.x · full duplex' },
      { id: 'lan1', label: 'LAN 1', up: true, speed: '1 Gbps', connectedTo: 'Living Room · AX3000T', detail: 'Living Room AP uplink (192.168.8.2)' },
      { id: 'lan2', label: 'LAN 2', up: true, speed: '1 Gbps', connectedTo: 'NAS Synology', detail: '192.168.8.10 · cable' },
      { id: 'lan3', label: 'LAN 3', up: true, speed: '1 Gbps', connectedTo: 'Study · NanoPi R4S', detail: 'Study AP uplink (192.168.8.3)' },
      { id: 'lan4', label: 'LAN 4', up: false },
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
      { name: 'phy0-ap0', up: true, speed: '—', role: 'Radio 2.4 GHz' },
      { name: 'phy1-ap0', up: true, speed: '—', role: 'Radio 5 GHz' },
    ],
    // AX3000T: 1× WAN + 3× LAN
    ethPorts: [
      { id: 'wan', label: 'WAN', up: true, speed: '1 Gbps', connectedTo: 'Uplink → Gateway', detail: 'Gateway · LAN 1 (192.168.8.1)' },
      { id: 'lan1', label: 'LAN 1', up: true, speed: '1 Gbps', connectedTo: 'PS5', detail: '192.168.8.31 · cable' },
      { id: 'lan2', label: 'LAN 2', up: false },
      { id: 'lan3', label: 'LAN 3', up: false },
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
      { name: 'eth1', up: true, speed: '1 Gbps', role: 'Study switch' },
      { name: 'phy0-ap0', up: true, speed: '—', role: 'Radio 2.4 GHz' },
      { name: 'phy1-ap0', up: true, speed: '—', role: 'Radio 5 GHz' },
    ],
    // NanoPi R4S: 2× 1G (WAN + LAN)
    ethPorts: [
      { id: 'wan', label: 'WAN', up: true, speed: '1 Gbps', connectedTo: 'Uplink → Gateway', detail: 'Gateway · LAN 3 (192.168.8.1)' },
      { id: 'lan1', label: 'LAN 1', up: true, speed: '1 Gbps', connectedTo: 'Study switch', detail: '8-port switch · 4 in use' },
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

export function getRouterExtras(id) {
  return routerExtras[id] ?? routerExtras.flint2
}

// ---------------------------------------------------------------------------
// Series de rendimiento (CPU / RAM / Temp) — deterministas, terminan en el
// valor actual canónico. Picos documentados: Gateway CPU 61 % (21:10), temp
// máx 58 °C.
// ---------------------------------------------------------------------------

/** Ruido determinista 0..1 a partir del índice y una semilla por router. */
function noise(i, seed) {
  const x = Math.sin(i * 127.1 + seed * 311.7) * 43758.5453
  return x - Math.floor(x)
}

function hourLabels() {
  return Array.from({ length: 24 }, (_, i) => `${String(i).padStart(2, '0')}:00`)
}

/** Etiquetas para 1h: puntos cada 3 minutos terminando "ahora". */
function hourRangeLabels(n = 20) {
  const labels = []
  const now = new Date()
  for (let i = n - 1; i >= 0; i--) {
    const d = new Date(now.getTime() - i * 3 * 60_000)
    labels.push(`${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`)
  }
  return labels
}

const DAY_LABELS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun']

export function perfSeries(router, range) {
  const seed = routers.findIndex((r) => r.id === router.id) + 1
  const isGateway = router.id === 'flint2'
  const n = range === '1h' ? 20 : range === '24h' ? 24 : 7
  const labels = range === '1h' ? hourRangeLabels(n) : range === '24h' ? hourLabels() : DAY_LABELS

  // Índice del pico: 21:00 en 24h, penúltimo tramo en 1h, viernes en 7d
  const peakIdx = range === '24h' ? 21 : range === '1h' ? 15 : 4
  const cpuPeak = isGateway ? 61 : Math.min(88, router.cpu + 34)
  const tempPeak = isGateway ? 58 : router.temp + 4

  const points = []
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
    points.push({ t: labels[i], cpu, ram, temp })
  }
  // La serie termina SIEMPRE en el valor canónico actual
  points[n - 1] = { t: labels[n - 1], cpu: router.cpu, ram: router.ram, temp: router.temp }
  return points
}

// ---------------------------------------------------------------------------
// Latencia WAN 24h (Flint 2): base 8-12 ms, pico 34 ms a las 21:00 (canon).
// ---------------------------------------------------------------------------

export const WAN_LATENCY_24H = [
  9, 8, 8, 7, 7, 8, 9, 10, 11, 12, 11, 12, 13, 12, 11, 12, 14, 16, 19, 24, 29, 34, 14, 8,
]

export const WAN_LATENCY_STATS = { avgMs: 11, jitterMs: 2.1, lossPct: 0 }

// ---------------------------------------------------------------------------
// AdGuard — serie horaria 24h: suma exacta del canon (84 312 / 15 687) con el
// punto documentado 21:00 → 5 412 consultas · 1 031 bloqueadas.
// ---------------------------------------------------------------------------

function buildAdGuardSeries() {
  // Pesos de actividad por hora (valle nocturno, pico 21h)
  const weights = [0.35, 0.22, 0.15, 0.12, 0.12, 0.18, 0.35, 0.6, 0.8, 0.9, 1, 1.05, 1.1, 1.05, 1, 1.05, 1.15, 1.3, 1.5, 1.7, 1.9, 2.1, 1.4, 0.7]
  const totalQ = adguard.queries24h
  const totalB = adguard.blocked24h
  const sumW = weights.reduce((a, b) => a + b, 0)
  const q = weights.map((w) => Math.round((w / sumW) * totalQ))
  // Fijar el punto canónico de las 21:00 y repartir el ajuste
  q[21] = 5412
  const drift = totalQ - q.reduce((a, b) => a + b, 0)
  q[20] += drift // absorber el redondeo en la hora anterior
  const b = q.map((v) => Math.round(v * (totalB / totalQ)))
  b[21] = 1031
  const driftB = totalB - b.reduce((a, c) => a + c, 0)
  b[20] += driftB
  return q.map((v, i) => ({
    t: `${String(i).padStart(2, '0')}:00`,
    permitidas: v - b[i],
    bloqueadas: b[i],
  }))
}

export const adguardSeries24h = buildAdGuardSeries()

// ---------------------------------------------------------------------------
// WireGuard — extras de peers (endpoint, allowed IPs, última IP de conexión)
// ---------------------------------------------------------------------------

export const wgPeerExtras = {
  'pixel-8-pro': { endpoint: '5.224.x.x:51820', allowedIps: '10.0.0.2/32', lastIp: '5.224.x.x' },
  'macbook-air': { endpoint: '92.58.x.x:51820', allowedIps: '10.0.0.3/32', lastIp: '92.58.x.x' },
  'ipad-air': { endpoint: '—', allowedIps: '10.0.0.4/32', lastIp: '81.34.x.x' },
  'portatil-trabajo': { endpoint: '—', allowedIps: '10.0.0.5/32', lastIp: '80.102.x.x' },
  'casa-familia': { endpoint: '—', allowedIps: '10.0.0.6/32', lastIp: '95.60.x.x' },
}

export const WG_TOTALS_30D = { rx: '17.9 GB', tx: '5.0 GB' }

// ---------------------------------------------------------------------------
// Utilidades
// ---------------------------------------------------------------------------

export function getRouter(id) {
  return routers.find((r) => r.id === id)
}

/** "32d 14h" → horas (para ordenar la tabla comparativa) */
export function uptimeHours(uptime) {
  const d = /([0-9]+)d/.exec(uptime)?.[1] ?? '0'
  const h = /([0-9]+)h/.exec(uptime)?.[1] ?? '0'
  return Number(d) * 24 + Number(h)
}
