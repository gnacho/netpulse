// speedtest_api_test.go — contrato de las rutas del test de velocidad WAN
// (issue #511): settings, histórico, estado, run-now y 503 en demo.
package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/apitoken"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/channelplan"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/httpapi"
	"github.com/gnacho/netpulse/server-go/internal/speedtest"
	"github.com/gnacho/netpulse/server-go/internal/sse"
)

// fakeRunner devuelve un resultado fijo; con release != nil bloquea hasta
// que se cierre (para probar el 409 de single-flight).
type fakeRunner struct {
	release chan struct{}
}

func (f fakeRunner) Run(ctx context.Context, serverID int) (speedtest.Result, error) {
	if f.release != nil {
		<-f.release
	}
	return speedtest.Result{DownMbps: 300, UpMbps: 150, ServerName: "fake-srv"}, nil
}

// makeSpeedtestServer: como makeTestServer pero con Deps.Speedtest montado
// sobre un runner fake (el arnés estándar no admite Deps custom).
func makeSpeedtestServer(t *testing.T, runner speedtest.Runner) *testServer {
	t.Helper()
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
	stStore, err := speedtest.NewStore(d.DB)
	if err != nil {
		t.Fatalf("speedtest store: %v", err)
	}
	handler := httpapi.NewHandler(httpapi.Deps{
		Config: cfg, DB: d, Adapter: adapters.NewDemo(),
		Hub: sse.NewHub(d, cfg.MaxSSEClients, func() any { return nil }),
		Secret: secret, Agents: adapters.NewAgentRegistry(0),
		TokenStore:  apitoken.NewStore(d, secret),
		ChannelPlan: channelplan.NewStore(d.DB),
		Speedtest:   speedtest.NewScheduler(stStore, d.DB, runner),
		Started:     time.Now(),
	})
	srv := httptest.NewServer(handler)
	ts := &testServer{Server: srv, db: d, secret: secret}
	t.Cleanup(func() {
		srv.Close()
		d.Close()
	})
	return ts
}

func speedtestRequest(t *testing.T, method, base, path, cookie, body string) (*http.Response, map[string]any) {
	t.Helper()
	var req *http.Request
	if body != "" {
		req, _ = http.NewRequest(method, base+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, _ = http.NewRequest(method, base+path, nil)
	}
	if cookie != "" {
		req.Header.Set("Cookie", "session="+cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	var parsed map[string]any
	raw, _ := io.ReadAll(res.Body)
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &parsed)
	}
	return res, parsed
}

// TestSpeedtestSettingsRoundtrip: PUT válido guarda, GET devuelve, PUT
// inválido → 400, sin sesión → 401.
func TestSpeedtestSettingsRoundtrip(t *testing.T) {
	srv := makeSpeedtestServer(t, fakeRunner{})
	cookie := adminCookie(t, srv)
	res, body := speedtestRequest(t, "PUT", srv.URL, "/api/settings/speedtest", cookie,
		`{"enabled":true,"intervalHours":12,"serverId":0,"alertPct":50}`)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("PUT: status %d (%v)", res.StatusCode, body)
	}
	if body["enabled"] != true || body["intervalHours"] != float64(12) || body["alertPct"] != float64(50) {
		t.Fatalf("PUT echo incorrecto: %v", body)
	}
	res, body = speedtestRequest(t, "GET", srv.URL, "/api/settings/speedtest", cookie, "")
	if res.StatusCode != http.StatusOK || body["enabled"] != true {
		t.Fatalf("GET tras PUT: %d %v", res.StatusCode, body)
	}
	res, _ = speedtestRequest(t, "PUT", srv.URL, "/api/settings/speedtest", cookie,
		`{"enabled":true,"intervalHours":5}`)
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("intervalo inválido: status %d, esperado 400", res.StatusCode)
	}
	res, _ = speedtestRequest(t, "GET", srv.URL, "/api/settings/speedtest", "", "")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("GET sin sesión: status %d, esperado 401", res.StatusCode)
	}
}

