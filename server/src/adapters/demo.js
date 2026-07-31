/**
 * Adapter demo: sirve el dataset canónico (port de mock.ts + routerExtras.ts +
 * devices-data.ts) con un random walk suave en cada tick:
 *   - cpu/ram/temp ±2 máx
 *   - tráfico ±5 %
 *   - latencia WAN acotada 6–11 ms
 *   - bytes WireGuard crecientes (peers activos)
 * El PRIMER snapshot (antes del primer tick) es exactamente el canon.
 */
import {
  routers as canonRouters,
  wan as canonWan,
  trafficByRange,
  healthScore,
  deviceTotals,
  allDevices,
  adguard as canonAdguard,
  wireguard as canonWireguard,
  alerts as canonAlerts,
  routerExtras as canonExtras,
  perfSeries,
  WAN_LATENCY_24H,
  WAN_LATENCY_STATS,
  adguardSeries24h,
  wgPeerExtras,
  WG_TOTALS_30D,
} from '../demo/dataset.js'

const GATEWAY_ID = 'flint2'

// ---------------------------------------------------------------------------
// Bytes WireGuard: parseo/formato ES ("1,2 GB" ⇄ 1.2e9) para el random walk
// ---------------------------------------------------------------------------

const UNIT_MULT = { B: 1, KB: 1e3, MB: 1e6, GB: 1e9, TB: 1e12 }

function parseBytes(str) {
  const m = /^([\d.,]+)\s*(B|KB|MB|GB|TB)$/.exec(str.trim())
  if (!m) return 0
  return Math.round(parseFloat(m[1].replace(/\./g, '').replace(',', '.')) * (UNIT_MULT[m[2]] ?? 1))
}

function fmtBytes(bytes) {
  if (bytes >= 1e9) {
    const v = bytes / 1e9
    return Number.isInteger(v) ? `${v} GB` : `${v.toFixed(1).replace('.', ',')} GB`
  }
  if (bytes >= 1e6) return `${Math.round(bytes / 1e6)} MB`
  if (bytes >= 1e3) return `${Math.round(bytes / 1e3)} KB`
  return `${Math.round(bytes)} B`
}

// ---------------------------------------------------------------------------
// Random walk helpers
// ---------------------------------------------------------------------------

const rnd = (min, max) => min + Math.random() * (max - min)
const clamp = (v, lo, hi) => Math.min(hi, Math.max(lo, v))
/** Entero en ±step del valor anterior, acotado. */
function walkInt(prev, step, lo, hi) {
  return clamp(Math.round(prev + rnd(-step, step)), lo, hi)
}
/** ±pct % del valor anterior (1 decimal), acotado. */
function walkPct(prev, pct, lo, hi) {
  return clamp(+(prev * (1 + rnd(-pct, pct))).toFixed(1), lo, hi)
}

// ---------------------------------------------------------------------------
// Adapter
// ---------------------------------------------------------------------------

