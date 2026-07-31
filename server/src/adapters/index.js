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
import { AdGuardGlinetClient } from './adguard-glinet.js'
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
    for (const id of [...layoutCache.keys()]) if (!ids.has(id)) layoutCache.delete(id)
    for (const id of [...extrasCache.keys()]) if (!ids.has(id)) extrasCache.delete(id)
    for (const id of [...lastPolled.keys()]) if (!ids.has(id)) lastPolled.delete(id)
    for (const id of [...failCount.keys()]) if (!ids.has(id)) failCount.delete(id)
  }

  // AdGuard: config desde kv (Ajustes, GL.iNet) con fallback a .env (AGH estándar)
  let adguardClient = null
  let adguardClientKey = ''
  function getAdguardClient() {
    let client = null
    let key = ''
    if (dbHandle) {
      const host = dbHandle.db.prepare("SELECT value FROM kv WHERE key='adguard_host'").get()?.value
      if (host) {
        const user = dbHandle.db.prepare("SELECT value FROM kv WHERE key='adguard_user'").get()?.value ?? 'root'
        const pass = dbHandle.db.prepare("SELECT value FROM kv WHERE key='adguard_pass'").get()?.value ?? ''
        if (pass) {
          key = `gl|${host}|${user}`
          if (adguardClientKey !== key) client = new AdGuardGlinetClient({ host, user, pass, sshKeyPath: config.sshKeyPath })
        }
      }
    }
    if (!client && !key && config.adguard?.pass) {
      key = `std|${config.adguard.url}`
      if (adguardClientKey !== key) client = new AdGuardClient(config.adguard)
    }
    if (!key) return null
    if (adguardClientKey !== key) {
      adguardClient = client
      adguardClientKey = key
    }
    return adguardClient
  }

  // Estado entre ticks
  const lastGood = new Map() // routerId → último Router online
  const lastStatus = new Map() // routerId → 'online' | 'offline'
  const boardCache = new Map() // routerId → system board
  const layoutCache = new Map() // routerId → layout de puertos (board.json, estático)
  const extrasCache = new Map() // routerId → { ports, radios, wireless, fdb } último bueno
  let alerts = [] // AlertEvent[], más recientes primero (máx 100)
  let lastWgPeersActive = new Set()
  const weakAlerted = new Map() // mac → ts última alerta de señal débil

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
    // Layout de puertos desde la CONFIG (board.json): se lee una vez y se
    // cachea; solo se reintenta mientras no se haya conseguido ninguno
    let layout = layoutCache.get(cfg.id)
    if (!layout) {
      layout = await client.getPortLayout().catch(() => null)
      if (layout?.length) layoutCache.set(cfg.id, layout)
    }
    const [sysInfo, cpu, temp, net, leases, wireless, ports, radios, fdb, brMac] = await Promise.all([
      client.getSystemInfo(),
      client.getCpuPercent().catch(() => null),
      client.getTempC().catch(() => null),
      client.getNetDevBps().catch(() => ({ rxBps: null, txBps: null })),
      client.getDhcpLeases().catch(() => []),
      client.getWirelessClients().catch(() => null),
      client.getEthPorts(layout ?? undefined).catch(() => []),
      client.getRadios().catch(() => []),
      client.getBridgeFdb().catch(() => null),
      client.getBridgeMac().catch(() => null),
    ])
    if (!boardCache.has(cfg.id)) {
      boardCache.set(cfg.id, await client.getBoard().catch(() => null))
    }
    const board = boardCache.get(cfg.id)
    // Anti-parpadeo: si una sonda puntual falla (tick parcial), conserva la
    // última lista buena en vez de devolver vacío. null = sonda fallida;
    // colección vacía = resultado real (p.ej. 0 clientes wifi)
    const cached = extrasCache.get(cfg.id) ?? { ports: [], radios: [], wireless: new Map(), fdb: new Map() }
    const portsGood = ports.length > 0 ? ports : cached.ports
    const radiosGood = radios.length > 0 ? radios : cached.radios
    const wirelessGood = wireless ?? cached.wireless
    const fdbGood = fdb ?? cached.fdb
    extrasCache.set(cfg.id, { ports: portsGood, radios: radiosGood, wireless: wirelessGood, fdb: fdbGood })
    const mem = sysInfo?.memory ?? {}
    // Uso real de RAM como en la UI del router: used = total − available
    // (MemAvailable ya descuenta caché recuperable; total−free−buffered infla)
    const ramPct =
      mem.total > 0
        ? Math.round(((mem.total - (mem.available ?? (mem.free || 0) + (mem.buffered || 0))) / mem.total) * 100)
        : null
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
      wireless: wirelessGood,
      ports: portsGood,
      radios: radiosGood,
      fdb: fdbGood,
      brMac,
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
      name: polled.board?.hostname || cfg.name || cfg.host,
      model,
      modelShort: model,
      role: cfg.id === gatewayCfg?.id ? 'Gateway principal' : 'Punto de acceso',
      roleBadge: cfg.id === gatewayCfg?.id ? 'Principal' : 'AP',
      ip: cfg.host,
      mac: polled.brMac ?? undefined,
      firmware: polled.board?.release?.description ?? undefined,
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
    const client = getAdguardClient()
    if (!client) return null
    try {
      return await client.getStats()
    } catch (err) {
      console.warn(`[netpulse] AdGuard inalcanzable: ${err.message}`)
      return {
        host: client.host ?? '',
        port: 3000,
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
   *
   * Persistencia (tabla device_attrib): el FDB del gateway aprende TODAS las
   * MACs (tráfico hacia internet), así que ver una MAC ahí no dice cómo
   * conecta. Guardamos la última atribución buena (wireless o FDB de
   * satélite) y la usamos cuando el dispositivo no está asociado ahora.
   */
  const attribStmt = dbHandle
    ? {
        upsert: dbHandle.db.prepare(
          'INSERT INTO device_attrib (mac, router_id, band, signal_dbm, last_seen) VALUES (?, ?, ?, ?, ?) ' +
            'ON CONFLICT(mac) DO UPDATE SET router_id=excluded.router_id, band=excluded.band, signal_dbm=excluded.signal_dbm, last_seen=excluded.last_seen',
        ),
        all: dbHandle.db.prepare('SELECT mac, router_id, band, signal_dbm FROM device_attrib'),
      }
    : null

  // Migración una vez (attrib_v2): las primeras versiones guardaban banda
  // desde el FDB (incluidas MACs wifi de paso, p.ej. shelly) → tabla limpia
  if (dbHandle && attribStmt) {
    const flag = dbHandle.db.prepare("SELECT value FROM kv WHERE key = 'attrib_v2'").get()
    if (!flag) {
      dbHandle.db.prepare('DELETE FROM device_attrib').run()
      dbHandle.db.prepare("INSERT INTO kv (key, value) VALUES ('attrib_v2', '1')").run()
      console.log('[netpulse] device_attrib limpiada (attrib_v2: solo wireless persiste)')
    }
  }

  function buildDevices(polled) {
    const leasesByMac = new Map()
    for (const [, p] of polled) {
      for (const l of p.leases ?? []) if (l.mac) leasesByMac.set(l.mac, l)
    }
    const known = new Map((attribStmt?.all.all() ?? []).map((r) => [r.mac, r]))
    const now = Date.now()

    const seen = new Map() // mac → { routerId, band, signalDbm }
    // (1) wireless de cualquier router: la mejor pista y la ÚNICA que se
    // persiste. El FDB (de satélites o gateway) también ve MACs de paso
    // (wifi de otro AP atravesando el bridge) → no sirve para recordar banda.
    for (const [routerId, p] of polled) {
      for (const [mac, w] of p.wireless ?? new Map()) {
        seen.set(mac, { routerId, band: w.band, signalDbm: w.signalDbm })
        attribStmt?.upsert.run(mac, routerId, w.band, w.signalDbm, now)
      }
    }
    // (2) FDB de satélites: pista solo de ESTE tick (no se guarda)
    const gwId = gatewayCfg?.id
    for (const [routerId, p] of polled) {
      if (routerId === gwId) continue
      for (const [mac] of p.fdb ?? new Map()) {
        if (!seen.has(mac)) {
          seen.set(mac, { routerId, band: 'cable', signalDbm: null })
        }
      }
    }
    // (3) FDB del gateway: solo si no hay memoria (puede ser wifi de cualquier
    // AP que pasa por el backhaul). No se guarda: es mala pista.
    const gwPolled = gwId ? polled.get(gwId) : null
    for (const [mac] of gwPolled?.fdb ?? new Map()) {
      if (!seen.has(mac) && !known.has(mac)) {
        seen.set(mac, { routerId: gwId, band: 'cable', signalDbm: null })
      }
    }

    const allMacs = new Set([...leasesByMac.keys(), ...seen.keys(), ...known.keys()])
    const routerMacs = new Set()
    for (const [, p] of polled) if (p.brMac) routerMacs.add(p.brMac)
    const devices = []
    for (const mac of allMacs) {
      if (routerMacs.has(mac)) continue // los routers no son clientes
      const lease = leasesByMac.get(mac)
      const s = seen.get(mac) // visto ESTE tick (assoc/FDB)
      const k = s ? null : known.get(mac) // última atribución buena
      devices.push({
        id: mac.toLowerCase().replace(/:/g, '-'),
        name: lease?.hostname || mac,
        type: 'desconocido',
        manufacturer: 'Desconocido',
        ip: lease?.ip ?? '',
        mac,
        routerId: s?.routerId ?? k?.router_id ?? gwId,
        band: s?.band ?? k?.band ?? '—',
        signalDbm: s?.signalDbm ?? k?.signal_dbm ?? null,
        trafficMbps: 0,
        // Lease NO es estar online (las reservas son 'infinite'): online =
        // visto asociado/conectado en este tick
        online: Boolean(s),
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
    // Aviso de señal débil (< -70 dBm): una alerta por dispositivo y día
    for (const d of devices) {
      if (d.online && d.signalDbm != null && d.signalDbm < -70) {
        const last = weakAlerted.get(d.mac) ?? 0
        if (Date.now() - last > 24 * 3600e3) {
          weakAlerted.set(d.mac, Date.now())
          pushAlert({
            id: `alert-weak-${d.mac}-${Date.now()}`,
            severity: 'warn',
            title: `Señal débil en ${d.name}`,
            description: `${d.signalDbm} dBm en ${d.routerId} — revisa cobertura o acerca un AP`,
            time: 'ahora mismo',
            read: false,
            routerId: d.routerId,
          })
        }
      }
    }
    // Clientes reales por router (atribución wireless/FDB, no leases)
    for (const r of routerList) {
      r.clients = devices.filter((d) => d.routerId === r.id && d.online).length
    }
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
    const routerByMac = new Map() // br-lan MAC → nombre del router
    for (const [, polled] of lastPolled) {
      for (const l of polled.leases ?? []) if (l.mac) leaseMap.set(l.mac, l)
      if (polled.brMac) routerByMac.set(polled.brMac, polled.cfg.name || polled.cfg.host)
    }
    // Boca → MACs aprendidas. Lo importante es el VECINO inmediato (lo que
    // hay al otro lado del cable), no cuántos equipos cuelgan detrás.
    const portMacs = new Map()
    for (const [mac, portName] of p?.fdb ?? new Map()) {
      if (!portMacs.has(portName)) portMacs.set(portName, [])
      portMacs.get(portName).push(mac)
    }
    const ports = (p?.ports ?? []).map((port) => {
      if (!port.up) return port
      const all = portMacs.get(port.id) ?? []
      // 1) ¿Hay otro router al otro lado? (uplink router↔router)
      const neighbor = all.find((mac) => routerByMac.has(mac))
      if (neighbor) {
        return { ...port, connectedTo: routerByMac.get(neighbor), detail: 'enlace entre routers' }
      }
      // 2) Un solo dispositivo final
      const endDevices = all
      if (endDevices.length === 1) {
        const mac = endDevices[0]
        const lease = leaseMap.get(mac)
        return {
          ...port,
          connectedTo: lease?.hostname || lease?.ip || mac,
          deviceMac: mac,
          detail: lease?.ip ? `${lease.ip} · full duplex` : undefined,
        }
      }
      // 3) Varios: el vecino es un switch/hub — si tiene nombre (lease), ese es
      if (endDevices.length > 1) {
        const infraMac = endDevices.find((mac) => leaseMap.get(mac)?.hostname)
        const infra = infraMac ? leaseMap.get(infraMac) : null
        return {
          ...port,
          connectedTo: infra?.hostname || 'Switch',
          deviceMac: infraMac || undefined,
          detail: infra?.ip ?? undefined,
        }
      }
      return port
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
        mac: p?.brMac ?? '—',
        firmware: p?.board?.release?.description ?? '—',
        firmwareUpdated: true,
        lastReboot: p?.uptimeSec
          ? new Date(Date.now() - p.uptimeSec * 1000).toLocaleString('es-ES', {
              day: '2-digit',
              month: '2-digit',
              year: 'numeric',
              hour: '2-digit',
              minute: '2-digit',
            })
          : '—',
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

  /** Red DAWN (roaming/band-steering): la malla completa la tiene cualquier nodo. */
  async function getDawn() {
    for (const [routerId, p] of lastPolled) {
      try {
        const data = await p.client.sshJson('ubus call dawn get_network')
        const aps = []
        for (const [ssid, bssids] of Object.entries(data ?? {})) {
          for (const [bssid, ap] of Object.entries(bssids)) {
            aps.push({
              ssid,
              bssid,
              hostname: ap.hostname ?? routerId,
              band: (ap.freq ?? 0) >= 5000 ? '5 GHz' : '2.4 GHz',
              channel: ap.channel ?? 0,
              utilizationPct: ap.channel_utilization ?? 0,
              clients: ap.num_sta ?? 0,
              local: Boolean(ap.local),
              iface: ap.iface ?? '',
            })
          }
        }
        aps.sort((a, b) => a.hostname.localeCompare(b.hostname) || a.band.localeCompare(b.band))
        return { aps, from: routerId }
      } catch {
        // prueba el siguiente router de la malla
      }
    }
    return null
  }

  /** Clientes AdGuard (solo si está configurado y responde). */
  async function getAdguardClients() {
    const client = getAdguardClient()
    if (!client || typeof client.queryClients !== 'function') return null
    return client.queryClients()
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
    getDawn,
    getAdguardClients,
    async close() {},
  }
}
