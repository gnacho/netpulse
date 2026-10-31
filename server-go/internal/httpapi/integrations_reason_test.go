package httpapi_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/rintegration"
)

// TestIntegrationsOfflineReasonEnFilas (#1718): la lista de integraciones
// lleva el motivo offline clasificado con los mismos códigos que la tarjeta
// y el detalle (#1714). Fresca o pausada no llevan motivo.
func TestIntegrationsOfflineReasonEnFilas(t *testing.T) {
	ts, mgr := makeIntegrationsTestServer(t)
	store := rintegration.NewStore(ts.db.DB)
	now := time.Now().Unix()

	// Fresca: empujó hace 10 s con intervalo 45 → sin motivo.
	fresh := bootIntegrationRow("fresh-push", "routeros", "local", true, "running")
	if err := store.Upsert(fresh); err != nil {
		t.Fatal(err)
	}
	// Stale (running huérfano): push viejo, sin errores → sin señal clara.
	stale := bootIntegrationRow("stale-noerr", "routeros", "local", true, "running")
	if err := store.Upsert(stale); err != nil {
		t.Fatal(err)
	}
	// Error de instalación con credenciales rechazadas en LastError.
	errRow := bootIntegrationRow("err-401", "routeros", "local", true, "error")
	errRow.LastError = `exit status 1: 401 Unauthorized: invalid user name or password (rosapi)`
	if err := store.Upsert(errRow); err != nil {
		t.Fatal(err)
	}
	// Stale cuyo último fallo de poll (log del pusher) es endpoint ausente.
	logRow := bootIntegrationRow("log-404", "routeros", "local", true, "running")
	if err := store.Upsert(logRow); err != nil {
		t.Fatal(err)
	}
	logBody := `time=2026-10-31T12:00:00+01:00 level=ERROR msg="routeros-pusher: collection failed" err="404 Not Found: no REST API"`
	if err := os.MkdirAll(mgr.Dir(), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(mgr.Dir(), "log-404.log"), []byte(logBody), 0o600); err != nil {
		t.Fatalf("log: %v", err)
	}
	// Parada intencional → pusher_stopped.
	stopped := bootIntegrationRow("stopped", "routeros", "local", true, "stopped")
	if err := store.Upsert(stopped); err != nil {
		t.Fatal(err)
	}
	// Pausada: aunque tenga error, la UI ya la pinta como pausa.
	paused := bootIntegrationRow("paused", "routeros", "local", false, "error")
	paused.LastError = "anything"
	if err := store.Upsert(paused); err != nil {
		t.Fatal(err)
	}

	for _, slug := range []string{"fresh-push", "stale-noerr", "err-401", "log-404", "stopped", "paused"} {
		if slug == "fresh-push" {
			continue
		}
		sec := now - 200
		if slug == "stopped" || slug == "paused" {
			sec = now - 10 // el estado manda antes que la frescura solo en stopped; paused se corta por enabled
		}
		if _, err := ts.db.Exec(
			"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
			"agent.last_push_ts."+slug, strconv.FormatInt(sec, 10),
		); err != nil {
			t.Fatal(err)
		}
	}

	items := integrationsBySlug(t, ts)
	want := map[string]string{
		"fresh-push":  "",
		"stale-noerr": "",
		"err-401":     rintegration.ReasonAuthFailed,
		"log-404":     rintegration.ReasonAPINotFound,
		"stopped":     rintegration.ReasonStopped,
		"paused":      "",
	}
	for slug, code := range want {
		it := items[slug]
		if it == nil {
			t.Fatalf("%s no está en la lista", slug)
		}
		got, _ := it["offlineReason"].(string)
		if got != code {
			t.Fatalf("%s: offlineReason = %q, want %q (item: %v)", slug, got, code, it)
		}
	}
}
