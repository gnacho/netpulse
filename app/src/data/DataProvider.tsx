/**
 * NetPulse — DataProvider (contrato SÍNCRONO, webapp-stack §Patrón frontend).
 *
 * Los componentes NUNCA ven HTTP: leen el bundle de dominio del contexto
 * (`useNetPulse()`). Internamente:
 * - Boot con el mock canónico → el primer paint es idéntico al mockup.
 * - Probe `GET /api/health` (timeout 2 s):
 *   - OK  → modo live: `GET /api/overview` + SSE `/api/stream`
 *     (`snapshot` reemplaza el bundle, `alert` añade alerta). onerror →
 *     `reconnecting` con backoff 2s→5s→15s y polling de `/api/overview`
 *     cada 15 s hasta reconectar. Cualquier 401 → redirect `/login`.
 *   - Fallo → modo demo: `isDemo=true`, tick local cada 3 s con random walk
 *     suave (cpu/ram/temp ±2, tráfico ±5 %, latencia WAN 6–11 ms).
 */
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react'
import type {
  AdGuardStats,
  AgentInfo,
  AlertCategory,
  AlertConfigLevel,
  AlertEvent,
  AlertQuery,
  AlertsConfig,
  Device,
  DeviceQuery,
  DeviceTotals,
  DistributionNode,
  HealthScore,
  OverviewBundle,
  Paged,
  Router,
  TimeRange,
  TopoSemantics,
  TrafficPoint,
  WanInfo,
  WireGuardStats,
} from '@/data/types'
import { VM_SUPPORTED } from '@/data/types'
import {
  adguard as mockAdGuard,
  alerts as mockAlerts,
  devices as mockDevices,
  deviceTotals as mockDeviceTotals,
  distributionNodes as mockDistributionNodes,
  healthScore as mockHealth,
  routers as mockRouters,
  trafficByRange as mockTraffic,
  wan as mockWan,
  wireguard as mockWireguard,
} from '@/data/mock'
import {
  countUnreadAlerts,
  loadDemoAlertsConfig,
  normalizeAlertsConfig,
  saveDemoAlertsConfig,
} from '@/data/alertConfig'
import { buildClientDevices } from '@/pages/devices-data'
import { getRouterExtras } from '@/components/routers/routerExtras'
import type { RouterExtras } from '@/components/routers/routerExtras'

// ---------------------------------------------------------------------------
// Tipos del contexto
// ---------------------------------------------------------------------------

export type ConnectionStatus = 'connected' | 'reconnecting' | 'demo'

export interface RouterDetailData {
  router: Router
  extras: RouterExtras
  clients: Device[]
  /** Solo en el gateway */
  adguard?: AdGuardStats
  wireguard?: WireGuardStats
}

export interface NetPulseData {
  health: HealthScore
  wan: WanInfo
  traffic: Record<TimeRange, TrafficPoint[]>
  adguard: AdGuardStats
  wireguard: WireGuardStats
  routers: Router[]
  devices: Device[]
  deviceTotals: DeviceTotals
  distributionNodes: DistributionNode[]
  topDevices: Device[]
  alerts: AlertEvent[]
  unreadAlerts: number
  /**
   * Versión del view-model del último overview (SPEC-65 D65-4). En demo y
   * antes del primer fetch vale VM_SUPPORTED (el canon local es vm=1).
   */
  vm: number
  /**
   * Semántica de topología precalculada por el servidor (SPEC-65 D65-3).
   * Ausente en demo y con servidores viejos → model.ts usa su cálculo local.
   */
  topology?: TopoSemantics
  /** Disponibilidad de DAWN (roaming); ausente = no mostrar /roaming. */
  dawn?: { available: boolean }
}