export function createDemoAdapter() {
  // Estado mutable: clon profundo del canon (el canon exportado no se toca)
  const state = structuredClone({
    routers: canonRouters,
    wan: canonWan,
    adguard: canonAdguard,
    wireguard: canonWireguard,
    alerts: canonAlerts,
    devices: allDevices,
    extras: canonExtras,
  })
  // Peers WG con contadores numéricos internos (rx/tx se formatean al servir)
  const wgBytes = new Map(
    state.wireguard.peers.map((p) => [p.id, { rx: parseBytes(p.rx), tx: parseBytes(p.tx), active: p.active }]),
  )

  /** Random walk suave de un tick (5 s). */
  function tick() {
    for (const r of state.routers) {
      r.cpu = walkInt(r.cpu, 2, 2, 95)
      r.ram = walkInt(r.ram, 2, 10, 92)
      r.temp = walkInt(r.temp, 2, 30, 82)
      const ex = state.extras[r.id]
      if (ex) ex.trafficNow = walkPct(ex.trafficNow || 0.1, 0.05, 0.05, 950)
    }
    state.wan.downMbps = walkPct(state.wan.downMbps, 0.05, 5, 580)
    state.wan.upMbps = walkPct(state.wan.upMbps, 0.05, 2, 120)
    state.wan.latencyMs = clamp(walkInt(state.wan.latencyMs, 1, 6, 11), 6, 11)
    for (const d of state.devices) {
      if (d.online && d.trafficMbps > 0) {
        d.trafficMbps = +walkPct(d.trafficMbps, 0.05, 0.001, 950).toFixed(2)
      }
    }
    // AdGuard: contadores siempre crecientes
    state.adguard.queries24h += Math.round(rnd(2, 10))
    const newBlocked = Math.round(rnd(0, 2))
    state.adguard.blocked24h += newBlocked
    state.adguard.trackersBlocked += Math.round(rnd(0, newBlocked))
    state.adguard.blockedPct = +((state.adguard.blocked24h / state.adguard.queries24h) * 100).toFixed(1)
    // WireGuard: bytes crecientes solo en peers activos
    for (const [id, b] of wgBytes) {
      if (!b.active) continue
      b.rx += Math.round(rnd(0.2, 1.7) * 1e6)
      b.tx += Math.round(rnd(0.04, 0.4) * 1e6)
      const peer = state.wireguard.peers.find((p) => p.id === id)
      if (peer) {
        peer.rx = fmtBytes(b.rx)
        peer.tx = fmtBytes(b.tx)
      }
    }
  }

  function wireguardSnapshot() {
    return { ...state.wireguard, peers: state.wireguard.peers.map((p) => ({ ...p })) }
  }

  function topDevices(n = 5) {
    return [...state.devices].sort((a, b) => b.trafficMbps - a.trafficMbps).slice(0, n)
  }

  function getOverview() {
    return {
      health: structuredClone(healthScore),
      wan: { ...state.wan },
      traffic: trafficByRange,
      adguard: { ...state.adguard },
      wireguard: wireguardSnapshot(),
      routers: state.routers.map((r) => ({ ...r })),
      deviceTotals: { ...deviceTotals },
      topDevices: topDevices(5),
      alerts: state.alerts.map((a) => ({ ...a })),
      unreadAlerts: state.alerts.filter((a) => !a.read).length,
      ts: Math.floor(Date.now() / 1000),
    }
  }

  function getRouters() {
    return state.routers.map((r) => ({ ...r }))
  }

  function getRouterDetail(id) {
    const router = state.routers.find((r) => r.id === id)
    if (!router) return null
    const extras = state.extras[id] ?? state.extras[GATEWAY_ID]
    const detail = {
      router: { ...router },
      ports: extras.ethPorts,
      radios: extras.radios.length ? extras.radios : null,
      backhaul: extras.backhaul ?? null,
      series: {
        '1h': perfSeries(router, '1h'),
        '24h': perfSeries(router, '24h'),
        '7d': perfSeries(router, '7d'),
      },
      clients: state.devices.filter((d) => d.routerId === id).map((d) => ({ ...d })),
      extras,
    }
    if (id === GATEWAY_ID) {
      detail.adguard = { ...state.adguard }
      detail.wireguard = wireguardSnapshot()
      detail.adguardSeries24h = adguardSeries24h
      detail.wanLatency = { last24h: WAN_LATENCY_24H, stats: WAN_LATENCY_STATS }
      detail.wgPeerExtras = wgPeerExtras
      detail.wgTotals30d = WG_TOTALS_30D
    }
    return detail
  }

  function getDevices() {
    return state.devices.map((d) => ({ ...d }))
  }

  function getAlerts() {
    return state.alerts.map((a) => ({ ...a }))
  }

  /** Filas para la tabla metrics del poller. */
  function getMetricsRows() {
    return state.routers.map((r) => {
      const ex = state.extras[r.id] ?? {}
      const rx = (ex.trafficNow ?? 0) * 1e6
      return {
        router_id: r.id,
        cpu: r.cpu,
        ram: r.ram,
        temp: r.temp,
        latency_ms: r.id === GATEWAY_ID ? state.wan.latencyMs : (ex.gatewayLatencyMs ?? null),
        rx_bps: Math.round(rx),
        tx_bps: Math.round(rx * 0.15),
      }
    })
  }

  function getAdguardRow() {
    return { queries: state.adguard.queries24h, blocked: state.adguard.blocked24h }
  }

  return {
    mode: 'demo',
    tick,
    setRouters() {}, // la demo ignora la configuración de routers
    getOverview,
    getRouters,
    getRouterDetail,
    getDevices,
    getAlerts,
    getMetricsRows,
    getAdguardRow,
    async close() {},
  }
}
