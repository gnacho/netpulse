/**
 * Factoría de adapters + adapter live.
 *
 * - DEMO_MODE=1 → adapter demo (dataset canónico). NO escribe en la BD.
 * - Resto → adapter live: OpenWrtClient por router (ubus HTTP + fallback SSH),
 *   AdGuardClient (HTTP API) y WireGuard vía SSH en el gateway.
 *   La lista de routers vive en SQLite (tabla `routers`) y puede cambiar en
 *   caliente desde Ajustes vía `setRouters()`. Con 0 routers configurados el
 *   overview sale vacío (la UI muestra estados vacíos), nunca demo implícito.
 *
 * Degradación elegante: si un router falla, se marca status 'offline' (con el
 * último dato bueno conocido), se genera una alerta y el resto sigue. El
 * poller NUNCA se cae por un fallo de sondeo.
 */
import { createDemoAdapter } from './demo.js'
import { OpenWrtClient } from './openwrt.js'
import { AdGuardClient } from './adguard.js'
import { getWireGuardStats } from './wireguard.js'

export function createAdapter(config, dbHandle, routers = []) {
  if (config.demoMode) {
    return createDemoAdapter()
  }
  return createLiveAdapter(config, dbHandle, routers)
}

// ---------------------------------------------------------------------------
// Adapter live
// ---------------------------------------------------------------------------

function fmtUptime(sec) {
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  return `${d}d ${h}h`
}

function mbps(bps) {
  return bps == null ? 0 : +(bps / 1e6).toFixed(1)
}

function pickGateway(routers) {
  return routers.find((r) => r.is_gateway) ?? routers.find((r) => r.type === 'glinet') ?? routers[0] ?? null
}

