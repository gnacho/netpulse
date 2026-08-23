// alerts_api_test.go — contrato de los endpoints de alertas (SPEC-ALERTAS §4):
// GET /api/alerts (+category, +unread=1), GET/PUT /api/alerts/config,
// POST /api/alerts/read y /api/alerts/read-all (server truth en overview).
package httpapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func doJSON(t *testing.T, method, base, path, cookie string, body string) *http.Response {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = bytes.NewBufferString(body)
	}
	req, _ := http.NewRequest(method, base+path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.Header.Set("Cookie", "session="+cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	return res
}

func alertItems(t *testing.T, res *http.Response) []map[string]any {
	t.Helper()
	defer res.Body.Close()
	var body struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("json: %v", err)
	}
	return body.Items
}

func TestAlertsContractFields(t *testing.T) {
	ts := makeTestServer(t)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	res := get(t, ts.URL, "/api/alerts", cookie)
	items := alertItems(t, res)
	if len(items) != 5 {
		t.Fatalf("alertas demo: %d, esperaba 5", len(items))
	}
	for _, a := range items {
		if a["category"] == nil || a["category"] == "" {
			t.Fatalf("alerta sin category: %v", a["id"])
		}
		if _, ok := a["urgent"].(bool); !ok {
			t.Fatalf("alerta sin urgent bool: %v", a["id"])
		}
		if ts64, ok := a["ts"].(float64); !ok || ts64 <= 0 {
			t.Fatalf("alerta sin ts: %v", a["id"])
		}
	}
	// Filtro por categoría: vpn → solo el handshake
	items = alertItems(t, get(t, ts.URL, "/api/alerts?category=vpn", cookie))
	if len(items) != 1 || items[0]["id"] != "alert-handshake-wg" {
		t.Fatalf("category=vpn: %v", items)
	}
	// Categoría inválida → 400
	res = get(t, ts.URL, "/api/alerts?category=wifi", cookie)
	if res.StatusCode != 400 {
		t.Fatalf("category inválida: %d, esperaba 400", res.StatusCode)
	}
	res.Body.Close()
	// unread=1 → las 2 no leídas del canon
	items = alertItems(t, get(t, ts.URL, "/api/alerts?unread=1", cookie))
	if len(items) != 2 {
		t.Fatalf("unread=1: %d, esperaba 2", len(items))
	}
	for _, a := range items {
		if a["read"] == true {
			t.Fatalf("unread=1 devolvió leída: %v", a["id"])
		}
	}
	// Combinado: category + unread
	items = alertItems(t, get(t, ts.URL, "/api/alerts?category=clients&unread=1", cookie))
	if len(items) != 0 {
		t.Fatalf("clients&unread: %d, esperaba 0 (la canon clients está leída)", len(items))
	}
}

func TestAlertsConfigEndpoints(t *testing.T) {
	ts := makeTestServer(t)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	res := get(t, ts.URL, "/api/alerts/config", cookie)
	cfg := readJSON(t, res)
	if len(cfg) != 6 {
		t.Fatalf("config: %d claves, esperaba 6", len(cfg))
	}
	want := map[string]string{"router": "urgent", "internet": "urgent", "clients": "all",
		"signal": "none", "vpn": "none", "system": "all"}
	for k, v := range want {
		if cfg[k] != v {
			t.Fatalf("default %s: %v, esperaba %s", k, cfg[k], v)
		}
	}
	// PUT parcial → 200 y devuelve la config efectiva completa
	res = doJSON(t, "PUT", ts.URL, "/api/alerts/config", cookie, `{"vpn":"all"}`)
	if res.StatusCode != 200 {
		t.Fatalf("PUT config: %d", res.StatusCode)
	}
	cfg = readJSON(t, res)
	if cfg["vpn"] != "all" || cfg["router"] != "urgent" {
		t.Fatalf("PUT parcial: %v", cfg)
	}
	// Roundtrip: GET refleja el cambio (persistido en kv)
	cfg = readJSON(t, get(t, ts.URL, "/api/alerts/config", cookie))
	if cfg["vpn"] != "all" {
		t.Fatalf("roundtrip: %v", cfg)
	}
	// Nivel inválido → 400
	res = doJSON(t, "PUT", ts.URL, "/api/alerts/config", cookie, `{"vpn":"todo"}`)
	if res.StatusCode != 400 {
		t.Fatalf("nivel inválido: %d, esperaba 400", res.StatusCode)
	}
	res.Body.Close()
	// Categoría desconocida → 400
	res = doJSON(t, "PUT", ts.URL, "/api/alerts/config", cookie, `{"wifi":"all"}`)
	if res.StatusCode != 400 {
		t.Fatalf("categoría desconocida: %d, esperaba 400", res.StatusCode)
	}
	res.Body.Close()
	// Body no-JSON → 400
	res = doJSON(t, "PUT", ts.URL, "/api/alerts/config", cookie, `nope`)
	if res.StatusCode != 400 {
		t.Fatalf("body inválido: %d, esperaba 400", res.StatusCode)
	}
	res.Body.Close()
}

func TestAlertsReadEndpoints(t *testing.T) {
	ts := makeTestServer(t)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	unreadOf := func() float64 {
		res := get(t, ts.URL, "/api/overview", cookie)
		body := readJSON(t, res)
		return body["unreadAlerts"].(float64)
	}
	if got := unreadOf(); got != 2 {
		t.Fatalf("unreadAlerts inicial: %v, esperaba 2", got)
	}
	// POST read {ids}: marca una
	res := doJSON(t, "POST", ts.URL, "/api/alerts/read", cookie, `{"ids":["alert-temp-patio"]}`)
	if res.StatusCode != 200 {
		t.Fatalf("read: %d", res.StatusCode)
	}
	res.Body.Close()
	if got := unreadOf(); got != 1 {
		t.Fatalf("unreadAlerts tras read: %v, esperaba 1", got)
	}
	items := alertItems(t, get(t, ts.URL, "/api/alerts", cookie))
	for _, a := range items {
		if a["id"] == "alert-temp-patio" && a["read"] != true {
			t.Fatal("read no aplicado en GET /api/alerts")
		}
	}
	// POST read-all → 0 en overview (server truth)
	res = doJSON(t, "POST", ts.URL, "/api/alerts/read-all", cookie, "")
	if res.StatusCode != 200 {
		t.Fatalf("read-all: %d", res.StatusCode)
	}
	res.Body.Close()
	if got := unreadOf(); got != 0 {
		t.Fatalf("unreadAlerts tras read-all: %v, esperaba 0", got)
	}
	items = alertItems(t, get(t, ts.URL, "/api/alerts?unread=1", cookie))
	if len(items) != 0 {
		t.Fatalf("unread=1 tras read-all: %d", len(items))
	}
}

// writeJSON sin '\n' final (paridad D5, SPEC-ALERTAS §4).
func TestAlertsNoTrailingNewline(t *testing.T) {
	ts := makeTestServer(t)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	res := get(t, ts.URL, "/api/alerts/config", cookie)
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if strings.HasSuffix(string(raw), "\n") {
		t.Fatal("writeJSON no debe terminar en \\n")
	}
}
