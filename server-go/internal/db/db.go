// Package db — SQLite (modernc.org/sqlite, sin CGO): schema literal de Node,
// pragmas (WAL, synchronous NORMAL, foreign_keys ON), migraciones tolerantes
// y jobs de mantenimiento (retención 7 días + wal_checkpoint(TRUNCATE) 1 h).
// Paridad con server/src/db.js (SPEC §4).
//
// Epochs: TODOS los timestamps de la DB son milisegundos (created_at,
// expires_at, metrics.ts, locked_until). Segundos solo en overview.ts.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/gnacho/netpulse/server-go/internal/portseries"
)

const (
	RetentionMS  = 7 * 24 * 60 * 60 * 1000 // 7 días
	Maintenance  = time.Hour
	databaseFile = "netpulse.db"
	// AttribRetentionMS: 90 días sin verse para purgar device_attrib
	// (issue #206; dispositivos que se fueron para siempre).
	AttribRetentionMS = 90 * 24 * 60 * 60 * 1000
)

// schemaSQL es el SQL literal de src/db.js:20-85.
const schemaSQL = `
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
  cpu REAL, ram REAL, temp REAL,
  latency_ms REAL, rx_bps REAL, tx_bps REAL,
  PRIMARY KEY (router_id, ts)
);
CREATE INDEX IF NOT EXISTS idx_metrics_router_ts ON metrics(router_id, ts);
-- Escalera de retención (skill sqlite-timeseries-daemon): raw 7 días → buckets
-- 5 min 1 año → daily ∞. El job nocturno hace el rollup en este orden; el
-- endpoint de histórico sirve largo plazo desde estos niveles.
CREATE TABLE IF NOT EXISTS metrics_buckets (
  router_id TEXT NOT NULL,
  bucket_ts INTEGER NOT NULL,
  n INTEGER NOT NULL,
  min_ts INTEGER NOT NULL,
  cpu_min REAL, cpu_max REAL, cpu_avg REAL,
  ram_min REAL, ram_max REAL, ram_avg REAL,
  temp_min REAL, temp_max REAL, temp_avg REAL,
  lat_min REAL, lat_max REAL, lat_avg REAL,
  rx_min REAL, rx_max REAL, rx_avg REAL,
  tx_min REAL, tx_max REAL, tx_avg REAL,
  PRIMARY KEY (router_id, bucket_ts)
);
CREATE INDEX IF NOT EXISTS idx_metrics_buckets_ts ON metrics_buckets(bucket_ts);
CREATE TABLE IF NOT EXISTS metrics_daily (
  router_id TEXT NOT NULL,
  date TEXT NOT NULL,
  n INTEGER NOT NULL,
  cpu_avg REAL, ram_avg REAL, temp_avg REAL,
  lat_avg REAL, rx_avg REAL, tx_avg REAL,
  rx_total REAL, tx_total REAL,
  -- up_min: minutos de recolección del día (SUM(n) * 5 / 60; n = muestras a
  -- 5 s → día completo ≈ 17280 muestras ≈ 1440 min).
  up_min INTEGER NOT NULL DEFAULT 0,
  -- up_count: nº de buckets de 5 min con datos (288 = día completo).
  up_count INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (router_id, date)
);
-- Estadísticas AdGuard por tick
CREATE TABLE IF NOT EXISTS adguard_stats (
  ts INTEGER NOT NULL,
  queries INTEGER, blocked INTEGER
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
-- Última atribución conocida por MAC
CREATE TABLE IF NOT EXISTS device_attrib (
  mac TEXT PRIMARY KEY,
  router_id TEXT,
  band TEXT,
  signal_dbm INTEGER,
  last_seen INTEGER NOT NULL
);
-- Routers configurados
CREATE TABLE IF NOT EXISTS routers (
  id TEXT PRIMARY KEY,
  name TEXT,
  host TEXT NOT NULL,
  type TEXT NOT NULL DEFAULT 'openwrt',
  is_gateway INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
-- Allowlist de dispositivos confiables (issue #196): una MAC conocida no
-- dispara "dispositivo desconocido" y su name se usa como alias.
CREATE TABLE IF NOT EXISTS known_macs (
  mac TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  note TEXT,
  created_at INTEGER NOT NULL
);
-- Suscripciones Web Push (Fase 3 Bloque C; sin paridad Node: el push es
-- nuevo en Go). created_at en milisegundos como el resto de epochs.
CREATE TABLE IF NOT EXISTS push_subscriptions (
  endpoint TEXT PRIMARY KEY,
  keys_auth TEXT NOT NULL,
  keys_p256dh TEXT NOT NULL,
  user_agent TEXT,
  created_at INTEGER NOT NULL
);
-- DLQ de webhooks salientes (Fase 8.7b): eventos no entregados tras agotar
-- reintentos. event_id único (idempotencia del emisor).
CREATE TABLE IF NOT EXISTS webhook_events (
  event_id TEXT PRIMARY KEY,
  payload TEXT NOT NULL,
  sent_at INTEGER NOT NULL,
  error TEXT
);

CREATE TABLE IF NOT EXISTS orchestr_plans (
  id         TEXT PRIMARY KEY,
  router_id  TEXT NOT NULL,
  resource   TEXT NOT NULL,
  desired    TEXT NOT NULL,
  diff       TEXT NOT NULL,
  status     TEXT NOT NULL DEFAULT 'pending',
  created_by TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  applied_at INTEGER,
  result     TEXT
);

CREATE TABLE IF NOT EXISTS orchestr_audit (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  plan_id    TEXT NOT NULL,
  action     TEXT NOT NULL,
  actor      TEXT NOT NULL,
  detail     TEXT,
  ts         INTEGER NOT NULL
);

-- roam_events: feed continuo de eventos hostapd/DAWN (Fase 14.5).
-- Ingesta cada 60s via logread grep; dedup por content_hash.
CREATE TABLE IF NOT EXISTS roam_events (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  ts_ms         INTEGER NOT NULL,
  router_id     TEXT NOT NULL,
  type          TEXT NOT NULL,
  mac           TEXT,
  iface         TEXT,
  detail        TEXT,
  content_hash  TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_roam_events_dedup ON roam_events(content_hash);
CREATE INDEX IF NOT EXISTS idx_roam_events_ts ON roam_events(ts_ms DESC);
CREATE INDEX IF NOT EXISTS idx_roam_events_router_ts ON roam_events(router_id, ts_ms DESC);

-- device_events: transiciones offline/online detectadas por el poller (issue #184).
-- Generados por el adapter Live en el ciclo de buildDevices; consultables por API.
CREATE TABLE IF NOT EXISTS device_events (
  id         INTEGER PRIMARY KEY AUTOINCREMENT,
  ts_ms      INTEGER NOT NULL,
  mac        TEXT NOT NULL,
  router_id  TEXT,
  state      TEXT NOT NULL,          -- 'offline' | 'online'
  signal_dbm INTEGER,
  detail     TEXT
);
CREATE INDEX IF NOT EXISTS idx_device_events_ts ON device_events(ts_ms DESC);
CREATE INDEX IF NOT EXISTS idx_device_events_mac_ts ON device_events(mac, ts_ms DESC);

-- Overrides manuales de topología (issue #142, Fase A): capa 2 sobre el
-- autodiscover. El builder los aplica DESPUÉS de inferTopology.
--   kind 'hypervisor' (target MAC): el dispositivo es un hipervisor; los CTs
--     con OUI de hipervisor del mismo puerto se anidan bajo él.
--   kind 'switch' (target MAC): el dispositivo es un switch manual (sin LLDP);
--     el puerto del target pasa a ser nodo managed con esa MAC.
--   kind 'attach' (target MAC, parent MAC): el target cuelga del parent.
CREATE TABLE IF NOT EXISTS topology_overrides (
  id         TEXT PRIMARY KEY,
  mac        TEXT NOT NULL,            -- target normalizado (minúsculas, '-')
  kind       TEXT NOT NULL,            -- 'hypervisor' | 'switch' | 'attach'
  name       TEXT,                     -- nombre personalizado (opcional)
  parent     TEXT,                     -- kind='attach': MAC del padre
  enabled    INTEGER NOT NULL DEFAULT 1,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_topo_overrides_mac ON topology_overrides(mac);

-- Historial de actualizaciones (issue #159): registro de cada apply del
-- updater. En canal rolling main version_from/to son SHAs de commit.
CREATE TABLE IF NOT EXISTS update_history (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id     TEXT NOT NULL,
  ts           INTEGER NOT NULL,
  action       TEXT NOT NULL,
  channel      TEXT NOT NULL,
  version_from TEXT,
  version_to   TEXT,
  initiated_by TEXT NOT NULL DEFAULT '',
  status       TEXT NOT NULL DEFAULT 'running',
  duration_ms  INTEGER,
  backup_path  TEXT,
  error        TEXT
);
CREATE INDEX IF NOT EXISTS idx_update_history_ts ON update_history(ts DESC);

-- Overrides manuales de dispositivo (issue #437): alias, icono y estado de
-- bloqueo por banda. La clave es la MAC normalizada (minúsculas, ':').
CREATE TABLE IF NOT EXISTS device_overrides (
  mac         TEXT PRIMARY KEY,
  icon        TEXT,
  banned_bands TEXT NOT NULL DEFAULT '', -- lista separada por comas: '2.4,5,6'
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL
);

-- Per-port time series (issue #302): raw (7d) -> 5m (1y) -> daily (forever).
CREATE TABLE IF NOT EXISTS port_series_raw (
  router_id TEXT NOT NULL,
  port_id TEXT NOT NULL,
  ts INTEGER NOT NULL,
  rx_bytes INTEGER NOT NULL DEFAULT 0,
  tx_bytes INTEGER NOT NULL DEFAULT 0,
  rx_errors INTEGER NOT NULL DEFAULT 0,
  tx_errors INTEGER NOT NULL DEFAULT 0,
  rx_frames INTEGER NOT NULL DEFAULT 0,
  tx_frames INTEGER NOT NULL DEFAULT 0,
  rx_bps REAL NOT NULL DEFAULT 0,
  tx_bps REAL NOT NULL DEFAULT 0,
  speed_mbps INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (router_id, port_id, ts)
);
CREATE INDEX IF NOT EXISTS idx_port_series_raw_ts ON port_series_raw(ts);
CREATE INDEX IF NOT EXISTS idx_port_series_raw_rpt ON port_series_raw(router_id, port_id, ts);

CREATE TABLE IF NOT EXISTS port_series_5m (
  router_id TEXT NOT NULL,
  port_id TEXT NOT NULL,
  bucket_ts INTEGER NOT NULL,
  n INTEGER NOT NULL,
  rx_bytes INTEGER NOT NULL DEFAULT 0,
  tx_bytes INTEGER NOT NULL DEFAULT 0,
  rx_errors INTEGER NOT NULL DEFAULT 0,
  tx_errors INTEGER NOT NULL DEFAULT 0,
  rx_frames INTEGER NOT NULL DEFAULT 0,
  tx_frames INTEGER NOT NULL DEFAULT 0,
  rx_bps_min REAL NOT NULL DEFAULT 0,
  rx_bps_max REAL NOT NULL DEFAULT 0,
  rx_bps_avg REAL NOT NULL DEFAULT 0,
  tx_bps_min REAL NOT NULL DEFAULT 0,
  tx_bps_max REAL NOT NULL DEFAULT 0,
  tx_bps_avg REAL NOT NULL DEFAULT 0,
  speed_mbps INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (router_id, port_id, bucket_ts)
);
CREATE INDEX IF NOT EXISTS idx_port_series_5m_ts ON port_series_5m(bucket_ts);

CREATE TABLE IF NOT EXISTS port_series_daily (
  router_id TEXT NOT NULL,
  port_id TEXT NOT NULL,
  date TEXT NOT NULL,
  n INTEGER NOT NULL,
  rx_bytes INTEGER NOT NULL DEFAULT 0,
  tx_bytes INTEGER NOT NULL DEFAULT 0,
  rx_errors INTEGER NOT NULL DEFAULT 0,
  tx_errors INTEGER NOT NULL DEFAULT 0,
  rx_frames INTEGER NOT NULL DEFAULT 0,
  tx_frames INTEGER NOT NULL DEFAULT 0,
  rx_bps_avg REAL NOT NULL DEFAULT 0,
  tx_bps_avg REAL NOT NULL DEFAULT 0,
  speed_mbps INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (router_id, port_id, date)
);

-- wifi_scans: scans pasivos de vecinos recogidos por el agente (#452).
-- Se guardan por push para poder hacer recomendaciones de canal.
CREATE TABLE IF NOT EXISTS wifi_scans (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  router_id   TEXT NOT NULL,
  iface       TEXT NOT NULL,
  bssid       TEXT NOT NULL,
  ssid        TEXT NOT NULL DEFAULT '',
  channel     INTEGER NOT NULL,
  freq        INTEGER NOT NULL,
  signal_dbm  INTEGER NOT NULL,
  ts          INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_wifi_scans_router_ts ON wifi_scans(router_id, ts DESC);
CREATE INDEX IF NOT EXISTS idx_wifi_scans_channel ON wifi_scans(channel, freq);

-- Firmware upgrades de routers OpenWrt (#453).
-- firmware_targets: información de firmware soportado por router.
CREATE TABLE IF NOT EXISTS firmware_targets (
  router_id       TEXT PRIMARY KEY,
  model           TEXT NOT NULL,
  current_version TEXT NOT NULL DEFAULT '',
  target_version  TEXT NOT NULL DEFAULT '',
  target_url      TEXT NOT NULL DEFAULT '',
  checksum        TEXT NOT NULL DEFAULT '',
  updated_at      INTEGER NOT NULL
);

-- firmware_upgrades: historial y estado de upgrades en curso.
CREATE TABLE IF NOT EXISTS firmware_upgrades (
  id             INTEGER PRIMARY KEY AUTOINCREMENT,
  router_id      TEXT NOT NULL,
  target_version TEXT NOT NULL,
  target_url     TEXT NOT NULL,
  checksum       TEXT NOT NULL DEFAULT '',
  status         TEXT NOT NULL DEFAULT 'idle',
  error          TEXT NOT NULL DEFAULT '',
  backup_path    TEXT,
  started_at     INTEGER NOT NULL,
  finished_at    INTEGER
);
CREATE INDEX IF NOT EXISTS idx_firmware_upgrades_router_started ON firmware_upgrades(router_id, started_at DESC);

`

