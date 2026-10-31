// integrations_boot_test.go - #1696: reanudación al arrancar de las
// integraciones locales habilitadas, detección de hijos muertos (la fila pasa
// a estado error con su motivo) y conciliación de estado running huérfano
// (sin push en varios intervalos la vista lo reporta como "stale", no sano).
package httpapi_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	"github.com/gnacho/netpulse/server-go/internal/rintegration"
	"github.com/gnacho/netpulse/server-go/internal/sse"
	"net/http/httptest"
)

// makeIntegrationsTestServerSeeded replica makeTestServerWithMode pero con el
// supervisor de integraciones cableado (como en producción). seed corre antes
// de construir el handler: las filas de router_integrations deben existir al
// "arrancar" para que el resume del boot las vea.
func makeIntegrationsTestServerSeeded(t *testing.T, seed func(d *db.DB, mgr *rintegration.Manager)) (*testServer, *rintegration.Manager) {
	return makeIntegrationsTestServerFull(t, seed, nil)
}

// makeIntegrationsTestServerFull es la variante con catálogo de vendors
// inyectable (#1697): catFn construye el VendorCatalog con la base ya abierta
// (nil → fallback de NewHandler: semilla embebida sin red). El parámetro
// variadic pool permite inyectar un SSHRunner fake (#1708): sin él el
// servidor no tiene pool, igual que el resto de harnesses de la app.
func makeIntegrationsTestServerFull(t *testing.T, seed func(d *db.DB, mgr *rintegration.Manager), catFn func(d *db.DB) *rintegration.Catalog, pool ...httpapi.SSHRunner) (*testServer, *rintegration.Manager) {
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
	hub := sse.NewHub(d, cfg.MaxSSEClients, func() any { return nil })
	mgr := rintegration.NewManager(filepath.Join(cfg.DataDir, "integrations"))
	if seed != nil {
		seed(d, mgr)
	}
	var cat *rintegration.Catalog
	if catFn != nil {
		cat = catFn(d)
	}
	deps := httpapi.Deps{
		Config: cfg, DB: d, Adapter: adapters.NewDemo(), Hub: hub, Secret: secret,
		Started:       time.Now(),
		TokenStore:    apitoken.NewStore(d, secret),
		ConfigBackup:  configBackup,
		Orchestr:      orchestr.New(d),
		Agents:        adapters.NewAgentRegistry(0),
		ChannelPlan:   channelplan.NewStore(d.DB),
		Integrations:  mgr,
		VendorCatalog: cat,
	}
	if len(pool) > 0 {
		deps.Pool = pool[0]
	}
	handler := httpapi.NewHandler(deps)
	srv := httptest.NewServer(handler)
	ts := &testServer{Server: srv, db: d, secret: secret}
	t.Cleanup(func() {
		srv.Close()
		d.Close()
	})
	return ts, mgr
}

func makeIntegrationsTestServer(t *testing.T) (*testServer, *rintegration.Manager) {
	t.Helper()
	return makeIntegrationsTestServerSeeded(t, nil)
}

func bootIntegrationRow(slug, vendor, mode string, enabled bool, state string) rintegration.Row {
	return rintegration.Row{
		Slug: slug, Vendor: vendor, Mode: mode, Host: "10.0.0.1", Username: "admin",
		Version: "0.1.1", Asset: vendor + ".tar.gz",
		Arch: "amd64", IntervalS: 45, Enabled: enabled, State: state,
	}
}

// installFakePusher escribe el "binario" instalado del pusher (script sh)
// dentro del directorio de la integración y su fichero de entorno 0600, el
// mismo layout que deja un alta real en disco.
func installFakePusher(t *testing.T, mgr *rintegration.Manager, slug, binary, body string) string {
	t.Helper()
	installDir := filepath.Join(mgr.Dir(), slug)
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", installDir, err)
	}
	bin := filepath.Join(installDir, binary)
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write %s: %v", bin, err)
	}
	if err := mgr.WriteEnv(slug, map[string]string{"NETPULSE_SLUG": slug}); err != nil {
		t.Fatalf("write env %s: %v", slug, err)
	}
	return bin
}

