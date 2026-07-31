/**
 * Auth single-user (decisión tomada: AUTH_USER/AUTH_PASS en .env).
 * - Cookie `session` = `id.hmac` (HMAC-SHA256), httpOnly, SameSite=Lax, 30d.
 *   Secure: siempre que la petición sea HTTPS (auto) o según COOKIE_SECURE.
 * - Secret HMAC: SESSION_SECRET o autogenerado persistido en kv.
 * - Password: hash bcryptjs en kv tras primer login — NUNCA texto plano
 *   persistido. Se guarda también sha256(AUTH_PASS) para detectar cambios de
 *   la password en .env y re-hashear (el hash viejo no se reutiliza).
 * - Rate-limit en SQLite (login_attempts): bloqueo 5 min tras 5 fallos
 *   (bloquea cuando attempts >= 4, ver registerLoginFail).
 * - Rotación de sesión tras login: se invalida la sesión anterior si existía.
 */
import crypto from 'node:crypto'
import bcrypt from 'bcryptjs'

export const SESSION_COOKIE = 'session'
export const SESSION_TTL_MS = 30 * 24 * 60 * 60 * 1000 // 30 días
const LOCK_MS = 5 * 60 * 1000 // 5 minutos

// ---------------------------------------------------------------------------
// Usuarios (multiusuario): seed admin desde .env si la tabla está vacía
// ---------------------------------------------------------------------------

/** Crea el admin inicial (AUTH_USER/AUTH_PASS) cuando no hay ningún usuario. */
export async function ensureUsers(db, config) {
  const count = db.prepare('SELECT COUNT(*) AS c FROM users').get().c
  if (count > 0) return
  const hash = await bcrypt.hash(config.authPass, 10)
  db.prepare('INSERT INTO users (username, pass_hash, role, created_at) VALUES (?, ?, ?, ?)').run(
    config.authUser,
    hash,
    'admin',
    Date.now(),
  )
  console.log(`[netpulse] usuario admin inicial creado: ${config.authUser}`)
}

export function getUserByName(db, username) {
  return db.prepare('SELECT id, username, pass_hash, role FROM users WHERE username = ?').get(username)
}

export function getUserById(db, id) {
  return db.prepare('SELECT id, username, role FROM users WHERE id = ?').get(id)
}

// ---------------------------------------------------------------------------
// Secret HMAC y hash de password
// ---------------------------------------------------------------------------

export function ensureSessionSecret(db, config) {
  if (config.sessionSecret) return config.sessionSecret
  const row = db.prepare('SELECT value FROM kv WHERE key = ?').get('session_secret')
  if (row?.value) return row.value
  const secret = crypto.randomBytes(32).toString('hex')
  db.prepare('INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value').run(
    'session_secret',
    secret,
  )
  return secret
}

/**
 * Devuelve el hash bcrypt de AUTH_PASS, generándolo y persistiéndolo en kv la
 * primera vez (o cuando AUTH_PASS cambia en .env).
 */
export async function ensurePasswordHash(db, config) {
  const envFingerprint = crypto.createHash('sha256').update(config.authPass).digest('hex')
  const storedHash = db.prepare('SELECT value FROM kv WHERE key = ?').get('auth_pass_hash')?.value
  const storedFp = db.prepare('SELECT value FROM kv WHERE key = ?').get('auth_pass_fp')?.value
  if (storedHash?.startsWith('$2') && storedFp === envFingerprint) return storedHash
  const hash = await bcrypt.hash(config.authPass, 10)
  const upsert = db.prepare(
    'INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value',
  )
  upsert.run('auth_pass_hash', hash)
  upsert.run('auth_pass_fp', envFingerprint)
  return hash
}

// ---------------------------------------------------------------------------
// Cookie id.hmac
// ---------------------------------------------------------------------------

function hmac(secret, id) {
  return crypto.createHmac('sha256', secret).update(id).digest('hex')
}

function safeEqual(a, b) {
  const ba = Buffer.from(String(a))
  const bb = Buffer.from(String(b))
  return ba.length === bb.length && crypto.timingSafeEqual(ba, bb)
}

export function signSessionId(secret, id) {
  return `${id}.${hmac(secret, id)}`
}

export function verifySessionCookie(secret, value) {
  if (!value || typeof value !== 'string') return null
  const dot = value.lastIndexOf('.')
  if (dot <= 0) return null
  const id = value.slice(0, dot)
  const sig = value.slice(dot + 1)
  if (!safeEqual(sig, hmac(secret, id))) return null
  return id
}

export function isSecureRequest(c) {
  if (c.req.url.startsWith('https://')) return true
  return c.req.header('x-forwarded-proto')?.split(',')[0].trim() === 'https'
}

export function cookieSecureFlag(config, c) {
  if (config.cookieSecure === 'always') return true
  if (config.cookieSecure === 'never') return false
  return isSecureRequest(c) // auto
}

export function buildSessionCookie(config, c, signedId, maxAgeSec) {
  const parts = [
    `${SESSION_COOKIE}=${signedId}`,
    'Path=/',
    'HttpOnly',
    'SameSite=Lax',
    `Max-Age=${maxAgeSec}`,
  ]
  if (cookieSecureFlag(config, c)) parts.push('Secure')
  return parts.join('; ')
}

