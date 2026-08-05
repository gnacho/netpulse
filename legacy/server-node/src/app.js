/**
 * Ensamblado de la app Hono: seguridad → auth → rutas API → estáticos + SPA.
 * Exportado para tests (createApp con deps inyectadas).
 */
import fs from 'node:fs'
import path from 'node:path'
import { Hono } from 'hono'
import { serveStatic } from '@hono/node-server/serve-static'
import { securityHeaders } from './security.js'
import { requireAuth } from './auth.js'
import { registerHealthRoutes } from './health.js'
import { registerAuthRoutes } from './routes/auth.js'
import { registerDataRoutes } from './routes/data.js'
import { registerConfigRoutes } from './routes/config.js'
import { registerUserRoutes } from './routes/users.js'
import { registerUpdateRoutes } from './routes/update.js'

const VERSION = '1.0.0'

export function createApp({ config, dbHandle, adapter, sse, poller, secret, updater }) {
  const app = new Hono()
  const mode = adapter.mode

  const getOverview = () => poller?.lastOverview ?? null

  // 1. Headers de seguridad (global)
  app.use('*', securityHeaders())

  // 2. Auth: todo /api/* salvo /api/health y /api/auth/login
  app.use('*', requireAuth(dbHandle.db, secret))

  // 3. Rutas API
  registerHealthRoutes(app, { dbHandle, mode, version: VERSION })
  registerAuthRoutes(app, { db: dbHandle.db, config, secret, mode })
  registerDataRoutes(app, { adapter, getOverview })
  registerConfigRoutes(app, { dbHandle, adapter, config })
  registerUserRoutes(app, { db: dbHandle.db })
  if (updater) registerUpdateRoutes(app, { updater })
  app.get('/api/stream', (c) => sse.handleStream(c))

  // 4. 404 JSON para /api/* desconocido
  app.all('/api/*', (c) => c.json({ error: 'not_found' }, 404))

  // 5. Estáticos + SPA fallback (excluye /api/* y /assets/*)
  const staticDir = config.staticDir
  const indexPath = path.join(staticDir, 'index.html')
  let indexHtml = null
  try {
    indexHtml = fs.readFileSync(indexPath, 'utf8')
  } catch {
    console.warn(`[netpulse] STATIC_DIR=${staticDir} sin index.html (¿falta "npm run build" en app/?)`)
  }

  app.use('/*', serveStatic({ root: staticDir }))
  app.get('*', (c) => {
    if (c.req.path.startsWith('/api/')) return c.json({ error: 'not_found' }, 404)
    if (c.req.path.startsWith('/assets/')) return c.notFound()
    if (!indexHtml) return c.text('NetPulse: frontend no compilado (cd app && npm run build)', 503)
    return c.html(indexHtml)
  })

  app.onError((err, c) => {
    console.error('[netpulse] error no controlado:', err)
    return c.json({ error: 'internal_error' }, 500)
  })

  return app
}

export { VERSION }