export interface NetPulseApi extends NetPulseData {
  connectionStatus: ConnectionStatus
  /**
   * Agentes nativos registrados (GET /api/agents, Fase 3). El `slug` casa con
   * el `id` del router. En demo: lista vacía (sin badges, comportamiento
   * honesto — no se mockean agentes). Se refresca cada ~30 s en live.
   */
  agents: AgentInfo[]
  /** true = sin backend: dataset local del mockup con tick simulado */
  isDemo: boolean
  /** Re-pide `/api/overview` (live); en demo es no-op */
  refresh: () => void
  /** Date.now() del último snapshot fresco (SSE o fetch de overview); 0 si aún no hubo */
  lastSnapshotAt: number
  /**
   * POST /api/refresh (live): fuerza un sondeo inmediato en el backend; el
   * snapshot fresco llega por SSE. Resuelve `true` si toca esperar snapshot
   * (202 o 429 — el tick de 5 s lo empuja igualmente) y `false` si no hay
   * backend (demo) o falló la petición. En demo es no-op → false.
   */
  requestServerRefresh: () => Promise<boolean>
  getRouterDetail: (id: string) => Promise<RouterDetailData | null>
  getDevices: (params?: DeviceQuery) => Promise<Paged<Device>>
  getAlerts: (params?: AlertQuery) => Promise<Paged<AlertEvent>>
  /**
   * Configuración de alertas por categoría (SPEC-ALERTAS §2). Live: GET/PUT
   * /api/alerts/config (kv en backend). Demo: localStorage (UI demostrable).
   */
  alertsConfig: AlertsConfig
  /** Cambia el nivel de UNA categoría (PUT parcial en live). Resuelve true si persistió. */
  setAlertConfig: (category: AlertCategory, level: AlertConfigLevel) => Promise<boolean>
  /** Read state en SERVIDOR (live): POST /api/alerts/read. Demo: estado local. Optimista. */
  markAlertsRead: (ids: string[]) => void
  /** POST /api/alerts/read-all (live). Demo: estado local. Optimista. */
  markAllAlertsRead: () => void
  /**
   * Fase 5 (Plan B): POST /api/agents/{slug}/rearm — reinicia el servicio
   * procd del agente en el router (vía SSH del servidor) y espera a que
   * vuelva a empujar. Devuelve `{ recovered }` o null si la petición falló
   * (demo siempre null: no hay agentes que rearmar).
   */
  rearmAgent: (slug: string) => Promise<{ recovered: boolean; message?: string } | null>
}

// ---------------------------------------------------------------------------
// Estado inicial = mock canónico (primer paint idéntico al mockup)
// ---------------------------------------------------------------------------

function initialBundle(): NetPulseData {
  return {
    health: mockHealth,
    wan: mockWan,
    traffic: mockTraffic,
    adguard: mockAdGuard,
    wireguard: mockWireguard,
    routers: mockRouters,
    devices: mockDevices,
    deviceTotals: mockDeviceTotals,
    distributionNodes: mockDistributionNodes,
    topDevices: [...mockDevices].sort((a, b) => b.trafficMbps - a.trafficMbps).slice(0, 5),
    alerts: mockAlerts,
    unreadAlerts: mockAlerts.filter((a) => !a.read).length,
    vm: VM_SUPPORTED,
  }
}

/**
 * Bundle VACÍO para el modo live: el primer paint ya es datos reales (los que
 * llegan del backend), nunca el mockup de diseño. Regla: BD/datos 100% vacíos.
 */
function emptyBundle(): NetPulseData {
  return {
    health: { score: 0, label: 'Bueno', caption: '', note: '', breakdown: [] },
    wan: {
      plan: '—',
      downMbps: 0,
      upMbps: 0,
      latencyMs: 0,
      lossPct: 0,
      publicIp: '—',
      isp: '—',
      peakTodayMbps: 0,
      peakTodayTime: '—',
      avgDownMbps: 0,
      total24h: '—',
    },
    traffic: { '1h': [], '24h': [], '7d': [], '30d': [] },
    adguard: {
      host: '',
      port: 0,
      status: 'inactive',
      queries24h: 0,
      blocked24h: 0,
      blockedPct: 0,
      trackersBlocked: 0,
      dnsLatencyMs: 0,
      clientsUsing: 0,
      clientsTotal: 0,
      topBlocked: [],
      filterLists: 0,
      rules: 0,
    },
    wireguard: { interface: '', subnet: '', status: 'inactive', peers: [] },
    routers: [],
    devices: [],
    deviceTotals: { total: 0, online: 0, knownOffline: 0, newToday: 0 },
    distributionNodes: [],
    topDevices: [],
    alerts: [],
    unreadAlerts: 0,
    vm: VM_SUPPORTED,
  }
}

/** Extras de detalle vacíos (live sin datos aún); el mock NO se usa en live. */
export const EMPTY_EXTRAS: RouterExtras = {
  mac: '—',
  firmware: '—',
  firmwareUpdated: true,
  lastReboot: '—',
  soc: '—',
  flash: '—',
  ramMb: 0,
  bandSplit: { band24: 0, band5: 0, cable: 0 },
  trafficNow: 0,
  gatewayLatencySpark: [],
  backhaulSignal: [],
  radios: [],
  ports: [],
  ethPorts: [],
}

