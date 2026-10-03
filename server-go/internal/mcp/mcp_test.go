package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/alerts"
	"github.com/gnacho/netpulse/server-go/internal/apitoken"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

// fakeOverview es el snapshot mínimo que cubre los siete tools.
func fakeOverview() *adapters.Overview {
	return &adapters.Overview{
		Ts: 1750000000,
		Routers: []adapters.Router{
			{ID: "gateway", Name: "Gateway", Model: "GL.iNet Flint 2", Role: "gateway", Status: "online", Health: 98, Clients: 5},
			{ID: "ap-living", Name: "AP Living", Model: "Redmi AX6", Role: "ap", Status: "offline", Health: 0, Clients: 0},
		},
		Devices: []adapters.Device{
			{ID: "dev-1", Name: "laptop", RouterID: "gateway", Online: true},
			{ID: "dev-2", Name: "phone", RouterID: "ap-living", Online: false},
		},
		DeviceTotals:    adapters.DeviceTotals{Total: 2, Online: 1},
		UnreadAlerts:    1,
		ServerUptimeSec: 3600,
		Alerts: []adapters.AlertEvent{
			{ID: "a1", Category: "router-offline", Severity: "critical", Ts: 1750000000},
			{ID: "a2", Category: "weak-signal", Severity: "warn", Ts: 1749999000, Read: true},
		},
		Wireguard: adapters.WireGuardStats{
			Interface: "wg0", Status: "active", Subnet: "10.0.0.0/24",
			Peers: []adapters.WGPeer{{ID: "peer-1", Name: "oficina", Active: true}},
		},
		Adguard: adapters.AdGuardStats{
			Host: "192.168.1.1", Status: "active", Queries24h: 12000, Blocked24h: 3000,
		},
		Topology: &adapters.TopoSemantics{
			Links: []adapters.TopoLink{{From: "gateway", To: "dev-1", Kind: "wired"}},
			Rings: map[string][]string{"gateway": {"dev-1"}},
		},
		DistributionNodes: []adapters.DistributionNode{{ID: "dist-1", Kind: "inferred", RouterID: "gateway"}},
	}
}

// testEnv levanta el handler MCP completo (auth + rate limit) sobre un token
// store real con un token válido. Devuelve la URL base y el token crudo.
func testEnv(t *testing.T, ov *adapters.Overview, ratePerMin int) (string, string) {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := apitoken.EnsureSchema(d); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	store := apitoken.NewStore(d, "test-secret")
	_, raw, err := store.Create("mcp-test", apitoken.ScopeRead, 1, 0)
	if err != nil {
		t.Fatalf("Create token: %v", err)
	}
	srv := New(Deps{
		LastOverview: func() *adapters.Overview { return ov },
		Version:      "0.0.0-test",
		RatePerMin:   ratePerMin,
	})
	// Montar como en producción: mux externo con /mcp (StreamableHTTPServer
	// valida el path por defecto).
	mux := http.NewServeMux()
	mux.Handle("/mcp", srv.Handler(store))
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts.URL, raw
}

// rpc hace una llamada JSON-RPC 2.0 contra /mcp y devuelve el body parseado.
// Acept JSON puro (sin SSE) para simplificar el parseo en tests.
func rpc(t *testing.T, url, token, method string, id int, params any) (int, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": method, "params": params,
	})
	req, err := http.NewRequest("POST", url+"/mcp", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("Decode (status %d): %v", resp.StatusCode, err)
	}
	return resp.StatusCode, out
}

func callTool(t *testing.T, url, token, name string, args map[string]any) map[string]any {
	t.Helper()
	_, out := rpc(t, url, token, "tools/call", 2, map[string]any{
		"name":      name,
		"arguments": args,
	})
	return out
}

// structured extrae el contenido de un CallToolResult: los tools devuelven
// NewToolResultJSON → Content[0].Text es el JSON serializado (y además va en
// structuredContent, pero el text basta y es version-proof).
func structured(t *testing.T, out map[string]any) map[string]any {
	t.Helper()
	res, ok := out["result"].(map[string]any)
	if !ok {
		t.Fatalf("sin result en %v", out)
	}
	if res["isError"] == true {
		t.Fatalf("tool error: %v", res)
	}
	content, ok := res["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("sin content en %v", res)
	}
	first, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("content[0] no es mapa: %v", content[0])
	}
	text, ok := first["text"].(string)
	if !ok {
		t.Fatalf("content[0].text no es string: %v", first)
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		t.Fatalf("content[0].text no es JSON (%q): %v", text, err)
	}
	return v
}

