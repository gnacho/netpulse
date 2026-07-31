/**
 * SQLite (better-sqlite3, síncrono): schema + helpers + jobs de mantenimiento.
 * WAL + synchronous NORMAL, checkpoint TRUNCATE periódico, retención 7 días.
 */
import fs from 'node:fs'
import path from 'node:path'
import Database from 'better-sqlite3'

const RETENTION_MS = 7 * 24 * 60 * 60 * 1000 // 7 días
const MAINTENANCE_MS = 60 * 60 * 1000 // 1 h

export function openDb(dataDir) {
  fs.mkdirSync(dataDir, { recursive: true })
  const dbPath = path.join(dataDir, 'netpulse.db')
  const db = new Database(dbPath)
  db.pragma('journal_mode = WAL')
  db.pragma('synchronous = NORMAL')
  db.pragma('foreign_keys = ON')

  db.exec(`
    -- Sesiones (cookie id.hmac)
    CREATE TABLE IF NOT EXISTS sessions (
      id TEXT PRIMARY KEY,
      created_at INTEGER NOT NULL,
      expires_at INTEGER NOT NULL,
      ua TEXT
    );
    -- Rate-limit de login por IP
    CREATE TABLE IF NOT EXISTS login_attempts (
      ip TEXT PRIMARY KEY,
      attempts INTEGER DEFAULT 0,
      locked_until INTEGER DEFAULT 0
    );
    -- Clave-valor (session_secret, auth_pass_hash, ...)
    CREATE TABLE IF NOT EXISTS kv (
      key TEXT PRIMARY KEY,
      value TEXT
    );
    -- Métricas por router y tick del poller
    CREATE TABLE IF NOT EXISTS metrics (
      router_id TEXT NOT NULL,
      ts INTEGER NOT NULL,
      cpu REAL,
      ram REAL,
      temp REAL,
      latency_ms REAL,
      rx_bps REAL,
      tx_bps REAL
    );
    CREATE INDEX IF NOT EXISTS idx_metrics_router_ts ON metrics(router_id, ts);
    -- Estadísticas AdGuard por tick
    CREATE TABLE IF NOT EXISTS adguard_stats (
      ts INTEGER NOT NULL,
      queries INTEGER,
      blocked INTEGER
    );
    CREATE INDEX IF NOT EXISTS idx_adguard_ts ON adguard_stats(ts);
    -- Usuarios (multiusuario: el admin gestiona altas/bajas/passwords)
    CREATE TABLE IF NOT EXISTS users (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      username TEXT UNIQUE NOT NULL,
      pass_hash TEXT NOT NULL,
      role TEXT NOT NULL DEFAULT 'user',
      created_at INTEGER NOT NULL
    );
    -- Última atribución conocida por MAC (banda/router donde se vio asociado;
    -- el FDB del gateway ve todo y no sirve para saber cómo conecta un equipo)
    CREATE TABLE IF NOT EXISTS device_attrib (
      mac TEXT PRIMARY KEY,
      router_id TEXT,
      band TEXT,
      signal_dbm INTEGER,
      last_seen INTEGER NOT NULL
    );
    -- Routers configurados (vacía al importar; se rellena por autodetección
    -- del gateway, ROUTERS_JSON o el CRUD de Ajustes)
    CREATE TABLE IF NOT EXISTS routers (
      id TEXT PRIMARY KEY,
      name TEXT,
      host TEXT NOT NULL,
      type TEXT NOT NULL DEFAULT 'openwrt',
      is_gateway INTEGER NOT NULL DEFAULT 0,
      created_at INTEGER NOT NULL
    );
  `)

  // Migraciones tolerantes (añadir columnas si faltan en DBs antiguas)
  migrate(db, 'sessions', 'ua', 'ALTER TABLE sessions ADD COLUMN ua TEXT')
  migrate(db, 'sessions', 'user_id', 'ALTER TABLE sessions ADD COLUMN user_id INTEGER')

  const stmts = {
    insertMetric: db.prepare(
      'INSERT INTO metrics (router_id, ts, cpu, ram, temp, latency_ms, rx_bps, tx_bps) VALUES (?, ?, ?, ?, ?, ?, ?, ?)',
    ),
    insertAdguard: db.prepare('INSERT INTO adguard_stats (ts, queries, blocked) VALUES (?, ?, ?)'),
    deleteOldMetrics: db.prepare('DELETE FROM metrics WHERE ts < ?'),
    deleteOldAdguard: db.prepare('DELETE FROM adguard_stats WHERE ts < ?'),
    deleteExpiredSessions: db.prepare('DELETE FROM sessions WHERE expires_at < ?'),
    listRouters: db.prepare('SELECT id, name, host, type, is_gateway, created_at FROM routers ORDER BY is_gateway DESC, created_at ASC'),
    insertRouter: db.prepare('INSERT INTO routers (id, name, host, type, is_gateway, created_at) VALUES (?, ?, ?, ?, ?, ?)'),
    deleteRouter: db.prepare('DELETE FROM routers WHERE id = ?'),
    clearGateways: db.prepare('UPDATE routers SET is_gateway = 0'),
    health: db.prepare('SELECT 1 AS ok'),
  }

  function maintenance() {
    const cutoff = Date.now() - RETENTION_MS
    try {
      stmts.deleteOldMetrics.run(cutoff)
      stmts.deleteOldAdguard.run(cutoff)
      stmts.deleteExpiredSessions.run(Date.now())
      db.pragma('wal_checkpoint(TRUNCATE)')
    } catch (err) {
      console.error('[netpulse] error en mantenimiento DB:', err.message)
    }
  }

  const maintenanceTimer = setInterval(maintenance, MAINTENANCE_MS)
  maintenanceTimer.unref()

  return {
    db,
    stmts,
    checkHealth() {
      try {
        return stmts.health.get().ok === 1
      } catch {
        return false
      }
    },
    maintenance,
    close() {
      clearInterval(maintenanceTimer)
      try {
        db.pragma('wal_checkpoint(TRUNCATE)')
      } catch {}
      db.close()
    },
  }
}

function migrate(db, table, column, ddl) {
  const cols = db.prepare(`PRAGMA table_info(${table})`).all()
  if (!cols.some((c) => c.name === column)) {
    try {
      db.exec(ddl)
    } catch {}
  }
}
