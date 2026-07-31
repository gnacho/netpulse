/**
 * Tests de auth (contrato + skill):
 *  - login incorrecto → 401 invalid_credentials
 *  - login correcto → 204 + Set-Cookie httpOnly; /api/auth/me con cookie → 200
 *  - overview sin cookie → 401
 *  - rate-limit SQLite: 5º fallo bloquea 5 min → siguiente intento 429 rate_limited
 *  - logout → 204 y la sesión queda revocada
 */
import { describe, it, before, after } from 'node:test'
import assert from 'node:assert/strict'
import { makeTestServer, loginCookie } from './helpers.js'

describe('auth', () => {
  let srv
  before(async () => {
    srv = await makeTestServer()
  })
  after(async () => {
    await srv.close()
  })

  it('rechaza login incorrecto con 401 invalid_credentials', async () => {
    const { status, cookie } = await loginCookie(srv.base, 'wrong-pass')
    assert.equal(status, 401)
    assert.equal(cookie, null)
  })

  it('acepta login correcto: 204 + Set-Cookie httpOnly, y me devuelve user/mode', async () => {
    const { status, cookie, setCookie } = await loginCookie(srv.base)
    assert.equal(status, 204)
    assert.ok(cookie, 'debe devolver cookie de sesión')
    assert.match(setCookie, /HttpOnly/)
    assert.match(setCookie, /SameSite=Lax/)

    const me = await fetch(`${srv.base}/api/auth/me`, { headers: { cookie: `session=${cookie}` } })
    assert.equal(me.status, 200)
    const body = await me.json()
    assert.deepEqual(body, { user: 'admin', role: 'admin', language: 'auto', mode: 'demo' })
  })

  it('protege /api/overview sin cookie (401) y la sirve con cookie (200)', async () => {
    const unauth = await fetch(`${srv.base}/api/overview`)
    assert.equal(unauth.status, 401)

    const { cookie } = await loginCookie(srv.base)
    const auth = await fetch(`${srv.base}/api/overview`, { headers: { cookie: `session=${cookie}` } })
    assert.equal(auth.status, 200)
  })

  it('logout: 204 y la sesión queda revocada', async () => {
    const { cookie } = await loginCookie(srv.base)
    const out = await fetch(`${srv.base}/api/auth/logout`, {
      method: 'POST',
      headers: { cookie: `session=${cookie}` },
    })
    assert.equal(out.status, 204)
    const after_ = await fetch(`${srv.base}/api/overview`, { headers: { cookie: `session=${cookie}` } })
    assert.equal(after_.status, 401)
  })

  it('rate-limit: bloquea tras el 5º fallo (429 rate_limited con retryAfterSec)', async () => {
    const ip = '10.9.9.9'
    const headers = { 'Content-Type': 'application/json', 'x-forwarded-for': ip }
    const bad = () =>
      fetch(`${srv.base}/api/auth/login`, {
        method: 'POST',
        headers,
        body: JSON.stringify({ username: 'admin', password: 'nope' }),
      })
    for (let i = 1; i <= 5; i++) {
      const res = await bad()
      assert.equal(res.status, 401, `fallo ${i} debe ser 401`)
    }
    // 6º intento: ya bloqueado (5 min)
    const blocked = await bad()
    assert.equal(blocked.status, 429)
    const body = await blocked.json()
    assert.equal(body.error, 'rate_limited')
    assert.ok(body.retryAfterSec > 0 && body.retryAfterSec <= 300)
    // Incluso con la password correcta sigue bloqueado
    const goodWhileLocked = await fetch(`${srv.base}/api/auth/login`, {
      method: 'POST',
      headers,
      body: JSON.stringify({ username: 'admin', password: 'test1234' }),
    })
    assert.equal(goodWhileLocked.status, 429)
  })

  it('el rate-limit persiste en SQLite (no solo memoria)', async () => {
    const row = srv.dbHandle.db.prepare('SELECT * FROM login_attempts WHERE ip = ?').get('10.9.9.9')
    assert.ok(row, 'debe existir fila en login_attempts')
    assert.ok(row.attempts >= 5)
    assert.ok(row.locked_until > Date.now())
  })
})
