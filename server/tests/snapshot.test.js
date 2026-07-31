/**
 * Tests del snapshot demo (contrato):
 *  - El overview contiene todas las claves del contrato con los shapes del mock.
 *  - El primer snapshot es EXACTAMENTE el canon (antes del random walk).
 *  - El random walk respeta los límites (cpu/ram/temp ±2, latencia 6–11 ms,
 *    bytes WG crecientes).
 */
import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { createDemoAdapter } from '../src/adapters/demo.js'
import { routers as canonRouters, wan as canonWan, adguard as canonAdguard } from '../src/demo/dataset.js'

describe('snapshot demo', () => {
  it('contiene todas las claves del contrato', () => {
    const adapter = createDemoAdapter()
    const snap = adapter.getOverview()
    for (const key of [
      'health', 'wan', 'traffic', 'adguard', 'wireguard',
      'routers', 'deviceTotals', 'topDevices', 'alerts', 'unreadAlerts', 'ts',
    ]) {
      assert.ok(key in snap, `falta ${key} en el snapshot`)
    }
    assert.equal(snap.routers.length, 4)
    assert.equal(snap.topDevices.length, 5)
    assert.ok(Array.isArray(snap.alerts))
    for (const range of ['1h', '24h', '7d', '30d']) {
      assert.ok(Array.isArray(snap.traffic[range]), `falta traffic.${range}`)
    }
    // Shapes clave (camelCase del mock)
    const r0 = snap.routers[0]
    for (const key of ['id', 'name', 'model', 'modelShort', 'roleBadge', 'ip', 'status', 'health', 'cpu', 'ram', 'temp', 'uptime', 'clients', 'sparkline']) {
      assert.ok(key in r0, `router sin ${key}`)
    }
    for (const key of ['queries24h', 'blocked24h', 'blockedPct', 'topBlocked', 'filterLists', 'rules']) {
      assert.ok(key in snap.adguard, `adguard sin ${key}`)
    }
    for (const key of ['interface', 'subnet', 'status', 'peers']) {
      assert.ok(key in snap.wireguard, `wireguard sin ${key}`)
    }
    for (const key of ['id', 'name', 'type', 'tunnelIp', 'active', 'lastHandshake', 'rx', 'tx']) {
      assert.ok(key in snap.wireguard.peers[0], `peer sin ${key}`)
    }
    assert.equal(typeof snap.deviceTotals.total, 'number')
    assert.equal(typeof snap.unreadAlerts, 'number')
    assert.equal(typeof snap.ts, 'number')
  })

  it('el primer snapshot es exactamente el canon', () => {
    const adapter = createDemoAdapter()
    const snap = adapter.getOverview()
    const flint = snap.routers.find((r) => r.id === 'flint2')
    assert.equal(flint.cpu, canonRouters[0].cpu)
    assert.equal(flint.ram, canonRouters[0].ram)
    assert.equal(flint.temp, canonRouters[0].temp)
    assert.equal(snap.wan.downMbps, canonWan.downMbps)
    assert.equal(snap.wan.latencyMs, canonWan.latencyMs)
    assert.equal(snap.adguard.queries24h, canonAdguard.queries24h)
    const pixel = snap.wireguard.peers.find((p) => p.id === 'pixel-8-pro')
    assert.equal(pixel.rx, '1,2 GB')
    assert.equal(pixel.tx, '214 MB')
    const inactivo = snap.wireguard.peers.find((p) => p.id === 'casa-familia')
    assert.equal(inactivo.rx, '12 GB')
    assert.equal(snap.deviceTotals.total, 47)
  })

  it('random walk: cpu/ram/temp ±2 máx, latencia 6–11 ms, bytes WG crecen', () => {
    const adapter = createDemoAdapter()
    const before = adapter.getOverview()
    adapter.tick()
    const after = adapter.getOverview()
    for (let i = 0; i < 4; i++) {
      for (const key of ['cpu', 'ram', 'temp']) {
        const delta = Math.abs(after.routers[i][key] - before.routers[i][key])
        assert.ok(delta <= 2, `${key} de ${after.routers[i].id} saltó ${delta} (>2)`)
      }
    }
    assert.ok(after.wan.latencyMs >= 6 && after.wan.latencyMs <= 11, `latencia ${after.wan.latencyMs} fuera de 6–11`)
    // AdGuard crece
    assert.ok(after.adguard.queries24h >= before.adguard.queries24h)
    // WG: parseo de formato ES y comparación numérica
    const parse = (s) => {
      const m = /^([\d,]+) (GB|MB|KB)$/.exec(s)
      const mult = { GB: 1e9, MB: 1e6, KB: 1e3 }[m[2]]
      return parseFloat(m[1].replace(',', '.')) * mult
    }
    for (const id of ['pixel-8-pro', 'macbook-air']) {
      const b = before.wireguard.peers.find((p) => p.id === id)
      const a = after.wireguard.peers.find((p) => p.id === id)
      assert.ok(parse(a.rx) >= parse(b.rx), `rx de ${id} debe crecer`)
      assert.ok(parse(a.tx) >= parse(b.tx), `tx de ${id} debe crecer`)
    }
    // Peers inactivos: no cambian
    const bIn = before.wireguard.peers.find((p) => p.id === 'ipad-air')
    const aIn = after.wireguard.peers.find((p) => p.id === 'ipad-air')
    assert.equal(aIn.rx, bIn.rx)
  })

  it('detalle de router: extras, series y 404 en id desconocido', async () => {
    const adapter = createDemoAdapter()
    const detail = await adapter.getRouterDetail('flint2')
    assert.ok(detail.ports.length >= 5, 'flint2 tiene 5 bocas RJ45')
    assert.equal(detail.radios, null, 'flint2 no define radios en el canon')
    assert.equal(detail.backhaul, null)
    for (const range of ['1h', '24h', '7d']) {
      assert.ok(Array.isArray(detail.series[range]), `falta series.${range}`)
      const last = detail.series[range].at(-1)
      // La serie termina SIEMPRE en el valor actual del router
      assert.equal(last.cpu, detail.router.cpu)
      assert.equal(last.temp, detail.router.temp)
    }
    assert.ok(detail.adguard, 'gateway incluye adguard')
    assert.ok(detail.wireguard, 'gateway incluye wireguard')
    assert.ok(Array.isArray(detail.clients))
    const living = await adapter.getRouterDetail('living')
    assert.ok(Array.isArray(living.radios) && living.radios.length === 2)
    assert.equal(living.adguard, undefined, 'AP no incluye adguard')
    assert.equal(await adapter.getRouterDetail('no-existe'), null)
  })
})
