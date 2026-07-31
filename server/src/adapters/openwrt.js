/**
 * Adapter live para routers OpenWrt / GL.iNet (Flint 2 usa firmware basado en
 * OpenWrt con ubus expuesto igualmente).
 *
 * Estrategia por consulta:
 *   1. ubus JSON-RPC sobre HTTP: POST http://<host>/ubus
 *      (login: method "call", ["00000000000000000000000000000000","session","login",
 *       {username, password}] → ubus_rpc_session).
 *   2. Fallback SSH (clave, sin passphrase interactiva):
 *      ssh -i $SSH_KEY_PATH -o BatchMode=yes -o ConnectTimeout=4 root@<host> <cmd>
 *
 * NOTA SANDBOX: este código no se puede probar contra routers reales aquí.
 * Todas las llamadas tienen timeout y degradan con excepción controlada:
 * el caller (live adapter) marca el router offline y sigue con el resto.
 */
import { execFile } from 'node:child_process'
import { sshBaseArgs } from '../sshkey.js'

const SSH_TIMEOUT_MS = 5000
const HTTP_TIMEOUT_MS = 4000

export class OpenWrtClient {
  /**
   * @param {{ id: string, host: string, name?: string, type?: string }} routerCfg
   * @param {{ sshKeyPath: string, user?: string, password?: string }} opts
   *   user/password son opcionales: solo necesarios para ubus HTTP (session login).
   */
  constructor(routerCfg, opts) {
    this.cfg = routerCfg
    this.host = routerCfg.host
    this.sshKeyPath = opts.sshKeyPath
    this.user = opts.user || 'root'
    this.password = opts.password || ''
    this._sid = null // sesión ubus HTTP cacheada
    this._lastStat = null // /proc/stat previo para cpu%
    this._lastNetDev = null // /proc/net/dev previo para bps
  }

  // -------------------------------------------------------------------------
  // Transporte SSH
  // -------------------------------------------------------------------------

  ssh(cmd, timeoutMs = SSH_TIMEOUT_MS) {
    return new Promise((resolve, reject) => {
      execFile(
        'ssh',
        [
          ...sshBaseArgs(this.sshKeyPath),
          // Multiplexación: reutiliza la conexión SSH entre ticks del poller
          '-o', 'ControlMaster=auto',
          '-o', `ControlPath=/tmp/netpulse-ssh-%r@%h:%p`,
          '-o', 'ControlPersist=60',
          `root@${this.host}`,
          cmd,
        ],
        { timeout: timeoutMs, maxBuffer: 4 * 1024 * 1024 },
        (err, stdout, stderr) => {
          if (err) return reject(new Error(`ssh ${this.host}: ${err.message} ${stderr || ''}`.trim()))
          resolve(stdout)
        },
      )
    })
  }

  async sshJson(cmd) {
    const out = await this.ssh(cmd)
    return JSON.parse(out)
  }

  // -------------------------------------------------------------------------
  // Transporte ubus HTTP (JSON-RPC)
  // -------------------------------------------------------------------------

