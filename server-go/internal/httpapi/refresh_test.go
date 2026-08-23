// refresh_test.go — contrato de POST /api/refresh (sondeo manual):
// 401 sin sesión, 202 {"ok":true} autenticado (dispara PollNow), 429 con
// Retry-After en ráfaga (< 5 s desde el anterior). En demo no hay 500.
package httpapi_test

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/httpapi"
	"github.com/gnacho/netpulse/server-go/internal/sse"
)

// makeRefreshServer es makeTestServer + espía de PollNow.
func makeRefreshServer(t *testing.T) (*testServer, *atomic.Int32) {
	t.Helper()
	dataDir := t.TempDir()
	cfg, err := config.Load(map[string]string{
		"AUTH_USER": "admin", "AUTH_PASS": "test123456",
		"DEMO_MODE": "1", "DATA_DIR": dataDir, "NODE_ENV": "test",
	}, dataDir)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	d, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	secret, err := auth.EnsureSessionSecret(d, cfg)
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	if err := auth.EnsureUsers(d, cfg); err != nil {
		t.Fatalf("users: %v", err)
	}
	var polls atomic.Int32
	handler := httpapi.NewHandler(httpapi.Deps{
		Config: cfg, DB: d, Adapter: adapters.NewDemo(),
		Hub:     sse.NewHub(d, cfg.MaxSSEClients, func() any { return nil }),
		Secret:  secret,
		PollNow: func() { polls.Add(1) },
		Started: time.Now(),
	})
	srv := httptest.NewServer(handler)
	ts := &testServer{Server: srv, db: d, secret: secret}
	t.Cleanup(func() {
		srv.Close()
		d.Close()
	})
	return ts, &polls
}

func postRefresh(t *testing.T, base, cookie string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", base+"/api/refresh", nil)
	if cookie != "" {
		req.Header.Set("Cookie", "session="+cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/refresh: %v", err)
	}
	return res
}

func TestRefreshSinSesion401(t *testing.T) {
	srv, polls := makeRefreshServer(t)
	res := postRefresh(t, srv.URL, "")
	body := readJSON(t, res)
	if res.StatusCode != 401 || body["error"] != "unauthorized" {
		t.Fatalf("sin sesión: got %d %v", res.StatusCode, body)
	}
	if polls.Load() != 0 {
		t.Fatal("sin sesión no debe disparar sondeo")
	}
}

func TestRefreshAutenticado202(t *testing.T) {
	srv, polls := makeRefreshServer(t)
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")
	res := postRefresh(t, srv.URL, cookie)
	body := readJSON(t, res)
	if res.StatusCode != 202 || body["ok"] != true {
		t.Fatalf("autenticado: got %d %v", res.StatusCode, body)
	}
	if len(body) != 1 {
		t.Fatalf("body con claves extra: %v", body)
	}
	if polls.Load() != 1 {
		t.Fatalf("PollNow llamado %d veces, want 1", polls.Load())
	}
	// El refresh manual lleva Cache-Control: no-store (como el resto de /api/*).
	if cc := res.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control: %q", cc)
	}
}

func TestRefreshRafaga429(t *testing.T) {
	srv, polls := makeRefreshServer(t)
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")
	res := postRefresh(t, srv.URL, cookie)
	res.Body.Close()
	if res.StatusCode != 202 {
		t.Fatalf("1er refresh: got %d want 202", res.StatusCode)
	}
	// Ráfaga inmediata → 429 con Retry-After, sin disparar otro sondeo.
	res = postRefresh(t, srv.URL, cookie)
	body := readJSON(t, res)
	if res.StatusCode != 429 || body["error"] != "rate_limited" {
		t.Fatalf("ráfaga: got %d %v", res.StatusCode, body)
	}
	retry, ok := body["retryAfterSec"].(float64)
	if !ok || retry <= 0 || retry > 5 {
		t.Fatalf("retryAfterSec: %v", body)
	}
	if ra := res.Header.Get("Retry-After"); ra == "" {
		t.Fatal("429 sin header Retry-After")
	}
	if polls.Load() != 1 {
		t.Fatalf("PollNow llamado %d veces, want 1 (429 no sondea)", polls.Load())
	}
}

func TestRefreshSinPollerNoRompe(t *testing.T) {
	// Deps sin PollNow (modo demo mínimo): 202 igualmente, nunca 500.
	srv := makeTestServer(t)
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")
	res := postRefresh(t, srv.URL, cookie)
	body := readJSON(t, res)
	if res.StatusCode != 202 || body["ok"] != true {
		t.Fatalf("sin poller: got %d %v", res.StatusCode, body)
	}
}
