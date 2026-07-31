/**
 * Helpers de test: app real sobre puerto efímero con adapter demo y DB temporal.
 */
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { serve } from '@hono/node-server'
import { loadConfig } from '../src/config.js'
import { openDb } from '../src/db.js'
import { ensureSessionSecret } from '../src/auth.js'
import { createDemoAdapter } from '../src/adapters/demo.js'
import { createSseHub } from '../src/sse.js'
import { createApp } from '../src/app.js'

export function makeTestServer(env = {}) {
  const dataDir = fs.mkdtempSync(path.join(os.tmpdir(), 'netpulse-test-'))
  const config = loadConfig({
    AUTH_USER: 'admin',
    AUTH_PASS: 'test1234',
    DEMO_MODE: '1',
    DATA_DIR: dataDir,
    NODE_ENV: 'test',
    ...env,
  })
  const dbHandle = openDb(dataDir)
  const secret = ensureSessionSecret(dbHandle.db, config)
  const adapter = createDemoAdapter()
  const sse = createSseHub({ db: dbHandle.db, maxClients: config.maxSseClients, getOverview: () => null })
  const app = createApp({ config, dbHandle, adapter, sse, poller: null, secret })

  const server = serve({ fetch: app.fetch, port: 0 })
  const address = server.address()
  const base = `http://127.0.0.1:${address.port}`

  return {
    base,
    config,
    dbHandle,
    adapter,
    async close() {
      await new Promise((resolve) => server.close(resolve))
      dbHandle.close()
      fs.rmSync(dataDir, { recursive: true, force: true })
    },
  }
}

/** Login y devuelve el valor de la cookie de sesión (id.hmac). */
export async function loginCookie(base, password = 'test1234') {
  const res = await fetch(`${base}/api/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password }),
  })
  const setCookie = res.headers.get('set-cookie') || ''
  const match = /session=([^;]+)/.exec(setCookie)
  return { status: res.status, cookie: match?.[1] ?? null, setCookie }
}
