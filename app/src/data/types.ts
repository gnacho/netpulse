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
  /**
   * Backhaul del AP (C1): 'wifi' = uplink inalámbrico (se dibuja dashed
   * ámbar); 'cable'/ausente = cableado o desconocido.
   */
  backhaul?: 'cable' | 'wifi'
  /**
   * El AP se anuncia por LLDP en el puerto del uplink (C2): el frontend
   * añade el sufijo "· LLDP" a la etiqueta del enlace. Ausente = sin dato.
   */
  lldp?: LldpInfo | null
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
  | 'switch'
  | 'desconocido'

export type Band = '5 GHz' | '2.4 GHz' | 'cable' | '—'

/** Identificación LLDP de un vecino (switch gestionado, AP, host…). */
export interface LldpInfo {
  chassis?: string
  mgmt?: string
  caps?: string
  portDesc?: string
}

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
  /** Puerto físico del router/switch donde se aprende la MAC (cableados; FDB). */
  port?: string | null
  /** Datos LLDP cuando el vecino se anuncia (switch gestionado identificado). */
  lldp?: LldpInfo | null
  /**
   * De qué hub cuelga en el mapa de topología: id de un router (defecto,
   * = routerId), de un DistributionNode (switch inferido) o de otro Device
   * (hipervisor / switch gestionado identificado).
   */
  attachTo?: string
}

/**
 * Nodo de distribución entre un router y varios cableados, inferido del FDB:
 * un puerto físico con varias MACs aprendidas. `inferred` = OUI heterogéneo
 * (switch o bridge desconocido, sin IP); `hypervisor` = OUI de hipervisor
 * (Proxmox/VMware/Hyper-V/KVM) → sus CTs/VMs se anidan bajo el host;
 * `managed` = switch gestionado identificado vía LLDP (tiene IP de mgmt).
 */
export interface DistributionNode {
  id: string
  kind: 'inferred' | 'hypervisor' | 'managed'
  routerId: string
  /** Puerto físico del router donde cuelga (lan3…). */
  port: string
  /** MACs aprendidas en ese puerto (FDB). */
  macCount: number
  /** Hypervisor: id del Device host (Proxmox…), si existe como cliente. */
  hostDeviceId?: string
  /** Nombre descriptivo (host o chasis LLDP del switch gestionado). */
  name?: string
  /** Managed: IP de gestión anunciada por LLDP. */
  ip?: string
  lldp?: LldpInfo | null
  /**
   * Managed: chassis-MAC del switch (D1). El switch gestionado existe a la
   * vez como Device (visible en /devices); el mapa excluye su chip filtrando
   * los Devices cuya MAC coincide con esta — se representa SOLO como nodo.
   */
  mac?: string
}

export type AlertSeverity = 'warn' | 'critical' | 'info' | 'ok'

/** Taxonomía de categorías de alerta (SPEC-ALERTAS §1, espejo del backend Go). */
export type AlertCategory = 'router' | 'internet' | 'clients' | 'signal' | 'vpn' | 'system'

/** Nivel de configuración por categoría (SPEC-ALERTAS §2). */
export type AlertConfigLevel = 'urgent' | 'all' | 'none'

/** Configuración de alertas: las 6 categorías con su nivel. */
export type AlertsConfig = Record<AlertCategory, AlertConfigLevel>

export interface AlertEvent {
  id: string
  /** Categoría del evento (filtrado + configuración por nivel) */
  category: AlertCategory
  /** true = "rompe silencio" (badge URGENTE; push en Bloque C) */
  urgent: boolean
  severity: AlertSeverity
  title: string
  description: string
  /** LEGADO display: "hace 12 min" — fallback si `ts` no es válido */
  time: string
  /** Unix SEGUNDOS; el frontend calcula el tiempo relativo */
  ts: number
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
  /**
   * Nodos de distribución inferidos del FDB (switches/hipervisores entre un
   * router y sus cableados). Ausente/vacío si aún no hay datos FDB (live sin
   * colector o primera pasada): el mapa cuelga los cableados del router.
   */
  distributionNodes?: DistributionNode[]
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

/** Parámetros de `GET /api/alerts?severity=&category=&unread=&page=&pageSize=`. */
export interface AlertQuery {
  severity?: AlertSeverity
  category?: AlertCategory
  unread?: boolean
  page?: number
  pageSize?: number
}
