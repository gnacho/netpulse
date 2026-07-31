/**
 * Configuración por entorno, validada con zod (fail-fast).
 * Lee server/.env manualmente (sin dependencia dotenv) y luego valida.
 */
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { z } from 'zod'

const SERVER_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')

/** Parser .env mínimo: KEY=VALUE, # comentarios, comillas simples/dobles opcionales. */
export function loadDotEnv(envPath = path.join(SERVER_ROOT, '.env')) {
  if (!fs.existsSync(envPath)) return
  for (const rawLine of fs.readFileSync(envPath, 'utf8').split('\n')) {
    const line = rawLine.trim()
    if (!line || line.startsWith('#')) continue
    const eq = line.indexOf('=')
    if (eq === -1) continue
    const key = line.slice(0, eq).trim()
    let value = line.slice(eq + 1).trim()
    if ((value.startsWith('"') && value.endsWith('"')) || (value.startsWith("'") && value.endsWith("'"))) {
      value = value.slice(1, -1)
    }
    if (!(key in process.env)) process.env[key] = value
  }
}

const routerSchema = z.object({
  id: z.string().min(1),
  name: z.string().optional(),
  host: z.string().min(1),
  type: z.enum(['glinet', 'openwrt']).default('openwrt'),
})

const envSchema = z.object({
  PORT: z.coerce.number().int().min(1).max(65535).default(3000),
  NODE_ENV: z.enum(['development', 'production', 'test']).default('development'),
  STATIC_DIR: z.string().default('../app/dist'),
  DATA_DIR: z.string().default('./data'),
  SESSION_SECRET: z.string().min(32).optional(), // autogenerado (kv) si falta
  AUTH_USER: z.string().min(1).default('admin'),
  AUTH_PASS: z.string().min(1), // obligatoria: fail-fast si falta
  DEMO_MODE: z.enum(['0', '1']).optional(), // si falta: demo cuando no hay ROUTERS_JSON
  MAX_SSE_CLIENTS: z.coerce.number().int().min(1).max(100).default(10),
  // --- Modo live ---
  ROUTERS_JSON: z.string().optional(),
  // Si no se define, la app genera y usa su propia clave en DATA_DIR/.ssh
  SSH_KEY_PATH: z.string().optional(),
  ADGUARD_URL: z.string().url().optional(),
  ADGUARD_USER: z.string().default('admin'),
  ADGUARD_PASS: z.string().optional(),
  WG_INTERFACE: z.string().default('wg0'),
  // Cookie Secure: 'auto' = solo si la petición llega por HTTPS (o X-Forwarded-Proto)
  COOKIE_SECURE: z.enum(['auto', 'always', 'never']).default('auto'),
})

/**
 * Valida process.env y devuelve la config normalizada.
 * Lanza (fail-fast) si falta algo crítico o ROUTERS_JSON está mal formado.
 */
export function loadConfig(env = process.env) {
  const parsed = envSchema.safeParse(env)
  if (!parsed.success) {
    const issues = parsed.error.issues.map((i) => `  - ${i.path.join('.')}: ${i.message}`).join('\n')
    throw new Error(`[netpulse] Configuración inválida (revisa server/.env):\n${issues}`)
  }
  const e = parsed.data

  let routers = []
  if (e.ROUTERS_JSON) {
    try {
      const raw = JSON.parse(e.ROUTERS_JSON)
      routers = z.array(routerSchema).parse(raw)
    } catch (err) {
      throw new Error(`[netpulse] ROUTERS_JSON inválido: ${err.message}`)
    }
  }

  // Demo SOLO si se fuerza con DEMO_MODE=1. Con 0 routers configurados el
  // adapter live arranca vacío (overview sin routers) a la espera de la
  // autodetección del gateway o del alta manual desde Ajustes.
  const demoMode = e.DEMO_MODE === '1'

  return {
    port: e.PORT,
    nodeEnv: e.NODE_ENV,
    staticDir: path.resolve(SERVER_ROOT, e.STATIC_DIR),
    dataDir: path.resolve(SERVER_ROOT, e.DATA_DIR),
    sessionSecret: e.SESSION_SECRET,
    authUser: e.AUTH_USER,
    authPass: e.AUTH_PASS,
    demoMode,
    maxSseClients: e.MAX_SSE_CLIENTS,
    routers,
    sshKeyPath: e.SSH_KEY_PATH
      ? e.SSH_KEY_PATH.replace(/^~/, process.env.HOME || '~')
      : path.join(path.resolve(SERVER_ROOT, e.DATA_DIR), '.ssh', 'id_ed25519'),
    adguard: e.ADGUARD_URL
      ? { url: e.ADGUARD_URL.replace(/\/$/, ''), user: e.ADGUARD_USER, pass: e.ADGUARD_PASS || '' }
      : null,
    wgInterface: e.WG_INTERFACE,
    cookieSecure: e.COOKIE_SECURE,
    serverRoot: SERVER_ROOT,
  }
}