// ---------------------------------------------------------------------------
// Sesiones en SQLite
// ---------------------------------------------------------------------------

export function createSession(db, ua, userId = null) {
  const id = crypto.randomUUID()
  const now = Date.now()
  db.prepare('INSERT INTO sessions (id, created_at, expires_at, ua, user_id) VALUES (?, ?, ?, ?, ?)').run(
    id,
    now,
    now + SESSION_TTL_MS,
    (ua || '').slice(0, 255),
    userId,
  )
  return id
}

export function destroySession(db, id) {
  if (id) db.prepare('DELETE FROM sessions WHERE id = ?').run(id)
}

export function destroyUserSessions(db, userId) {
  db.prepare('DELETE FROM sessions WHERE user_id = ?').run(userId)
}

export function getSession(db, id) {
  if (!id) return null
  const row = db.prepare('SELECT id, expires_at, user_id FROM sessions WHERE id = ?').get(id)
  if (!row) return null
  if (row.expires_at < Date.now()) {
    destroySession(db, id)
    return null
  }
  return row
}

/** Sesión válida + usuario asociado (null si la sesión es antigua sin user). */
export function sessionUserFromRequest(db, secret, c) {
  const header = c.req.header('cookie') || ''
  const match = /(?:^|;\s*)session=([^;]+)/.exec(header)
  const id = verifySessionCookie(secret, match?.[1])
  const session = id && getSession(db, id)
  if (!session || !session.user_id) return null
  const user = getUserById(db, session.user_id)
  return user ? { sessionId: id, user } : null
}

export function sessionIdFromRequest(db, secret, c) {
  const header = c.req.header('cookie') || ''
  const match = /(?:^|;\s*)session=([^;]+)/.exec(header)
  const id = verifySessionCookie(secret, match?.[1])
  return id && getSession(db, id) ? id : null
}

// ---------------------------------------------------------------------------
// Rate-limit en SQLite (persiste tras reinicios)
// ---------------------------------------------------------------------------

export function clientIp(c) {
  return (
    c.req.header('x-forwarded-for')?.split(',')[0].trim() ||
    c.env?.incoming?.socket?.remoteAddress ||
    'unknown'
  )
}

/** true si la IP está bloqueada; devuelve también segundos restantes. */
export function loginRateLimited(db, c) {
  const ip = clientIp(c)
  const row = db.prepare('SELECT locked_until FROM login_attempts WHERE ip = ?').get(ip)
  if (!row) return { limited: false, retryAfterSec: 0 }
  const remaining = row.locked_until - Date.now()
  if (remaining > 0) return { limited: true, retryAfterSec: Math.ceil(remaining / 1000) }
  return { limited: false, retryAfterSec: 0 }
}

/** Registra un fallo: bloquea 5 min cuando se alcanza el 5º fallo (attempts >= 4). */
export function registerLoginFail(db, c) {
  const ip = clientIp(c)
  db.prepare(`
    INSERT INTO login_attempts (ip, attempts, locked_until)
    VALUES (?, 1, 0)
    ON CONFLICT(ip) DO UPDATE SET
      attempts = attempts + 1,
      locked_until = CASE
        WHEN attempts >= 4 THEN ?
        ELSE locked_until
      END
  `).run(ip, Date.now() + LOCK_MS)
}

export function loginOk(db, c) {
  db.prepare('DELETE FROM login_attempts WHERE ip = ?').run(clientIp(c))
}

// ---------------------------------------------------------------------------
// Login / logout
// ---------------------------------------------------------------------------

/**
 * Valida credenciales contra la tabla users y crea sesión nueva
 * (rotación: invalida la anterior).
 * @returns { id, user } o null si credenciales inválidas.
 */
export async function handleLogin(db, config, secret, c, body) {
  const { username, password } = body || {}
  if (!username || !password) return null
  const user = getUserByName(db, username)
  if (!user) return null
  const valid = await bcrypt.compare(password, user.pass_hash)
  if (!valid) return null
  // Rotación de sesión: invalida la cookie previa si existía
  const prevId = sessionIdFromRequest(db, secret, c)
  if (prevId) destroySession(db, prevId)
  const id = createSession(db, c.req.header('user-agent'), user.id)
  return { id, user: { id: user.id, username: user.username, role: user.role } }
}

export function handleLogout(db, secret, c) {
  const id = sessionIdFromRequest(db, secret, c)
  if (id) destroySession(db, id)
}

// ---------------------------------------------------------------------------
// Middleware requireAuth: todo /api/* salvo login y health
// ---------------------------------------------------------------------------

export function requireAuth(db, secret) {
  return async (c, next) => {
    const path = c.req.path
    if (!path.startsWith('/api/')) return next()
    if (path === '/api/health' || path === '/api/auth/login') return next()
    const sess = sessionUserFromRequest(db, secret, c)
    if (!sess) return c.json({ error: 'unauthorized' }, 401)
    c.set('sessionId', sess.sessionId)
    c.set('user', sess.user)
    return next()
  }
}

/** Middleware admin: tras requireAuth, exige role === 'admin'. */
export function requireAdmin() {
  return async (c, next) => {
    const user = c.get('user')
    if (!user || user.role !== 'admin') return c.json({ error: 'forbidden' }, 403)
    return next()
  }
}
