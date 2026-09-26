// rtlconsole_test.go — #639: sondeo HTTP de la consola RTLPlayground.
// El test levanta un httptest que emula el firmware (login por POST /login
// con Set-Cookie session, /information.json con sw_ver/hw_ver, /cmd time con
// el contador en hex) y verifica fetch/snapshot/attach contra él.
package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
)

// srvRequests cuenta las peticiones que recibe el servidor fake de consola
// (issue #863: con el toggle OFF no debe recibir NINGUNA).
var srvRequests int32

// newRTLConsoleServer: emula la consola RTLPlayground. uptimeSec es el valor
// que devuelve /cmd time (en segundos); el servidor lo convierte a "0x…".
func newRTLConsoleServer(t *testing.T, uptimeSec uint64) *httptest.Server {
	t.Helper()
	atomic.StoreInt32(&srvRequests, 0)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&srvRequests, 1)
	})
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil || r.Form.Get("pwd") != "1234" {
			http.Error(w, "bad", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Set-Cookie", "session=test-session-abc; SameSite=Strict")
		w.Header().Set("Location", "index.html")
		w.WriteHeader(http.StatusFound)
	})
	mux.HandleFunc("/information.json", func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Cookie"), "session=") {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"sw_ver":"v0.1.0-e1fa080-dirty","hw_ver":"keepLink KP-9000-9XHML-X V3.1","mac_address":"78:d8:00:32:31:49"}`)
	})
	mux.HandleFunc("/cmd", func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Cookie"), "session=") {
			http.Error(w, "no auth", http.StatusUnauthorized)
			return
		}
		// El firmware exige Content-Type en todo POST (sin él responde 404).
		if r.Header.Get("Content-Type") == "" {
			http.Error(w, "no content type", http.StatusNotFound)
			return
		}
		body := make([]byte, 32)
		n, _ := r.Body.Read(body)
		if strings.TrimSpace(string(body[:n])) != "time" {
			http.Error(w, "unknown command", http.StatusBadRequest)
			return
		}
		fmt.Fprintf(w, "0x%08x\n", uptimeSec)
	})
	return httptest.NewServer(mux)
}

func TestRtlConsoleFetch(t *testing.T) {
	srv := newRTLConsoleServer(t, 167581) // ~46.5 h
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	c := newRtlConsoleCache("1234", 300)
	e, err := c.fetch(host)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if e.Firmware != "v0.1.0-e1fa080-dirty" {
		t.Fatalf("Firmware = %q, esperaba v0.1.0-e1fa080-dirty", e.Firmware)
	}
	if e.Model != "keepLink KP-9000-9XHML-X V3.1" {
		t.Fatalf("Model = %q", e.Model)
	}
	if e.MAC != "78:d8:00:32:31:49" {
		t.Fatalf("MAC = %q, esperaba 78:d8:00:32:31:49", e.MAC)
	}
	// bootUnix ≈ now - 167581s
	age := time.Since(e.BootUnix)
	if age < 167580*time.Second || age > 167582*time.Second {
		t.Fatalf("BootUnix no cuadra con uptime 167581s: age=%v", age)
	}
}

func TestRtlConsoleSnapshotAttachesSystem(t *testing.T) {
	srv := newRTLConsoleServer(t, 3600)
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	c := newRtlConsoleCache("1234", 300)
	// snapshot dispara el poll en background; esperamos a que haya datos.
	var e *rtlConsoleEntry
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		e = c.snapshot("switch16", host)
		if e != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if e == nil {
		t.Fatal("snapshot no produjo datos en 3 s")
	}

	// attach sobre un payload de beacon: System debe llevar board+uptime.
	s := &server{rtlConsole: c}
	pl := &probe.Payload{Data: probe.PayloadData{}}
	s.attachRTLConsole("switch16", host, pl)
	if pl.Data.System == nil {
		t.Fatal("attach no adjuntó System")
	}
	if pl.Data.System.Board == nil || pl.Data.System.Board.Release.Description != "RTLPlayground v0.1.0-e1fa080-dirty" {
		t.Fatalf("Board inesperado: %+v", pl.Data.System.Board)
	}
	// uptime derivado ≈ 1h (3600 s) ± margen del poll.
	if pl.Data.System.SysInfo == nil || pl.Data.System.SysInfo.Uptime < 3590 || pl.Data.System.SysInfo.Uptime > 3700 {
		t.Fatalf("Uptime inesperado: %+v", pl.Data.System.SysInfo)
	}
	// BridgeMAC normalizada a mayúsculas (formato canónico del resto de la app).
	if pl.Data.System.BridgeMAC != "78:D8:00:32:31:49" {
		t.Fatalf("BridgeMAC = %q, esperaba 78:D8:00:32:31:49", pl.Data.System.BridgeMAC)
	}
}

func TestRtlConsoleInvalidaTrasReboot(t *testing.T) {
	srv := newRTLConsoleServer(t, 3600)
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	c := newRtlConsoleCache("1234", 300)
	var e *rtlConsoleEntry
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		e = c.snapshot("switch16", host)
		if e != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if e == nil {
		t.Fatal("snapshot inicial sin datos")
	}
	c.invalidate("switch16")
	c.mu.Lock()
	polledAt := c.byID["switch16"].PolledAt
	c.mu.Unlock()
	if !polledAt.IsZero() {
		t.Fatalf("invalidate no reseteó PolledAt: %v", polledAt)
	}
}

