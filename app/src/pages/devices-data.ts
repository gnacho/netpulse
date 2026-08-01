/**
 * Devices — extensión local del canon (design.md §11 / devices.md §④).
 * Los 10 dispositivos destacados de `@/data/mock` se reutilizan tal cual;
 * la entrada agregada "Bombillas Ikea ×6" se expande en sus 6 bombillas
 * individuales y se añaden los clientes restantes hasta los totales del
 * canon: 47 conocidos · 39 online · 8 offline (Gateway 14, Salón 18,
 * Estudio 9, Patio 6). Nada aquí contradice `@/data/mock`.
 */
import type { LucideIcon } from 'lucide-react'
import { BookOpen, Network, Printer } from 'lucide-react'
import type { Device } from '@/data/mock'
import { devices as canonDevices } from '@/data/mock'

// ---------------------------------------------------------------------------
// Tipos
// ---------------------------------------------------------------------------

export type FilterGroup = 'moviles' | 'ordenadores' | 'tv' | 'iot' | 'red' | 'otros'

export interface ClientDevice extends Device {
  hostname: string
  /** "renews in 9h 12min" · "Static IP (reservation)" · "Expired" */
  dhcpLease: string
  /** "84 days ago" · "today" */
  firstSeen: string
  /** Tráfico total 24h, ya formateado ES */
  traffic24hRx: string
  traffic24hTx: string
  /** Cliente protegido por AdGuard Home */
  adguard: boolean
  /** Grupo del filtro "Tipo" (devices.md §③) */
  group: FilterGroup
  /** Visto por primera vez en los últimos 7 días (stat "Nuevos (7 días)") */
  newThisWeek?: boolean
  /** Icono lucide específico cuando el tipo genérico no basta */
  iconOverride?: LucideIcon
}

export const GROUP_LABELS: Record<FilterGroup, string> = {
  moviles: 'Mobiles',
  ordenadores: 'Computers',
  tv: 'TV & consoles',
  iot: 'IoT & home',
  red: 'Network',
  otros: 'Others',
}

export const GROUP_ORDER: FilterGroup[] = ['moviles', 'ordenadores', 'tv', 'iot', 'red', 'otros']

// ---------------------------------------------------------------------------
// Detalles extra de los dispositivos canónicos (mock.ts §Dispositivos)
// ---------------------------------------------------------------------------

const CANON_DETAILS: Record<string, Omit<ClientDevice, keyof Device>> = {
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

// ---------------------------------------------------------------------------
// Sparkline determinista (mock): paseo pseudo-aleatorio alrededor de `base`
// ---------------------------------------------------------------------------

function spark(seed: number, base: number, spread: number): number[] {
  let s = seed
  const out: number[] = []
  for (let i = 0; i < 12; i++) {
    s = (s * 9301 + 49297) % 233280
    const r = s / 233280
    out.push(Math.max(0, +(base + (r - 0.45) * spread).toFixed(2)))
  }
  out[out.length - 1] = base
  return out
}

function extra(spec: Omit<Device, 'sparkline'> & { seed?: number; spread?: number }): Device {
  const { seed = 1, spread, ...d } = spec
  return { ...d, sparkline: d.online ? spark(seed, d.trafficMbps, spread ?? d.trafficMbps * 0.8) : [] }
}

function bulb(n: number, name: string, ip: string, mac: string, dbm: number): ClientDevice {
  return {
    id: `bombilla-${n}`, name, type: 'iot', manufacturer: 'Ikea Trådfri',
    ip, mac, routerId: 'living', band: '2.4 GHz', signalDbm: dbm,
    trafficMbps: 0, online: true, sparkline: spark(40 + n, 0.005, 0.02),
    hostname: `tradfri-${n}`, dhcpLease: 'renews in 12h 0min', firstSeen: '280 days ago',
    traffic24hRx: '4 MB', traffic24hTx: '1 MB', adguard: true, group: 'iot',
  }
}

// ---------------------------------------------------------------------------
// Dispositivos adicionales (canon: los 10 destacados ya están en mock.ts;
// aquí están el resto hasta 47 — devices.md §④ "29 adicionales plausibles")
// ---------------------------------------------------------------------------

const ADDITIONAL: ClientDevice[] = [
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
    iconOverride: Network,
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
    iconOverride: Printer,
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
    iconOverride: BookOpen,
  },
]

const LIVING: ClientDevice[] = [
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

const ESTUDIO: ClientDevice[] = [
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

const PATIO: ClientDevice[] = [
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

// ---------------------------------------------------------------------------
// Lista completa: 47 clientes (los 10 del canon — salvo el agregado de
// bombillas, que se expande — + 37 adicionales)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Builder: enriquece una lista de `Device` (mock local o API) con los
// metadatos de cliente (hostname, DHCP, AdGuard, grupo de filtro).
// - `withSynthetic=true` (modo demo local, sin backend): expande el canon a
//   los 47 clientes del mockup (agregado de bombillas + adicionales).
// - `withSynthetic=false` (backend live): solo fusiona detalles conocidos por
//   id y deriva valores plausibles para clientes nuevos — nunca inventa
//   dispositivos que la API no ha reportado.
// ---------------------------------------------------------------------------

const TYPE_TO_GROUP: Record<Device['type'], FilterGroup> = {
  movil: 'moviles',
  tablet: 'moviles',
  ordenador: 'ordenadores',
  portatil: 'ordenadores',
  tv: 'tv',
  consola: 'tv',
  iot: 'iot',
  camara: 'iot',
  altavoz: 'iot',
  servidor: 'red',
  switch: 'red',
  desconocido: 'otros',
}

function slug(name: string): string {
  return (
    name
      .toLowerCase()
      .normalize('NFD')
      .replace(/[̀-ͯ]/g, '')
      .replace(/[^a-z0-9]+/g, '-')
      .replace(/^-|-$/g, '') || 'cliente'
  )
}

/** Detalles por defecto para un cliente que la API reporta sin metadatos. */
function defaultDetails(d: Device): Omit<ClientDevice, keyof Device> {
  return {
    hostname: slug(d.name),
    dhcpLease: d.online ? 'renews in 12h 0min' : 'Expired',
    firstSeen: '—',
    traffic24hRx: '—',
    traffic24hTx: '—',
    adguard: true,
    group: TYPE_TO_GROUP[d.type],
  }
}

export function buildClientDevices(canon: Device[], withSynthetic: boolean): ClientDevice[] {
  const expanded: ClientDevice[] = canon
    .filter((d) => !withSynthetic || d.id !== 'bombillas-ikea')
    .map((d) => ({ ...d, ...(CANON_DETAILS[d.id] ?? defaultDetails(d)) }))
  if (!withSynthetic) return expanded
  return [...expanded, ...ADDITIONAL, ...LIVING, ...ESTUDIO, ...PATIO]
}

const canonExpanded: ClientDevice[] = canonDevices
  .filter((d) => d.id !== 'bombillas-ikea')
  .map((d) => ({ ...d, ...CANON_DETAILS[d.id] }))

export const allDevices: ClientDevice[] = [...canonExpanded, ...ADDITIONAL, ...LIVING, ...ESTUDIO, ...PATIO]

/** Señal débil: online con < −70 dBm (devices.md §②) */
export const weakSignalCount = allDevices.filter((d) => d.online && d.signalDbm !== null && d.signalDbm < -70).length

export const newThisWeekDevices = allDevices.filter((d) => d.newThisWeek)

export const adguardProtected = allDevices.filter((d) => d.adguard).length