func TestFleetStatus(t *testing.T) {
	url, tok := testEnv(t, fakeOverview(), 60)

	out := callTool(t, url, tok, "fleet_status", nil)
	v := structured(t, out)
	if v["version"] != "0.0.0-test" {
		t.Errorf("version = %v", v["version"])
	}
	if v["ts"] != float64(1750000000) {
		t.Errorf("ts = %v", v["ts"])
	}
	routers, ok := v["routers"].([]any)
	if !ok || len(routers) != 2 {
		t.Fatalf("routers = %v", v["routers"])
	}
	gw := routers[0].(map[string]any)
	if gw["id"] != "gateway" || gw["status"] != "online" || gw["health"] != float64(98) {
		t.Errorf("gateway brief = %v", gw)
	}
	if v["unreadAlerts"] != float64(1) || v["alertCount"] != float64(2) {
		t.Errorf("alertas = %v / %v", v["unreadAlerts"], v["alertCount"])
	}
	totals, ok := v["deviceTotals"].(map[string]any)
	if !ok || totals["total"] != float64(2) || totals["online"] != float64(1) {
		t.Errorf("deviceTotals = %v", v["deviceTotals"])
	}
}

func TestToolsListAllReadOnly(t *testing.T) {
	url, tok := testEnv(t, fakeOverview(), 60)

	_, out := rpc(t, url, tok, "tools/list", 1, nil)
	res, ok := out["result"].(map[string]any)
	if !ok {
		t.Fatalf("sin result: %v", out)
	}
	tools, ok := res["tools"].([]any)
	if !ok || len(tools) != 7 {
		t.Fatalf("tools = %v", res["tools"])
	}
	want := map[string]bool{
		"fleet_status": true, "router_health": true, "list_devices": true,
		"wireguard_peers": true, "adguard_stats": true, "topology": true,
		"alerts": true,
	}
	for _, raw := range tools {
		tool := raw.(map[string]any)
		name := tool["name"].(string)
		if !want[name] {
			t.Errorf("tool inesperada: %s", name)
		}
		delete(want, name)
		ann, ok := tool["annotations"].(map[string]any)
		if !ok {
			t.Errorf("%s sin annotations", name)
			continue
		}
		if ann["readOnlyHint"] != true {
			t.Errorf("%s readOnlyHint != true: %v", name, ann)
		}
		for _, forbidden := range []string{"destructiveHint", "openWorldHint"} {
			if ann[forbidden] == true {
				t.Errorf("%s no debe declarar %s=true en la tanda read-only", name, forbidden)
			}
		}
	}
	if len(want) > 0 {
		t.Errorf("tools sin registrar: %v", want)
	}
}

func TestRouterHealth(t *testing.T) {
	url, tok := testEnv(t, fakeOverview(), 60)

	v := structured(t, callTool(t, url, tok, "router_health", map[string]any{"router_id": "ap-living"}))
	if v["id"] != "ap-living" || v["status"] != "offline" {
		t.Errorf("router = %v", v)
	}

	// Router desconocido: isError con los ids conocidos en el mensaje.
	out := callTool(t, url, tok, "router_health", map[string]any{"router_id": "nope"})
	res := out["result"].(map[string]any)
	if res["isError"] != true {
		t.Errorf("isError = %v", res)
	}
}

func TestListDevices(t *testing.T) {
	url, tok := testEnv(t, fakeOverview(), 60)

	v := structured(t, callTool(t, url, tok, "list_devices", nil))
	if v["total"] != float64(2) {
		t.Errorf("total = %v", v["total"])
	}
	v = structured(t, callTool(t, url, tok, "list_devices", map[string]any{"router_id": "gateway"}))
	if v["total"] != float64(1) {
		t.Errorf("total filtrado = %v", v["total"])
	}
	devs := v["devices"].([]any)
	if devs[0].(map[string]any)["id"] != "dev-1" {
		t.Errorf("devices = %v", devs)
	}
}

func TestWireGuardAndAdGuard(t *testing.T) {
	url, tok := testEnv(t, fakeOverview(), 60)

	wg := structured(t, callTool(t, url, tok, "wireguard_peers", nil))
	if wg["interface"] != "wg0" {
		t.Errorf("wg = %v", wg)
	}
	agh := structured(t, callTool(t, url, tok, "adguard_stats", nil))
	if agh["queries24h"] != float64(12000) {
		t.Errorf("agh = %v", agh)
	}

	// Sin datos: isError honesto, no un struct vacío.
	empty := &adapters.Overview{Ts: 1}
	url2, tok2 := testEnv(t, empty, 60)
	out := callTool(t, url2, tok2, "wireguard_peers", nil)
	if out["result"].(map[string]any)["isError"] != true {
		t.Errorf("wireguard sin datos debería ser isError: %v", out)
	}
	out = callTool(t, url2, tok2, "adguard_stats", nil)
	if out["result"].(map[string]any)["isError"] != true {
		t.Errorf("adguard sin datos debería ser isError: %v", out)
	}
}

