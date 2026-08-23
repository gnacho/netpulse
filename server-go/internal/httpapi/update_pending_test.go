// update_pending_test.go — confirmación post-update y POST pending-confirm
// (issue #161).
package httpapi_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/httpapi"
	"github.com/gnacho/netpulse/server-go/internal/sse"
	"github.com/gnacho/netpulse/server-go/internal/updater"
)

func TestPendingConfirmEndpoint(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git no disponible")
	}
	root := t.TempDir()
	writeDeployScript(t, root)
	old := writeGitHead(t, root)
	newSHA := gitCreaSegundoCommit(t, root)

	// Server con la BD sembrada ANTES de crear el updater (como un arranque
	// real): el marcador se procesa en updater.New.
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
	if _, err := d.Exec(
		`INSERT INTO kv (key, value) VALUES ('update.pending_apply', ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		fmt.Sprintf(`{"from":%q,"to":%q}`, old, newSHA)); err != nil {
		t.Fatal(err)
	}
	secret, err := auth.EnsureSessionSecret(d, cfg)
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	if err := auth.EnsureUsers(d, cfg); err != nil {
		t.Fatalf("users: %v", err)
	}
	adapter := adapters.NewDemo()
	hub := sse.NewHub(d, cfg.MaxSSEClients, func() any { return nil })
	up := updater.New(root, "owner/netpulse", "", "2.0.0", d.DB)
	if up.Status().PendingApply == nil {
		t.Fatal("pendingApply debería confirmarse al arrancar con el marcador")
	}
	handler := httpapi.NewHandler(httpapi.Deps{
		Config: cfg, DB: d, Adapter: adapter, Hub: hub, Secret: secret,
		Started: time.Now(), Updater: up,
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(func() {
		srv.Close()
		d.Close()
	})
	ts := &testServer{Server: srv, db: d, secret: secret}

	cookie := adminCookie(t, ts)
	body := readJSON(t, get(t, ts.URL, "/api/update/status", cookie))
	pa, ok := body["pendingApply"].(map[string]any)
	if !ok {
		t.Fatalf("pendingApply ausente en status: %+v", body)
	}
	if pa["from"] != old || pa["to"] != newSHA {
		t.Errorf("pendingApply: %+v, want from=%s to=%s", pa, old, newSHA)
	}

	// POST pending-confirm → 204 y pendingApply desaparece.
	req, _ := http.NewRequest("POST", ts.URL+"/api/update/pending-confirm", nil)
	req.Header.Set("Cookie", "session="+cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 204 {
		t.Fatalf("pending-confirm: got %d want 204", res.StatusCode)
	}
	body2 := readJSON(t, get(t, ts.URL, "/api/update/status", cookie))
	if _, ok := body2["pendingApply"]; ok {
		t.Errorf("pendingApply debería desaparecer tras confirmar: %+v", body2)
	}
}