// ---------------------------------------------------------------------------
// Utilidades de red / demo
// ---------------------------------------------------------------------------

function redirectLogin(): never {
  window.dispatchEvent(new Event('netpulse-unauthorized'))
  window.location.assign('/login')
  throw new Error('unauthorized')
}

/** Probe del backend: OK solo si responde JSON `{ ok: true }` (evita falsos
 * positivos del fallback SPA de la preview estática, que devuelve HTML 200). */
async function probeBackend(): Promise<boolean> {
  try {
    const res = await fetch('/api/health', { signal: AbortSignal.timeout(2000) })
    if (!res.ok) return false
    if (!(res.headers.get('content-type') ?? '').includes('application/json')) return false
    const data = (await res.json()) as { ok?: boolean }
    return data?.ok === true
  } catch {
    return false
  }
}

const clamp = (v: number, min: number, max: number) => Math.min(max, Math.max(min, v))
const walkInt = (v: number, step: number, min: number, max: number) =>
  clamp(Math.round(v + (Math.random() * 2 - 1) * step), min, max)
const walkPct = (v: number, pct = 5, min = 0) =>
  Math.max(min, Math.round(v * (1 + ((Math.random() * 2 - 1) * pct) / 100) * 10) / 10)

/** Tick del modo demo: random walk suave (design feel "vivo"). */
function demoTick(prev: NetPulseData): NetPulseData {
  const routers = prev.routers.map((r) => ({
    ...r,
    cpu: walkInt(r.cpu, 2, 2, 97),
    ram: walkInt(r.ram, 2, 5, 97),
    temp: walkInt(r.temp, 2, 30, 90),
  }))
  const wan: WanInfo = {
    ...prev.wan,
    downMbps: walkPct(prev.wan.downMbps, 5, 1),
    upMbps: walkPct(prev.wan.upMbps, 5, 0.5),
    latencyMs: 6 + Math.round(Math.random() * 5),
  }
  const traffic = Object.fromEntries(
    (Object.keys(prev.traffic) as TimeRange[]).map((range) => {
      const series = prev.traffic[range]
      if (series.length === 0) return [range, series]
      const last = series[series.length - 1]
      const next = { ...last, down: walkPct(last.down, 5, 1), up: walkPct(last.up, 5, 0.5) }
      return [range, [...series.slice(0, -1), next]]
    }),
  ) as Record<TimeRange, TrafficPoint[]>
  const devices = prev.devices.map((d) =>
    d.online ? { ...d, trafficMbps: walkPct(d.trafficMbps, 5) } : d,
  )
  return {
    ...prev,
    routers,
    wan,
    traffic,
    devices,
    topDevices: [...devices].sort((a, b) => b.trafficMbps - a.trafficMbps).slice(0, 5),
  }
}

function toQuery(params: Record<string, string | number | undefined>): string {
  const q = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== '') q.set(k, String(v))
  }
  const s = q.toString()
  return s ? `?${s}` : ''
}

/** Flag de módulo (B3): el aviso de vm > VM_SUPPORTED sale UNA sola vez. */
let vmVersionWarned = false

const BACKOFF_MS = [2000, 5000, 15000]
const POLL_MS = 15000
const DEMO_TICK_MS = 3000
// Refresco del estado de agentes (fresh cambia solo en el backend, TTL ~90 s)
const AGENTS_POLL_MS = 30000

// ---------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------

const NetPulseContext = createContext<NetPulseApi | null>(null)

