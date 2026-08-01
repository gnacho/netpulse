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
	if _, err := d.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		log.Printf("[netpulse] error en mantenimiento DB: %v", err)
	}
}

func (d *DB) maintenanceLoop() {
	defer d.wg.Done()
	t := time.NewTicker(Maintenance)
	defer t.Stop()
	for {
		select {
		case <-d.stop:
			return
		case <-t.C:
			d.Maintenance()
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
