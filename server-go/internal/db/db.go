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
)

const (
	RetentionMS  = 7 * 24 * 60 * 60 * 1000 // 7 días
	Maintenance  = time.Hour
	databaseFile = "netpulse.db"
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
  latency_ms REAL, rx_bps REAL, tx_bps REAL
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
  up_min INTEGER NOT NULL DEFAULT 0,
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
-- Suscripciones Web Push (Fase 3 Bloque C; sin paridad Node: el push es
-- nuevo en Go). created_at en milisegundos como el resto de epochs.
CREATE TABLE IF NOT EXISTS push_subscriptions (
  endpoint TEXT PRIMARY KEY,
  keys_auth TEXT NOT NULL,
  keys_p256dh TEXT NOT NULL,
  user_agent TEXT,
  created_at INTEGER NOT NULL
);
`

// DB envuelve *sql.DB con los jobs y helpers de paridad.
type DB struct {
	*sql.DB
	Path  string
	stop  chan struct{}
	wg    sync.WaitGroup
	close sync.Once
}

// NowMS devuelve el epoch actual en milisegundos (como Date.now()).
func NowMS() int64 { return time.Now().UnixMilli() }

// Open abre (o crea) DATA_DIR/netpulse.db, ejecuta la migración Node→Go si la
// DB ya existía con esquema Node (backup atómico antes de tocar nada),
// aplica pragmas, crea el schema y arranca el mantenimiento horario.
func Open(dataDir string) (*DB, error) {
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

	for _, p := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
	} {
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

	// Si no hubo migración Node (instalación fresca creada por Go), marca la
	// DB para que el siguiente arranque no dispare una "migración" espuria
	// (backup + reset de login_attempts) sobre una DB ya Go.
	if rep == nil {
		_, _ = sqldb.Exec(
			"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO NOTHING",
			migrationMarkerKey, time.Now().Format(time.RFC3339),
		)
	}

	d := &DB{DB: sqldb, Path: dbPath, stop: make(chan struct{})}
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
// retención 7 días en metrics/adguard_stats, sesiones expiradas y checkpoint.
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
	// Purga de buckets > 1 año (el raw ya se purgó arriba; daily nunca).
	bucketCutoff := NowMS() - BucketsRetentionMS
	if _, err := d.Exec("DELETE FROM metrics_buckets WHERE bucket_ts < ?", bucketCutoff); err != nil {
		log.Printf("[netpulse] error en mantenimiento DB: %v", err)
	}
	if _, err := d.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		log.Printf("[netpulse] error en mantenimiento DB: %v", err)
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

	// 3) checkpoint tras los DELETE/INSERT grandes (concentra el WAL)
	_, _ = d.Exec("PRAGMA wal_checkpoint(TRUNCATE)")

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

	log.Printf("[netpulse] rollup nocturno: fin (%s)", time.Since(start).Round(time.Millisecond))
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
			MIN(min_ts), COUNT(*)
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