// DB envuelve *sql.DB con los jobs y helpers de paridad.
type DB struct {
	*sql.DB
	Path  string
	stop  chan struct{}
	wg    sync.WaitGroup
	close sync.Once
	// rollbackJournal: journal DELETE en vez de WAL (modo on-box, Fase 9 R6).
	// Los wal_checkpoint de mantenimiento se omiten en este modo.
	rollbackJournal bool
	// PortSeries: per-port time series store (issue #302).
	PortSeries *portseries.Store
}

// NowMS devuelve el epoch actual en milisegundos (como Date.now()).
func NowMS() int64 { return time.Now().UnixMilli() }

// OpenOption configura Open (variádica: los callers existentes no cambian).
type OpenOption func(*openOptions)

type openOptions struct {
	rollbackJournal bool
}

// WithRollbackJournal (Fase 9 R6, modo on-box): `journal_mode=DELETE` +
// `synchronous=FULL` en vez de WAL+NORMAL, y sin wal_checkpoint en los jobs.
// Motivo: en la flash de un router el fichero -wal y sus checkpoints son
// churn de escritura evitable; y en rollback-journal, NORMAL deja una
// ventana de corrupción ante un corte de luz (frecuente en un router) que
// WAL+NORMAL no tiene — de ahí FULL (docs de PRAGMA synchronous).
func WithRollbackJournal() OpenOption {
	return func(o *openOptions) { o.rollbackJournal = true }
}

