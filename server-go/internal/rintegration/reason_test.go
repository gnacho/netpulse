package rintegration

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func TestClassifyPollError(t *testing.T) {
	cases := []struct {
		msg  string
		want string
	}{
		{"", ""},
		// REST 401 y login fallido por la API binaria.
		{`routeros: GET /rest/system/resource: 401 Unauthorized: {"message":"bad username and password"}`, ReasonAuthFailed},
		{`routeros api: login: invalid user name or password`, ReasonAuthFailed},
		// Endpoint ausente: firmware sin REST (RouterOS 6 con solo www).
		{`routeros: GET /rest/system/resource: 404 Not Found: <html>`, ReasonAPINotFound},
		// Familia de conexión.
		{`routeros api: dial tcp 10.0.0.1:8728: connect: connection refused; rest: dial tcp: connection refused`, ReasonConnRefused},
		{`routeros: GET /rest/system/resource: context deadline exceeded (i/o timeout)`, ReasonConnRefused},
		// Cualquier otro error no vacío cae al genérico.
		{"routeros: no rows from /system/resource/print", ReasonError},
	}
	for _, c := range cases {
		if got := ClassifyPollError(c.msg); got != c.want {
			t.Errorf("ClassifyPollError(%q) = %q, want %q", c.msg, got, c.want)
		}
	}
}

// writeTestLog añade body al log del slug, igual que hace el Manager.
func writeTestLog(t *testing.T, dir, slug, body string) {
	t.Helper()
	p := filepath.Join(dir, slug+".log")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	if _, err := f.WriteString(body); err != nil {
		t.Fatalf("write log: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close log: %v", err)
	}
}

func setTestLastPush(t *testing.T, d *sql.DB, slug string, sec int64) {
	t.Helper()
	_, err := d.Exec(
		"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value",
		"agent.last_push_ts."+slug, fmt.Sprintf("%d", sec),
	)
	if err != nil {
		t.Fatalf("setLastPush: %v", err)
	}
}

func TestLastPollError(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(dir)

	// Sin log: vacío y sin error.
	if got := m.LastPollError("hex"); got != "" {
		t.Fatalf("empty log: got %q", got)
	}

	// Log con una línea limpia y la última de fallo con escapes de slog.
	writeTestLog(t, dir, "hex", strings.Join([]string{
		`2026-10-31T09:00:00Z INF routeros-pusher: pushed slug=hex`,
		`2026-10-31T09:01:00Z ERR routeros-pusher: collection failed err="routeros api: dial tcp: connection refused; rest: 401 Unauthorized"`,
	}, "\n")+"\n")
	got := m.LastPollError("hex")
	want := `routeros api: dial tcp: connection refused; rest: 401 Unauthorized`
	if got != want {
		t.Fatalf("LastPollError = %q, want %q", got, want)
	}

	// Varias líneas de fallo: gana la ÚLTIMA.
	writeTestLog(t, dir, "hex", `2026-10-31T09:02:00Z ERR routeros-pusher: collection failed err="routeros: GET /rest/system/resource: 404 Not Found"`+"\n")
	if got := m.LastPollError("hex"); !strings.Contains(got, "404") {
		t.Fatalf("LastPollError = %q, want the latest 404 line", got)
	}

	// Escapes de slog dentro del valor (comillas y barra invertida).
	writeTestLog(t, dir, "esc", `2026-10-31T09:03:00Z ERR routeros-pusher: collection failed err="rest: bad reply \"x\" + \\ tail"`+"\n")
	if got := m.LastPollError("esc"); got != `rest: bad reply "x" + \ tail` {
		t.Fatalf("unescape: got %q", got)
	}
}

func TestStatusReasons(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	st := NewStore(d.DB)
	dir := t.TempDir()
	m := NewManager(dir)

	// pusher parado: motivo propio, sin mirar el log.
	if err := st.Upsert(Row{Slug: "hex", Vendor: "routeros", IntervalS: 30, Enabled: true, State: "stopped"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	// integración deshabilitada: se omite.
	if err := st.Upsert(Row{Slug: "off", Vendor: "routeros", IntervalS: 30, Enabled: false, State: "running"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	// integración con push fresco: se omite aunque el log tenga fallos.
	if err := st.Upsert(Row{Slug: "fresh", Vendor: "routeros", IntervalS: 30, Enabled: true, State: "running"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	setTestLastPush(t, d.DB, "fresh", time.Now().Unix())
	writeTestLog(t, dir, "fresh", `2026-10-31T09:00:00Z ERR routeros-pusher: collection failed err="x: 401 Unauthorized"`+"\n")
	// integración stale con fallo de poll en el log: clasifica.
	if err := st.Upsert(Row{Slug: "hexbad", Vendor: "routeros", IntervalS: 30, Enabled: true, State: "running"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	setTestLastPush(t, d.DB, "hexbad", time.Now().Add(-time.Hour).Unix())
	writeTestLog(t, dir, "hexbad", `2026-10-31T09:00:00Z ERR routeros-pusher: collection failed err="routeros: GET /rest/system/resource: 401 Unauthorized"`+"\n")
	// integración stale sin log ni LastError: sin motivo (no está en el mapa).
	if err := st.Upsert(Row{Slug: "silent", Vendor: "routeros", IntervalS: 30, Enabled: true, State: "running"}); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	got := StatusReasons(d.DB, m)
	if got["hex"] != ReasonStopped {
		t.Errorf("hex: got %q, want %q", got["hex"], ReasonStopped)
	}
	if _, ok := got["off"]; ok {
		t.Errorf("disabled integration must be skipped, got %q", got["off"])
	}
	if _, ok := got["fresh"]; ok {
		t.Errorf("fresh integration must be skipped, got %q", got["fresh"])
	}
	if got["hexbad"] != ReasonAuthFailed {
		t.Errorf("hexbad: got %q, want %q", got["hexbad"], ReasonAuthFailed)
	}
	if _, ok := got["silent"]; ok {
		t.Errorf("silent stale integration must have no reason, got %q", got["silent"])
	}

	// nil-safety: sin db o sin manager no hay mapa.
	if StatusReasons(nil, m) != nil || StatusReasons(d.DB, nil) != nil {
		t.Error("nil db/manager must return nil map")
	}
}
