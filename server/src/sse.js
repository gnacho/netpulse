/**
 * SSE (Server-Sent Events) — /api/stream
 * Reglas del skill/contrato:
 *   - NUNCA Content-Encoding: gzip manual (SSE es texto chunked; nginx ya
 *     comprime transparente si procede).
 *   - X-Accel-Buffering: no + Cache-Control: no-cache.
 *   - Primer evento `snapshot` inmediato; luego uno por tick del poller (5 s).
 *   - Evento `alert` cuando se genera una alerta nueva.
 *   - Heartbeat `:hb` cada 30 s.
 *   - MAX_SSE_CLIENTS (429/503 al superarlo) → contrato: 503 too_many_clients.
 *   - En pérdida de sesión → evento `bye` y cierre.
 */
import { streamSSE } from 'hono/streaming'
import { getSession } from './auth.js'

const HEARTBEAT_MS = 30_000

export function createSseHub({ db, maxClients, getOverview }) {
  /** @type {Set<{id: number, sessionId: string, stream: any}>} */
  const clients = new Set()
  let nextId = 1

  function broadcast(event, data) {
    const payload = `event: ${event}\ndata: ${JSON.stringify(data)}\n\n`
    for (const client of clients) {
      // Sesión revocada/expirada → evento bye y cierre
      if (!getSession(db, client.sessionId)) {
        try {
          client.stream.writeSSE({ event: 'bye', data: '{}' }).then(() => client.stream.close()).catch(() => {})
        } catch {}
        clients.delete(client)
        continue
      }
      client.stream.write(payload).catch(() => clients.delete(client))
    }
  }

  function notifyShutdown() {
    for (const client of clients) {
      try {
        client.stream.write('event: shutdown\ndata: {}\n\n').catch(() => {})
      } catch {}
    }
  }

  /** Handler Hono para GET /api/stream (requiere sesión → requireAuth antes). */
  function handleStream(c) {
    if (clients.size >= maxClients) {
      return c.json({ error: 'too_many_clients' }, 503)
    }
    const sessionId = c.get('sessionId')
    const overview = getOverview?.()

    c.header('X-Accel-Buffering', 'no')
    c.header('Cache-Control', 'no-cache')
    // Deliberadamente SIN Content-Encoding (ver cabecera del fichero)

    return streamSSE(c, async (stream) => {
      const client = { id: nextId++, sessionId, stream }
      clients.add(client)

      // Primer snapshot inmediato al conectar
      if (overview) {
        await stream.writeSSE({ event: 'snapshot', data: JSON.stringify(overview) })
      }

      const heartbeat = setInterval(() => {
        stream.write(':hb\n\n').catch(() => {})
      }, HEARTBEAT_MS)

      // Mantener la conexión abierta hasta que el cliente cierre
      await new Promise((resolve) => {
        stream.onAbort(() => {
          clearInterval(heartbeat)
          clients.delete(client)
          resolve()
        })
      })
    })
  }

  return {
    handleStream,
    broadcast,
    notifyShutdown,
    get size() {
      return clients.size
    },
  }
}
