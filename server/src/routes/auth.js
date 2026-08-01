/**
 * Rutas de autenticación (contrato):
 *   POST /api/auth/login   {username, password} → 204 + Set-Cookie · 401 · 429
 *   POST /api/auth/logout  → 204 (borra cookie)
 *   GET  /api/auth/me      → 200 {user, role, mode} · 401 (requireAuth)
 */
import { z } from 'zod'
import bcrypt from 'bcryptjs'
import {
  SESSION_COOKIE,
  SESSION_TTL_MS,
  buildSessionCookie,
  handleLogin,
  handleLogout,
  loginOk,
  loginRateLimited,
  registerLoginFail,
  sessionIdFromRequest,
  signSessionId,
} from '../auth.js'

const loginSchema = z.object({
  username: z.string().min(1).max(64),
  password: z.string().min(1),
})

const ownPasswordSchema = z.object({
  current: z.string().min(1),
  password: z.string().min(6).max(128),
})

export function registerAuthRoutes(app, { db, config, secret, mode }) {
  app.post('/api/auth/login', async (c) => {
    const { limited, retryAfterSec } = loginRateLimited(db, c)
    if (limited) {
      return c.json({ error: 'rate_limited', retryAfterSec }, 429)
    }
    const body = await c.req.json().catch(() => null)
    const parsed = loginSchema.safeParse(body)
    if (!parsed.success) {
      return c.json({ error: 'invalid_body', message: 'Se esperaba { "username": string, "password": string }' }, 400)
    }
    const result = await handleLogin(db, config, secret, c, parsed.data)
    if (!result) {
      registerLoginFail(db, c)
      return c.json({ error: 'invalid_credentials' }, 401)
    }
    loginOk(db, c)
    const signed = signSessionId(secret, result.id)
    c.header('Set-Cookie', buildSessionCookie(config, c, signed, Math.floor(SESSION_TTL_MS / 1000)))
    return c.body(null, 204)
  })

  app.post('/api/auth/logout', (c) => {
    handleLogout(db, secret, c)
    // Borra la cookie (mismos atributos para que el navegador la reemplace)
    c.header('Set-Cookie', `${SESSION_COOKIE}=; Path=/; HttpOnly; SameSite=Lax; Max-Age=0`)
    return c.body(null, 204)
  })

  // Cambio de la propia contraseña (verifica la actual, cierra las demás sesiones)
  app.put('/api/auth/password', async (c) => {
    const user = c.get('user')
    const body = await c.req.json().catch(() => null)
    const parsed = ownPasswordSchema.safeParse(body)
    if (!parsed.success) {
      return c.json({ error: 'invalid_input', message: 'password mínimo 6 caracteres' }, 400)
    }
    const row = db.prepare('SELECT id, pass_hash FROM users WHERE id = ?').get(user.id)
    if (!row) return c.json({ error: 'not_found' }, 404)
    const ok = await bcrypt.compare(parsed.data.current, row.pass_hash)
    if (!ok) return c.json({ error: 'invalid_current', message: 'La contraseña actual no es correcta' }, 403)
    const hash = await bcrypt.hash(parsed.data.password, 10)
    db.prepare('UPDATE users SET pass_hash = ? WHERE id = ?').run(hash, user.id)
    const sid = sessionIdFromRequest(db, secret, c)
    db.prepare('DELETE FROM sessions WHERE user_id = ? AND id != ?').run(user.id, sid ?? '')
    return c.body(null, 204)
  })

  app.get('/api/auth/me', (c) => {
    const user = c.get('user')
    const row = db.prepare('SELECT language FROM users WHERE id = ?').get(user.id)
    return c.json({ user: user.username, role: user.role, language: row?.language ?? 'auto', mode })
  })
}
