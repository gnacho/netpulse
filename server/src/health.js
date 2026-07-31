/**
 * Health endpoints:
 *   GET /api/health (público, contrato): { ok, version, mode, uptimeSec, db }
 *   GET /health (skill): { status, uptime, memory, db }
 */
export function registerHealthRoutes(app, { dbHandle, mode, version }) {
  app.get('/api/health', (c) => {
    const dbOk = dbHandle.checkHealth()
    return c.json({
      ok: dbOk,
      version,
      mode,
      uptimeSec: Math.floor(process.uptime()),
      db: dbOk ? 'ok' : 'error',
    })
  })

  app.get('/health', (c) => {
    const dbOk = dbHandle.checkHealth()
    const mem = process.memoryUsage()
    return c.json({
      status: dbOk ? 'ok' : 'degraded',
      uptime: process.uptime(),
      memory: { rss: mem.rss, heap: mem.heapUsed },
      db: dbOk ? 'connected' : 'error',
    })
  })
}