// Open abre (o crea) DATA_DIR/netpulse.db, ejecuta la migración Node→Go si la
// DB ya existía con esquema Node (backup atómico antes de tocar nada),
// aplica pragmas, crea el schema y arranca el mantenimiento horario.
func Open(dataDir string, opts ...OpenOption) (*DB, error) {
	var o openOptions
	for _, opt := range opts {
		opt(&o)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	dbPath := filepath.Join(dataDir, databaseFile)

	// Migración Node→Go (§12 del SPEC): si el fichero ya existe con esquema
	// Node, backup + importación (preserva users/kv/sessions/routers/metrics).
	rep, err := MigrateNodeDB(dbPath)
	if err != nil {
		return nil, fmt.Errorf("migración Node→Go: %w", err)
	}
	if rep != nil {
		rep.Log()
	}

	// Una sola conexión: better-sqlite3 es síncrono/serial; con maxOpenConns=1
	// Go serializa igual y evita SQLITE_BUSY en WAL.
	sqldb, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	sqldb.SetMaxOpenConns(1)

	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
	}
	if o.rollbackJournal {
		// Una DB previa en WAL se convierte sola: con una única conexión el
		// cambio de journal_mode hace checkpoint y elimina el -wal.
		pragmas = []string{
			"PRAGMA journal_mode = DELETE",
			"PRAGMA synchronous = FULL",
			"PRAGMA foreign_keys = ON",
		}
	}
	for _, p := range pragmas {
		if _, err := sqldb.Exec(p); err != nil {
			sqldb.Close()
			return nil, fmt.Errorf("pragma %q: %w", p, err)
		}
	}
	if _, err := sqldb.Exec(schemaSQL); err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}

	// Migraciones tolerantes (ALTER solo si falta la columna; errores tragados)
	migrate(sqldb, "sessions", "ua", "ALTER TABLE sessions ADD COLUMN ua TEXT")
	migrate(sqldb, "sessions", "user_id", "ALTER TABLE sessions ADD COLUMN user_id INTEGER")
	migrate(sqldb, "users", "language", "ALTER TABLE users ADD COLUMN language TEXT DEFAULT 'auto'")
	// SPEC-65 D65-5: nombre visible por usuario ("" = usar el username).
	migrate(sqldb, "users", "display_name", "ALTER TABLE users ADD COLUMN display_name TEXT DEFAULT ''")
	migrate(sqldb, "routers", "agent_only", "ALTER TABLE routers ADD COLUMN agent_only INTEGER NOT NULL DEFAULT 0")
	// issue #196: bridgeMAC persistido del router (exclusión de "desconocido").
	migrate(sqldb, "routers", "mac", "ALTER TABLE routers ADD COLUMN mac TEXT")
	// issue #241: target de firmware por router (string libre; NULL/"" = sin comprobar).
	migrate(sqldb, "routers", "firmware_target", "ALTER TABLE routers ADD COLUMN firmware_target TEXT")
	// issue #309: SNMP polling for managed switches.
	migrate(sqldb, "routers", "snmp_enabled", "ALTER TABLE routers ADD COLUMN snmp_enabled INTEGER NOT NULL DEFAULT 0")
	migrate(sqldb, "routers", "snmp_community", "ALTER TABLE routers ADD COLUMN snmp_community TEXT")
	migrate(sqldb, "routers", "snmp_port", "ALTER TABLE routers ADD COLUMN snmp_port INTEGER NOT NULL DEFAULT 0")
	// issue #414: intervalo de polling SNMP configurable (segundos; default 60).
	migrate(sqldb, "routers", "snmp_poll_interval", "ALTER TABLE routers ADD COLUMN snmp_poll_interval INTEGER NOT NULL DEFAULT 60")

	// Si no hubo migración Node (instalación fresca creada por Go), marca la
	// DB para que el siguiente arranque no dispare una "migración" espuria
	// (backup + reset de login_attempts) sobre una DB ya Go.
	if rep == nil {
		_, _ = sqldb.Exec(
			"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO NOTHING",
			migrationMarkerKey, time.Now().Format(time.RFC3339),
		)
	}

	d := &DB{DB: sqldb, Path: dbPath, stop: make(chan struct{}), rollbackJournal: o.rollbackJournal}
	ps, err := portseries.NewStore(sqldb)
	if err != nil {
		sqldb.Close()
		return nil, fmt.Errorf("port_series store: %w", err)
	}
	d.PortSeries = ps
	d.wg.Add(1)
	go d.maintenanceLoop()
	return d, nil
}

