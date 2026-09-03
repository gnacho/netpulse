// firmware_test.go — tests del endpoint de firmware upgrades (#453).
package httpapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/channelplan"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/configbackup"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/firmware"
	"github.com/gnacho/netpulse/server-go/internal/httpapi"
	"github.com/gnacho/netpulse/server-go/internal/orchestr"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
	"github.com/gnacho/netpulse/server-go/internal/sse"
)

// firmwareTestServer crea un servidor de test con firmware + agent hub activos.
func firmwareTestServer(t *testing.T) (*testServer, *sse.AgentHub) {
	return firmwareTestServerWithAdapter(t, adapters.NewDemo())
}

// detectAdapter envuelve un Snapshotter y sirve un BoardInfoFor fijo (#477).
type detectAdapter struct {
	adapters.Snapshotter
	boards map[string]*adapters.BoardInfo
}

func (f *detectAdapter) BoardInfoFor(id string) *adapters.BoardInfo { return f.boards[id] }

func firmwareTestServerWithAdapter(t *testing.T, adapter adapters.Snapshotter) (*testServer, *sse.AgentHub) {
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
	fwStore := firmware.NewStore(d.DB)
	configBackup, _ := configbackup.NewStore(d)
	orchestrMgr := orchestr.New(d)
	agents := adapters.NewAgentRegistry(0)
	agentHub := sse.NewAgentHub(func(_, _ string) bool { return true })
	hub := sse.NewHub(d, cfg.MaxSSEClients, func() any { return nil })
	chPlan := channelplan.NewStore(d.DB)
	handler := httpapi.NewHandler(httpapi.Deps{
		Config:       cfg, DB: d, Adapter: adapter, Hub: hub, Secret: secret, Started: time.Now(),
		ConfigBackup: configBackup,
		Orchestr:     orchestrMgr,
		Firmware:     fwStore,
		AgentHub:     agentHub,
		ChannelPlan:  chPlan,
		Agents:       agents,
	})
	srv := httptest.NewServer(handler)
	ts := &testServer{Server: srv, db: d, secret: secret}
	t.Cleanup(func() {
		srv.Close()
		d.Close()
	})
	return ts, agentHub
}

func addTestRouter(t *testing.T, db *db.DB) string {
	t.Helper()
	r, err := routerstore.AddRouter(db.DB, routerstore.AddInput{
		Name: "RouterFW", Host: "192.168.1.50", Type: "openwrt",
		FirmwareTarget: "23.05.3",
	})
	if err != nil {
		t.Fatalf("add router: %v", err)
	}
	return r.ID
}

func adminCookieFor(t *testing.T, srv *testServer) string {
	t.Helper()
	_, cookie, _ := loginCookie(t, srv.URL, "admin", "test123456")
	if cookie == "" {
		t.Fatal("login failed")
	}
	return cookie
}

func readJSONArray(t *testing.T, res *http.Response) []any {
	t.Helper()
	defer res.Body.Close()
	var body []any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("json array: %v", err)
	}
	return body
}

func TestFirmwareTargetCRUD(t *testing.T) {
	srv, _ := firmwareTestServer(t)
	cookie := adminCookieFor(t, srv)
	rid := addTestRouter(t, srv.db)

	// Sin target, la lista devuelve la fila sin current/target.
	res := get(t, srv.URL, "/api/firmware-upgrades", cookie)
	arr := readJSONArray(t, res)
	if len(arr) != 1 {
		t.Fatalf("esperaba 1 router, got %d", len(arr))
	}

	// Configurar target.
	payload := `{"model":"glinet-flint2","currentVersion":"23.05.3","targetVersion":"23.05.4","targetUrl":"http://x/image.bin","checksum":"abc"}`
	res = postJSON(t, srv.URL, "/api/firmware-upgrades/"+rid+"/target", payload, cookie)
	if res.StatusCode != 200 {
		t.Fatalf("POST target: status %d body %v", res.StatusCode, readJSON(t, res))
	}
	res.Body.Close()

	// GET individual.
	res = get(t, srv.URL, "/api/firmware-upgrades/"+rid, cookie)
	body := readJSON(t, res)
	if res.StatusCode != 200 {
		t.Fatalf("GET detail: status %d body %v", res.StatusCode, body)
	}
	if body["targetVersion"] != "23.05.4" {
		t.Fatalf("targetVersion no coincide: %v", body["targetVersion"])
	}
}

