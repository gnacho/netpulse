// hostkey_test.go — contrato de POST /api/routers/{id}/accept-host-key (#603),
// la acción que la UI expone desde #1477: admin-only, adapter que no la
// soporta, router desconocido y camino feliz (borra la entrada y fuerza un
// sondeo inmediato para que la clave nueva se registre por TOFU).
package httpapi_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/apitoken"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/channelplan"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/configbackup"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/httpapi"
	"github.com/gnacho/netpulse/server-go/internal/orchestr"
	"github.com/gnacho/netpulse/server-go/internal/sse"
)

// hostKeyAdapter: demo + AcceptHostKey con un conjunto de routers "conocidos".
type hostKeyAdapter struct {
	*adapters.Demo
	mu    sync.Mutex
	known map[string]bool
}

func (a *hostKeyAdapter) AcceptHostKey(routerID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.known[routerID] {
		return fmt.Errorf("router %q no configurado", routerID)
	}
	delete(a.known, routerID)
	return nil
}

// acceptHostKeyServer monta el handler con el adapter dado y cuenta los
// sondeos disparados (Deps.PollNow).
func acceptHostKeyServer(t *testing.T, adapter adapters.Snapshotter) (*testServer, *int) {
	t.Helper()
	auth.SetTrustProxy(true)
	t.Cleanup(func() { auth.SetTrustProxy(false) })
	dataDir := t.TempDir()
	cfg, err := config.Load(map[string]string{
		"AUTH_USER": "admin", "AUTH_PASS": "test123456",
		"DEMO_MODE": "0", "DATA_DIR": dataDir, "NODE_ENV": "test",
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
	if err := apitoken.EnsureSchema(d); err != nil {
		t.Fatalf("api_tokens schema: %v", err)
	}
	configBackup, err := configbackup.NewStore(d)
	if err != nil {
		t.Fatalf("config backup store: %v", err)
	}
	polls := 0
	handler := httpapi.NewHandler(httpapi.Deps{
		Config: cfg, DB: d,
		Adapter:      adapter,
		Hub:          sse.NewHub(d, cfg.MaxSSEClients, func() any { return nil }),
		Secret:       secret,
		Started:      time.Now(),
		TokenStore:   apitoken.NewStore(d, secret),
		ConfigBackup: configBackup,
		Orchestr:     orchestr.New(d),
		Agents:       adapters.NewAgentRegistry(0),
		ChannelPlan:  channelplan.NewStore(d.DB),
		PollNow:      func() { polls++ },
	})
	srv := httptest.NewServer(handler)
	ts := &testServer{Server: srv, db: d, secret: secret}
	t.Cleanup(func() {
		srv.Close()
		d.Close()
	})
	return ts, &polls
}

func postAcceptHostKey(t *testing.T, base, id, cookie string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", base+"/api/routers/"+id+"/accept-host-key", nil)
	if cookie != "" {
		req.Header.Set("Cookie", "session="+cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST accept-host-key: %v", err)
	}
	defer res.Body.Close()
	out := decodeBody(t, res)
	return res.StatusCode, out
}

// Adapter sin la capacidad: el endpoint responde 503 (no un 404 engañoso).
func TestAcceptHostKeyUnsupportedAdapter(t *testing.T) {
	ts, polls := acceptHostKeyServer(t, adapters.NewDemo())
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	code, body := postAcceptHostKey(t, ts.URL, "rt4", cookie)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503 (%v)", code, body)
	}
	if body["error"] != "unavailable" {
		t.Errorf("error %v, want unavailable", body["error"])
	}
	if *polls != 0 {
		t.Errorf("no debería sondease sin acción: %d sondeos", *polls)
	}
}

// Camino feliz: borra la entrada y dispara un sondeo inmediato (TOFU).
func TestAcceptHostKeyResetsKnownRouter(t *testing.T) {
	adapter := &hostKeyAdapter{Demo: adapters.NewDemo(), known: map[string]bool{"rt4": true}}
	ts, polls := acceptHostKeyServer(t, adapter)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	code, body := postAcceptHostKey(t, ts.URL, "rt4", cookie)
	if code != http.StatusAccepted {
		t.Fatalf("status %d, want 202 (%v)", code, body)
	}
	if *polls != 1 {
		t.Errorf("sondeos %d, want 1 (TOFU inmediato)", *polls)
	}
	// La entrada ya no existe: segundo intento = router desconocido.
	code, body = postAcceptHostKey(t, ts.URL, "rt4", cookie)
	if code != http.StatusNotFound {
		t.Fatalf("segundo intento: status %d, want 404 (%v)", code, body)
	}
}

// Router desconocido: 404 con el error del adapter.
func TestAcceptHostKeyUnknownRouter(t *testing.T) {
	adapter := &hostKeyAdapter{Demo: adapters.NewDemo(), known: map[string]bool{}}
	ts, polls := acceptHostKeyServer(t, adapter)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	code, body := postAcceptHostKey(t, ts.URL, "no-existe", cookie)
	if code != http.StatusNotFound {
		t.Fatalf("status %d, want 404 (%v)", code, body)
	}
	if *polls != 0 {
		t.Errorf("no debería sondease con router desconocido: %d sondeos", *polls)
	}
}

// Admin-only: sin sesión no se borra nada.
func TestAcceptHostKeyRequiresAdmin(t *testing.T) {
	adapter := &hostKeyAdapter{Demo: adapters.NewDemo(), known: map[string]bool{"rt4": true}}
	ts, polls := acceptHostKeyServer(t, adapter)

	code, _ := postAcceptHostKey(t, ts.URL, "rt4", "")
	if code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", code)
	}
	if *polls != 0 {
		t.Errorf("no debería sondease sin sesión: %d sondeos", *polls)
	}
	if err := adapter.AcceptHostKey("rt4"); err != nil {
		t.Errorf("la entrada no debería haberse borrado: %v", err)
	}
}