// migrate replica src/db.js:143-150: introspección PRAGMA table_info y DDL
// tragándose errores.
func migrate(db *sql.DB, table, column, ddl string) {
	rows, err := db.Query(fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err == nil && name == column {
			found = true
		}
	}
	if !found {
		_, _ = db.Exec(ddl) // errores tragados (paridad)
	}
}

// Maintenance ejecuta una pasada de limpieza (paridad src/db.js:107-120):
// retención 7 días en metrics/adguard_stats, sesiones expiradas, atributos de
// dispositivos sin verse en 90 días (issue #206) y checkpoint.
func (d *DB) Maintenance() {
	cutoff := NowMS() - RetentionMS
	steps := []string{
		"DELETE FROM metrics WHERE ts < ?",
		"DELETE FROM adguard_stats WHERE ts < ?",
	}
	for _, q := range steps {
		if _, err := d.Exec(q, cutoff); err != nil {
			log.Printf("[netpulse] error en mantenimiento DB: %v", err)
		}
	}
	if _, err := d.Exec("DELETE FROM sessions WHERE expires_at < ?", NowMS()); err != nil {
		log.Printf("[netpulse] error en mantenimiento DB: %v", err)
	}
	// Atributos de dispositivos que no se ven desde hace 90 días (se fueron
	// para siempre): sin esta purga device_attrib crece sin límite y alimenta
	// el listado de dispositivos offline (issue #206).
	attribCutoff := NowMS() - AttribRetentionMS
	if _, err := d.Exec("DELETE FROM device_attrib WHERE last_seen < ?", attribCutoff); err != nil {
		log.Printf("[netpulse] error en mantenimiento DB: %v", err)
	}
	// Purga de buckets > 1 año (el raw ya se purgó arriba; daily nunca).
	bucketCutoff := NowMS() - BucketsRetentionMS
	if _, err := d.Exec("DELETE FROM metrics_buckets WHERE bucket_ts < ?", bucketCutoff); err != nil {
		log.Printf("[netpulse] error en mantenimiento DB: %v", err)
	}
	if !d.rollbackJournal {
		if _, err := d.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			log.Printf("[netpulse] error en mantenimiento DB: %v", err)
		}
	}
}

