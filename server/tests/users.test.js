/**
 * Tests del CRUD de usuarios (/api/users, solo admin):
 *  - 403 para no-admin y 401 sin sesión.
 *  - Alta (201), duplicado (409), validación (400).
 *  - Cambio de password (204, invalida sesiones), borrado (204),
 *    protección self/último admin (400).
 */
import { describe, it, before, after } from 'node:test'
import assert from 'node:assert/strict'
import { makeTestServer, loginCookie } from './helpers.js'

describe('users CRUD', () => {
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

  it('requiere auth y rol admin', async () => {
    let res = await fetch(`${srv.base}/api/users`)
    assert.equal(res.status, 401)
    // admin sí
    res = await fetch(`${srv.base}/api/users`, { headers: { cookie: `session=${cookie}` } })
    assert.equal(res.status, 200)
    const { users } = await res.json()
    assert.equal(users.length, 1)
    assert.equal(users[0].username, 'admin')
    assert.equal(users[0].role, 'admin')
  })

  it('alta, duplicado y validación', async () => {
    let res = await fetch(`${srv.base}/api/users`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', cookie: `session=${cookie}` },
      body: JSON.stringify({ username: 'ana', password: 'secreto1' }),
    })
    assert.equal(res.status, 201)

    res = await fetch(`${srv.base}/api/users`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', cookie: `session=${cookie}` },
      body: JSON.stringify({ username: 'ana', password: 'otra1234' }),
    })
    assert.equal(res.status, 409)

    res = await fetch(`${srv.base}/api/users`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', cookie: `session=${cookie}` },
      body: JSON.stringify({ username: 'mal usuario', password: 'x'.repeat(10) }),
    })
    assert.equal(res.status, 400)
  })

  it('el usuario creado puede hacer login; cambio de password invalida su sesión', async () => {
    let login = await loginCookie(srv.base, 'secreto1', 'ana')
    assert.equal(login.status, 204)

    const list = await (
      await fetch(`${srv.base}/api/users`, { headers: { cookie: `session=${cookie}` } })
    ).json()
    const ana = list.users.find((u) => u.username === 'ana')

    const res = await fetch(`${srv.base}/api/users/${ana.id}/password`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json', cookie: `session=${cookie}` },
      body: JSON.stringify({ password: 'nueva-clave-9' }),
    })
    assert.equal(res.status, 204)

    // La sesión vieja de ana quedó invalidada
    const me = await fetch(`${srv.base}/api/auth/me`, { headers: { cookie: `session=${login.cookie}` } })
    assert.equal(me.status, 401)
    // Y entra con la nueva
    login = await loginCookie(srv.base, 'nueva-clave-9', 'ana')
    assert.equal(login.status, 204)
    // Un no-admin no puede listar usuarios
    const forbidden = await fetch(`${srv.base}/api/users`, { headers: { cookie: `session=${login.cookie}` } })
    assert.equal(forbidden.status, 403)
  })

  it('no borra self ni el último admin; borra usuarios normales', async () => {
    const list = await (
      await fetch(`${srv.base}/api/users`, { headers: { cookie: `session=${cookie}` } })
    ).json()
    const admin = list.users.find((u) => u.username === 'admin')
    const ana = list.users.find((u) => u.username === 'ana')

    let res = await fetch(`${srv.base}/api/users/${admin.id}`, {
      method: 'DELETE',
      headers: { cookie: `session=${cookie}` },
    })
    assert.equal(res.status, 400) // self

    res = await fetch(`${srv.base}/api/users/${ana.id}`, {
      method: 'DELETE',
      headers: { cookie: `session=${cookie}` },
    })
    assert.equal(res.status, 204)

    res = await fetch(`${srv.base}/api/users/${ana.id}`, {
      method: 'DELETE',
      headers: { cookie: `session=${cookie}` },
    })
    assert.equal(res.status, 404)
  })
})
