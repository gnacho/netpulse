/**
 * Descubrimiento de routers candidatos en la LAN:
 *
 *  1. Subred: se deriva de la puerta de enlace por defecto (`ip route`),
 *     asumiendo /24 (LAN doméstica).
 *  2. Barrido TCP del puerto 22 en los 254 hosts con concurrencia limitada
 *     (NO se usa ping: necesita cap_net_raw, no disponible en LXC ni con el
 *     hardening del servicio).
 *  3. A los vivos se les prueba la firma OpenWrt: POST http://<ip>/ubus
 *     (cualquier respuesta JSON-RPC, incluso "access denied", delata uhttpd
 *     con ubus ⇒ router OpenWrt/GL.iNet).
 *  4. A los candidatos se les prueba SSH con la clave propia (BatchMode):
 *       - OK → `authorized: true` + modelo (ubus system board)
 *       - Permission denied → `authorized: false` (falta autorizar la clave)
 *
 * El resultado se cachea 60 s (el barrido tarda varios segundos).
 */
import net from 'node:net'
import http from 'node:http'
import https from 'node:https'
import { execFile } from 'node:child_process'
import { detectGatewayIp, listRouters } from './routerstore.js'
import { sshBaseArgs } from './sshkey.js'

const CACHE_MS = 60_000
const TCP_TIMEOUT_MS = 800
const HTTP_TIMEOUT_MS = 1500
const SSH_TIMEOUT_MS = 4000

let cache = { at: 0, subnet: null, results: [] }

function exec(cmd, args, timeoutMs) {
  return new Promise((resolve) => {
    execFile(cmd, args, { timeout: timeoutMs }, (err, stdout) => {
      resolve({ ok: !err, stdout: stdout || '' })
    })
  })
}

/** true si el puerto TCP responde (SYN/ACK) antes del timeout. */
function tcpOpen(host, port, timeoutMs = TCP_TIMEOUT_MS) {
  return new Promise((resolve) => {
    const sock = net.connect({ host, port, timeout: timeoutMs })
    sock.once('connect', () => {
      sock.destroy()
      resolve(true)
    })
    sock.once('timeout', () => {
      sock.destroy()
      resolve(false)
    })
    sock.once('error', () => resolve(false))
  })
}

/** Ejecuta tareas con concurrencia limitada. */
async function pool(items, size, fn) {
  const out = new Array(items.length)
  let i = 0
  await Promise.all(
    Array.from({ length: Math.min(size, items.length) }, async () => {
      while (i < items.length) {
        const idx = i++
        out[idx] = await fn(items[idx])
      }
    }),
  )
  return out
}

/** POST /ubus crudo (http u https con cert ignorado) — devuelve {status, body} o null. */
function postUbus(host, useHttps) {
  return new Promise((resolve) => {
    const body = JSON.stringify({
      jsonrpc: '2.0',
      id: 1,
      method: 'call',
      params: ['0'.repeat(32), 'session', 'login', { username: 'netpulse-probe', password: '' }],
    })
    const mod = useHttps ? https : http
    const req = mod.request(
      {
        host,
        port: useHttps ? 443 : 80,
        path: '/ubus',
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Content-Length': Buffer.byteLength(body) },
        timeout: HTTP_TIMEOUT_MS,
        rejectUnauthorized: false,
      },
      (res) => {
        let data = ''
        res.on('data', (chunk) => {
          data += chunk
          if (data.length > 8192) req.destroy()
        })
        res.on('end', () => resolve({ status: res.statusCode ?? 0, body: data }))
      },
    )
    req.on('timeout', () => {
      req.destroy()
      resolve(null)
    })
    req.on('error', () => resolve(null))
    req.write(body)
    req.end()
  })
}

/**
 * true si el host huele a OpenWrt/GL.iNet:
 *  1. POST /ubus (http, luego https si hay redirect) con firma JSON-RPC.
 *  2. El firmware GL.iNet NO expone /ubus (nginx lo redirige a su UI) →
 *     firma alternativa: el HTML raíz contiene "gl-ui"/"GL.iNet".
 */
