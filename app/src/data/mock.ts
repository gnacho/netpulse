/**
 * NetPulse — Datos mock canónicos (design.md §11).
 * CONTRATO: todas las páginas importan de `@/data/mock`.
 *
 * SPEC-65 D65-1 (single-source): el canon (routers, devices, deviceTotals,
 * distributionNodes, adguard, wireguard, wan, health) vive SOLO en
 * `demo-canon.json`, generado desde Go (`go run ./cmd/gen-demo-canon` en
 * server-go). NUNCA editar el JSON a mano: hay un test Go de frescura.
 * Aquí quedan solo: paleta, tráfico local del tick demo (random walk — no
 * está en el JSON a propósito), alertas del tick demo y helpers.
 */

// ---------------------------------------------------------------------------
// Tipos — viven en `@/data/types` (forma exacta del contrato API);
// aquí solo se re-exportan para no romper imports existentes.
// ---------------------------------------------------------------------------

import i18n, { numLocale } from '@/i18n'
import canon from '@/data/demo-canon.json'
import type {
  AdGuardStats,
  AlertEvent,
  Device,
  DeviceTotals,
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
  DeviceInfra,
  DeviceTotals,
  DeviceType,
  DistributionNode,
  HealthScore,
  LldpInfo,
  PeerType,
  Router,
  Status,
  TimeRange,
  TopoSemantics,
  TopoSemLink,
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
// Canon demo (SPEC-65 D65-1): re-exportado desde demo-canon.json con casteo
// explícito al contrato de `@/data/types` (el JSON se tipa por inferencia).
// Los valores del JSON son la verdad reconciliada del canon Go de fase 6.
// ---------------------------------------------------------------------------

export const routers = canon.routers as unknown as Router[]
export const devices = canon.devices as unknown as Device[]
export const deviceTotals: DeviceTotals = canon.deviceTotals
export const distributionNodes = canon.distributionNodes as unknown as DistributionNode[]
export const adguard = canon.adguard as unknown as AdGuardStats
export const wireguard = canon.wireguard as unknown as WireGuardStats
export const wan: WanInfo = canon.wan
export const healthScore = canon.health as unknown as HealthScore

// ---------------------------------------------------------------------------
// Nombres propios del canon demo en inglés (issue #238)
// El canon Go (demo-canon.json) es ES y NO se edita a mano. DataProvider
// traduce estos textos al idioma activo SOLO en modo demo (en live los
// nombres vienen del backend y nunca se tocan). Si el canon Go añade un
// nombre en castellano, hay que mapearlo aquí o saldrá en ES con la UI en EN.
// ---------------------------------------------------------------------------

/** id de router/device → nombre EN. Los no mapeados son neutros ("Patio", "Gateway"). */
export const DEMO_NAME_EN: Record<string, string> = {
  // routers
  living: 'Living Room',
  estudio: 'Study',
  // devices
  'imac-salon': 'iMac Living Room',
  'tv-salon-cable': 'Living Room TV (wired)',
  'tv-samsung': 'Samsung TV',
  'portatil-trabajo': 'Work laptop',
  'portatil-invitado': 'Guest laptop',
  'portatil-antiguo': 'Old laptop',
  'macbook-viejo': 'Old MacBook',
  'pc-invitado': 'Guest PC',
  'iphone-ana': "Ana's iPhone",
  'iphone-trabajo': 'Work iPhone',
  'bombilla-1': 'Living room bulb 1',
  'bombilla-2': 'Living room bulb 2',
  'bombilla-3': 'Floor lamp bulb',
  'bombilla-4': 'Entrance bulb',
  'bombilla-5': 'Hallway bulb',
  'bombilla-6': 'Kitchen bulb',
  'camara-garaje': 'Garage camera',
  'camara-jardin': 'Garden camera',
  'camara-porche': 'Porch camera',
  'timbre-nest': 'Nest doorbell',
  'robot-aspirador': 'Robot vacuum',
  'enchufe-lavadora': 'Washing machine plug',
  'enchufe-ventilador': 'Fan plug',
  'enchufe-calefactor': 'Heater plug',
  'macbook-pro': "Marc's MacBook Pro",
  'pc-sobremesa': 'Desktop PC',
  'sensor-riego': 'Irrigation sensor',
  'impresora-hp': 'HP printer',
  'receptor-denon': 'Denon receiver',
  'receptor-av': 'AV receiver',
  'hue-hub': 'Philips Hue Hub',
}

/** `role` del router (texto libre ES) → EN. roleBadge ya se traduce con roleLabel(). */
export const DEMO_ROLE_EN: Record<string, string> = {
  'Gateway principal': 'Main gateway',
  'Punto de acceso': 'Access point',
  'AP exterior': 'Outdoor AP',
}

/** Textos libres de salud del canon (caption/note/breakdown) → EN. */
export const DEMO_HEALTH_EN: Record<string, string> = {
  'Puntuación de salud de la red': 'Network health score',
  'Penalizado por la temperatura del Patio.': 'Penalized by the Patio temperature.',
  'temp. Patio': 'Patio temp.',
  'cobertura Patio': 'Patio coverage',
  'canal 2.4 GHz congestionado': 'congested 2.4 GHz channel',
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
// Alertas (canon §11) — bilingües ES/EN (issue #239)
// ---------------------------------------------------------------------------

// Ts unix SEGUNDOS escalonados en coherencia con el string legado
// ("38s ago" → now-38, "12 min ago" → now-720, ...) — espejo del canon Go
// (server-go/internal/adapters/demo_dataset.go canonAlerts, SPEC-ALERTAS §5).
const alertNow = Math.floor(Date.now() / 1000)

/** Texto bilingüe de alerta demo: es/en según el idioma activo (issue #239). */
function demoText({ es, en }: { es: string; en: string }): string {
  return i18n.language?.startsWith('es') ? es : en
}

// Los títulos/descripciones se resuelven con getters al leerlos: un cambio de
// idioma re-renderiza (useTranslation) y las alertas se muestran en el idioma
// activo sin recargar, aunque el bundle ya tenga la referencia al array.
export const alerts: AlertEvent[] = [
  {
    id: 'alert-temp-patio', category: 'router', urgent: true, severity: 'warn',
    get title() { return demoText({ es: 'Temperatura alta en Patio', en: 'High temperature on Patio' }) },
    get description() { return demoText({ es: '71 °C, por encima del umbral (65 °C)', en: '71 °C, above the threshold (65 °C)' }) },
    time: '12 min ago', ts: alertNow - 12 * 60, read: false, routerId: 'patio',
  },
  {
    id: 'alert-firmware-estudio', category: 'system', urgent: false, severity: 'warn',
    get title() { return demoText({ es: 'Firmware disponible', en: 'Firmware available' }) },
    get description() { return demoText({ es: 'OpenWrt 24.10.1 para Estudio', en: 'OpenWrt 24.10.1 for Study' }) },
    time: '3h ago', ts: alertNow - 3 * 3600, read: false, routerId: 'estudio',
  },
  {
    id: 'alert-nuevo-tab', category: 'clients', urgent: false, severity: 'info',
    get title() { return demoText({ es: 'Nuevo dispositivo', en: 'New device' }) },
    get description() { return demoText({ es: "'Galaxy Tab S9' se ha unido a Salón", en: "'Galaxy Tab S9' joined Living Room" }) },
    time: '26 min ago', ts: alertNow - 26 * 60, read: true, routerId: 'living',
  },
  {
    id: 'alert-handshake-wg', category: 'vpn', urgent: false, severity: 'info',
    get title() { return demoText({ es: 'Handshake WireGuard', en: 'WireGuard handshake' }) },
    get description() { return demoText({ es: 'Pixel 8 Pro conectado desde 5.224.x.x', en: 'Pixel 8 Pro connected from 5.224.x.x' }) },
    time: '38s ago', ts: alertNow - 38, read: true, routerId: 'flint2',
  },
  {
    id: 'alert-backup-adguard', category: 'system', urgent: false, severity: 'ok',
    get title() { return demoText({ es: 'Copia de AdGuard completada', en: 'AdGuard backup completed' }) },
    get description() { return demoText({ es: 'Configuración y listas respaldadas en el NAS', en: 'Configuration and lists backed up to the NAS' }) },
    time: '1 day ago', ts: alertNow - 24 * 3600, read: true, routerId: 'flint2',
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