// NightlyJob ejecuta el rollup de la escalera de retención (skill
// sqlite-timeseries-daemon): samples→buckets (solape 48 h) → buckets→daily
// (solape 35 días) → checkpoint → optimize. Orden obligatorio: agregar antes
// de purgar; idempotente (INSERT OR REPLACE) por si el daemon estuvo caído.
func (d *DB) NightlyJob() {
	start := time.Now()
	log.Printf("[netpulse] rollup nocturno: inicio")

	// 1) samples → buckets 5 min (ventana: últimos 48 h con solape de seguridad)
	if err := d.rollupSamplesToBuckets(48 * time.Hour); err != nil {
		log.Printf("[netpulse] error rollup samples→buckets: %v", err)
	} else {
		log.Printf("[netpulse] rollup samples→buckets OK (%s)", time.Since(start).Round(time.Millisecond))
	}

	// 2) buckets → daily (ventana: últimos 35 días con solape)
	if err := d.rollupBucketsToDaily(35 * 24 * time.Hour); err != nil {
		log.Printf("[netpulse] error rollup buckets→daily: %v", err)
	} else {
		log.Printf("[netpulse] rollup buckets→daily OK (%s)", time.Since(start).Round(time.Millisecond))
	}

	// 3) checkpoint tras los DELETE/INSERT grandes (concentra el WAL);
	// en rollback-journal (on-box) no hay WAL que consolidar.
	if !d.rollbackJournal {
		_, _ = d.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	}

	// 4) optimize + ANALYZE (estadísticas del planner tras la purga)
	_, _ = d.Exec("PRAGMA optimize")
	_, _ = d.Exec("ANALYZE")

	// 5) VACUUM solo si el freelist creció (> ~4 MB con page_size 4096)
	var freelist int
	if err := d.QueryRow("PRAGMA freelist_count").Scan(&freelist); err == nil && freelist > 1024 {
		if _, err := d.Exec("VACUUM"); err != nil {
			log.Printf("[netpulse] aviso: VACUUM falló: %v", err)
		} else {
			log.Printf("[netpulse] rollup nocturno: VACUUM tras freelist=%d", freelist)
		}
	}

	// 6) roam_events: retención 30 días (Fase 14.5).
	cutoff := NowMS() - 30*24*60*60*1000
	if res, err := d.Exec("DELETE FROM roam_events WHERE ts_ms < ?", cutoff); err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			log.Printf("[netpulse] roam_events: purgados %d eventos >30d", n)
		}
	}
	// 7) device_events: misma retención 30 días (issue #184).
	if res, err := d.Exec("DELETE FROM device_events WHERE ts_ms < ?", cutoff); err == nil {
		if n, _ := res.RowsAffected(); n > 0 {
			log.Printf("[netpulse] device_events: purgados %d eventos >30d", n)
		}
	}

	log.Printf("[netpulse] rollup nocturno: fin (%s)", time.Since(start).Round(time.Millisecond))

	// Per-port time series rollup (issue #302).
	if d.PortSeries != nil {
		d.PortSeries.NightlyJob()
	}
}

