/**
 * Rutas del actualizador (solo admin):
 *   GET  /api/update/status → estado actual (versión, última, disponible, progreso)
 *   POST /api/update/check  → fuerza un chequeo contra GitHub
 *   POST /api/update/apply  → aplica la actualización (git pull + build + restart)
 */
import { requireAdmin } from '../auth.js'

export function registerUpdateRoutes(app, { updater }) {
  app.use('/api/update', requireAdmin())
  app.use('/api/update/*', requireAdmin())

  app.get('/api/update/status', (c) => c.json(updater.status))

  app.post('/api/update/check', async (c) => {
    await updater.check()
    return c.json(updater.status)
  })

  app.post('/api/update/apply', (c) => {
    const started = updater.apply()
    if (!started) return c.json({ error: 'already_updating' }, 409)
    return c.json(updater.status, 202)
  })
}
