/**
 * Tests de paginación y filtros de /api/devices (paginación obligatoria, skill).
 */
import { describe, it, before, after } from 'node:test'
import assert from 'node:assert/strict'
import { makeTestServer, loginCookie } from './helpers.js'

describe('GET /api/devices', () => {
  let srv
  let cookie
  before(async () => {
    srv = makeTestServer()
    ;({ cookie } = await loginCookie(srv.base))
  })
  after(async () => {
    await srv.close()
  })

  const get = (qs) =>
    fetch(`${srv.base}/api/devices?${qs}`, { headers: { cookie: `session=${cookie}` } }).then((r) => r.json())

  it('devuelve los 47 clientes paginados (page 1 de 10)', async () => {
    const body = await get('page=1&pageSize=10')
    assert.equal(body.total, 47)
    assert.equal(body.items.length, 10)
    assert.equal(body.page, 1)
    assert.equal(body.pageSize, 10)
  })

  it('última página con resto (page 5 de 10 → 7 items)', async () => {
    const body = await get('page=5&pageSize=10')
    assert.equal(body.total, 47)
    assert.equal(body.items.length, 7)
    // Sin solape con la página anterior
    const prev = await get('page=4&pageSize=10')
    const prevIds = new Set(prev.items.map((d) => d.id))
    assert.ok(body.items.every((d) => !prevIds.has(d.id)))
  })

  it('filtros: routerId, band, status, type y búsqueda q', async () => {
    const living = await get('routerId=living&pageSize=200')
    assert.equal(living.total, 18)
    assert.ok(living.items.every((d) => d.routerId === 'living'))

    const cable = await get('band=cable&pageSize=200')
    assert.ok(cable.items.every((d) => d.band === 'cable'))

    const offline = await get('status=offline&pageSize=200')
    assert.equal(offline.total, 8)
    assert.ok(offline.items.every((d) => !d.online))

    const moviles = await get('type=movil&pageSize=200')
    assert.ok(moviles.items.every((d) => d.type === 'movil' || d.group === 'moviles'))

    const q = await get('q=pixel&pageSize=200')
    assert.ok(q.total >= 2, 'pixel-8-pro y pixel-7')
    assert.ok(q.items.some((d) => d.id === 'pixel-8-pro'))
  })

  it('valida parámetros (band inválida → 400)', async () => {
    const res = await fetch(`${srv.base}/api/devices?band=wifi6`, { headers: { cookie: `session=${cookie}` } })
    assert.equal(res.status, 400)
    const body = await res.json()
    assert.equal(body.error, 'invalid_query')
  })

  it('los items llevan el shape Device (camelCase del contrato)', async () => {
    const body = await get('page=1&pageSize=1')
    const d = body.items[0]
    for (const key of ['id', 'name', 'type', 'manufacturer', 'ip', 'mac', 'routerId', 'band', 'signalDbm', 'trafficMbps', 'online', 'sparkline']) {
      assert.ok(key in d, `device sin ${key}`)
    }
  })
})
