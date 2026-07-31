/**
 * NetPulse — Tipos de dominio (canon design.md §11 / api-contract.md).
 * Son la forma JSON exacta que sirve el backend (`/api/*`): mismos nombres
 * de campo en camelCase, sin capa de mapeo. `mock.ts` re-exporta estos tipos
 * para no romper imports existentes.
 */

// ---------------------------------------------------------------------------
// Tipos base
// ---------------------------------------------------------------------------

export type Status = 'online' | 'warn' | 'offline'
export type TimeRange = '1h' | '24h' | '7d' | '30d'

export interface Router {
  id: string
  /** Nombre corto (UI): "Gateway", "Salón"… */
  name: string
  /** Modelo completo: "GL.iNet Flint 2 (GL-MT6000)" */
  model: string
  /** Modelo corto para captions: "GL.iNet Flint 2" */
  modelShort: string
  role: string
  /** Pill de rol: "Principal" | "AP" */
  roleBadge: 'Principal' | 'AP'
  ip: string
  /** MAC del bridge br-lan (live) */
  mac?: string
  /** Descripción del firmware (live, system board) */
  firmware?: string
  status: Status
  /** Salud 0–100 */
  health: number
  cpu: number // %
  ram: number // %
  temp: number // °C
  uptime: string
  clients: number
  /** Métrica en umbral (se pinta --warn), p. ej. 'temp' en Patio */
  hotMetric?: 'cpu' | 'ram' | 'temp'
  /** Sparkline de tráfico 24h (Mbps, 24 puntos) */
  sparkline: number[]
}

export interface WanInfo {
  plan: string
  downMbps: number
  upMbps: number
  latencyMs: number
  lossPct: number
  publicIp: string
  isp: string
  peakTodayMbps: number
  peakTodayTime: string
  avgDownMbps: number
  total24h: string
}

export interface TrafficPoint {
  /** Etiqueta del eje X ("14:00", "Lun", "12 nov"…) */
  t: string
  down: number // Mbps
  up: number // Mbps
}

export interface HealthScore {
  score: number
  label: 'Excelente' | 'Bueno' | 'Atención'
  caption: string
  note: string
  breakdown: { label: string; delta: number }[]
}

export interface AdGuardStats {
  host: string
  port: number
  status: 'active' | 'inactive'
  queries24h: number
  blocked24h: number
  blockedPct: number
  trackersBlocked: number
  dnsLatencyMs: number
  clientsUsing: number
  clientsTotal: number
  topBlocked: { domain: string; count: number }[]
  filterLists: number
  rules: number
}

export type PeerType = 'movil' | 'portatil' | 'tablet' | 'sitio'

export interface WGPeer {
  id: string
  name: string
  type: PeerType
  tunnelIp: string
  active: boolean
  lastHandshake: string
  rx: string
  tx: string
}

export interface WireGuardStats {
  interface: string
  subnet: string
  status: 'active' | 'inactive'
  peers: WGPeer[]
}

export type DeviceType =
  | 'ordenador'
  | 'tv'
  | 'movil'
  | 'portatil'
  | 'consola'
  | 'iot'
  | 'camara'
  | 'altavoz'
  | 'servidor'
  | 'tablet'
  | 'desconocido'

export type Band = '5 GHz' | '2.4 GHz' | 'cable' | '—'

export interface Device {
  id: string
  name: string
  type: DeviceType
  manufacturer: string
  ip: string
  mac: string
  routerId: string
  band: Band
  /** dBm; null si va por cable */
  signalDbm: number | null
  /** Tráfico actual Mbps */
  trafficMbps: number
  online: boolean
  isNew?: boolean
  sparkline: number[]
}

export type AlertSeverity = 'warn' | 'critical' | 'info' | 'ok'

export interface AlertEvent {
  id: string
  severity: AlertSeverity
  title: string
  description: string
  /** Timestamp relativo ya formateado: "hace 12 min" */
  time: string
  read: boolean
  routerId?: string
}

// ---------------------------------------------------------------------------
// Formas del contrato API (api-contract.md)
// ---------------------------------------------------------------------------

/** Shape de `deviceTotals` (mismo que en mock.ts). */
export interface DeviceTotals {
  total: number
  online: number
  knownOffline: number
  newToday: number
}

/** Bundle de `GET /api/overview` y del evento SSE `snapshot`. */
export interface OverviewBundle {
  health: HealthScore
  wan: WanInfo
  traffic: Record<TimeRange, TrafficPoint[]>
  adguard: AdGuardStats
  wireguard: WireGuardStats
  routers: Router[]
  deviceTotals: DeviceTotals
  topDevices: Device[]
  alerts: AlertEvent[]
  unreadAlerts: number
  ts: number
}

/** Respuesta paginada (`GET /api/devices`, `GET /api/alerts`). */
export interface Paged<T> {
  items: T[]
  total: number
  page: number
  pageSize: number
}

/** Parámetros de `GET /api/devices?q=&routerId=&band=&type=&status=&page=&pageSize=`. */
export interface DeviceQuery {
  q?: string
  routerId?: string
  band?: Band
  type?: DeviceType
  status?: 'online' | 'offline'
  page?: number
  pageSize?: number
}

/** Parámetros de `GET /api/alerts?severity=&page=&pageSize=`. */
export interface AlertQuery {
  severity?: AlertSeverity
  page?: number
  pageSize?: number
}
