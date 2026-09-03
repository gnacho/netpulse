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
  /** Pill de rol: "Principal" | "AP" | "SW" */
  roleBadge: 'Principal' | 'AP' | 'SW'
  ip: string
  /** MAC del bridge br-lan (live) */
  mac?: string
  /** Descripción del firmware (live, system board) */
  firmware?: string
  /** Versión objetivo configurada por el admin (issue #241). Ausente si no se configura. */
  firmwareTarget?: string
  /** true = el firmware instalado no cumple el target configurado (live). */
  firmwareOutdated?: boolean
  /** Router configurado para funcionar solo con agente (sin SSH). */
  agentOnly?: boolean
  /** Tipo de dispositivo: "glinet"|"openwrt"|"managed-switch"|"external". */
  type?: string
  status: Status
  /** Salud 0–100 */
  health: number
  /**
   * #441: false cuando la fuente del router no puede reportar métricas de
   * sistema (switch SNMP o pusher beacon/scraper). En ese caso cpu/ram/temp
   * llegan a null y la UI no los pinta. Ausente = disponibles.
   */
  vitalsAvailable?: boolean
  cpu: number | null // %
  ram: number | null // %
  temp: number | null // °C
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
  /** Conexión WAN real (issue #276), solo live: protocolo, gateway, DNS. */
  proto?: string
  gateway?: string
  dns?: string[]
  /** Velocidad contratada declarada por el admin (Mbps, issue #151). Ausente si no configurada. */
  contractDownMbps?: number
  contractUpMbps?: number
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
  subscores?: { key: string; label: string; score: number }[]
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

/**
 * Rol de infraestructura sellado server-side (SPEC-65 D65-2):
 * "hypervisor" (host Proxmox/VMware/…), "ct" (CT/VM anidado bajo un
 * hipervisor), "managed-switch" (switch con gestión identificado por LLDP).
 */
export type DeviceInfra = 'hypervisor' | 'ct' | 'managed-switch'

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
  /**
   * Nombre amigable del puerto definido en LuCI (issue #258, `config
   * switchvlan 'port_labels'` en /etc/config/luci). La app lo muestra como
   * nombre preferente sobre `port` cuando existe.
   */
  portLabel?: string
  /** Datos LLDP cuando el vecino se anuncia (switch gestionado identificado). */
  lldp?: LldpInfo | null
  /**
   * De qué hub cuelga en el mapa de topología: id de un router (defecto,
   * = routerId), de un DistributionNode (switch inferido) o de otro Device
   * (hipervisor / switch gestionado identificado).
   */
  attachTo?: string
  /**
   * Rol de infraestructura sellado server-side (SPEC-65 D65-2): la app NO
   * infiere; pinta el badge si viene. Ausente = dispositivo normal (la app
   * puede seguir infiriendo como fallback para datos viejos, B2).
   */
  infra?: DeviceInfra
  /** Segundos restantes del lease DHCP (issue #437). */
  leaseRemaining?: number | null
  /** Nombre del icono manual elegido por el usuario (issue #437). */
  iconOverride?: string
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
  /**
   * id de otro DistributionNode del que cuelga este switch en una cadena
   * LLDP switch→switch (issue #300). Ausente = cuelga del router (`routerId`).
   */
  parent?: string
  /** Nombre amigable del puerto definido en LuCI (issue #258); preferente sobre `port`. */
  portLabel?: string
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
  /** Sugerencia accionable por tipo de alerta (issue #310). Ausente si no aplica. */
  hint?: string
  /** LEGADO display: "hace 12 min" — fallback si `ts` no es válido */
  time: string
  /** Unix SEGUNDOS; el frontend calcula el tiempo relativo */
  ts: number
  read: boolean
  routerId?: string
  /** Router padre que suprime esta alerta (issue #332). Ausente si no aplica. */
  suppressedBy?: string
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

/**
 * Versión del view-model que la app soporta (SPEC-65 D65-4). Si el servidor
 * manda `vm` mayor → la app avisa una vez por consola y sigue (nunca rompe).
 */
export const VM_SUPPORTED = 1

export interface VlanEntry {
  id: number
  tagged: boolean
  pvid: boolean
}

export interface VlanPort {
  port: string
  vlans: VlanEntry[]
}

/** Enlace semántico del mapa (SPEC-65 D65-3), sin geometría. */
export interface TopoSemLink {
  /** id: router | device | distnode | "internet" | "peer-<wgPeerId>" */
  from: string
  to: string
  kind: 'wan' | 'uplink' | 'wired' | 'dist' | 'wg'
  /** puerto físico si aplica */
  port?: string
}

/**
 * Override manual de topología (issue #142, Fase A/B): capa 2 sobre el
 * autodiscover aplicada server-side. El admin etiqueta hardware sin tocar
 * la inferencia automática.
 *   - kind 'hypervisor': el host es un hipervisor; los CTs del mismo puerto
 *     se anidan bajo él.
 *   - kind 'switch': el equipo es un switch gestionado (aunque sin LLDP).
 *   - kind 'attach': el dispositivo cuelga de `parent` (VM con MAC random).
 */
