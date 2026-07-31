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
  AlertEvent,
  AlertQuery,
  Device,
  DeviceQuery,
  DeviceTotals,
  HealthScore,
  OverviewBundle,
  Paged,
  Router,
  TimeRange,
  TrafficPoint,
  WanInfo,
  WireGuardStats,
} from '@/data/types'
import {
  adguard as mockAdGuard,
  alerts as mockAlerts,
  devices as mockDevices,
  deviceTotals as mockDeviceTotals,
  healthScore as mockHealth,
  routers as mockRouters,
  trafficByRange as mockTraffic,
  wan as mockWan,
  wireguard as mockWireguard,
} from '@/data/mock'
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
  topDevices: Device[]
  alerts: AlertEvent[]
  unreadAlerts: number
}

export interface NetPulseApi extends NetPulseData {
  connectionStatus: ConnectionStatus
  /** true = sin backend: dataset local del mockup con tick simulado */
  isDemo: boolean
  /** Re-pide `/api/overview` (live); en demo es no-op */
  refresh: () => void
  getRouterDetail: (id: string) => Promise<RouterDetailData | null>
  getDevices: (params?: DeviceQuery) => Promise<Paged<Device>>
  getAlerts: (params?: AlertQuery) => Promise<Paged<AlertEvent>>
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
    topDevices: [...mockDevices].sort((a, b) => b.trafficMbps - a.trafficMbps).slice(0, 5),
    alerts: mockAlerts,
    unreadAlerts: mockAlerts.filter((a) => !a.read).length,
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
    topDevices: [],
    alerts: [],
    unreadAlerts: 0,
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

const BACKOFF_MS = [2000, 5000, 15000]
const POLL_MS = 15000
const DEMO_TICK_MS = 3000

// ---------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------

const NetPulseContext = createContext<NetPulseApi | null>(null)

export function DataProvider({ children }: { children: React.ReactNode }) {
  const [bundle, setBundle] = useState<NetPulseData>(initialBundle)
  const [connectionStatus, setConnectionStatus] = useState<ConnectionStatus>('demo')
  const [isDemo, setIsDemo] = useState(false)

  // Refs para closures estables (getters async con la misma firma en ambos modos)
  const bundleRef = useRef(bundle)
  bundleRef.current = bundle
  const modeRef = useRef<'boot' | 'live' | 'demo'>('boot')

  const applyOverview = useCallback((o: OverviewBundle) => {
    setBundle((prev) => ({
      ...prev,
      health: o.health,
      wan: o.wan,
      traffic: o.traffic,
      adguard: o.adguard,
      wireguard: o.wireguard,
      routers: o.routers,
      deviceTotals: o.deviceTotals,
      topDevices: o.topDevices,
      alerts: o.alerts,
      unreadAlerts: o.unreadAlerts,
    }))
  }, [])

  // -- boot ------------------------------------------------------------------
  useEffect(() => {
    let disposed = false
    let es: EventSource | null = null
    let pollId: number | undefined
    let reconnectId: number | undefined
    let tickId: number | undefined
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

    const startLive = async () => {
      modeRef.current = 'live'
      setIsDemo(false)
      setBundle(emptyBundle()) // live: cero datos del mockup, solo backend
      const ok = await fetchOverview()
      await fetchDevices()
      if (disposed) return
      if (ok) setConnectionStatus('connected')
      else {
        setConnectionStatus('reconnecting')
        pollId = window.setInterval(() => void fetchOverview(), POLL_MS)
      }
      startSse()
    }

    const startDemo = () => {
      modeRef.current = 'demo'
      setIsDemo(true)
      setConnectionStatus('demo')
      tickId = window.setInterval(() => {
        if (!disposed) setBundle((prev) => demoTick(prev))
      }, DEMO_TICK_MS)
    }

    void (async () => {
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
    const { severity, page = 1, pageSize = 20 } = params
    if (modeRef.current === 'live') {
      const res = await fetch(`/api/alerts${toQuery({ severity, page, pageSize })}`)
      if (res.status === 401) redirectLogin()
      if (!res.ok) throw new Error(`GET /api/alerts → ${res.status}`)
      return (await res.json()) as Paged<AlertEvent>
    }
    let items = bundleRef.current.alerts
    if (severity) items = items.filter((a) => a.severity === severity)
    const start = (page - 1) * pageSize
    return { items: items.slice(start, start + pageSize), total: items.length, page, pageSize }
  }, [])

  const value = useMemo<NetPulseApi>(
    () => ({
      ...bundle,
      connectionStatus,
      isDemo,
      refresh,
      getRouterDetail,
      getDevices,
      getAlerts,
    }),
    [bundle, connectionStatus, isDemo, refresh, getRouterDetail, getDevices, getAlerts],
  )

  return <NetPulseContext.Provider value={value}>{children}</NetPulseContext.Provider>
}

export function useNetPulse(): NetPulseApi {
  const ctx = useContext(NetPulseContext)
  if (!ctx) throw new Error('useNetPulse debe usarse dentro de <DataProvider>')
  return ctx
}
