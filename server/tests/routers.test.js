/**
 * Tests del CRUD de routers (/api/config/routers):
 *  - Auth obligatoria (401 sin cookie).
 *  - Alta, listado, duplicados (409), validación (400) y borrado (204/404).
 *  - La tabla nace VACÍA (regla de la skill: BD sin datos al importar).
 */
import { describe, it, before, after } from 'node:test'
import assert from 'node:assert/strict'
import { makeTestServer, loginCookie } from './helpers.js'

describe('config routers', () => {
  let srv
  let cookie

  before(async () => {
    srv = await makeTestServer()
    const login = await loginCookie(srv.base)
    assert.equal(login.status, 204)
    cookie = login.cookie
  })

  after(async () => {
    await srv.close()
  })

  it('requiere auth', async () => {
    const res = await fetch(`${srv.base}/api/config/routers`)
    assert.equal(res.status, 401)
  })

  it('la tabla nace vacía y admite alta', async () => {
    let res = await fetch(`${srv.base}/api/config/routers`, {
      headers: { cookie: `session=${cookie}` },
    })
    assert.equal(res.status, 200)
    assert.deepEqual((await res.json()).routers, [])

    res = await fetch(`${srv.base}/api/config/routers`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', cookie: `session=${cookie}` },
      body: JSON.stringify({ name: 'Gateway', host: '192.168.1.1', type: 'glinet', gateway: true }),
    })
    assert.equal(res.status, 201)
    const { router } = await res.json()
    assert.equal(router.host, '192.168.1.1')
    assert.equal(router.is_gateway, true)

    res = await fetch(`${srv.base}/api/config/routers`, {
      headers: { cookie: `session=${cookie}` },
    })
    const { routers } = await res.json()
    assert.equal(routers.length, 1)
    assert.equal(routers[0].id, 'gateway')
  })

  it('rechaza duplicados y entradas inválidas', async () => {
    let res = await fetch(`${srv.base}/api/config/routers`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', cookie: `session=${cookie}` },
      body: JSON.stringify({ host: '192.168.1.1' }),
    })
    assert.equal(res.status, 409)

    res = await fetch(`${srv.base}/api/config/routers`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', cookie: `session=${cookie}` },
      body: JSON.stringify({ host: '192.168.1.2; rm -rf /' }),
    })
    assert.equal(res.status, 400)

    res = await fetch(`${srv.base}/api/config/routers`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', cookie: `session=${cookie}` },
      body: JSON.stringify({ host: '192.168.1.2', type: 'cisco' }),
    })
    assert.equal(res.status, 400)
  })

  it('solo un gateway: el nuevo desbanca al anterior', async () => {
    const res = await fetch(`${srv.base}/api/config/routers`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', cookie: `session=${cookie}` },
      body: JSON.stringify({ name: 'Patio', host: '192.168.1.3', gateway: true }),
    })
    assert.equal(res.status, 201)
    const list = await (
      await fetch(`${srv.base}/api/config/routers`, { headers: { cookie: `session=${cookie}` } })
    ).json()
    const gateways = list.routers.filter((r) => r.is_gateway)
    assert.equal(gateways.length, 1)
    assert.equal(gateways[0].host, '192.168.1.3')
  })

  it('borra y devuelve 404 si no existe', async () => {
    const list = await (
      await fetch(`${srv.base}/api/config/routers`, { headers: { cookie: `session=${cookie}` } })
    ).json()
    for (const r of list.routers) {
      const res = await fetch(`${srv.base}/api/config/routers/${r.id}`, {
        method: 'DELETE',
        headers: { cookie: `session=${cookie}` },
      })
      assert.equal(res.status, 204)
    }
    const res = await fetch(`${srv.base}/api/config/routers/no-existe`, {
      method: 'DELETE',
      headers: { cookie: `session=${cookie}` },
    })
    assert.equal(res.status, 404)
    const after = await (
      await fetch(`${srv.base}/api/config/routers`, { headers: { cookie: `session=${cookie}` } })
    ).json()
    assert.deepEqual(after.routers, [])
  })
})
