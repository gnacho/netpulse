// proxmox_detect_internal_test.go - POST /api/config/proxmox/detect (#967):
// barrido en dos fases (TCP masivo + confirmacion TLS EN SERIE) sobre la /24
// de los hosts de `routers`, y validacion de la sonda TLS real.
package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
)

// TestDetectProxmoxTwoPhase: fase 1 barre la /24 entera en TCP; fase 2
// confirma TLS EN SERIE (concurrencia maxima 1, causa raiz del bug en vivo:
// pveproxy limita conexiones simultaneas por IP).
func TestDetectProxmoxTwoPhase(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if _, err := routerstore.AddRouter(d.DB, routerstore.AddInput{Name: "gw", Host: "192.0.2.1", IsGateway: true}); err != nil {
		t.Fatalf("AddRouter: %v", err)
	}

	var tcpCalls atomic.Int64
	oldTCP := pveTCPProbe
	t.Cleanup(func() { pveTCPProbe = oldTCP })
	pveTCPProbe = func(host string, _ int, _ time.Duration) bool {
		tcpCalls.Add(1)
		// Dos candidatos abiertos en la /24: .100 (confirma) y .200 (no).
		return host == "192.0.2.100" || host == "192.0.2.200"
	}

	var concurrent, maxConcurrent atomic.Int64
	confirmed := []string{}
	var mu sync.Mutex
	oldTLS := pveTLSConfirm
	t.Cleanup(func() { pveTLSConfirm = oldTLS })
	pveTLSConfirm = func(host string, _ int, _ time.Duration) bool {
		n := concurrent.Add(1)
		for {
			m := maxConcurrent.Load()
			if n <= m || maxConcurrent.CompareAndSwap(m, n) {
				break
			}
		}
		defer concurrent.Add(-1)
		time.Sleep(5 * time.Millisecond) // solape visible si hubiera paralelismo
		mu.Lock()
		confirmed = append(confirmed, host)
		mu.Unlock()
		return host == "192.0.2.100"
	}

	s := &server{db: d}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/config/proxmox/detect", nil)
	s.handleDetectProxmox(rec, req)
	if rec.Code != 200 {
		t.Fatalf("detect: status %d", rec.Code)
	}
	var out proxmoxDetectResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("detect json: %v", err)
	}
	if !out.Found || len(out.Hosts) != 1 || out.Hosts[0] != "192.0.2.100" {
		t.Fatalf("respuesta: %+v", out)
	}
	if n := tcpCalls.Load(); n != 254 {
		t.Fatalf("fase 1: %d sondeos TCP, esperado 254 (/24 completa)", n)
	}
	if m := maxConcurrent.Load(); m != 1 {
		t.Fatalf("fase 2: concurrencia maxima %d, esperado 1 (serie estricta)", m)
	}
	if len(confirmed) != 2 {
		t.Fatalf("fase 2: %d confirmaciones, esperado 2 candidatos", len(confirmed))
	}
}

// TestConfirmPVEHost: la sonda TLS real acepta header Server pve-api-daemon
// o un body con "pve", y rechaza cualquier otro servicio TLS en :8006.
func TestConfirmPVEHost(t *testing.T) {
	hostPort := func(t *testing.T, h http.Handler) (string, int) {
		t.Helper()
		srv := httptest.NewTLSServer(h)
		t.Cleanup(srv.Close)
		parts := strings.Split(strings.TrimPrefix(srv.URL, "https://"), ":")
		port, err := strconv.Atoi(parts[1])
		if err != nil {
			t.Fatalf("port: %v", err)
		}
		return parts[0], port
	}

	// Body con "pve" (version API real).
	h, p := hostPort(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"version":"8.2.4","release":"pve","repoid":"2"}}`))
	}))
	if !confirmPVEHost(h, p, 3*time.Second) {
		t.Fatal("body pve: no confirmado")
	}

	// Sin body pve pero con header Server pve-api-daemon.
	h, p = hostPort(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Server", "pve-api-daemon/3.10.8")
		_, _ = w.Write([]byte(`{"data":null}`))
	}))
	if !confirmPVEHost(h, p, 3*time.Second) {
		t.Fatal("header pve-api-daemon: no confirmado")
	}

	// Otro servicio TLS cualquiera: no es PVE.
	h, p = hostPort(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html>router admin</html>`))
	}))
	if confirmPVEHost(h, p, 3*time.Second) {
		t.Fatal("servicio ajeno: confirmado como PVE")
	}
}

// TestDetectProxmoxNoRouters: sin hosts IPv4 configurados, found=false.
func TestDetectProxmoxNoRouters(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	s := &server{db: d}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/config/proxmox/detect", nil)
	s.handleDetectProxmox(rec, req)
	var out proxmoxDetectResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("detect json: %v", err)
	}
	if out.Found || len(out.Hosts) != 0 {
		t.Fatalf("respuesta: %+v, esperado found=false", out)
	}
}