// TestFirmwareListSurfacesDetectedBoard (#477 P2): la lista y el detalle
// exponen el board info reportado por el agente (board_name, release.version,
// release.target) para que la UI pueda prefillar el formulario.
func TestFirmwareListSurfacesDetectedBoard(t *testing.T) {
	boards := map[string]*adapters.BoardInfo{}
	srv, _ := firmwareTestServerWithAdapter(t, &detectAdapter{
		Snapshotter: adapters.NewDemo(),
		boards:      boards,
	})
	cookie := adminCookieFor(t, srv)
	rid := addTestRouter(t, srv.db)

	detected := &adapters.BoardInfo{Model: "Redmi AX6", BoardName: "redmi,ax6"}
	detected.Release.Version = "25.12.5"
	detected.Release.Target = "qualcommax/ipq807x"
	boards[rid] = detected

	res := get(t, srv.URL, "/api/firmware-upgrades", cookie)
	arr := readJSONArray(t, res)
	res.Body.Close()
	if len(arr) != 1 {
		t.Fatalf("esperaba 1 router, got %d", len(arr))
	}
	item := arr[0].(map[string]any)
	for k, want := range map[string]string{
		"detectedModel": "Redmi AX6", "detectedBoard": "redmi,ax6",
		"detectedVersion": "25.12.5", "detectedTarget": "qualcommax/ipq807x",
	} {
		if item[k] != want {
			t.Fatalf("%s = %v, want %q", k, item[k], want)
		}
	}

	// El detalle individual también lleva la detección.
	res = get(t, srv.URL, "/api/firmware-upgrades/"+rid, cookie)
	body := readJSON(t, res)
	res.Body.Close()
	if body["detectedBoard"] != "redmi,ax6" {
		t.Fatalf("detalle sin detectedBoard: %v", body["detectedBoard"])
	}
}

// TestFirmwareListWithoutDetectedBoard: sin board info los campos detectados
// no viajan (omitempty), la UI no muestra detección.
func TestFirmwareListWithoutDetectedBoard(t *testing.T) {
	srv, _ := firmwareTestServer(t)
	cookie := adminCookieFor(t, srv)
	addTestRouter(t, srv.db)

	res := get(t, srv.URL, "/api/firmware-upgrades", cookie)
	arr := readJSONArray(t, res)
	res.Body.Close()
	item := arr[0].(map[string]any)
	for _, k := range []string{"detectedModel", "detectedBoard", "detectedVersion", "detectedTarget"} {
		if _, ok := item[k]; ok {
			t.Fatalf("%s no debería viajar sin detección: %v", k, item[k])
		}
	}
}

func TestFirmwareUpgradeNoAgent(t *testing.T) {
	srv, _ := firmwareTestServer(t)
	cookie := adminCookieFor(t, srv)
	rid := addTestRouter(t, srv.db)

	payload := `{"model":"glinet-flint2","currentVersion":"23.05.3","targetVersion":"23.05.4","targetUrl":"http://x/image.bin","checksum":"abc"}`
	postJSON(t, srv.URL, "/api/firmware-upgrades/"+rid+"/target", payload, cookie).Body.Close()

	res := postJSON(t, srv.URL, "/api/firmware-upgrades/"+rid+"/upgrade", "{}", cookie)
	body := readJSON(t, res)
	if res.StatusCode != 503 {
		t.Fatalf("esperaba 503 agent_not_connected, got %d %v", res.StatusCode, body)
	}
	if body["error"] != "agent_not_connected" {
		t.Fatalf("error: %v", body["error"])
	}
}

func TestFirmwareUpgradeRequested(t *testing.T) {
	srv, _ := firmwareTestServer(t)
	cookie := adminCookieFor(t, srv)
	rid := addTestRouter(t, srv.db)

	// Conectar un agente falso al SSE para que Send tenga éxito.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var connected bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/agents/"+rid+"/stream", nil)
		req.Header.Set("Authorization", "Bearer token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		connected = true
		// Leemos hasta que se cancele el contexto o cierre el servidor.
		buf := make([]byte, 1024)
		for {
			resp.Body.Read(buf)
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
	}()
	// Dar tiempo a que el stream se registre.
	time.Sleep(200 * time.Millisecond)
	if !connected {
		t.Fatal("el stream SSE no se conectó")
	}

	payload := `{"model":"glinet-flint2","currentVersion":"23.05.3","targetVersion":"23.05.4","targetUrl":"http://x/image.bin","checksum":"abc"}`
	postJSON(t, srv.URL, "/api/firmware-upgrades/"+rid+"/target", payload, cookie).Body.Close()

	res := postJSON(t, srv.URL, "/api/firmware-upgrades/"+rid+"/upgrade", "{}", cookie)
	body := readJSON(t, res)
	if res.StatusCode != 202 {
		t.Fatalf("esperaba 202, got %d %v", res.StatusCode, body)
	}
	if body["status"] != "requested" {
		t.Fatalf("status: %v", body["status"])
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
	// El cleanup cerrará el httptest.Server una vez el cliente haya salido.
}

func postJSON(t *testing.T, base, path, payload, cookie string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", base+path, strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.Header.Set("Cookie", "session="+cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return res
}
