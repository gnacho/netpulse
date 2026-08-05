/**
 * Almacén de routers configurados (tabla `routers` de SQLite) + bootstrap:
 *
 *  1. Si la tabla tiene filas → es la fuente de verdad.
 *  2. Si está vacía y hay ROUTERS_JSON en el entorno → se siembra desde ahí.
 *  3. Si sigue vacía y NO estamos en demo forzado → autodetección de la
 *     puerta de enlace: `ip route show default` en el propio servidor y
 *     sondeo SSH (`ubus call system board`) para averiguar modelo y tipo
 *     (GL.iNet vs OpenWrt genérico). El resto de routers se añaden a mano
 *     desde Ajustes (CRUD /api/config/routers).
 *
 * La tabla NACE VACÍA al importar el código: no hay datos de semilla.
 */
import { execFile } from 'node:child_process'
import { sshBaseArgs } from './sshkey.js'

const PROBE_TIMEOUT_MS = 4000

export function listRouters(db) {
  return db
    .prepare('SELECT id, name, host, type, is_gateway, created_at FROM routers ORDER BY is_gateway DESC, created_at ASC')
    .all()
    .map((r) => ({ ...r, is_gateway: r.is_gateway === 1 }))
}

function slugify(text) {
  return String(text)
    .toLowerCase()
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '')
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 32)
}

/** Genera un id único a partir del nombre o del host. */
function uniqueId(db, name, host) {
  const base = slugify(name || '') || `router-${String(host).split('.').pop()}`
  let id = base || 'router'
  let n = 2
  while (db.prepare('SELECT 1 FROM routers WHERE id = ?').get(id)) {
    id = `${base}-${n++}`
  }
  return id
}

/**
 * Inserta un router y devuelve la fila creada.
 * Si `isGateway`, el resto pierde el flag (solo un gateway).
 */
export function addRouter(db, { name, host, type = 'openwrt', isGateway = false }) {
  const id = uniqueId(db, name, host)
  const now = Date.now()
  const tx = db.transaction(() => {
    if (isGateway) db.prepare('UPDATE routers SET is_gateway = 0').run()
    db.prepare('INSERT INTO routers (id, name, host, type, is_gateway, created_at) VALUES (?, ?, ?, ?, ?, ?)').run(
      id,
      name || host,
      host,
      type,
      isGateway ? 1 : 0,
      now,
    )
  })
  tx()
  return listRouters(db).find((r) => r.id === id)
}

export function removeRouter(db, id) {
  return db.prepare('DELETE FROM routers WHERE id = ?').run(id).changes > 0
}

// ---------------------------------------------------------------------------
// Autodetección de la puerta de enlace
// ---------------------------------------------------------------------------

function exec(cmd, args, timeoutMs = PROBE_TIMEOUT_MS) {
  return new Promise((resolve, reject) => {
    execFile(cmd, args, { timeout: timeoutMs, maxBuffer: 1024 * 1024 }, (err, stdout, stderr) => {
      if (err) return reject(new Error(`${cmd}: ${err.message} ${stderr || ''}`.trim()))
      resolve(stdout)
    })
  })
}

/** IP de la puerta de enlace por defecto del servidor (null si no se sabe). */
export async function detectGatewayIp() {
  try {
    const out = await exec('ip', ['route', 'show', 'default'])
    const m = /default via (\d+\.\d+\.\d+\.\d+)/.exec(out)
    return m ? m[1] : null
  } catch {
    return null
  }
}

/** Sondea el modelo del gateway por SSH (ubus system board). */
async function probeGatewayModel(host, sshKeyPath) {
  try {
    const out = await exec(
      'ssh',
      [
        ...sshBaseArgs(sshKeyPath),
        '-o', 'ConnectTimeout=3',
        '-o', 'ControlMaster=no',
        `root@${host}`,
        'ubus call system board',
      ],
      PROBE_TIMEOUT_MS + 1000,
    )
    const board = JSON.parse(out)
    const model = board?.model || ''
    const isGlinet = /GL[.-]?iNet|GL-[A-Z]/i.test(model) || /glinet/i.test(board?.release?.distribution || '')
    return { model, type: isGlinet ? 'glinet' : 'openwrt' }
  } catch {
    return { model: '', type: 'openwrt' }
  }
}

/**
 * Bootstrap de la tabla routers. Devuelve la lista final configurada.
 * @param {object} db - handle better-sqlite3
 * @param {object} config - config cargada (routers de ROUTERS_JSON, sshKeyPath, demoMode)
 * @param {(msg: string) => void} log
 */
export async function ensureInitialRouters(db, config, log = console.log) {
  const existing = listRouters(db)
  if (existing.length > 0) return existing

  // 1. Semilla desde ROUTERS_JSON (primer arranque)
  if (config.routers.length > 0) {
    for (const [i, r] of config.routers.entries()) {
      addRouter(db, { name: r.name, host: r.host, type: r.type, isGateway: r.type === 'glinet' || i === 0 })
    }
    log(`[netpulse] routers sembrados desde ROUTERS_JSON (${config.routers.length})`)
    return listRouters(db)
  }

  // 2. Autodetección de la puerta de enlace (nunca en demo forzado)
  if (config.demoMode) return []

  const gwIp = await detectGatewayIp()
  if (!gwIp) {
    log('[netpulse] sin puerta de enlace detectada; añade routers desde Ajustes')
    return []
  }
  const probe = await probeGatewayModel(gwIp, config.sshKeyPath)
  const created = addRouter(db, {
    name: probe.model || 'Gateway',
    host: gwIp,
    type: probe.type,
    isGateway: true,
  })
  log(
    `[netpulse] gateway autodetectado: ${created.host} (${probe.model || 'modelo desconocido'}, tipo ${created.type}). El resto se añade desde Ajustes.`,
  )
  return listRouters(db)
}
