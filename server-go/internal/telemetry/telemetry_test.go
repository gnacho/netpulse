// telemetry_test.go — #822: el ping anónimo solo lleva id+versión+os/arch,
// se desactiva con NETPULSE_TELEMETRY=0 y no reenvía antes de 23 h.
package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestDisabledByEnvAndDemo(t *testing.T) {
	t.Setenv("NETPULSE_TELEMETRY", "0")
	if New(openTestDB(t), "v1", false).Enabled() {
		t.Fatal("NETPULSE_TELEMETRY=0 debería desactivar el aviso")
	}
	t.Setenv("NETPULSE_TELEMETRY", "1")
	if New(openTestDB(t), "v1", true).Enabled() {
		t.Fatal("DEMO_MODE debería desactivar el aviso")
	}
}

func TestSendOncePayloadAndLastSent(t *testing.T) {
	var gotPath, gotQuery, gotUA string
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		gotPath, gotQuery, gotUA = r.URL.Path, r.URL.RawQuery, r.UserAgent()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	t.Setenv("NETPULSE_TELEMETRY", "1")
	t.Setenv("NETPULSE_TELEMETRY_URL", srv.URL+"/instances")
	d := openTestDB(t)
	a := New(d, "v9.9.9", false)
	a.newID = func() (string, error) { return "abc123", nil }
	fixed := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return fixed }

	if err := a.SendOnce(context.Background()); err != nil {
		t.Fatalf("SendOnce: %v", err)
	}
	if hits != 1 {
		t.Fatalf("hits = %d, esperaba 1", hits)
	}
	if gotPath != "/instances/abc123" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotUA != "netpulse-instance" {
		t.Fatalf("ua = %q", gotUA)
	}
	// El query solo puede llevar v y os.
	for _, kv := range strings.Split(gotQuery, "&") {
		if kv == "" {
			continue
		}
		k := strings.SplitN(kv, "=", 2)[0]
		if k != "v" && k != "os" {
			t.Fatalf("parámetro inesperado en el ping: %q", k)
		}
	}
	if !strings.Contains(gotQuery, "v=v9.9.9") {
		t.Fatalf("falta la versión: %q", gotQuery)
	}
	// Tras enviar, no vuelve a tocar hasta pasadas 23 h.
	if a.Due() {
		t.Fatal("no debería tocar reenviar justo después")
	}
	a.now = func() time.Time { return fixed.Add(24 * time.Hour) }
	if !a.Due() {
		t.Fatal("debería tocar reenviar a las 24 h")
	}
	if err := a.SendOnce(context.Background()); err != nil {
		t.Fatalf("SendOnce 2: %v", err)
	}
	if hits != 2 {
		t.Fatalf("hits = %d, esperaba 2", hits)
	}
}

func TestInstallIDStableAndPersisted(t *testing.T) {
	t.Setenv("NETPULSE_TELEMETRY", "1")
	d := openTestDB(t)
	a := New(d, "v1", false)
	id1, err := a.installID()
	if err != nil || len(id1) != 32 {
		t.Fatalf("installID = %q, %v", id1, err)
	}
	id2, _ := a.installID()
	if id1 != id2 {
		t.Fatalf("el id no es estable: %q != %q", id1, id2)
	}
	// Un agente nuevo sobre la misma BD reutiliza el id.
	a2 := New(d, "v1", false)
	id3, _ := a2.installID()
	if id3 != id1 {
		t.Fatalf("el id debería persistir entre arranques: %q != %q", id3, id1)
	}
}

func TestHTTPErrorDoesNotMarkSent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	t.Setenv("NETPULSE_TELEMETRY", "1")
	t.Setenv("NETPULSE_TELEMETRY_URL", srv.URL+"/instances")
	d := openTestDB(t)
	a := New(d, "v1", false)
	if err := a.SendOnce(context.Background()); err == nil {
		t.Fatal("un 404 debería devolver error")
	}
	if !a.Due() {
		t.Fatal("un fallo no debe marcar como enviado (se reintentará)")
	}
}