// BucketMS es el tamaño del bucket medio en ms (5 minutos).
const BucketMS = 5 * 60 * 1000

// BucketsRetentionMS: 1 año en ms.
const BucketsRetentionMS = 365 * 24 * 60 * 60 * 1000

func (d *DB) rollupSamplesToBuckets(window time.Duration) error {
	// Ventana con solape: recomputa buckets de los últimos `window` para
	// cubrir caídas del daemon de hasta ese tiempo. INSERT OR REPLACE idempotente.
	since := NowMS() - window.Milliseconds()
	_, err := d.Exec(`
		INSERT OR REPLACE INTO metrics_buckets
			(router_id, bucket_ts, n, min_ts,
			 cpu_min, cpu_max, cpu_avg, ram_min, ram_max, ram_avg,
			 temp_min, temp_max, temp_avg, lat_min, lat_max, lat_avg,
			 rx_min, rx_max, rx_avg, tx_min, tx_max, tx_avg)
		SELECT
			router_id,
			(ts / ?) * ?,
			COUNT(*),
			MIN(ts),
			MIN(cpu), MAX(cpu), AVG(cpu),
			MIN(ram), MAX(ram), AVG(ram),
			MIN(temp), MAX(temp), AVG(temp),
			MIN(latency_ms), MAX(latency_ms), AVG(latency_ms),
			MIN(rx_bps), MAX(rx_bps), AVG(rx_bps),
			MIN(tx_bps), MAX(tx_bps), AVG(tx_bps)
		FROM metrics
		WHERE ts >= ?
		GROUP BY router_id, (ts / ?)`,
		BucketMS, BucketMS, since, BucketMS)
	return err
}

