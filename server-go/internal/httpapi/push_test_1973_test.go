// push_test_1973_test.go - contrato de POST /api/push/test (#1973): solo la
// suscripcion del propio usuario, una prueba cada ventana, y el resultado del
// push service tal cual (incluida la suscripcion caducada).
package httpapi_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/httpapi"
	"github.com/gnacho/netpulse/server-go/internal/push"
	"github.com/gnacho/netpulse/server-go/internal/sse"

	"net/http/httptest"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
)

type fakePush struct {
	res   push.TestResult
	calls int
	last  push.Subscription
}

func (f *fakePush) SendTest(s push.Subscription) push.TestResult {
	f.calls++
	f.last = s
	return f.res
}

// makePushTestServer: handler real con un PushTester falso y dos usuarios con
// una suscripcion cada uno.
func makePushTestServer(t *testing.T, fake *fakePush) *testServer {
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
	hub := sse.NewHub(d, cfg.MaxSSEClients, func() any { return nil })
	var deps httpapi.PushTester
	if fake != nil {
		deps = fake
	}
	handler := httpapi.NewHandler(httpapi.Deps{
		Config: cfg, DB: d, Adapter: adapters.NewDemo(), Hub: hub, Secret: secret,
		Started: time.Now(), Push: deps,
	})
	srv := httptest.NewServer(handler)
	ts := &testServer{Server: srv, db: d, secret: secret}
	t.Cleanup(func() {
		srv.Close()
		d.Close()
	})
	return ts
}

func adminID(t *testing.T, d *db.DB) int64 {
	t.Helper()
	var id int64
	if err := d.QueryRow("SELECT id FROM users WHERE username = 'admin'").Scan(&id); err != nil {
		t.Fatalf("admin id: %v", err)
	}
	return id
}

func seedSubscription(t *testing.T, d *db.DB, endpoint string, userID int64) {
	t.Helper()
	if _, err := d.Exec(`INSERT INTO push_subscriptions (endpoint, keys_auth, keys_p256dh, user_agent, created_at, user_id, language)
		VALUES (?, 'auth', 'p256dh', 'test', ?, ?, 'es')`, endpoint, db.NowMS(), userID); err != nil {
		t.Fatalf("seed subscription: %v", err)
	}
}

func TestPushTestSendsToOwnSubscription(t *testing.T) {
	fake := &fakePush{res: push.TestResult{Status: http.StatusCreated}}
	ts := makePushTestServer(t, fake)
	cookie := adminCookie(t, ts)
	me := adminID(t, ts.db)
	seedSubscription(t, ts.db, "https://push.example/own", me)

	res := postJSON(t, ts.URL, "/api/push/test", `{"endpoint":"https://push.example/own"}`, cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, esperaba 200", res.StatusCode)
	}
	body := readJSON(t, res)
	if ok, _ := body["ok"].(bool); !ok {
		t.Fatalf("body = %+v", body)
	}
	if fake.calls != 1 || fake.last.Endpoint != "https://push.example/own" {
		t.Fatalf("fake: calls=%d endpoint=%q", fake.calls, fake.last.Endpoint)
	}
}

func TestPushTestRejectsForeignEndpoint(t *testing.T) {
	fake := &fakePush{res: push.TestResult{Status: http.StatusCreated}}
	ts := makePushTestServer(t, fake)
	cookie := adminCookie(t, ts)
	seedSubscription(t, ts.db, "https://push.example/other", 9999)

	res := postJSON(t, ts.URL, "/api/push/test", `{"endpoint":"https://push.example/other"}`, cookie)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, esperaba 404", res.StatusCode)
	}
	if fake.calls != 0 {
		t.Fatalf("no debia enviarse nada: %d envios", fake.calls)
	}
}

func TestPushTestRateLimited(t *testing.T) {
	old := httpapi.PushTestCooldownForTests(20 * time.Millisecond)
	t.Cleanup(func() { httpapi.PushTestCooldownForTests(old) })
	fake := &fakePush{res: push.TestResult{Status: http.StatusCreated}}
	ts := makePushTestServer(t, fake)
	cookie := adminCookie(t, ts)
	me := adminID(t, ts.db)
	seedSubscription(t, ts.db, "https://push.example/own", me)

	first := postJSON(t, ts.URL, "/api/push/test", `{"endpoint":"https://push.example/own"}`, cookie)
	if first.StatusCode != http.StatusOK {
		t.Fatalf("primera: %d", first.StatusCode)
	}
	second := postJSON(t, ts.URL, "/api/push/test", `{"endpoint":"https://push.example/own"}`, cookie)
	if second.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("segunda: %d, esperaba 429", second.StatusCode)
	}
	time.Sleep(30 * time.Millisecond)
	third := postJSON(t, ts.URL, "/api/push/test", `{"endpoint":"https://push.example/own"}`, cookie)
	if third.StatusCode != http.StatusOK {
		t.Fatalf("tercera tras la ventana: %d", third.StatusCode)
	}
}

func TestPushTestExpiredPurgesSubscription(t *testing.T) {
	fake := &fakePush{res: push.TestResult{Status: http.StatusGone, Expired: true, Reason: "gone"}}
	ts := makePushTestServer(t, fake)
	cookie := adminCookie(t, ts)
	me := adminID(t, ts.db)
	seedSubscription(t, ts.db, "https://push.example/dead", me)

	res := postJSON(t, ts.URL, "/api/push/test", `{"endpoint":"https://push.example/dead"}`, cookie)
	body := readJSON(t, res)
	if expired, _ := body["expired"].(bool); !expired {
		t.Fatalf("body = %+v", body)
	}
	var n int
	if err := ts.db.QueryRow("SELECT COUNT(*) FROM push_subscriptions WHERE endpoint = ?", "https://push.example/dead").Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 0 {
		t.Fatalf("la suscripcion caducada sigue en la DB (%d filas)", n)
	}
}

func TestPushTestWithoutNotifier(t *testing.T) {
	ts := makePushTestServer(t, nil)
	cookie := adminCookie(t, ts)
	res := postJSON(t, ts.URL, "/api/push/test", `{"endpoint":"https://push.example/own"}`, cookie)
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, esperaba 503", res.StatusCode)
	}
}
