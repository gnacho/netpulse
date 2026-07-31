/**
 * Rutas de datos (contrato API v1). Todas bajo requireAuth, Cache-Control:
 * no-store, errores { error, message? }. Paginación obligatoria (skill).
 */
import { z } from 'zod'

const BANDS = ['5 GHz', '2.4 GHz', 'cable']
const SEVERITIES = ['warn', 'critical', 'info', 'ok']

const pageSchema = z.object({
  page: z.coerce.number().int().min(1).default(1),
  pageSize: z.coerce.number().int().min(1).max(1000).default(50),
})

function paginate(items, page, pageSize) {
  const total = items.length
  const start = (page - 1) * pageSize
  return { items: items.slice(start, start + pageSize), total, page, pageSize }
}

/** Middleware: Cache-Control: no-store en todas las respuestas de datos. */
function noStore() {
  return async (c, next) => {
    c.header('Cache-Control', 'no-store')
    await next()
  }
}

export function registerDataRoutes(app, { adapter, getOverview }) {
  app.use('/api/*', noStore())

  // GET /api/overview — bundle Home + Layout + stream SSE
  app.get('/api/overview', async (c) => {
    const overview = getOverview() ?? (await adapter.getOverview())
    return c.json(overview)
  })

  // GET /api/routers
  app.get('/api/routers', (c) => {
    return c.json({ routers: adapter.getRouters() })
  })

  // GET /api/routers/:id
  app.get('/api/routers/:id', async (c) => {
    const detail = await adapter.getRouterDetail(c.req.param('id'))
    if (!detail) return c.json({ error: 'not_found' }, 404)
    return c.json(detail)
  })

  // GET /api/devices?q=&routerId=&band=&type=&status=&page=1&pageSize=50
  app.get('/api/devices', (c) => {
    const parsed = pageSchema.safeParse({
      page: c.req.query('page'),
      pageSize: c.req.query('pageSize'),
    })
    if (!parsed.success) return c.json({ error: 'invalid_query', message: 'page/pageSize inválidos' }, 400)
    const { page, pageSize } = parsed.data
    const q = (c.req.query('q') || '').trim().toLowerCase().slice(0, 100)
    const routerId = c.req.query('routerId') || ''
    const band = c.req.query('band') || ''
    const type = c.req.query('type') || ''
    const status = c.req.query('status') || ''

    if (band && !BANDS.includes(band)) {
      return c.json({ error: 'invalid_query', message: `band debe ser una de: ${BANDS.join(', ')}` }, 400)
    }
    if (status && !['online', 'offline'].includes(status)) {
      return c.json({ error: 'invalid_query', message: 'status debe ser online|offline' }, 400)
    }

    let items = adapter.getDevices()
    if (routerId) items = items.filter((d) => d.routerId === routerId)
    if (band) items = items.filter((d) => d.band === band)
    if (type) items = items.filter((d) => d.type === type || d.group === type)
    if (status) items = items.filter((d) => (status === 'online' ? d.online : !d.online))
    if (q) {
      items = items.filter((d) =>
        [d.name, d.hostname, d.ip, d.mac, d.manufacturer]
          .filter(Boolean)
          .some((v) => String(v).toLowerCase().includes(q)),
      )
    }
    return c.json(paginate(items, page, pageSize))
  })

  // GET /api/alerts?severity=&page=1&pageSize=20
  app.get('/api/alerts', (c) => {
    const parsed = pageSchema.safeParse({
      page: c.req.query('page'),
      pageSize: c.req.query('pageSize') ?? 20,
    })
    if (!parsed.success) return c.json({ error: 'invalid_query', message: 'page/pageSize inválidos' }, 400)
    const { page, pageSize } = parsed.data
    const severity = c.req.query('severity') || ''
    if (severity && !SEVERITIES.includes(severity)) {
      return c.json({ error: 'invalid_query', message: `severity debe ser una de: ${SEVERITIES.join(', ')}` }, 400)
    }
    let items = adapter.getAlerts()
    if (severity) items = items.filter((a) => a.severity === severity)
    return c.json(paginate(items, page, pageSize))
  })

  // GET /api/topology
  app.get('/api/topology', (c) => {
    return c.json({ routers: adapter.getRouters(), devices: adapter.getDevices() })
  })
}