func (d *DB) rollupBucketsToDaily(window time.Duration) error {
	// Agrega buckets → daily por router y día (epoch-UTC). Solape de 35 días
	// para cubrir caídas; INSERT OR REPLACE idempotente.
	since := NowMS() - window.Milliseconds()
	_, err := d.Exec(`
		INSERT OR REPLACE INTO metrics_daily
			(router_id, date, n,
			 cpu_avg, ram_avg, temp_avg, lat_avg, rx_avg, tx_avg,
			 rx_total, tx_total, up_min, up_count)
		SELECT
			router_id,
			strftime('%Y-%m-%d', bucket_ts / 1000, 'unixepoch'),
			SUM(n),
			AVG(cpu_avg), AVG(ram_avg), AVG(temp_avg),
			AVG(lat_avg), AVG(rx_avg), AVG(tx_avg),
			SUM(rx_avg * n), SUM(tx_avg * n),
			SUM(n) * 5 / 60, COUNT(*)
		FROM metrics_buckets
		WHERE bucket_ts >= ?
		GROUP BY router_id, strftime('%Y-%m-%d', bucket_ts / 1000, 'unixepoch')`,
		since)
	return err
}

// ShouldRunNightly decide si toca ejecutar el rollup: una vez al día en hora
// valle (03:00-04:00 local). Hora local del host (como la UI).
func ShouldRunNightly(now time.Time) bool {
	return now.Hour() == 3
}

