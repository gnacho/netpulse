/**
 * Poller: snapshot cada 5 s del adapter activo.
 *   - Primer snapshot inmediato al arrancar.
 *   - Broadcast SSE `snapshot` (overview) a los clientes conectados.
 *   - Alertas nuevas → evento SSE `alert`.
 *   - Persistencia: metrics(router_id, ts, ...) + adguard_stats(ts, ...).
 *   - Nunca se cae por un fallo del adapter (try/catch + log).
 */
const TICK_MS = 5000

export function createPoller({ adapter, dbHandle, sse }) {
  let timer = null
  let lastOverview = null
  let lastAdguard = null
  const knownAlertIds = new Set()
  let alertsPrimed = false

  async function tick() {
    try {
      await adapter.tick?.()
      const overview = await adapter.getOverview()
      lastOverview = overview
      lastAdguard = overview.adguard ?? null

      // Persistencia (solo live: en demo la BD se mantiene 100% vacía)
      if (adapter.mode !== 'demo') {
        const ts = Date.now()
        for (const row of adapter.getMetricsRows()) {
          dbHandle.stmts.insertMetric.run(
            row.router_id,
            ts,
            row.cpu,
            row.ram,
            row.temp,
            row.latency_ms,
            row.rx_bps,
            row.tx_bps,
          )
        }
        if (lastAdguard) {
          dbHandle.stmts.insertAdguard.run(ts, lastAdguard.queries24h, lastAdguard.blocked24h)
        }
      }

      // SSE: snapshot + alertas nuevas
      sse.broadcast('snapshot', overview)
      if (alertsPrimed) {
        for (const alert of overview.alerts ?? []) {
          if (!knownAlertIds.has(alert.id)) {
            knownAlertIds.add(alert.id)
            sse.broadcast('alert', alert)
          }
        }
      } else {
        // Primer tick: no re-notificar alertas preexistentes
        for (const alert of overview.alerts ?? []) knownAlertIds.add(alert.id)
        alertsPrimed = true
      }
    } catch (err) {
      console.error('[netpulse] error en tick del poller:', err)
    }
  }

  return {
    start() {
      tick() // primer snapshot inmediato
      timer = setInterval(tick, TICK_MS)
      timer.unref()
    },
    stop() {
      if (timer) clearInterval(timer)
      timer = null
    },
    get lastOverview() {
      return lastOverview
    },
  }
}