func integrationsBySlug(t *testing.T, ts *testServer) map[string]map[string]any {
	t.Helper()
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	res := get(t, ts.URL, "/api/integrations", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/integrations: status %d", res.StatusCode)
	}
	body := readJSON(t, res)
	rawItems, ok := body["integrations"].([]any)
	if !ok {
		t.Fatalf("GET /api/integrations: integrations missing: %v", body)
	}
	out := map[string]map[string]any{}
	for _, raw := range rawItems {
		it, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("integration item no es un objeto: %v", raw)
		}
		slug, _ := it["slug"].(string)
		out[slug] = it
	}
	return out
}

func waitIntegrationState(t *testing.T, store *rintegration.Store, slug, state string) rintegration.Row {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		row, ok := store.Get(slug)
		if ok && row.State == state {
			return row
		}
		if time.Now().After(deadline) {
			row, _ = store.Get(slug)
			t.Fatalf("la fila %s no llegó a estado %q a tiempo (estado %q, lastError %q)", slug, state, row.State, row.LastError)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestIntegrationsResumeOnBoot: al arrancar el servidor, la integración local
// habilitada se reanuda sola con el binario y el env ya en disco; la que no
// tiene binario queda en error sin tirar el arranque; la deshabilitada no se
// toca; la de modo ssh deja de decir running (no verificada desde aquí).
func TestIntegrationsResumeOnBoot(t *testing.T) {
	v := seedVendor(t, "routeros")
	ts, mgr := makeIntegrationsTestServerSeeded(t, func(d *db.DB, mgr *rintegration.Manager) {
		store := rintegration.NewStore(d.DB)
		installFakePusher(t, mgr, "lab", v.Binary, "sleep 30")
		_ = store.Upsert(bootIntegrationRow("lab", "routeros", "local", true, "running"))
		_ = mgr.WriteEnv("broken", map[string]string{"NETPULSE_SLUG": "broken"})
		_ = store.Upsert(bootIntegrationRow("broken", "routeros", "local", true, "running"))
		_ = store.Upsert(bootIntegrationRow("off", "routeros", "local", false, "stopped"))
		_ = store.Upsert(bootIntegrationRow("wrt", "asuswrt", "ssh", true, "running"))
	})
	store := rintegration.NewStore(ts.db.DB)

	deadline := time.Now().Add(5 * time.Second)
	for !mgr.Running("lab") {
		if time.Now().After(deadline) {
			t.Fatal("la integración local habilitada no se reanudó al arrancar")
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Cleanup(func() { _ = mgr.Stop("lab") })

	row := waitIntegrationState(t, store, "lab", "running")
	if row.LastStartMs == 0 {
		t.Fatalf("lab reanudada sin last_start_ms: %+v", row)
	}
	if row.LastError != "" {
		t.Fatalf("lab reanudada con lastError %q", row.LastError)
	}
	broken := waitIntegrationState(t, store, "broken", "error")
	if broken.LastError == "" {
		t.Fatal("broken quedó en error sin lastError")
	}
	if mgr.Running("broken") {
		t.Fatal("broken no debe tener proceso")
	}
	if row, _ := store.Get("off"); row.State != "stopped" || mgr.Running("off") {
		t.Fatalf("la deshabilitada no se debe tocar: state=%q running=%v", row.State, mgr.Running("off"))
	}
	if row, _ := store.Get("wrt"); row.State != "unverified" {
		t.Fatalf("la ssh habilitada debe dejar running tras el arranque, state=%q", row.State)
	}
	if mgr.Running("wrt") {
		t.Fatal("la ssh no debe arrancar en el servidor")
	}

	items := integrationsBySlug(t, ts)
	if it := items["lab"]; it == nil || it["state"] != "running" {
		t.Fatalf("API lab: %v", items["lab"])
	}
	if it := items["broken"]; it == nil || it["state"] != "error" || it["lastError"] == "" {
		t.Fatalf("API broken: %v", items["broken"])
	}
	if it := items["wrt"]; it == nil || it["state"] != "unverified" {
		t.Fatalf("API wrt: %v", items["wrt"])
	}
}

// TestIntegrationsDeadChildFlipsToError: un pusher que muere solo (código de
// error, salida limpia o un kill externo) deja la fila en estado error con su
// motivo y deja de reportarse como running.
func TestIntegrationsDeadChildFlipsToError(t *testing.T) {
	for _, tc := range []struct {
		name, script, wantErr string
	}{
		{"crash", "exit 3", "exit status 3"},
		{"clean-exit", "exit 0", "exit code 0"},
		{"killed", `kill -9 $$`, "signal: killed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := seedVendor(t, "routeros")
			slug := "dead-" + tc.name
			ts, mgr := makeIntegrationsTestServer(t)
			store := rintegration.NewStore(ts.db.DB)
			bin := installFakePusher(t, mgr, slug, v.Binary, tc.script)
			if err := store.Upsert(bootIntegrationRow(slug, "routeros", "local", true, "running")); err != nil {
				t.Fatal(err)
			}
			if err := mgr.Start(slug, nil, bin); err != nil {
				t.Fatalf("start: %v", err)
			}
			row := waitIntegrationState(t, store, slug, "error")
			if row.LastError == "" || !strings.Contains(row.LastError, tc.wantErr) {
				t.Fatalf("lastError = %q, want contener %q", row.LastError, tc.wantErr)
			}
			if mgr.Running(slug) {
				t.Fatal("el hijo muerto sigue reportándose como running")
			}
			items := integrationsBySlug(t, ts)
			if it := items[slug]; it == nil || it["state"] != "error" {
				t.Fatalf("API %s: %v", slug, items[slug])
			}
		})
	}
}

// TestIntegrationsIntentionalStopDoesNotError: parar por API es un paro
// intencional: la fila queda stopped y el callback de muerte no la marca error.
func TestIntegrationsIntentionalStopDoesNotError(t *testing.T) {
	v := seedVendor(t, "routeros")
	ts, mgr := makeIntegrationsTestServer(t)
	store := rintegration.NewStore(ts.db.DB)
	bin := installFakePusher(t, mgr, "manual", v.Binary, "sleep 30")
	if err := store.Upsert(bootIntegrationRow("manual", "routeros", "local", true, "running")); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Start("manual", nil, bin); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("manual") })

	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	res, body := wanSpeedRequest(t, "POST", ts.URL, "/api/integrations/manual/stop", cookie, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST stop: status %d (%v)", res.StatusCode, body)
	}
	if it := body["integration"].(map[string]any); it["state"] != "stopped" {
		t.Fatalf("estado tras stop: %v", it["state"])
	}
	// Ventana corta de ausencia: ningún callback tardío debe convertir el
	// paro intencional en error.
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if row, _ := store.Get("manual"); row.State == "error" {
			t.Fatal("el paro intencional acabó en estado error")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestIntegrationsCheckReconcilesState: un check con el proceso
// demostradamente vivo saca la fila de un estado desfasado (unverified tras un
// reinicio del servidor) y la devuelve a running; sin proceso, la fila no se
// toca (el error sigue siendo error).
func TestIntegrationsCheckReconcilesState(t *testing.T) {
	v := seedVendor(t, "routeros")
	ts, mgr := makeIntegrationsTestServer(t)
	store := rintegration.NewStore(ts.db.DB)
	bin := installFakePusher(t, mgr, "verify", v.Binary, "sleep 30")
	if err := store.Upsert(bootIntegrationRow("verify", "routeros", "local", true, "unverified")); err != nil {
		t.Fatal(err)
	}
	if err := store.Upsert(bootIntegrationRow("dead-check", "routeros", "local", true, "error")); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Start("verify", nil, bin); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Stop("verify") })

	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	res, body := wanSpeedRequest(t, "POST", ts.URL, "/api/integrations/verify/check", cookie, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST check: status %d (%v)", res.StatusCode, body)
	}
	if body["running"] != true {
		t.Fatalf("check running: %v", body["running"])
	}
	if it := body["integration"].(map[string]any); it["state"] != "running" {
		t.Fatalf("estado tras check: %v", it["state"])
	}
	if row, _ := store.Get("verify"); row.State != "running" {
		t.Fatalf("la fila no se reconcilió con el check: %q", row.State)
	}

	res, body = wanSpeedRequest(t, "POST", ts.URL, "/api/integrations/dead-check/check", cookie, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST check dead: status %d (%v)", res.StatusCode, body)
	}
	if body["running"] != false {
		t.Fatalf("check running dead: %v", body["running"])
	}
	if row, _ := store.Get("dead-check"); row.State != "error" {
		t.Fatalf("el check sin proceso no debe tocar la fila: %q", row.State)
	}
}

// TestIntegrationsStaleNotReportedRunning: conciliación al leer. Una fila en
// running cuyo último push es más viejo que 3 intervalos se reporta como
// "stale" en la API (y no fresh), sin tocar la fila; con push reciente sigue
// siendo running/fresh; sin push nunca no se puede juzgar y sigue running.
func TestIntegrationsStaleNotReportedRunning(t *testing.T) {
	ts, _ := makeIntegrationsTestServer(t)
	store := rintegration.NewStore(ts.db.DB)
	now := time.Now().Unix()
	rows := []struct {
		slug, vendor, mode, state string
		pushSec                   int64
	}{
		{"fresh-push", "routeros", "local", "running", now - 10},
		{"old-push", "routeros", "local", "running", now - 200},
		{"no-push", "routeros", "local", "running", 0},
		{"errored", "routeros", "local", "error", now - 200},
		{"stale-ssh", "asuswrt", "ssh", "running", now - 200},
	}
	for _, r := range rows {
		if err := store.Upsert(bootIntegrationRow(r.slug, r.vendor, r.mode, true, r.state)); err != nil {
			t.Fatal(err)
		}
		if r.pushSec > 0 {
			if _, err := ts.db.Exec(
				"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
				"agent.last_push_ts."+r.slug, strconv.FormatInt(r.pushSec, 10),
			); err != nil {
				t.Fatal(err)
			}
		}
	}

	items := integrationsBySlug(t, ts)
	if it := items["fresh-push"]; it == nil || it["state"] != "running" || it["fresh"] != true {
		t.Fatalf("fresh-push: %v", items["fresh-push"])
	}
	if it := items["old-push"]; it == nil || it["state"] != "stale" || it["fresh"] != false {
		t.Fatalf("old-push debe salir stale sin fresh: %v", items["old-push"])
	}
	if it := items["no-push"]; it == nil || it["state"] != "running" {
		t.Fatalf("no-push (sin push aún) no se puede juzgar: %v", items["no-push"])
	}
	if it := items["errored"]; it == nil || it["state"] != "error" {
		t.Fatalf("errored: %v", items["errored"])
	}
	if it := items["stale-ssh"]; it == nil || it["state"] != "stale" {
		t.Fatalf("stale-ssh: %v", items["stale-ssh"])
	}
	// La conciliación es de vista: la fila sigue en running en la base de
	// datos y el propio cálculo la devuelve a running cuando vuelva a empujar.
	if row, _ := store.Get("old-push"); row.State != "running" {
		t.Fatalf("la fila no se debe tocar: state=%q", row.State)
	}
}