func (d *DB) maintenanceLoop() {
	defer d.wg.Done()
	t := time.NewTicker(Maintenance)
	defer t.Stop()
	// El ticker arranca en Maintenance; el primer fire llega a la 1ª hora.
	// No ejecutar el rollup al arrancar: los datos raw de los últimos 7 días
	// ya están; el siguiente 03:00 los consolida. Evita competir con el
	// arranque (migraciones, primer poll).
	for {
		select {
		case <-d.stop:
			return
		case now := <-t.C:
			if ShouldRunNightly(now) {
				d.NightlyJob()
			} else {
				d.Maintenance()
			}
		}
	}
}

// CheckHealth replica dbHandle.checkHealth(): SELECT 1.
func (d *DB) CheckHealth() bool {
	var ok int
	if err := d.QueryRow("SELECT 1").Scan(&ok); err != nil {
		return false
	}
	return ok == 1
}

// Close para el timer, checkpoint TRUNCATE (best-effort) y cierra.
func (d *DB) Close() error {
	var err error
	d.close.Do(func() {
		close(d.stop)
		d.wg.Wait()
		_, _ = d.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
		err = d.DB.Close()
	})
	return err
}

// ShutdownContext cierra el pool con contexto (para el salvavidas de 3 s).
func (d *DB) ShutdownContext(ctx context.Context) error {
	done := make(chan error, 1)
	go func() { done <- d.Close() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// KnownMac es una fila de la allowlist (issue #196).
type KnownMac struct {
	MAC       string
	Name      string
	Note      string
	CreatedAt int64
}

// ListKnownMacs devuelve la allowlist ordenada por MAC.
func (d *DB) ListKnownMacs() ([]KnownMac, error) {
	rows, err := d.Query("SELECT mac, name, note, created_at FROM known_macs ORDER BY mac")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []KnownMac{}
	for rows.Next() {
		var k KnownMac
		var note sql.NullString
		if err := rows.Scan(&k.MAC, &k.Name, &note, &k.CreatedAt); err == nil {
			k.Note = note.String
			out = append(out, k)
		}
	}
	return out, rows.Err()
}

// UpsertKnownMac inserta o actualiza una MAC de la allowlist (por MAC).
func (d *DB) UpsertKnownMac(k KnownMac) error {
	_, err := d.Exec(
		`INSERT INTO known_macs (mac, name, note, created_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(mac) DO UPDATE SET name=excluded.name, note=excluded.note`,
		k.MAC, k.Name, k.Note, NowMS())
	return err
}

// DeleteKnownMac quita una MAC de la allowlist.
func (d *DB) DeleteKnownMac(mac string) error {
	_, err := d.Exec("DELETE FROM known_macs WHERE mac = ?", mac)
	return err
}
