/**
 * Rutas de configuración en caliente: CRUD de routers (tabla `routers`),
 * clave SSH propia del servidor y descubrimiento de routers en la LAN.
 * Tras cada mutación se sincroniza el adapter live (setRouters) sin reiniciar.
 */
import { z } from 'zod'
import { listRouters, addRouter, removeRouter } from '../routerstore.js'
import { getPublicKey } from '../sshkey.js'
import { discoverRouters } from '../discover.js'
import { requireAdmin } from '../auth.js'

const hostSchema = z
  .string()
  .trim()
  .min(1)
  .max(253)
  .regex(/^[a-zA-Z0-9._-]+$/, 'host debe ser una IP o hostname válido')

const routerInputSchema = z.object({
  name: z.string().trim().min(1).max(60).optional(),
  host: hostSchema,
  type: z.enum(['glinet', 'openwrt']).default('openwrt'),
  gateway: z.boolean().default(false),
})

export function registerConfigRoutes(app, { dbHandle, adapter, config }) {
  const sync = () => adapter.setRouters?.(listRouters(dbHandle.db))

  // GET /api/config/sshkey — clave pública propia para autorizar en routers
  app.get('/api/config/sshkey', async (c) => {
    const key = await getPublicKey(config.sshKeyPath)
    if (!key) return c.json({ error: 'no_key' }, 500)
    return c.json(key)
  })

  // GET /api/config/discover?force=1 — escaneo de la LAN (cacheado 60 s)
  app.get('/api/config/discover', async (c) => {
    try {
      const result = await discoverRouters(dbHandle.db, config.sshKeyPath, c.req.query('force') === '1')
      return c.json(result)
    } catch (err) {
      return c.json({ error: 'discover_failed', message: err.message }, 500)
    }
  })


  // GET /api/config/routers — lista configurada (no el estado sondeado)
  app.get('/api/config/routers', (c) => {
    return c.json({ routers: listRouters(dbHandle.db) })
  })

  // POST /api/config/routers — añadir router manualmente desde Ajustes
  app.post('/api/config/routers', async (c) => {
    let body
    try {
      body = await c.req.json()
    } catch {
      return c.json({ error: 'invalid_json' }, 400)
    }
    const parsed = routerInputSchema.safeParse(body)
    if (!parsed.success) {
      return c.json({ error: 'invalid_input', message: parsed.error.issues[0]?.message }, 400)
    }
    const { name, host, type, gateway } = parsed.data
    if (listRouters(dbHandle.db).some((r) => r.host === host)) {
      return c.json({ error: 'duplicate_host', message: `Ya hay un router con ${host}` }, 409)
    }
    const created = addRouter(dbHandle.db, { name, host, type, isGateway: gateway })
    sync()
    return c.json({ router: created }, 201)
  })

  // DELETE /api/config/routers/:id
  app.delete('/api/config/routers/:id', (c) => {
    const ok = removeRouter(dbHandle.db, c.req.param('id'))
    if (!ok) return c.json({ error: 'not_found' }, 404)
    sync()
    return c.body(null, 204)
  })

  // --- AdGuard Home (GL.iNet) — solo admin; la contraseña NO se devuelve ---

  app.get('/api/config/adguard', requireAdmin(), (c) => {
    const get = (k) => dbHandle.db.prepare(`SELECT value FROM kv WHERE key = ?`).get(k)?.value ?? ''
    return c.json({
      host: get('adguard_host'),
      user: get('adguard_user') || 'root',
      passSet: Boolean(get('adguard_pass')),
    })
  })

  const adguardSchema = z.object({
    host: hostSchema,
    user: z.string().trim().min(1).max(64).default('root'),
    password: z.string().max(128).optional(),
  })

  app.put('/api/config/adguard', requireAdmin(), async (c) => {
    let body
    try {
      body = await c.req.json()
    } catch {
      return c.json({ error: 'invalid_json' }, 400)
    }
    const parsed = adguardSchema.safeParse(body)
    if (!parsed.success) {
      return c.json({ error: 'invalid_input', message: parsed.error.issues[0]?.message }, 400)
    }
    const upsert = dbHandle.db.prepare(
      'INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value',
    )
    upsert.run('adguard_host', parsed.data.host)
    upsert.run('adguard_user', parsed.data.user)
    if (parsed.data.password) upsert.run('adguard_pass', parsed.data.password)
    return c.body(null, 204)
  })
}