export function DataProvider({ children }: { children: React.ReactNode }) {
  const [bundle, setBundle] = useState<NetPulseData>(initialBundle)
  const [connectionStatus, setConnectionStatus] = useState<ConnectionStatus>('demo')
  // isDemo desde el primer render si hay flag de demo (evita que los managers
  // de Ajustes disparen fetches antes del boot y fuercen redirect a /login)
  const [isDemo, setIsDemo] = useState(() => sessionStorage.getItem('netpulse-demo') === '1')
  // Marca temporal del último snapshot fresco (el botón "Refrescar" gira
  // hasta que esto avanza o expira su timeout de seguridad)
  const [lastSnapshotAt, setLastSnapshotAt] = useState(0)
  // Config de alertas: arranca con la demo (localStorage); en live la
  // reemplaza GET /api/alerts/config durante el boot
  const [alertsConfig, setAlertsConfigState] = useState<AlertsConfig>(loadDemoAlertsConfig)
  // Agentes nativos (live); demo = siempre vacío
  const [agents, setAgents] = useState<AgentInfo[]>([])

  // Refs para closures estables (getters async con la misma firma en ambos modos)
  const bundleRef = useRef(bundle)
  bundleRef.current = bundle
  const modeRef = useRef<'boot' | 'live' | 'demo'>('boot')
  // Se sincroniza en applyAlertsConfig y en el fetch del boot (nunca en render)
  const configRef = useRef(alertsConfig)

  const applyOverview = useCallback((o: OverviewBundle) => {
    // SPEC-65 D65-4/D65-9 B3: view-model versionado. Un `vm` mayor que el
    // soportado avisa UNA sola vez por consola y nunca rompe la UI. Servidor
    // viejo sin `vm` → se asume la versión soportada. En demo no aplica
    // (el canon local es vm=1 y no pasa por aquí).
    const vm = o.vm ?? VM_SUPPORTED
    if (vm > VM_SUPPORTED && !vmVersionWarned) {
      vmVersionWarned = true
      console.warn(
        `[NetPulse] El servidor sirve view-model vm=${vm}, pero esta app soporta hasta vm=${VM_SUPPORTED}. ` +
          'La UI sigue funcionando; actualiza la app para ver los campos nuevos.',
      )
    }
    setLastSnapshotAt(Date.now())
    setBundle((prev) => ({
      ...prev,
      health: o.health,
      wan: o.wan,
      traffic: o.traffic,
      adguard: o.adguard,
      wireguard: o.wireguard,
      routers: o.routers,
      deviceTotals: o.deviceTotals,
      distributionNodes: o.distributionNodes ?? [],
      topDevices: o.topDevices,
      alerts: o.alerts,
      unreadAlerts: o.unreadAlerts,
      vm,
      topology: o.topology,
      dawn: o.dawn,
    }))
  }, [])

  // -- boot ------------------------------------------------------------------
  useEffect(() => {
    let disposed = false
    let es: EventSource | null = null
    let pollId: number | undefined
    let reconnectId: number | undefined
    let tickId: number | undefined
    let agentsPollId: number | undefined
    let backoffIdx = 0

    const fetchJson = async (url: string): Promise<Response> => {
      const res = await fetch(url)
      if (res.status === 401) redirectLogin()
      return res
    }

    const fetchOverview = async (): Promise<boolean> => {
      try {
        const res = await fetchJson('/api/overview')
        if (!res.ok) return false
        applyOverview((await res.json()) as OverviewBundle)
        return true
      } catch {
        return false
      }
    }

    const fetchDevices = async (): Promise<void> => {
      try {
        const res = await fetchJson(`/api/devices${toQuery({ page: 1, pageSize: 500 })}`)
        if (!res.ok) return
        const json = (await res.json()) as Paged<Device>
        if (!disposed) setBundle((prev) => ({ ...prev, devices: json.items }))
      } catch {
        /* sin lista completa: se queda la del bundle */
      }
    }

    const stopPolling = () => {
      window.clearInterval(pollId)
      pollId = undefined
    }
    const stopSse = () => {
      es?.close()
      es = null
    }

    const startSse = () => {
      stopSse()
      if (disposed) return
      es = new EventSource('/api/stream')
      es.addEventListener('snapshot', (ev) => {
        try {
          applyOverview(JSON.parse((ev as MessageEvent).data as string) as OverviewBundle)
          backoffIdx = 0
          stopPolling()
          setConnectionStatus('connected')
        } catch {
          /* payload inválido: se ignora */
        }
      })
      es.addEventListener('alert', (ev) => {
        try {
          const alert = JSON.parse((ev as MessageEvent).data as string) as AlertEvent
          setBundle((prev) => ({
            ...prev,
            alerts: [alert, ...prev.alerts],
            unreadAlerts: prev.unreadAlerts + (alert.read ? 0 : 1),
          }))
        } catch {
          /* payload inválido: se ignora */
        }
      })
      es.onopen = () => {
        backoffIdx = 0
        stopPolling()
        setConnectionStatus('connected')
      }
      es.onerror = () => {
        if (disposed) return
        setConnectionStatus('reconnecting')
        stopSse()
        // Polling de respaldo cada 15 s hasta que el SSE reconecte
        if (pollId === undefined) {
          pollId = window.setInterval(() => {
            void fetchOverview()
          }, POLL_MS)
        }
        // Reintento SSE con backoff 2s → 5s → 15s
        reconnectId = window.setTimeout(
          () => {
            backoffIdx = Math.min(backoffIdx + 1, BACKOFF_MS.length - 1)
            startSse()
          },
          BACKOFF_MS[backoffIdx],
        )
      }
    }

    const fetchAlertsConfig = async (): Promise<void> => {
      try {
        const res = await fetchJson('/api/alerts/config')
        if (!res.ok) return
        const cfg = normalizeAlertsConfig(await res.json())
        if (!disposed) {
          configRef.current = cfg
          setAlertsConfigState(cfg)
        }
      } catch {
        /* sin config: se quedan los defaults del SPEC §2 */
      }
    }

    // Agentes nativos (Fase 3): no forman parte del overview; se sondean
    // aparte cada ~30 s. Sin agentes registrados → [] (sin badges).
    const fetchAgents = async (): Promise<void> => {
      try {
        const res = await fetchJson('/api/agents')
        if (!res.ok) return
        const json = (await res.json()) as { agents?: AgentInfo[] } | AgentInfo[]
        const list = Array.isArray(json) ? json : (json.agents ?? [])
        if (!disposed) setAgents(list)
      } catch {
        /* sin agentes: se mantiene el último estado conocido */
      }
    }

    const startLive = async () => {
      modeRef.current = 'live'
      setIsDemo(false)
      setBundle(emptyBundle()) // live: cero datos del mockup, solo backend
      const ok = await fetchOverview()
      await fetchDevices()
      await fetchAlertsConfig()
      await fetchAgents()
      if (disposed) return
      if (ok) setConnectionStatus('connected')
      else {
        setConnectionStatus('reconnecting')
        pollId = window.setInterval(() => void fetchOverview(), POLL_MS)
      }
      agentsPollId = window.setInterval(() => void fetchAgents(), AGENTS_POLL_MS)
      startSse()
    }

    const startDemo = () => {
      modeRef.current = 'demo'
      setIsDemo(true)
      setConnectionStatus('demo')
      // Demo: el badge de no leídas respeta la config guardada (SPEC §2)
      setBundle((prev) => ({ ...prev, unreadAlerts: countUnreadAlerts(prev.alerts, configRef.current) }))
      tickId = window.setInterval(() => {
        if (!disposed) setBundle((prev) => demoTick(prev))
      }, DEMO_TICK_MS)
    }

    void (async () => {
      // Modo demo local (flag del login): dataset simulado sin backend
      if (sessionStorage.getItem('netpulse-demo') === '1') {
        startDemo()
        return
      }
      const hasBackend = await probeBackend()
      if (disposed) return
      if (hasBackend) await startLive()
      else startDemo()
    })()

    return () => {
      disposed = true
      stopSse()
      stopPolling()
      window.clearTimeout(reconnectId)
      window.clearInterval(tickId)
      window.clearInterval(agentsPollId)
    }
  }, [applyOverview])

  // -- API pública --------------------------------------------------------------

  const refresh = useCallback(() => {
    if (modeRef.current !== 'live') return
    void (async () => {
      try {
        const res = await fetch('/api/overview')
        if (res.status === 401) redirectLogin()
        if (res.ok) applyOverview((await res.json()) as OverviewBundle)
      } catch {
        /* el polling/SSE ya reintentará */
      }
    })()
  }, [applyOverview])

  // Sondeo manual (botón "Refrescar" de Topología): el backend sondea ya y
  // empuja el snapshot por SSE; aquí solo se dispara la petición. Un 429
  // (anti-martilleo, min 5 s) también cuenta como "espera": el tick regular
  // de 5 s del poller empujará un snapshot fresco antes del timeout del botón.
  const requestServerRefresh = useCallback(async (): Promise<boolean> => {
    if (modeRef.current !== 'live') return false
    try {
      const res = await fetch('/api/refresh', { method: 'POST' })
      if (res.status === 401) redirectLogin()
      return res.status === 202 || res.status === 429
    } catch {
      return false
    }
  }, [])

  // -- Alertas: config + read state (SPEC-ALERTAS §2/§4) -----------------------

  /** Aplica una config nueva al estado y, en demo, la persiste y recalcula el badge. */
  const applyAlertsConfig = useCallback((cfg: AlertsConfig) => {
    configRef.current = cfg
    setAlertsConfigState(cfg)
    if (modeRef.current !== 'live') {
      saveDemoAlertsConfig(cfg)
      setBundle((prev) => ({ ...prev, unreadAlerts: countUnreadAlerts(prev.alerts, cfg) }))
    }
  }, [])

  const setAlertConfig = useCallback(
    async (category: AlertCategory, level: AlertConfigLevel): Promise<boolean> => {
      const optimistic = { ...configRef.current, [category]: level }
      applyAlertsConfig(optimistic)
      if (modeRef.current !== 'live') return true
      try {
        const res = await fetch('/api/alerts/config', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ [category]: level }),
        })
        if (res.status === 401) redirectLogin()
        if (!res.ok) {
          // Resync con la verdad del servidor (p. ej. 400 por valor inválido)
          const g = await fetch('/api/alerts/config')
          if (g.status === 401) redirectLogin()
          if (g.ok) applyAlertsConfig(normalizeAlertsConfig(await g.json()))
          return false
        }
        applyAlertsConfig(normalizeAlertsConfig(await res.json()))
        refresh() // la lista/badge pueden cambiar con la nueva config
        return true
      } catch {
        return false
      }
    },
    [applyAlertsConfig, refresh],
  )

  /** Marca leídas de forma optimista; en live además POST /api/alerts/read. */
  const markAlertsRead = useCallback((ids: string[]) => {
    if (ids.length === 0) return
    const idSet = new Set(ids)
    setBundle((prev) => {
      const alerts = prev.alerts.map((a) => (idSet.has(a.id) ? { ...a, read: true } : a))
      const unreadAlerts =
        modeRef.current === 'live'
          ? alerts.filter((a) => !a.read).length
          : countUnreadAlerts(alerts, configRef.current)
      return { ...prev, alerts, unreadAlerts }
    })
    if (modeRef.current !== 'live') return
    void (async () => {
      try {
        const res = await fetch('/api/alerts/read', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ ids }),
        })
        if (res.status === 401) redirectLogin()
      } catch {
        /* el próximo snapshot/SSE resincroniza el read state */
      }
    })()
  }, [])

  const markAllAlertsRead = useCallback(() => {
    setBundle((prev) => ({
      ...prev,
      alerts: prev.alerts.map((a) => ({ ...a, read: true })),
      unreadAlerts: 0,
    }))
    if (modeRef.current !== 'live') return
    void (async () => {
      try {
        const res = await fetch('/api/alerts/read-all', { method: 'POST' })
        if (res.status === 401) redirectLogin()
      } catch {
        /* idem: resync por snapshot */
      }
    })()
  }, [])

  /**
   * Fase 5 (Plan B): rearme del servicio del agente en el router. El
   * backend ejecuta `init.d restart` por SSH y espera hasta 30 s el push de
   * vuelta → por eso aquí NO hay timeout corto: se deja respirar. null =
   * petición fallida (red, 4xx/5xx, demo).
   */
  const rearmAgent = useCallback(
    async (slug: string): Promise<{ recovered: boolean; message?: string } | null> => {
      if (modeRef.current !== 'live') return null
      try {
        const res = await fetch(`/api/agents/${encodeURIComponent(slug)}/rearm`, {
          method: 'POST',
          signal: AbortSignal.timeout(60_000),
        })
        if (res.status === 401) redirectLogin()
        if (!res.ok) return null
        const json = (await res.json()) as { recovered?: boolean; message?: string }
        return { recovered: json.recovered ?? false, message: json.message }
      } catch {
        return null
      }
    },
    [],
  )

  const getRouterDetail = useCallback(async (id: string): Promise<RouterDetailData | null> => {
    if (modeRef.current === 'live') {
      const res = await fetch(`/api/routers/${encodeURIComponent(id)}`)
      if (res.status === 401) redirectLogin()
      if (res.status === 404) return null
      if (!res.ok) return null
      const json = (await res.json()) as {
        router: Router
        ports?: RouterExtras['ethPorts']
        radios?: RouterExtras['radios'] | null
        backhaul?: RouterExtras['backhaul'] | null
        clients?: Device[]
        extras?: RouterExtras
        adguard?: AdGuardStats
        wireguard?: WireGuardStats
      }
      // Live: extras SOLO del backend (datos reales); nunca el mock local
      const extras: RouterExtras = {
        ...(json.extras ?? EMPTY_EXTRAS),
        ethPorts: json.ports ?? json.extras?.ethPorts ?? [],
        radios: json.radios ?? json.extras?.radios ?? [],
        backhaul: json.backhaul ?? json.extras?.backhaul,
      }
      return {
        router: json.router,
        extras,
        clients: json.clients ?? [],
        adguard: json.adguard,
        wireguard: json.wireguard,
      }
    }
    // Demo: resolución local desde mock + routerExtras (misma firma async)
    const { routers, devices, adguard, wireguard } = bundleRef.current
    const router = routers.find((r) => r.id === id)
    if (!router) return null
    const isGateway = router.roleBadge === 'Principal'
    return {
      router,
      extras: getRouterExtras(id),
      clients: devices.filter((d) => d.routerId === id),
      adguard: isGateway ? adguard : undefined,
      wireguard: isGateway ? wireguard : undefined,
    }
  }, [])

  const getDevices = useCallback(async (params: DeviceQuery = {}): Promise<Paged<Device>> => {
    const { q, routerId, band, type, status, page = 1, pageSize = 50 } = params
    if (modeRef.current === 'live') {
      const res = await fetch(`/api/devices${toQuery({ q, routerId, band, type, status, page, pageSize })}`)
      if (res.status === 401) redirectLogin()
      if (!res.ok) throw new Error(`GET /api/devices → ${res.status}`)
      return (await res.json()) as Paged<Device>
    }
    // Demo: filtra y pagina el dataset completo del mockup (47 clientes)
    let items = buildClientDevices(bundleRef.current.devices, true) as Device[]
    if (routerId) items = items.filter((d) => d.routerId === routerId)
    if (band) items = items.filter((d) => d.band === band)
    if (type) items = items.filter((d) => d.type === type)
    if (status) items = items.filter((d) => (status === 'online' ? d.online : !d.online))
    if (q) {
      const needle = q.toLowerCase()
      items = items.filter((d) =>
        `${d.name} ${d.ip} ${d.mac} ${d.manufacturer}`.toLowerCase().includes(needle),
      )
    }
    const start = (page - 1) * pageSize
    return { items: items.slice(start, start + pageSize), total: items.length, page, pageSize }
  }, [])

  const getAlerts = useCallback(async (params: AlertQuery = {}): Promise<Paged<AlertEvent>> => {
    const { severity, category, unread, page = 1, pageSize = 20 } = params
    if (modeRef.current === 'live') {
      const res = await fetch(`/api/alerts${toQuery({ severity, category, unread: unread ? 1 : undefined, page, pageSize })}`)
      if (res.status === 401) redirectLogin()
      if (!res.ok) throw new Error(`GET /api/alerts → ${res.status}`)
      return (await res.json()) as Paged<AlertEvent>
    }
    let items = bundleRef.current.alerts
    if (severity) items = items.filter((a) => a.severity === severity)
    if (category) items = items.filter((a) => a.category === category)
    if (unread) items = items.filter((a) => !a.read)
    const start = (page - 1) * pageSize
    return { items: items.slice(start, start + pageSize), total: items.length, page, pageSize }
  }, [])

  const value = useMemo<NetPulseApi>(
    () => ({
      ...bundle,
      connectionStatus,
      agents,
      isDemo,
      refresh,
      lastSnapshotAt,
      requestServerRefresh,
      getRouterDetail,
      getDevices,
      getAlerts,
      alertsConfig,
      setAlertConfig,
      markAlertsRead,
      markAllAlertsRead,
      rearmAgent,
    }),
    [bundle, connectionStatus, agents, isDemo, refresh, lastSnapshotAt, requestServerRefresh, getRouterDetail, getDevices, getAlerts, alertsConfig, setAlertConfig, markAlertsRead, markAllAlertsRead, rearmAgent],
  )

  return <NetPulseContext.Provider value={value}>{children}</NetPulseContext.Provider>
}

export function useNetPulse(): NetPulseApi {
  const ctx = useContext(NetPulseContext)
  if (!ctx) throw new Error('useNetPulse debe usarse dentro de <DataProvider>')
  return ctx
}
