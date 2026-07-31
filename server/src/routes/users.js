/**
 * Rutas de gestión de usuarios (solo admin):
 *   GET    /api/users                 → lista [{id, username, role, created_at}]
 *   POST   /api/users                 {username, password, role?} → 201
 *   PUT    /api/users/:id/password    {password} → 204 (invalida sus sesiones)
 *   DELETE /api/users/:id             → 204 (no self, no último admin)
 */
import { z } from 'zod'
import bcrypt from 'bcryptjs'
import { requireAdmin, destroyUserSessions } from '../auth.js'

const usernameSchema = z
  .string()
  .trim()
  .min(2)
  .max(32)
  .regex(/^[a-zA-Z0-9_.-]+$/, 'usuario debe ser alfanumérico (.-_ permitidos)')

const createSchema = z.object({
  username: usernameSchema,
  password: z.string().min(6).max(128),
  role: z.enum(['admin', 'user']).default('user'),
})

const passwordSchema = z.object({
  password: z.string().min(6).max(128),
})

export function registerUserRoutes(app, { db }) {
  app.use('/api/users', requireAdmin())
  app.use('/api/users/*', requireAdmin())

  app.get('/api/users', (c) => {
    const users = db
      .prepare('SELECT id, username, role, created_at FROM users ORDER BY username')
      .all()
    return c.json({ users })
  })

  app.post('/api/users', async (c) => {
    const body = await c.req.json().catch(() => null)
    const parsed = createSchema.safeParse(body)
    if (!parsed.success) {
      return c.json({ error: 'invalid_input', message: parsed.error.issues[0]?.message }, 400)
    }
    const { username, password, role } = parsed.data
    if (db.prepare('SELECT 1 FROM users WHERE username = ?').get(username)) {
      return c.json({ error: 'duplicate_user', message: `Ya existe el usuario ${username}` }, 409)
    }
    const hash = await bcrypt.hash(password, 10)
    const info = db
      .prepare('INSERT INTO users (username, pass_hash, role, created_at) VALUES (?, ?, ?, ?)')
      .run(username, hash, role, Date.now())
    return c.json({ user: { id: info.lastInsertRowid, username, role } }, 201)
  })

  app.put('/api/users/:id/password', async (c) => {
    const id = Number(c.req.param('id'))
    const target = db.prepare('SELECT id, username FROM users WHERE id = ?').get(id)
    if (!target) return c.json({ error: 'not_found' }, 404)
    const body = await c.req.json().catch(() => null)
    const parsed = passwordSchema.safeParse(body)
    if (!parsed.success) {
      return c.json({ error: 'invalid_input', message: 'password mínimo 6 caracteres' }, 400)
    }
    const hash = await bcrypt.hash(parsed.data.password, 10)
    db.prepare('UPDATE users SET pass_hash = ? WHERE id = ?').run(hash, id)
    destroyUserSessions(db, id) // fuerza re-login con la nueva contraseña
    return c.body(null, 204)
  })

  app.put('/api/users/:id/role', (c) => {
    const id = Number(c.req.param('id'))
    const me = c.get('user')
    const target = db.prepare('SELECT id, username, role FROM users WHERE id = ?').get(id)
    if (!target) return c.json({ error: 'not_found' }, 404)
    const role = c.req.query('role') ?? ''
    if (!['admin', 'user'].includes(role)) {
      return c.json({ error: 'invalid_input', message: 'role debe ser admin|user' }, 400)
    }
    if (target.id === me.id) {
      return c.json({ error: 'cannot_change_self', message: 'No puedes cambiar tu propio rol' }, 400)
    }
    if (target.role === 'admin' && role === 'user') {
      const admins = db.prepare("SELECT COUNT(*) AS c FROM users WHERE role = 'admin'").get().c
      if (admins <= 1) {
        return c.json({ error: 'last_admin', message: 'Debe quedar al menos un admin' }, 400)
      }
    }
    db.prepare('UPDATE users SET role = ? WHERE id = ?').run(role, id)
    destroyUserSessions(db, id) // fuerza re-login con el nuevo rol
    return c.body(null, 204)
  })

  app.delete('/api/users/:id', (c) => {
    const id = Number(c.req.param('id'))
    const me = c.get('user')
    const target = db.prepare('SELECT id, username, role FROM users WHERE id = ?').get(id)
    if (!target) return c.json({ error: 'not_found' }, 404)
    if (target.id === me.id) {
      return c.json({ error: 'cannot_delete_self', message: 'No puedes borrar tu propio usuario' }, 400)
    }
    if (target.role === 'admin') {
      const admins = db.prepare("SELECT COUNT(*) AS c FROM users WHERE role = 'admin'").get().c
      if (admins <= 1) {
        return c.json({ error: 'last_admin', message: 'Debe quedar al menos un admin' }, 400)
      }
    }
    destroyUserSessions(db, id)
    db.prepare('DELETE FROM users WHERE id = ?').run(id)
    return c.body(null, 204)
  })
}
