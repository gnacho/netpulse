/**
 * Entry: valida .env (fail-fast), abre SQLite, crea adapter (demo/live),
 * hub SSE, poller (5 s), app Hono y arranca el servidor.
 * Graceful shutdown en SIGTERM/SIGINT.
 */
import { serve } from '@hono/node-server'
import path from 'node:path'
import { loadDotEnv, loadConfig } from './config.js'
import { openDb } from './db.js'
import { ensureSessionSecret, ensureUsers } from './auth.js'
import { ensureSshKeypair } from './sshkey.js'
import { ensureInitialRouters } from './routerstore.js'
import { createAdapter } from './adapters/index.js'
import { createSseHub } from './sse.js'
import { createPoller } from './poller.js'
import { createUpdater } from './updater.js'
import { createApp, VERSION } from './app.js'

async function main() {
  loadDotEnv()
  let config
  try {
    config = loadConfig()
  } catch (err) {
    console.error(err.message)
    process.exit(1)
  }

  const dbHandle = openDb(config.dataDir)
  const secret = ensureSessionSecret(dbHandle.db, config)
  // Multiusuario: seed del admin desde .env si la tabla users está vacía
  await ensureUsers(dbHandle.db, config)
  // Clave SSH propia para sondear routers (se genera la primera vez)
  await ensureSshKeypair(config.sshKeyPath)
  // Bootstrap de routers: tabla vacía → ROUTERS_JSON o autodetección del gateway
  const routers = await ensureInitialRouters(dbHandle.db, config)
  const adapter = createAdapter(config, dbHandle, routers)

  // Dependencia circular sse↔poller resuelta con un holder
  const holder = { poller: null }
  const sse = createSseHub({
    db: dbHandle.db,
    maxClients: config.maxSseClients,
    getOverview: () => holder.poller?.lastOverview ?? null,
  })
  const poller = createPoller({ adapter, dbHandle, sse })
  holder.poller = poller

  // Actualizador: repoRoot = /opt/netpulse (padre de server/)
  const updater = createUpdater({
    repoRoot: path.resolve(config.serverRoot, '..'),
    repo: config.githubRepo,
    token: config.githubToken,
  })

  const app = createApp({ config, dbHandle, adapter, sse, poller, secret, updater })

  const server = serve({ fetch: app.fetch, port: config.port }, (info) => {
    console.log(`[netpulse] v${VERSION} · modo ${adapter.mode} · http://localhost:${info.port}`)
    console.log(`[netpulse] datos: ${config.dataDir} · estáticos: ${config.staticDir}`)
    poller.start()
    updater.start()
  })

  function shutdown(signal) {
    console.log(`[netpulse] ${signal} recibido, cerrando...`)
    poller.stop()
    updater.stop()
    sse.notifyShutdown()
    adapter.close?.()
    server.close(() => {
      dbHandle.close()
      process.exit(0)
    })
    // Salvavidas: si el server no cierra en 3 s, salir igualmente
    setTimeout(() => {
      dbHandle.close()
      process.exit(0)
    }, 3000).unref()
  }

  process.on('SIGTERM', () => shutdown('SIGTERM'))
  process.on('SIGINT', () => shutdown('SIGINT'))
}

main().catch((err) => {
  console.error('[netpulse] error fatal en arranque:', err)
  process.exit(1)
})
