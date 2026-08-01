// migrate_e2e_test.go — migración Node→Go END-TO-END sobre HTTP:
// fixture con esquema Node → db.Open (migra) → el usuario importado hace
// login con su password y la sesión importada vale en /api/auth/me.
package httpapi_test

import (
	"database/sql"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/httpapi"
	"github.com/gnacho/netpulse/server-go/internal/sse"
)

func TestMigrationEndToEndHTTP(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "netpulse.db")

	// Fixture Node: esquema literal + admin bcrypt + kv(session_secret) +
	// sesión viva + login_attempts con bloqueo (se resetea en la migración).
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	secret := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	password := "n0de-p4ss"
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	now := db.NowMS()
	ddl := []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL, ua TEXT)`,
		`CREATE TABLE login_attempts (ip TEXT PRIMARY KEY, attempts INTEGER DEFAULT 0, locked_until INTEGER DEFAULT 0)`,
		`CREATE TABLE kv (key TEXT PRIMARY KEY, value TEXT)`,
		`CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT UNIQUE NOT NULL, pass_hash TEXT NOT NULL, role TEXT NOT NULL DEFAULT 'user', created_at INTEGER NOT NULL)`,
		`ALTER TABLE sessions ADD COLUMN user_id INTEGER`,
		`ALTER TABLE users ADD COLUMN language TEXT DEFAULT 'auto'`,
	}
	for _, q := range ddl {
		if _, err := raw.Exec(q); err != nil {
			t.Fatalf("ddl: %v", err)
		}
	}
	if _, err := raw.Exec("INSERT INTO users (username, pass_hash, role, created_at) VALUES ('admin', ?, 'admin', ?)", hash, now); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("INSERT INTO kv (key, value) VALUES ('session_secret', ?)", secret); err != nil {
		t.Fatal(err)
	}
	nodeSessionID := "99999999-8888-4777-8666-555555555555"
	if _, err := raw.Exec("INSERT INTO sessions (id, created_at, expires_at, ua, user_id) VALUES (?, ?, ?, 'Node/1.0', 1)",
		nodeSessionID, now, now+30*24*3600*1000); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("INSERT INTO login_attempts (ip, attempts, locked_until) VALUES ('5.6.7.8', 9, ?)", now+60000); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	// db.Open dispara la migración (backup + reset login_attempts + marcador).
	d, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	cfg, err := config.Load(map[string]string{
		"AUTH_USER": "admin", "AUTH_PASS": password, "DEMO_MODE": "1",
		"DATA_DIR": dir, "NODE_ENV": "test",
	}, dir)
	if err != nil {
		t.Fatal(err)
	}
	gotSecret, err := auth.EnsureSessionSecret(d, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if gotSecret != secret {
		t.Fatal("session_secret debe ser el importado (las sesiones Node sobreviven)")
	}
	if err := auth.EnsureUsers(d, cfg); err != nil {
		t.Fatal(err)
	}
	// El bloqueo de login_attempts de la época Node se reseteó.
	var attempts int
	if err := d.QueryRow("SELECT COUNT(*) FROM login_attempts").Scan(&attempts); err != nil || attempts != 0 {
		t.Fatalf("login_attempts reseteado: %v n=%d", err, attempts)
	}

	handler := httpapi.NewHandler(httpapi.Deps{
		Config: cfg, DB: d, Adapter: adapters.NewDemo(),
		Hub:    sse.NewHub(d, cfg.MaxSSEClients, func() any { return nil }),
		Secret: gotSecret, Started: time.Now(),
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	// 1) El usuario importado hace login con su password → 204 + cookie.
	status, _, _ := loginCookie(t, srv.URL, "admin", password)
	if status != 204 {
		t.Fatalf("login tras migración: got %d want 204", status)
	}

	// 2) La sesión importada es válida en /api/auth/me (SIN re-login).
	cookie := auth.SignSessionID(secret, nodeSessionID)
	res := get(t, srv.URL, "/api/auth/me", cookie)
	body := readJSON(t, res)
	if res.StatusCode != 200 || body["user"] != "admin" || body["role"] != "admin" || body["language"] != "auto" || body["mode"] != "demo" {
		t.Fatalf("me con sesión importada: %d %v", res.StatusCode, body)
	}

	// 3) Una cookie firmada con OTRO secret no vale (el secret manda).
	forged := auth.SignSessionID("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", nodeSessionID)
	res = get(t, srv.URL, "/api/auth/me", forged)
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("cookie con secret ajeno: got %d want 401", res.StatusCode)
	}

	// 4) El backup existe.
	matches, _ := filepath.Glob(dbPath + ".bak-*")
	if len(matches) != 1 {
		t.Fatalf("backup: %v", matches)
	}
}