// TestSpeedtestHistoryAndStatus: la serie insertada sale por history y el
// último por status; hours inválido → 400.
func TestSpeedtestHistoryAndStatus(t *testing.T) {
	srv := makeSpeedtestServer(t, fakeRunner{})
	cookie := adminCookie(t, srv)
	// Siembro dos resultados directamente en el store: no hay acceso directo
	// desde aquí, así que uso el API tras un run-now inmediato... el fake no
	// bloquea: mejor sembrar via run + esperar status.
	if res, body := speedtestRequest(t, "POST", srv.URL, "/api/speedtest/run", cookie, ""); res.StatusCode != http.StatusAccepted {
		t.Fatalf("run: %d %v", res.StatusCode, body)
	}
	waitFor(t, 2*time.Second, func() bool {
		_, body := speedtestRequest(t, "GET", srv.URL, "/api/speedtest/status", cookie, "")
		return body["last"] != nil
	})
	res, body := speedtestRequest(t, "GET", srv.URL, "/api/speedtest/history?hours=1", cookie, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("history: %d", res.StatusCode)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("history: %d items, esperado 1", len(items))
	}
	first, _ := items[0].(map[string]any)
	if first["downMbps"] != float64(300) || first["origin"] != "manual" {
		t.Fatalf("item incorrecto: %v", first)
	}
	res, _ = speedtestRequest(t, "GET", srv.URL, "/api/speedtest/history?hours=bogus", cookie, "")
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("hours=bogus: %d, esperado 400", res.StatusCode)
	}
	_, st := speedtestRequest(t, "GET", srv.URL, "/api/speedtest/status", cookie, "")
	if st["running"] != false || st["last"] == nil {
		t.Fatalf("status: %v", st)
	}
}

// TestSpeedtestRunNowSingleFlight: segundo POST mientras corre → 409.
func TestSpeedtestRunNowSingleFlight(t *testing.T) {
	runner := fakeRunner{release: make(chan struct{})}
	srv := makeSpeedtestServer(t, runner)
	cookie := adminCookie(t, srv)
	res, body := speedtestRequest(t, "POST", srv.URL, "/api/speedtest/run", cookie, "")
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("primer run: %d %v", res.StatusCode, body)
	}
	res, _ = speedtestRequest(t, "POST", srv.URL, "/api/speedtest/run", cookie, "")
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("segundo run: %d, esperado 409", res.StatusCode)
	}
	close(runner.release)
	waitFor(t, 2*time.Second, func() bool {
		_, body := speedtestRequest(t, "GET", srv.URL, "/api/speedtest/status", cookie, "")
		return body["running"] == false && body["last"] != nil
	})
	res, _ = speedtestRequest(t, "POST", srv.URL, "/api/speedtest/run", "", "")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("run sin sesión: %d, esperado 401", res.StatusCode)
	}
}

// TestSpeedtestUnavailableInDemo: Deps.Speedtest nil → 503 en todas las
// rutas (con sesión válida; sin sesión el gate global responde 401 antes).
func TestSpeedtestUnavailableInDemo(t *testing.T) {
	srv := makeTestServer(t)
	cookie := adminCookie(t, srv)
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/speedtest/status"},
		{"GET", "/api/speedtest/history"},
		{"POST", "/api/speedtest/run"},
		{"GET", "/api/settings/speedtest"},
	} {
		req, _ := http.NewRequest(tc.method, srv.URL+tc.path, nil)
		req.Header.Set("Cookie", "session="+cookie)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", tc.method, tc.path, err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusServiceUnavailable {
			t.Errorf("%s %s: %d, esperado 503", tc.method, tc.path, res.StatusCode)
		}
	}
}

// TestSpeedtestOverviewInjection: tras un run, el overview expone la última
// medición en wan.speedtestDownMbps (issue #511).
func TestSpeedtestOverviewInjection(t *testing.T) {
	srv := makeSpeedtestServer(t, fakeRunner{})
	cookie := adminCookie(t, srv)
	speedtestRequest(t, "POST", srv.URL, "/api/speedtest/run", cookie, "")
	waitFor(t, 2*time.Second, func() bool {
		_, body := speedtestRequest(t, "GET", srv.URL, "/api/overview", cookie, "")
		wan, _ := body["wan"].(map[string]any)
		return wan != nil && wan["speedtestDownMbps"] != nil
	})
	// Sin sesión el overview también requiere auth (gate global).
	res, _ := speedtestRequest(t, "GET", srv.URL, "/api/overview", "", "")
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("overview sin sesión: %d, esperado 401", res.StatusCode)
	}
}

// waitFor sondea cond() hasta deadline (los runs manuales son asíncronos).
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("condición no cumplida en %v", timeout)
}