async function probeUbus(host) {
  const isUbus = (r) => r && (r.body.includes('jsonrpc') || r.body.includes('ubus_rpc_session'))
  const httpRes = await postUbus(host, false)
  if (isUbus(httpRes)) return true
  if (httpRes && [301, 302, 307, 308].includes(httpRes.status)) {
    if (isUbus(await postUbus(host, true))) return true
    const root = await getRoot(host, true)
    if (root && /gl-ui|GL\.iNet|glinet/i.test(root.body)) return true
  }
  return false
}

/** GET / (http/https, cert ignorado) — devuelve {status, body} o null. */
function getRoot(host, useHttps) {
  return new Promise((resolve) => {
    const mod = useHttps ? https : http
    const req = mod.request(
      { host, port: useHttps ? 443 : 80, path: '/', method: 'GET', timeout: HTTP_TIMEOUT_MS, rejectUnauthorized: false },
      (res) => {
        let data = ''
        res.on('data', (chunk) => {
          data += chunk
          if (data.length > 16384) req.destroy()
        })
        res.on('end', () => resolve({ status: res.statusCode ?? 0, body: data }))
        res.on('error', () => resolve(null))
      },
    )
    req.on('timeout', () => {
      req.destroy()
      resolve(null)
    })
    req.on('error', () => resolve(null))
    req.end()
  })
}

/** Prueba SSH con la clave propia; devuelve { authorized, model }. */
async function probeSsh(host, sshKeyPath) {
  const { ok, stdout } = await exec(
    'ssh',
    [
      ...sshBaseArgs(sshKeyPath),
      '-o', 'ConnectTimeout=2',
      '-o', 'ControlMaster=no',
      `root@${host}`,
      'ubus call system board | jsonfilter -e @.model',
    ],
    SSH_TIMEOUT_MS,
  )
  if (!ok) return { authorized: false, model: null }
  return { authorized: true, model: stdout.trim() || null }
}

/**
 * Escanea la LAN y devuelve candidatos a router OpenWrt.
 * @param {object} db - handle better-sqlite3 (para marcar los ya configurados)
 * @param {string} sshKeyPath
 * @param {boolean} force - ignora la caché
 */
export async function discoverRouters(db, sshKeyPath, force = false) {
  if (!force && Date.now() - cache.at < CACHE_MS) return { ...cache, cached: true }

  const gwIp = await detectGatewayIp()
  if (!gwIp) return { subnet: null, results: [], cached: false, error: 'no_gateway' }
  const prefix = gwIp.split('.').slice(0, 3).join('.')
  const subnet = `${prefix}.0/24`

  // Hosts con SSH a la vista (barrido TCP :22) — se excluye la propia IP
  const selfIps = new Set(
    (await exec('sh', ['-c', 'ip -o -4 addr show | grep -oP "inet \\K[0-9.]+"'], 3000)).stdout
      .split('\n')
      .filter(Boolean),
  )
  const hosts = Array.from({ length: 254 }, (_, i) => `${prefix}.${i + 1}`).filter((h) => !selfIps.has(h))
  const alive = (
    await pool(hosts, 100, async (h) => ((await tcpOpen(h, 22)) ? h : null))
  ).filter(Boolean)

  // De los vivos, los que huelen a OpenWrt (ubus HTTP)
  const configured = new Set(listRouters(db).map((r) => r.host))
  const candidates = (
    await pool(alive, 16, async (h) => {
      if (!(await probeUbus(h))) return null
      const { authorized, model } = await probeSsh(h, sshKeyPath)
      return {
        host: h,
        isGateway: h === gwIp,
        authorized,
        model,
        configured: configured.has(h),
      }
    })
  ).filter(Boolean)

  candidates.sort((a, b) => Number(b.isGateway) - Number(a.isGateway) || a.host.localeCompare(b.host))
  cache = { at: Date.now(), subnet, results: candidates }
  return { ...cache, cached: false }
}