func TestRtlConsoleFetchLoginFalloNoAdjunta(t *testing.T) {
	// El servidor rechaza el login (pass distinta): fetch devuelve error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	c := newRtlConsoleCache("1234", 300)
	if _, err := c.fetch(host); err == nil {
		t.Fatal("fetch debería fallar con login 401")
	}
}

// #785: el parser de uptime debe aceptar el formato nuevo (Tick/Sec counter)
// además del hex plano legado.
func TestParseTimeUptimeFormats(t *testing.T) {
	cases := []struct {
		name string
		body string
		want uint64
	}{
		{"legado hex con 0x", "0x00028e9d\n", 0x28e9d},
		{"legado hex sin prefijo", "00028e9d", 0x28e9d},
		{"nuevo dos líneas (KP-9000 v0.1.0-ad9e2ee)", "  Tick counter: 0x0244599a   Sec Counter: 0x0002e6d0\n", 0x2e6d0},
		{"nuevo con saltos de línea", "Tick counter: 0x0244599a\nSec Counter: 0x0002e6d0\n", 0x2e6d0},
		{"nuevo sec counter sin 0x", "Tick counter: 0x99   Sec Counter: 2e6d0", 0x2e6d0},
	}
	for _, tc := range cases {
		got, err := parseTimeUptime(tc.body)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Fatalf("%s: uptime = %#x, want %#x", tc.name, got, tc.want)
		}
	}

	for _, bad := range []string{"", "garbage", "Tick counter: 0x1", "Sec Counter: zz"} {
		if _, err := parseTimeUptime(bad); err == nil {
			t.Fatalf("entrada inválida %q no devolvió error", bad)
		}
	}
}

// #863: con el toggle de sondeo por consola DESACTIVADO para el router, el
// attach no consulta la consola (cero requests al firmware: cada login tumba
// la sesión humana) y no inyecta System. Con el toggle activo (default) el
// comportamiento es el de siempre.
func TestRtlConsoleTogglePorRouter(t *testing.T) {
	srv := newRTLConsoleServer(t, 3600)
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	cfg := &config.Config{DataDir: t.TempDir(), AuthUser: "admin", AuthPass: "test12345678"}
	d, err := db.Open(cfg.DataDir)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	off := false
	on := true
	if _, err := routerstore.AddRouter(d.DB, routerstore.AddInput{
		Name: "switch-off", Host: host, Type: "managed-switch", ConsolePolling: &off,
	}); err != nil {
		t.Fatalf("add switch-off: %v", err)
	}
	if _, err := routerstore.AddRouter(d.DB, routerstore.AddInput{
		Name: "switch-on", Host: host, Type: "managed-switch", ConsolePolling: &on,
	}); err != nil {
		t.Fatalf("add switch-on: %v", err)
	}
	// Un tercer router heredado: columna a DEFAULT 1 (alta sin el campo).
	if _, err := routerstore.AddRouter(d.DB, routerstore.AddInput{
		Name: "switch-legacy", Host: host, Type: "managed-switch",
	}); err != nil {
		t.Fatalf("add switch-legacy: %v", err)
	}

	c := newRtlConsoleCache("1234", 300)
	s := &server{rtlConsole: c, db: d}

	// OFF: el attach no toca la consola ni inyecta System.
	plOff := &probe.Payload{Data: probe.PayloadData{}}
	s.attachRTLConsole("switch-off", host, plOff)
	if plOff.Data.System != nil {
		t.Fatalf("toggle OFF inyectó System: %+v", plOff.Data.System)
	}
	if got := atomic.LoadInt32(&srvRequests); got != 0 {
		t.Fatalf("toggle OFF hizo %d requests a la consola, esperaba 0", got)
	}

	// ON explícito: inyecta System como siempre.
	plOn := &probe.Payload{Data: probe.PayloadData{}}
	s.attachRTLConsole("switch-on", host, plOn)
	deadline := time.Now().Add(3 * time.Second)
	for plOn.Data.System == nil && time.Now().Before(deadline) {
		s.attachRTLConsole("switch-on", host, plOn)
		time.Sleep(50 * time.Millisecond)
	}
	if plOn.Data.System == nil {
		t.Fatal("toggle ON no inyectó System")
	}

	// Legacy (DEFAULT 1): también inyecta.
	plLegacy := &probe.Payload{Data: probe.PayloadData{}}
	s.attachRTLConsole("switch-legacy", host, plLegacy)
	deadline = time.Now().Add(3 * time.Second)
	for plLegacy.Data.System == nil && time.Now().Before(deadline) {
		s.attachRTLConsole("switch-legacy", host, plLegacy)
		time.Sleep(50 * time.Millisecond)
	}
	if plLegacy.Data.System == nil {
		t.Fatal("router legacy (default ON) no inyectó System")
	}
}