func TestTopology(t *testing.T) {
	url, tok := testEnv(t, fakeOverview(), 60)

	v := structured(t, callTool(t, url, tok, "topology", nil))
	links := v["links"].([]any)
	if len(links) != 1 || links[0].(map[string]any)["kind"] != "wired" {
		t.Errorf("links = %v", links)
	}
	rings := v["rings"].(map[string]any)
	if len(rings["gateway"].([]any)) != 1 {
		t.Errorf("rings = %v", rings)
	}
	dists := v["distributionNodes"].([]any)
	if len(dists) != 1 || dists[0].(map[string]any)["kind"] != "inferred" {
		t.Errorf("distributionNodes = %v", dists)
	}
}

func TestAlertsActiveOnly(t *testing.T) {
	url, tok := testEnv(t, fakeOverview(), 60)

	v := structured(t, callTool(t, url, tok, "alerts", nil))
	if v["total"] != float64(2) {
		t.Errorf("total = %v", v["total"])
	}
	v = structured(t, callTool(t, url, tok, "alerts", map[string]any{"active_only": true}))
	if v["total"] != float64(1) {
		t.Errorf("total active_only = %v", v["total"])
	}
}

func TestAuthBearerOnly(t *testing.T) {
	url, tok := testEnv(t, fakeOverview(), 60)

	// Sin token.
	status, _ := rpc(t, url, "", "tools/list", 1, nil)
	if status != http.StatusUnauthorized {
		t.Errorf("sin token: status %d", status)
	}
	// Token inválido.
	status, _ = rpc(t, url, "np_bogus000000000000000000000000", "tools/list", 1, nil)
	if status != http.StatusUnauthorized {
		t.Errorf("token inválido: status %d", status)
	}
	// Token válido.
	status, _ = rpc(t, url, tok, "tools/list", 1, nil)
	if status != http.StatusOK {
		t.Errorf("token válido: status %d", status)
	}
}

func TestRateLimit(t *testing.T) {
	url, tok := testEnv(t, fakeOverview(), 2)

	for i := 1; i <= 2; i++ {
		status, _ := rpc(t, url, tok, "tools/list", i, nil)
		if status != http.StatusOK {
			t.Fatalf("petición %d: status %d", i, status)
		}
	}
	status, body := rpc(t, url, tok, "tools/list", 3, nil)
	if status != http.StatusTooManyRequests {
		t.Errorf("petición 3: status %d body %v", status, body)
	}
}

func TestHandlerNilTokensFailClosed(t *testing.T) {
	srv := New(Deps{LastOverview: fakeOverview})
	ts := httptest.NewServer(srv.Handler(nil))
	t.Cleanup(ts.Close)

	status, _ := rpc(t, ts.URL, "", "tools/list", 1, nil)
	if status != http.StatusServiceUnavailable {
		t.Errorf("sin token store debería ser 503 fail-closed, status %d", status)
	}
}

func TestNilSnapshot(t *testing.T) {
	url, tok := testEnv(t, nil, 60)

	out := callTool(t, url, tok, "fleet_status", nil)
	res := out["result"].(map[string]any)
	if res["isError"] != true {
		t.Errorf("snapshot nil debería ser isError: %v", out)
	}
}

// TestAlertsWithEngine comprueba la fuente canónica (motor de alertas) cuando
// está disponible: engine.List() manda sobre el snapshot.
func TestAlertsWithEngine(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	engine := alerts.New(d, nil)
	engine.Emit(alerts.AlertEvent{ID: "live-1", Category: "router-offline", Severity: "critical"})

	if err := apitoken.EnsureSchema(d); err != nil {
		t.Fatalf("EnsureSchema: %v", err)
	}
	store := apitoken.NewStore(d, "test-secret")
	_, raw, err := store.Create("mcp-test", apitoken.ScopeRead, 1, 0)
	if err != nil {
		t.Fatalf("Create token: %v", err)
	}
	srv := New(Deps{
		LastOverview: func() *adapters.Overview { return fakeOverview() },
		Engine:       engine,
		Version:      "0.0.0-test",
	})
	mux := http.NewServeMux()
	mux.Handle("/mcp", srv.Handler(store))
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	v := structured(t, callTool(t, ts.URL, raw, "alerts", nil))
	found := false
	for _, a := range v["alerts"].([]any) {
		if a.(map[string]any)["id"] == "live-1" {
			found = true
		}
	}
	if !found {
		t.Errorf("alerts del engine no presentes: %v", v["alerts"])
	}
}

func Example() {
	// Documentación viva del wiring mínimo.
	srv := New(Deps{
		LastOverview: func() *adapters.Overview { return nil },
		Version:      "2.31.1",
	})
	_ = srv
	fmt.Println("ok")
	// Output: ok
}
