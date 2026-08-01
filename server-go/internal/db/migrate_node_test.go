// migrate_node_test.go — migración Node→Go con fixture: DB creada con el SQL
// literal del db.js de Node (esquema + columnas migradas) y datos de verdad.
package db_test

import (
	"database/sql"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

// nodeSchemaDDL es el DDL literal de src/db.js:20-85 + las 3 migraciones
// (columnas ua, user_id, language) que una DB Node real ya tiene aplicadas.
const nodeSchemaDDL = `
CREATE TABLE sessions (id TEXT PRIMARY KEY, created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, ua TEXT);
CREATE TABLE login_attempts (ip TEXT PRIMARY KEY, attempts INTEGER DEFAULT 0, locked_until INTEGER DEFAULT 0);
CREATE TABLE kv (key TEXT PRIMARY KEY, value TEXT);
CREATE TABLE metrics (router_id TEXT NOT NULL, ts INTEGER NOT NULL, cpu REAL, ram REAL, temp REAL, latency_ms REAL, rx_bps REAL, tx_bps REAL);
CREATE INDEX idx_metrics_router_ts ON metrics(router_id, ts);
CREATE TABLE adguard_stats (ts INTEGER NOT NULL, queries INTEGER, blocked INTEGER);
CREATE INDEX idx_adguard_ts ON adguard_stats(ts);
CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT UNIQUE NOT NULL, pass_hash TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'user', created_at INTEGER NOT NULL);
CREATE TABLE device_attrib (mac TEXT PRIMARY KEY, router_id TEXT, band TEXT, signal_dbm INTEGER, last_seen INTEGER NOT NULL);
CREATE TABLE routers (id TEXT PRIMARY KEY, name TEXT, host TEXT NOT NULL, type TEXT NOT NULL DEFAULT 'openwrt', is_gateway INTEGER NOT NULL DEFAULT 0, created_at INTEGER NOT NULL);
ALTER TABLE sessions ADD COLUMN user_id INTEGER;
ALTER TABLE users ADD COLUMN language TEXT DEFAULT 'auto';
`

const fixtureSecret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
const fixturePassword = "n0de-p4ss"
const fixtureSessionID = "11111111-2222-4333-8444-555555555555"

// buildNodeFixture crea una DB Node en dir con: admin bcrypt, kv completo,
// sesión viva, routers, device_attrib, metrics, adguard_stats y login_attempts.
func buildNodeFixture(t *testing.T, dir string) string {
	t.Helper()
	dbPath := filepath.Join(dir, "netpulse.db")
	sqldb, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer sqldb.Close()
	if _, err := sqldb.Exec("PRAGMA journal_mode = WAL"); err != nil {
		t.Fatal(err)
	}
	if _, err := sqldb.Exec(nodeSchemaDDL); err != nil {
		t.Fatalf("schema fixture: %v", err)
	}
	hash, err := auth.HashPassword(fixturePassword)
	if err != nil {
		t.Fatal(err)
	}
	now := db.NowMS()
	stmts := []struct {
		q    string
		args []any
	}{
		{"INSERT INTO users (username, pass_hash, role, created_at, language) VALUES ('admin', ?, 'admin', ?, 'es')", []any{hash, now}},
		{"INSERT INTO kv (key, value) VALUES ('session_secret', ?)", []any{fixtureSecret}},
		{"INSERT INTO kv (key, value) VALUES ('attrib_v2', '1')", nil},
		{"INSERT INTO kv (key, value) VALUES ('adguard_host', '192.168.8.1')", nil},
		{"INSERT INTO kv (key, value) VALUES ('auth_pass_hash', '$2a$10$legacydeadbeef')", nil}, // legacy muerto: no debe romper
		// Sesión viva (30 d) del admin — debe seguir válida tras la migración.
		{"INSERT INTO sessions (id, created_at, expires_at, ua, user_id) VALUES (?, ?, ?, 'NodeAgent/1.0', 1)", []any{fixtureSessionID, now, now + 30*24*3600*1000}},
		{"INSERT INTO routers (id, name, host, type, is_gateway, created_at) VALUES ('flint2', 'Gateway', '192.168.8.1', 'glinet', 1, ?)", []any{now}},
		{"INSERT INTO device_attrib (mac, router_id, band, signal_dbm, last_seen) VALUES ('AA:BB:CC:DD:EE:FF', 'flint2', '5 GHz', -50, ?)", []any{now}},
		{"INSERT INTO metrics (router_id, ts, cpu, ram, temp, latency_ms, rx_bps, tx_bps) VALUES ('flint2', ?, 23, 41, 54, 8, 84000000, 12600000)", []any{now}},
		{"INSERT INTO adguard_stats (ts, queries, blocked) VALUES (?, 84312, 15687)", []any{now}},
		{"INSERT INTO login_attempts (ip, attempts, locked_until) VALUES ('1.2.3.4', 7, ?)", []any{now + 300000}},
	}
	for _, s := range stmts {
		if _, err := sqldb.Exec(s.q, s.args...); err != nil {
			t.Fatalf("fixture %q: %v", s.q, err)
		}
	}
	if _, err := sqldb.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}
	return dbPath
}

func TestMigrateNodeDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := buildNodeFixture(t, dir)

	rep, err := db.MigrateNodeDB(dbPath)
	if err != nil {
		t.Fatalf("migración: %v", err)
	}
	if rep == nil {
		t.Fatal("debe detectar la DB Node y migrar")
	}
	// Conteos del informe
	if rep.Users != 1 || rep.Sessions != 1 || rep.Routers != 1 || rep.DeviceAttrib != 1 ||
		rep.Metrics != 1 || rep.AdguardStats != 1 || rep.LoginAttemptsReset != 1 {
		t.Fatalf("reporte: %+v", rep)
	}
	if rep.KV != 4 {
		t.Fatalf("kv: %+v", rep)
	}
	// Backup existe y es una SQLite válida con los datos
	if _, err := os.Stat(rep.BackupPath); err != nil {
		t.Fatalf("backup: %v", err)
	}
	if !strings.Contains(filepath.Base(rep.BackupPath), "netpulse.db.bak-") {
		t.Fatalf("nombre de backup: %s", rep.BackupPath)
	}
	bak, err := sql.Open("sqlite", rep.BackupPath+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer bak.Close()
	var n int
	if err := bak.QueryRow("SELECT COUNT(*) FROM users").Scan(&n); err != nil || n != 1 {
		t.Fatalf("backup users: %v n=%d", err, n)
	}
	// El backup conserva la fila de login_attempts (copia previa al reset).
	if err := bak.QueryRow("SELECT COUNT(*) FROM login_attempts").Scan(&n); err != nil || n != 1 {
		t.Fatalf("backup login_attempts: %v n=%d", err, n)
	}

	// La DB migrada: login_attempts reseteada, datos preservados, marcador puesto.
	d, err := db.Open(dir)
	if err != nil {
		t.Fatalf("open migrada: %v", err)
	}
	defer d.Close()
	if err := d.QueryRow("SELECT COUNT(*) FROM login_attempts").Scan(&n); err != nil || n != 0 {
		t.Fatalf("login_attempts debe quedar reseteada: %v n=%d", err, n)
	}
	var secret string
	if err := d.QueryRow("SELECT value FROM kv WHERE key = 'session_secret'").Scan(&secret); err != nil || secret != fixtureSecret {
		t.Fatalf("session_secret preservado: %v %q", err, secret)
	}
	if err := d.QueryRow("SELECT COUNT(*) FROM sessions WHERE id = ?", fixtureSessionID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("sesión viva preservada: %v n=%d", err, n)
	}
	if err := d.QueryRow("SELECT COUNT(*) FROM routers WHERE id = 'flint2' AND is_gateway = 1").Scan(&n); err != nil || n != 1 {
		t.Fatalf("routers preservados: %v n=%d", err, n)
	}
	var lang string
	if err := d.QueryRow("SELECT language FROM users WHERE username = 'admin'").Scan(&lang); err != nil || lang != "es" {
		t.Fatalf("language preservado: %v %q", err, lang)
	}

	// Idempotencia: una segunda migración no hace nada (el rate-limit
	// persistente de Node no se borra en cada arranque).
	rep2, err := db.MigrateNodeDB(dbPath)
	if err != nil || rep2 != nil {
		t.Fatalf("segunda migración debe ser no-op: %v %+v", err, rep2)
	}
}

// TestMigrationLoginAndSession: el usuario importado hace login con su
// password y la sesión importada sigue válida (SIN re-login forzado).
func TestMigrationLoginAndSession(t *testing.T) {
	dir := t.TempDir()
	buildNodeFixture(t, dir)

	d, err := db.Open(dir) // dispara la migración
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	cfg := &config.Config{AuthUser: "admin", AuthPass: fixturePassword, CookieSecure: "auto"}
	secret, err := auth.EnsureSessionSecret(d, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if secret != fixtureSecret {
		t.Fatal("el secret debe ser el importado de kv (no regenerado)")
	}
	// EnsureUsers NO re-siembra (ya hay usuarios).
	if err := auth.EnsureUsers(d, cfg); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = d.QueryRow("SELECT COUNT(*) FROM users").Scan(&n)
	if n != 1 {
		t.Fatalf("users: %d (no debe duplicar el admin)", n)
	}

	// 1) Login con la password de la época Node (hash bcrypt $2a portable).
	login := auth.HandleLogin(d, secret, httptest.NewRequest("POST", "/api/auth/login", nil), "admin", fixturePassword)
	if login == nil {
		t.Fatal("el usuario importado debe poder hacer login con su password")
	}
	if auth.HandleLogin(d, secret, httptest.NewRequest("POST", "/api/auth/login", nil), "admin", "otra-cosa") != nil {
		t.Fatal("password incorrecta debe fallar")
	}

	// 2) La sesión importada sigue válida: cookie firmada con el secret
	// importado autentica como el admin (sin re-login).
	signed := auth.SignSessionID(secret, fixtureSessionID)
	sess := auth.GetSession(d, auth.VerifySessionCookie(secret, signed))
	if sess == nil || !sess.UserID.Valid || sess.UserID.Int64 != 1 {
		t.Fatalf("sesión importada inválida: %+v", sess)
	}
}