  async _ubusHttpRpc(sid, object, method, params = {}) {
    const ctrl = new AbortController()
    const timer = setTimeout(() => ctrl.abort(), HTTP_TIMEOUT_MS)
    try {
      const res = await fetch(`http://${this.host}/ubus`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          jsonrpc: '2.0',
          id: 1,
          method: 'call',
          params: [sid, object, method, params],
        }),
        signal: ctrl.signal,
      })
      if (!res.ok) throw new Error(`ubus HTTP ${res.status}`)
      const data = await res.json()
      if (data.error) throw new Error(`ubus RPC error: ${JSON.stringify(data.error)}`)
      const result = data.result
      // ubus devuelve [exitCode, payload]
      if (Array.isArray(result) && result[0] === 0) return result[1]
      throw new Error(`ubus call falló: ${JSON.stringify(result)}`)
    } finally {
      clearTimeout(timer)
    }
  }

  async _ubusLogin() {
    const result = await this._ubusHttpRpc(
      '00000000000000000000000000000000',
      'session',
      'login',
      { username: this.user, password: this.password },
    )
    this._sid = result.ubus_rpc_session
    return this._sid
  }

  /**
   * ubus call con HTTP + login cacheado; reintenta login una vez si la sesión
   * expiró; fallback a SSH si HTTP no está disponible.
   */
  async ubusCall(object, method, params = {}) {
    try {
      if (!this._sid) await this._ubusLogin()
      try {
        return await this._ubusHttpRpc(this._sid, object, method, params)
      } catch (err) {
        // Sesión caducada → un solo re-login
        this._sid = null
        await this._ubusLogin()
        return await this._ubusHttpRpc(this._sid, object, method, params)
      }
    } catch (httpErr) {
      // Fallback SSH: ubus call <object> <method> '<json>'
      const paramsJson = JSON.stringify(params).replace(/'/g, "'\\''")
      return this.sshJson(`ubus call ${object} ${method} '${paramsJson}'`)
    }
  }

  // -------------------------------------------------------------------------
  // Sonda de sistema
  // -------------------------------------------------------------------------

  /** Modelo, release, kernel → metadatos estáticos del router. */
  async getBoard() {
    return this.ubusCall('system', 'board')
  }

  /** uptime, memoria, load. */
  async getSystemInfo() {
    return this.ubusCall('system', 'info')
  }

  /**
   * CPU % real por delta de /proc/stat (vía SSH; ubus no lo expone).
   * Primera muestra: null (no hay delta aún).
   */
  async getCpuPercent() {
    const out = await this.ssh("grep '^cpu ' /proc/stat")
    const parts = out.trim().split(/\s+/).slice(1).map(Number)
    const [user, nice, system, idle, iowait, irq, softirq] = parts
    const idleAll = idle + (iowait || 0)
    const nonIdle = user + nice + system + (irq || 0) + (softirq || 0)
    const total = idleAll + nonIdle
    const prev = this._lastStat
    this._lastStat = { total, idleAll }
    if (!prev) return null
    const dTotal = total - prev.total
    const dIdle = idleAll - prev.idleAll
    if (dTotal <= 0) return null
    return Math.round(((dTotal - dIdle) / dTotal) * 100)
  }

  /** Temperatura °C del primer thermal zone disponible (vía SSH). */
  async getTempC() {
    const out = await this.ssh(
      'for f in /sys/class/thermal/thermal_zone*/temp; do [ -r "$f" ] && cat "$f" && break; done',
    )
    const milli = parseInt(out.trim(), 10)
    return Number.isFinite(milli) ? Math.round(milli / 1000) : null
  }

  /**
   * Tráfico agregado (bps) por delta de /proc/net/dev en interfaces físicas
   * (excluye lo, bridges, ifb, wg, tun/tap para no duplicar contadores).
   */
  async getNetDevBps() {
    const out = await this.ssh("cat /proc/net/dev | tail -n +3")
    let rx = 0
    let tx = 0
    for (const line of out.trim().split('\n')) {
      const m = /^\s*([\w.-]+):\s*(.*)$/.exec(line)
      if (!m) continue
      const iface = m[1]
      if (/^(lo|br-|ifb|wg|tun|tap)/.test(iface)) continue
      const fields = m[2].trim().split(/\s+/).map(Number)
      rx += fields[0] || 0
      tx += fields[8] || 0
    }
    const now = Date.now()
    const prev = this._lastNetDev
    this._lastNetDev = { rx, tx, at: now }
    if (!prev) return { rxBps: null, txBps: null }
    const dt = (now - prev.at) / 1000
    if (dt <= 0) return { rxBps: null, txBps: null }
    return {
      rxBps: Math.max(0, Math.round(((rx - prev.rx) * 8) / dt)),
      txBps: Math.max(0, Math.round(((tx - prev.tx) * 8) / dt)),
    }
  }

  /** Latencia/pérdida WAN (ping a internet) — solo tiene sentido en el gateway. */
  async getWanLatency(target = '1.1.1.1') {
    const out = await this.ssh(`ping -c 3 -W 2 ${target} 2>/dev/null | tail -2`)
    const loss = /(\d+(?:\.\d+)?)% packet loss/.exec(out)
    const rtt = /= [\d.]+\/([\d.]+)\/[\d.]+\/[\d.]+ ms/.exec(out)
    return {
      latencyMs: rtt ? Math.round(parseFloat(rtt[1])) : null,
      lossPct: loss ? parseFloat(loss[1]) : null,
    }
  }

  /** Latencia al gateway desde un AP (ping corto). */
  async getGatewayLatency(gatewayHost) {
    const out = await this.ssh(`ping -c 2 -W 2 ${gatewayHost} 2>/dev/null | tail -1`)
    const rtt = /= [\d.]+\/([\d.]+)\/[\d.]+ ms/.exec(out)
    return rtt ? Math.round(parseFloat(rtt[1])) : null
  }

  // -------------------------------------------------------------------------
  // Clientes (DHCP + wireless)
  // -------------------------------------------------------------------------

  /** Leases DHCP: ubus dhcp ipv4leases; fallback /tmp/dhcp.leases. */
  async getDhcpLeases() {
    try {
      const data = await this.ubusCall('dhcp', 'ipv4leases')
      const leases = data?.lease ?? data?.leases ?? []
      return leases.map((l) => ({
        mac: (l.mac || '').toUpperCase(),
        ip: l['ip-address'] || l.ip || '',
        hostname: l.hostname || '',
      }))
    } catch {
      const out = await this.ssh('cat /tmp/dhcp.leases 2>/dev/null || true')
      // Formato: <expiry> <mac> <ip> <hostname> <clientid>
      return out
        .trim()
        .split('\n')
        .filter(Boolean)
        .map((line) => {
          const p = line.split(/\s+/)
          return { mac: (p[1] || '').toUpperCase(), ip: p[2] || '', hostname: p[3] === '*' ? '' : p[3] || '' }
        })
    }
  }

  /**
   * Clientes wireless asociados: iwinfo <iface> assoclist por cada radio.
   * Compatible BusyBox (sin grep -P): awk para listar ifaces, sed BRE para parsear.
   * Devuelve mapa mac → { signalDbm, band }.
   */
  async getWirelessClients() {
    const out = await this.ssh(
      'for i in $(iwinfo 2>/dev/null | awk \'/^[a-z]/ {print $1}\'); do ' +
        'freq=$(iwinfo "$i" info 2>/dev/null | sed -n \'s/.*Channel: [0-9]* (\\([0-9.]*\\) GHz).*/\\1/p\' | head -1); ' +
        'iwinfo "$i" assoclist 2>/dev/null | sed -n \'s/^\\([0-9A-Fa-f:]\\{17\\}\\) *\\(-[0-9]*\\).*/\\1 \\2/p\' | while read mac sig; do ' +
        'echo "$mac $sig $freq"; done; done',
      8000,
    )
    const map = new Map()
    for (const line of out.trim().split('\n').filter(Boolean)) {
      const [mac, sig, freq] = line.split(/\s+/)
      const ghz = parseFloat(freq || '0')
      map.set(mac.toUpperCase(), {
        signalDbm: parseInt(sig, 10),
        band: ghz >= 5 ? '5 GHz' : '2.4 GHz',
      })
    }
    return map
  }

  // -------------------------------------------------------------------------
  // Puertos
  // -------------------------------------------------------------------------

  /** Estado de interfaces: operstate + speed desde /sys (vía SSH). */
  async getPortStates() {
    const out = await this.ssh(
      'for d in /sys/class/net/*; do i=$(basename "$d"); ' +
        'echo "$i $(cat $d/operstate 2>/dev/null) $(cat $d/speed 2>/dev/null || echo -1)"; done',
    )
    return out
      .trim()
      .split('\n')
      .filter(Boolean)
      .map((line) => {
        const [name, oper, speed] = line.split(/\s+/)
        const mbps = parseInt(speed, 10)
        return {
          name,
          up: oper === 'up',
          speed: mbps > 0 ? (mbps >= 1000 ? `${mbps / 1000} Gbps` : `${mbps} Mbps`) : '—',
        }
      })
  }

  /**
   * Layout canónico de puertos desde /etc/board.json (la CONFIG del router).
   * Es estático por hardware: el adapter lo cachea y no se re-lee cada tick.
   * Devuelve [{ id, name, label, role }] — WAN normalizado a id 'wan'.
   */
  async getPortLayout() {
    const out = await this.ssh('cat /etc/board.json 2>/dev/null')
    const net = JSON.parse(out)?.network ?? {}
    const ports = []
    for (const name of net.lan?.ports ?? []) {
      ports.push({ id: name, name, label: name.replace('lan', 'LAN '), role: 'lan' })
    }
    const wanDev = net.wan?.device
    if (wanDev) {
      ports.unshift({ id: 'wan', name: wanDev, label: 'WAN', role: 'wan' })
    }
    return ports
  }

  /**
   * Bocas Ethernet = layout (config) + estado /sys por nombre de interfaz.
   * En APs en modo bridge el puerto físico "wan" está metido en br-lan (es
   * una boca LAN más, no WAN): se detecta por brif y se re-etiqueta.
   * Si el layout no está disponible, heurística: lanN + wan/pppoe-wan/eth1.
   * Devuelve [{ id, label, up, speed }] listo para EthPort.
   */
  async getEthPorts(layout = null) {
    const states = await this.getPortStates()
    const byName = new Map(states.map((p) => [p.name, p]))
    if (layout?.length) {
      // Miembros del bridge: si "wan" está en br-lan, este router es un AP
      const brif = await this.ssh('ls /sys/class/net/br-lan/brif/ 2>/dev/null').catch(() => '')
      const members = new Set(brif.trim().split('\n').filter(Boolean))
      const lanCount = layout.filter((p) => p.role === 'lan').length
      return layout.map((p) => {
        const st = byName.get(p.name)
        const up = st?.up ?? false
        const isWanInBridge = p.role === 'wan' && members.has(p.name)
        return {
          id: p.id,
          label: isWanInBridge ? `LAN ${lanCount + 1}` : p.label,
          up,
          speed: up ? st?.speed : undefined,
        }
      })
    }
    // Fallback sin config
    const lan = states
      .filter((p) => /^lan\d+$/.test(p.name))
      .sort((a, b) => a.name.localeCompare(b.name, undefined, { numeric: true }))
      .map((p) => ({ id: p.name, label: p.name.replace('lan', 'LAN '), up: p.up, speed: p.up ? p.speed : undefined }))
    let wanName = byName.has('wan') ? 'wan' : byName.has('pppoe-wan') ? 'eth1' : byName.has('eth1') ? 'eth1' : null
    if (wanName) {
      const st = byName.get(wanName)
      lan.unshift({ id: 'wan', label: 'WAN', up: st.up, speed: st.up ? st.speed : undefined })
    }
    return lan
  }

  /**
   * Tabla de reenvío del bridge: MAC aprendida → puerto (lanN/wan).
   * Usa brctl (BusyBox lo trae; `bridge` no existe en estos routers) +
   * /sys/class/net/br-lan/brif/<iface>/port_no para mapear nº de puerto → nombre.
   * Devuelve Map mac → port.
   */
  async getBridgeFdb() {
    const out = await this.ssh(
      'echo "==PORTS=="; for d in /sys/class/net/br-lan/brif/*; do [ -r "$d/port_no" ] && echo "$(cat $d/port_no) $(basename $d)"; done; ' +
        'echo "==MACS=="; brctl showmacs br-lan 2>/dev/null | awk \'NR>1 && $3=="no" {print $1, $2}\'',
    )
    const map = new Map()
    const portNames = new Map()
    let section = ''
    for (const line of out.split('\n')) {
      const t = line.trim()
      if (t.startsWith('==PORTS==')) {
        section = 'p'
        continue
      }
      if (t.startsWith('==MACS==')) {
        section = 'm'
        continue
      }
      if (section === 'p') {
        const [no, name] = t.split(/\s+/)
        // port_no puede venir en hex (0x2) o decimal; brctl lo muestra decimal
        const dec = no?.startsWith('0x') ? String(parseInt(no, 16)) : no
        if (dec && name) portNames.set(dec, name)
      } else if (section === 'm') {
        const [no, mac] = t.split(/\s+/)
        const port = portNames.get(no)
        if (mac && port && /^(lan\d+|wan)$/.test(port)) map.set(mac.toUpperCase(), port)
      }
    }
    return map
  }

  // -------------------------------------------------------------------------
  // Radios WiFi
  // -------------------------------------------------------------------------

  /**
   * Radios activas: por cada interfaz AP con ESSID, canal/ancho/potencia
   * (iwinfo info) y nº de clientes (iwinfo assoclist). Agregado por banda.
   * Compatible BusyBox (awk/sed, sin grep -P).
   * Devuelve [{ name, channel, widthMhz, powerDbm, clients }].
   */
  async getRadios() {
    const out = await this.ssh(
      'for i in $(iwinfo 2>/dev/null | awk \'/^[a-z]/ {print $1}\'); do ' +
        'info=$(iwinfo "$i" info 2>/dev/null) || continue; ' +
        'echo "$info" | grep -q ESSID || continue; ' +
        'freq=$(echo "$info" | sed -n \'s/.*Channel: [0-9]* (\\([0-9.]*\\) GHz).*/\\1/p\' | head -1); ' +
        'ch=$(echo "$info" | sed -n \'s/.*Channel: \\([0-9][0-9]*\\).*/\\1/p\' | head -1); ' +
        'ht=$(echo "$info" | sed -n \'s/.*HT [Mm]ode: \\([A-Za-z0-9]*\\).*/\\1/p\' | head -1); ' +
        'tx=$(echo "$info" | sed -n \'s/.*Tx-Power: \\([0-9]*\\).*/\\1/p\' | head -1); ' +
        'n=$(iwinfo "$i" assoclist 2>/dev/null | grep -c \'^[0-9A-Fa-f:]\'); ' +
        'echo "$freq|$ch|$ht|$tx|$n"; done',
      8000,
    )
    const byBand = new Map()
    for (const line of out.trim().split('\n').filter(Boolean)) {
      const [freqS, chS, ht, txS, nS] = line.split('|')
      const freq = parseFloat(freqS || '0')
      if (!freq) continue
      const band = freq >= 5 ? '5 GHz' : '2.4 GHz'
      const widthMhz = parseInt((ht || '').replace(/\D/g, ''), 10) || 20
      const clients = parseInt(nS || '0', 10) || 0
      const cur = byBand.get(band)
      if (!cur) {
        byBand.set(band, {
          name: band,
          channel: parseInt(chS || '0', 10) || 0,
          widthMhz,
          powerDbm: parseInt(txS || '0', 10) || 0,
          clients,
        })
      } else {
        cur.clients += clients // SSID extra en la misma banda (suma clientes)
      }
    }
    return [...byBand.values()]
  }
}