export type TopologyOverrideKind = 'hypervisor' | 'switch' | 'attach'

export interface TopologyOverride {
  /** id = mac normalizada */
  id: string
  mac: string
  kind: TopologyOverrideKind
  name?: string
  parent?: string
  enabled: boolean
  createdAt: number
  updatedAt: number
}

/**
 * Modelo SEMÁNTICO de la topología (SPEC-65 D65-3): asignaciones de anillo,
 * enlaces y conteos de peers ocultos llegan calculados del servidor; la app
 * conserva solo la geometría de píxeles. Ausente en snapshots viejos → la
 * app usa su cálculo local como fallback.
 */
export interface TopoSemantics {
  links: TopoSemLink[]
  /** routerId → ids de Device en su anillo (cableados primero, luego 5/2.4 GHz) */
  rings: Record<string, string[]>
  /** routerId → nº de clientes no pintados como chip (el "+N") */
  hiddenPeers?: Record<string, number>
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
  /**
   * Versión del view-model (SPEC-65 D65-4). Siempre presente en servidores
   * nuevos; ausente en servidores viejos (se asume VM_SUPPORTED).
   */
  vm?: number
  /** Semántica de topología precalculada (SPEC-65 D65-3); ausente = fallback local. */
  topology?: TopoSemantics
  /** Disponibilidad de usteer (roaming/band-steering); ausente = no mostrar /roaming. */
  usteer?: { available: boolean }
  /** Daemon de roaming detectado: none | dawn | usteer | both (#428). */
  roamingDaemon?: 'none' | 'dawn' | 'usteer' | 'both'
  /** true si algún router reporta DAWN; la UI avisa de su deprecación (#426). */
  dawnDeprecated?: boolean
  /** Menú de orquestación activado por el admin (#121); ausente = oculto. */
  orchestration?: boolean
  /** Devices from the same polled snapshot as topology.rings. When present,
   *  the frontend uses this instead of the separate /api/devices fetch
   *  to guarantee ID consistency with topology rings. */
  devices?: Device[]
  ts: number
}

/**
 * Item de `GET /api/agents` (Fase 3): agente nativo registrado en un router.
 * `slug` coincide con el `id` del router (mismo alfabeto que routerstore).
 * `fresh` = el agente empujó dentro del TTL (~90 s); false → caído, el
 * backend vuelve a sondear por SSH.
 */
export interface AgentInfo {
  slug: string
  /** id del router asociado. Puede no coincidir con `slug` (#282). */
  routerId?: string
  /** Hostname del board del último payload: tercera clave de emparejamiento
   *  para routers legacy cuyo id de overview difiere del id de tabla (#483). */
  hostname?: string
  /** Unix SEGUNDOS del último push; null si nunca empujó */
  lastSeen: number | null
  /** Versión del binario netpulse-agent ("" si aún no se conoce) */
  version?: string
  /** "native" (agente netpulse-agent) o "external" (pusher tipo scraper, #285/#288) */
  kind?: 'native' | 'external' | 'netgrip'
  /** Cadencia de push declarada en segundos (solo externos, #288) */
  interval?: number
  fresh: boolean
  /**
   * true si el agente reporta una versión distinta de la del binario embebido
   * (Fase 6.3, issue #243): hay upgrade disponible vía POST /api/agents/{slug}/upgrade.
   */
  updateAvailable?: boolean
  /**
   * Progreso en vivo del self-update (#284): último paso reportado por el
   * agente (o sembrado por el server al enviar el comando). Ausente si no
   * hay actividad reciente.
   */
  upgrade?: AgentUpgradeProgress
}

/** Paso de progreso del self-update de un agente (GET /api/agents, #284). */
export interface AgentUpgradeProgress {
  /** requested | downloading | verifying | swapping | restarting | done | failed | queued */
  step: 'requested' | 'downloading' | 'verifying' | 'swapping' | 'restarting' | 'done' | 'failed' | 'queued'
  /** 0-100, solo en "downloading" */
  pct?: number
  /** mensaje de error, solo en "failed" */
  error?: string
  /** Unix SEGUNDOS del último reporte */
  ts: number
  /** Historia de pasos recorridos con sus timestamps (timeline, #284). */
  steps?: { step: AgentUpgradeProgress['step']; pct?: number; ts: number }[]
}

/** Respuesta paginada (`GET /api/devices`, `GET /api/alerts`). */
export interface Paged<T> {
  items: T[]
  total: number
  page: number
  pageSize: number
  /** Solo `GET /api/devices`: inventario agrupado por tipo con conteos. */
  typeCounts?: Record<string, number>
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

export interface ReanchorRecommendation {
  mac: string
  currentBssid: string
  currentHostname: string
  currentIface: string
  currentSignal: number
  recommendedBssid: string
  recommendedHostname: string
  recommendedIface: string
  recommendedSignal: number
  deltaDbm: number
}

export interface ReanchorResponse {
  daemon: 'none' | 'dawn' | 'usteer' | 'both'
  recommendations: ReanchorRecommendation[]
}
