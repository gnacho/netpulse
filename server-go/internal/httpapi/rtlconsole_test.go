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
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
)

// newRTLConsoleServer: emula la consola RTLPlayground. uptimeSec es el valor
// que devuelve /cmd time (en segundos); el servidor lo convierte a "0x…".
func newRTLConsoleServer(t *testing.T, uptimeSec uint64) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
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
		fmt.Fprintf(w, `{"sw_ver":"v0.1.0-e1fa080-dirty","hw_ver":"keepLink KP-9000-9XHML-X V3.1"}`)
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