function createLiveAdapter(config, dbHandle, initialRouters) {
  let routers = [...initialRouters]
  let gatewayCfg = pickGateway(routers)
  let clients = new Map()
  const adguardClient = config.adguard ? new AdGuardClient(config.adguard) : null

  function rebuildClients() {
    clients = new Map(
      routers.map((cfg) => [
        cfg.id,
        new OpenWrtClient(cfg, { sshKeyPath: config.sshKeyPath }),
      ]),
    )
  }
  rebuildClients()

  /** Actualiza la lista de routers en caliente (CRUD de Ajustes). */
  function setRouters(list) {
    routers = [...list]
    gatewayCfg = pickGateway(routers)
    rebuildClients()
    // Limpia cachés de routers que ya no existen
    const ids = new Set(routers.map((r) => r.id))
    for (const id of [...lastGood.keys()]) if (!ids.has(id)) lastGood.delete(id)
    for (const id of [...lastStatus.keys()]) if (!ids.has(id)) lastStatus.delete(id)
    for (const id of [...boardCache.keys()]) if (!ids.has(id)) boardCache.delete(id)
    for (const id of [...lastPolled.keys()]) if (!ids.has(id)) lastPolled.delete(id)
    for (const id of [...failCount.keys()]) if (!ids.has(id)) failCount.delete(id)
  }

  // Estado entre ticks
  const lastGood = new Map() // routerId → último Router online
  const lastStatus = new Map() // routerId → 'online' | 'offline'
  const boardCache = new Map() // routerId → system board
  let alerts = [] // AlertEvent[], más recientes primero (máx 100)
  let lastWgPeersActive = new Set()

  function pushAlert(alert) {
    alerts = [alert, ...alerts].slice(0, 100)
    return alert
  }

  /** Consulta métricas históricas del router para sparkline/series. */
  function metricsHistory(routerId, range) {
    if (!dbHandle || !routerId) return []
    const now = Date.now()
    const ranges = {
      '1h': { span: 3600e3, bucket: 180e3, fmt: (d) => `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}` },
      '24h': { span: 86400e3, bucket: 3600e3, fmt: (d) => `${String(d.getHours()).padStart(2, '0')}` },
      '7d': { span: 7 * 86400e3, bucket: 86400e3, fmt: (d) => ['Dom', 'Lun', 'Mar', 'Mié', 'Jue', 'Vie', 'Sáb'][d.getDay()] },
      '30d': { span: 30 * 86400e3, bucket: 86400e3, fmt: (d) => String(d.getDate()) },
    }
    const r = ranges[range]
    if (!r) return []
    const rows = dbHandle.db
      .prepare(
        `SELECT (ts / ?) AS bucket, AVG(rx_bps) AS rx, AVG(tx_bps) AS tx, AVG(cpu) AS cpu, AVG(ram) AS ram, AVG(temp) AS temp, MIN(ts) AS t0
         FROM metrics WHERE router_id = ? AND ts >= ? GROUP BY bucket ORDER BY bucket`,
      )
      .all(r.bucket, routerId, now - r.span)
    return rows.map((row) => ({
      t: r.fmt(new Date(row.t0)),
      down: mbps(row.rx),
      up: mbps(row.tx),
      cpu: Math.round(row.cpu ?? 0),
      ram: Math.round(row.ram ?? 0),
      temp: Math.round(row.temp ?? 0),
    }))
  }

  /** Sondea un router; lanza si está inalcanzable. */
  async function pollRouter(cfg) {
    const client = clients.get(cfg.id)
    // Calienta el ControlMaster SSH en serie: las 7 llamadas en paralelo de
    // abajo multiplexan sobre la conexión ya establecida (en frío, todas
    // compiten por crear el socket y algunas mueren → falso "offline").
    await client.ssh('true')
    const [sysInfo, cpu, temp, net, leases, wireless, ports, radios, fdb] = await Promise.all([
      client.getSystemInfo(),
      client.getCpuPercent().catch(() => null),
      client.getTempC().catch(() => null),
      client.getNetDevBps().catch(() => ({ rxBps: null, txBps: null })),
      client.getDhcpLeases().catch(() => []),
      client.getWirelessClients().catch(() => new Map()),
      client.getEthPorts().catch(() => []),
      client.getRadios().catch(() => []),
      client.getBridgeFdb().catch(() => new Map()),
    ])
    if (!boardCache.has(cfg.id)) {
      boardCache.set(cfg.id, await client.getBoard().catch(() => null))
    }
    const board = boardCache.get(cfg.id)
    const mem = sysInfo?.memory ?? {}
    const ramTotal = (mem.total || 0) + (mem.shared || 0)
    const ramPct = ramTotal > 0 ? Math.round(((ramTotal - (mem.free || 0) - (mem.buffered || 0)) / ramTotal) * 100) : null
    const isGw = cfg.id === gatewayCfg?.id
    const latency = isGw
      ? await client.getWanLatency().catch(() => ({ latencyMs: null, lossPct: null }))
      : { latencyMs: await client.getGatewayLatency(gatewayCfg?.host).catch(() => null), lossPct: null }

    return {
      cfg,
      client,
      sysInfo,
      board,
      cpu: cpu ?? 0,
      ram: ramPct ?? 0,
      temp: temp ?? 0,
      uptimeSec: sysInfo?.uptime ?? 0,
      net,
      leases,
      wireless,
      ports,
      radios,
      fdb,
      latency,
    }
  }

  /** Construye el objeto Router del contrato a partir del sondeo. */
  function buildRouter(polled, history) {
    const { cfg } = polled
    const model = polled.board?.model || cfg.name || cfg.host
    const temp = polled.temp
    const health = Math.max(
      0,
      100 - (polled.cpu > 85 ? 20 : polled.cpu > 70 ? 10 : 0) - (temp > 75 ? 25 : temp > 65 ? 12 : 0) - (polled.ram > 90 ? 15 : 0),
    )
    return {
      id: cfg.id,
      name: cfg.name || cfg.host,
      model,
      modelShort: model,
      role: cfg.id === gatewayCfg?.id ? 'Gateway principal' : 'Punto de acceso',
      roleBadge: cfg.id === gatewayCfg?.id ? 'Principal' : 'AP',
      ip: cfg.host,
      status: temp > 65 || polled.cpu > 85 ? 'warn' : 'online',
      health,
      cpu: polled.cpu,
      ram: polled.ram,
      temp,
      uptime: fmtUptime(polled.uptimeSec),
      clients: polled.leases.length,
      ...(temp > 65 ? { hotMetric: 'temp' } : {}),
      sparkline: history.map((p) => p.down),
    }
  }

  function offlineRouter(cfg) {
    const prev = lastGood.get(cfg.id)
    return {
      ...(prev ?? {
        id: cfg.id,
        name: cfg.name || cfg.host,
        model: cfg.type === 'glinet' ? 'GL.iNet' : 'OpenWrt',
        modelShort: cfg.type === 'glinet' ? 'GL.iNet' : 'OpenWrt',
        role: cfg.id === gatewayCfg?.id ? 'Gateway principal' : 'Punto de acceso',
        roleBadge: cfg.id === gatewayCfg?.id ? 'Principal' : 'AP',
        ip: cfg.host,
        health: 0,
        cpu: 0,
        ram: 0,
        temp: 0,
        uptime: '—',
        clients: 0,
        sparkline: [],
      }),
      status: 'offline',
    }
  }

  /** Cache de sondeos del último tick (para detalle/persistencia). */
  let lastPolled = new Map()
  const failCount = new Map() // routerId → fallos consecutivos

  async function pollAll() {
    const results = await Promise.allSettled(routers.map((cfg) => pollRouter(cfg)))
    const polled = new Map()
    results.forEach((res, i) => {
      const cfg = routers[i]
      if (res.status === 'fulfilled') {
        polled.set(cfg.id, res.value)
        failCount.set(cfg.id, 0)
        lastStatus.set(cfg.id, 'online')
      } else {
        const fails = (failCount.get(cfg.id) ?? 0) + 1
        failCount.set(cfg.id, fails)
        console.warn(`[netpulse] router ${cfg.id} inalcanzable (${fails}): ${res.reason?.message}`)
        // Alerta solo tras 2 fallos seguidos: un fallo suelto (SSH frío,
        // red ocupada) no es una caída real del router
        if (fails >= 2 && lastStatus.get(cfg.id) !== 'offline') {
          pushAlert({
            id: `alert-offline-${cfg.id}-${Date.now()}`,
            severity: 'critical',
            title: `${cfg.name || cfg.host} offline`,
            description: `Sin respuesta de ${cfg.host}: ${res.reason?.message ?? 'error desconocido'}`,
            time: 'ahora mismo',
            read: false,
            routerId: cfg.id,
          })
        }
        if (fails >= 2) lastStatus.set(cfg.id, 'offline')
      }
    })
    lastPolled = polled
    return polled
  }

  async function pollAdGuard() {
    if (!adguardClient) return null
    try {
      return await adguardClient.getStats()
    } catch (err) {
      console.warn(`[netpulse] AdGuard inalcanzable: ${err.message}`)
      return {
        host: new URL(config.adguard.url).hostname,
        port: Number(new URL(config.adguard.url).port || 80),
        status: 'inactive',
        queries24h: 0, blocked24h: 0, blockedPct: 0, trackersBlocked: 0,
        dnsLatencyMs: 0, clientsUsing: 0, clientsTotal: 0,
        topBlocked: [], filterLists: 0, rules: 0,
      }
    }
  }

  async function pollWireGuard(devices) {
    if (!gatewayCfg) return null
    try {
      const stats = await getWireGuardStats({
        host: gatewayCfg?.host,
        sshKeyPath: config.sshKeyPath,
        iface: config.wgInterface,
        subnet: '',
        peerNames: Object.fromEntries(
          devices.filter((d) => d.ip).map((d) => [d.ip, { id: d.id, name: d.name, type: d.type }]),
        ),
      })
      // Alerta en handshake nuevo (peer pasa a activo)
      const activeNow = new Set(stats.peers.filter((p) => p.active).map((p) => p.id))
      for (const id of activeNow) {
        if (!lastWgPeersActive.has(id) && lastWgPeersActive.size > 0) {
          const peer = stats.peers.find((p) => p.id === id)
          pushAlert({
            id: `alert-wg-${id}-${Date.now()}`,
            severity: 'info',
            title: 'Handshake WireGuard',
            description: `${peer?.name ?? id} conectado`,
            time: 'ahora mismo',
            read: false,
            routerId: gatewayCfg?.id,
          })
        }
      }
      lastWgPeersActive = activeNow
      return stats
    } catch (err) {
      console.warn(`[netpulse] WireGuard no disponible: ${err.message}`)
      return { interface: config.wgInterface, subnet: '', status: 'inactive', peers: [] }
    }
  }

  /**
   * Dispositivos = unión de leases DHCP (nombre/IP) y MACs VISTAS por cada
   * router (assoclist WiFi + FDB del bridge). El routerId es el router que
   * realmente tiene asociado/conectado el dispositivo; si nadie lo ve pero
   * tiene lease, se atribuye al gateway.
   */
  function buildDevices(polled) {
    const leasesByMac = new Map()
    for (const [, p] of polled) {
      for (const l of p.leases ?? []) if (l.mac) leasesByMac.set(l.mac, l)
    }
    const seen = new Map() // mac → { routerId, band, signalDbm }
    for (const [routerId, p] of polled) {
      for (const [mac, w] of p.wireless ?? new Map()) {
        seen.set(mac, { routerId, band: w.band, signalDbm: w.signalDbm })
      }
      for (const [mac, port] of p.fdb ?? new Map()) {
        if (!seen.has(mac)) seen.set(mac, { routerId, band: 'cable', signalDbm: null, port })
      }
    }
    const allMacs = new Set([...leasesByMac.keys(), ...seen.keys()])
    const devices = []
    for (const mac of allMacs) {
      const lease = leasesByMac.get(mac)
      const s = seen.get(mac)
      devices.push({
        id: mac.toLowerCase().replace(/:/g, '-'),
        name: lease?.hostname || mac,
        type: 'desconocido',
        manufacturer: 'Desconocido',
        ip: lease?.ip ?? '',
        mac,
        routerId: s?.routerId ?? gatewayCfg?.id,
        band: s?.band ?? 'cable',
        signalDbm: s?.signalDbm ?? null,
        trafficMbps: 0,
        online: Boolean(s) || Boolean(lease),
        sparkline: [],
      })
    }
    return devices
  }

  function computeHealth(routers, adguard) {
    let score = 100
    const breakdown = []
    for (const r of routers) {
      if (r.status === 'offline') {
        score -= 30
        breakdown.push({ label: `${r.name} offline`, delta: -30 })
      } else if (r.temp > 65) {
        score -= 8
        breakdown.push({ label: `temp. ${r.name}`, delta: -8 })
      }
    }
    if (adguard && adguard.status !== 'active') {
      score -= 5
      breakdown.push({ label: 'AdGuard inactivo', delta: -5 })
    }
    score = Math.max(0, score)
    return {
      score,
      label: score >= 85 ? 'Excelente' : score >= 65 ? 'Bueno' : 'Atención',
      caption: 'Puntuación de salud de la red',
      note: breakdown.length ? `Penalizado por: ${breakdown.map((b) => b.label).join(', ')}.` : 'Sin penalizaciones.',
      breakdown,
    }
  }

  function defaultWan(gw) {
    const p = gw ? lastPolled.get(gw.id) : null
    return {
      plan: '—',
      downMbps: mbps(p?.net?.rxBps),
      upMbps: mbps(p?.net?.txBps),
      latencyMs: p?.latency?.latencyMs ?? 0,
      lossPct: p?.latency?.lossPct ?? 0,
      publicIp: '—',
      isp: '—',
      peakTodayMbps: 0,
      peakTodayTime: '—',
      avgDownMbps: 0,
      total24h: '—',
    }
  }

  async function getOverview() {
    const polled = await pollAll()
    const routerList = routers.map((cfg) => {
      const p = polled.get(cfg.id)
      if (!p) {
        // Fallo transitorio (1 tick): muestra el último dato bueno tal cual
        const prev = lastGood.get(cfg.id)
        if (prev && (failCount.get(cfg.id) ?? 0) < 2) return prev
        return offlineRouter(cfg)
      }
      const router = buildRouter(p, metricsHistory(cfg.id, '24h'))
      lastGood.set(cfg.id, router)
      if (router.temp > 65 && lastStatus.get(`${cfg.id}:temp`) !== 'warn') {
        lastStatus.set(`${cfg.id}:temp`, 'warn')
        pushAlert({
          id: `alert-temp-${cfg.id}-${Date.now()}`,
          severity: 'warn',
          title: `Temperatura alta en ${router.name}`,
          description: `${router.temp} °C, por encima del umbral (65 °C)`,
          time: 'ahora mismo',
          read: false,
          routerId: cfg.id,
        })
      }
      return router
    })
    const devices = buildDevices(polled)
    const [adguard, wireguard] = await Promise.all([pollAdGuard(), pollWireGuard(devices)])
    const gw = routerList.find((r) => r.id === gatewayCfg?.id)
    const wan = defaultWan(gatewayCfg)
    if (gw?.status === 'offline') {
      wan.publicIp = '—'
    }
    return {
      health: computeHealth(routerList, adguard),
      wan,
      traffic: {
        '1h': metricsHistory(gatewayCfg?.id, '1h').map(({ t, down, up }) => ({ t, down, up })),
        '24h': metricsHistory(gatewayCfg?.id, '24h').map(({ t, down, up }) => ({ t, down, up })),
        '7d': metricsHistory(gatewayCfg?.id, '7d').map(({ t, down, up }) => ({ t, down, up })),
        '30d': metricsHistory(gatewayCfg?.id, '30d').map(({ t, down, up }) => ({ t, down, up })),
      },
      adguard: adguard ?? {
        host: '', port: 0, status: 'inactive', queries24h: 0, blocked24h: 0, blockedPct: 0,
        trackersBlocked: 0, dnsLatencyMs: 0, clientsUsing: 0, clientsTotal: 0,
        topBlocked: [], filterLists: 0, rules: 0,
      },
      wireguard: wireguard ?? { interface: config.wgInterface, subnet: '', status: 'inactive', peers: [] },
      routers: routerList,
      deviceTotals: {
        total: devices.length,
        online: devices.filter((d) => d.online).length,
        knownOffline: devices.filter((d) => !d.online).length,
        newToday: 0,
      },
      topDevices: [...devices].sort((a, b) => b.trafficMbps - a.trafficMbps).slice(0, 5),
      alerts: [...alerts],
      unreadAlerts: alerts.filter((a) => !a.read).length,
      ts: Math.floor(Date.now() / 1000),
    }
  }

  function getRouters() {
    return routers.map((cfg) => {
      const p = lastPolled.get(cfg.id)
      if (!p) {
        const prev = lastGood.get(cfg.id)
        if (prev && (failCount.get(cfg.id) ?? 0) < 2) return prev
        return offlineRouter(cfg)
      }
      return buildRouter(p, metricsHistory(cfg.id, '24h'))
    })
  }

  async function getRouterDetail(id) {
    const cfg = routers.find((r) => r.id === id)
    if (!cfg) return null
    const p = lastPolled.get(id)
    const router = p ? buildRouter(p, metricsHistory(id, '24h')) : offlineRouter(cfg)
    // Mapa global MAC → lease (los satélites no tienen DHCP: hay que mirar
    // la tabla del gateway para nombrar lo que cuelga de sus bocas)
    const leaseMap = new Map()
    for (const [, polled] of lastPolled) {
      for (const l of polled.leases ?? []) if (l.mac) leaseMap.set(l.mac, l)
    }
    const fdb = p?.fdb ?? new Map()
    const ports = (p?.ports ?? []).map((port) => {
      if (!port.up) return port
      const mac = [...fdb].find(([, portName]) => portName === port.id)?.[0]
      const lease = mac ? leaseMap.get(mac) : null
      return {
        ...port,
        connectedTo: lease?.hostname || lease?.ip || mac || undefined,
        deviceMac: mac || undefined,
        detail: lease?.ip ? `${lease.ip} · full duplex` : undefined,
      }
    })
    const radios = p?.radios ?? []
    const detail = {
      router,
      ports,
      radios,
      backhaul: null,
      series: {
        '1h': metricsHistory(id, '1h').map(({ t, cpu, ram, temp }) => ({ t, cpu, ram, temp })),
        '24h': metricsHistory(id, '24h').map(({ t, cpu, ram, temp }) => ({ t, cpu, ram, temp })),
        '7d': metricsHistory(id, '7d').map(({ t, cpu, ram, temp }) => ({ t, cpu, ram, temp })),
      },
      clients: buildDevices(lastPolled).filter((d) => d.routerId === id),
      extras: {
        mac: p?.board ? (p.board['mac'] ?? '—') : '—',
        firmware: p?.board?.release?.description ?? '—',
        firmwareUpdated: true,
        lastReboot: '—',
        soc: p?.board?.system ?? '—',
        flash: '—',
        ramMb: p?.sysInfo?.memory?.total ? Math.round(p.sysInfo.memory.total / 1e6) : 0,
        bandSplit: { band24: 0, band5: 0, cable: 0 },
        trafficNow: mbps(p?.net?.rxBps),
        gatewayLatencySpark: [],
        backhaulSignal: [],
        radios,
        ports,
        ethPorts: ports,
      },
    }
    if (id === gatewayCfg?.id) {
      detail.adguard = await pollAdGuard()
      detail.wireguard = await pollWireGuard(detail.clients)
    }
    return detail
  }

  function getDevices() {
    return buildDevices(lastPolled)
  }

  function getAlerts() {
    return [...alerts]
  }

  function getMetricsRows() {
    const rows = []
    for (const [id, p] of lastPolled) {
      rows.push({
        router_id: id,
        cpu: p.cpu,
        ram: p.ram,
        temp: p.temp,
        latency_ms: p.latency?.latencyMs ?? null,
        rx_bps: p.net?.rxBps ?? null,
        tx_bps: p.net?.txBps ?? null,
      })
    }
    return rows
  }

  function getAdguardRow() {
    return null // el poller usa el último overview (adguard del snapshot)
  }

  return {
    mode: 'live',
    tick() {}, // el sondeo real ocurre en getOverview() (async)
    setRouters,
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
