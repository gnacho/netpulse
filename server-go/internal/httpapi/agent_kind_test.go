package httpapi_test

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestAgentsNetgripKind (#363): un push con kind "netgrip" se lista como tal,
// no marca updateAvailable y el servidor rechaza upgrade, rearm y reinstall
// (el agente es el panel NetGrip; se gestiona desde el propio router).
func TestAgentsNetgripKind(t *testing.T) {
	ts := makeAgentTestServer(t)
	_, token, _ := createAgentToken(t, ts, "patio")

	payload := fmt.Sprintf(`{"router":"patio","ts":%d,"version":"0.23.0","kind":"netgrip","data":{"system":{"sysinfo":{"uptime":100,"load":[0,0,0],"memory":{"total":100,"free":50,"buffered":0,"available":50}},"cpu":10,"temp":40},"wireless":{"clients":{}},"dhcp":{"leases":[]},"fdb":{"macs":{}}}}`, time.Now().Unix())
	res := ingest(t, ts, token, "10.0.3.7", payload)
	res.Body.Close()
	if res.StatusCode != 202 {
		t.Fatalf("ingest netgrip: %d", res.StatusCode)
	}

	res = get(t, ts.URL, "/api/agents", ts.cookie)
	body := readJSON(t, res)
	agents, _ := body["agents"].([]any)
	var patio map[string]any
	for _, a := range agents {
		m, _ := a.(map[string]any)
		if m["slug"] == "patio" {
			patio = m
		}
	}
	if patio == nil {
		t.Fatalf("patio no está en la lista: %v", body)
	}
	if patio["kind"] != "netgrip" {
		t.Fatalf("patio kind: %v (want netgrip)", patio["kind"])
	}
	if patio["version"] != "0.23.0" {
		t.Fatalf("patio version: %v", patio["version"])
	}
	if upd, ok := patio["updateAvailable"].(bool); ok && upd {
		t.Fatalf("netgrip agent no debe ofrecer upgrade: %v", patio)
	}

	post := func(path string) int {
		req, _ := http.NewRequest("POST", ts.URL+path, nil)
		req.Header.Set("Cookie", "session="+ts.cookie)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		defer r.Body.Close()
		return r.StatusCode
	}

	if c := post("/api/agents/patio/upgrade"); c != http.StatusConflict {
		b, _ := io.ReadAll(http.NoBody)
		t.Fatalf("upgrade netgrip: %d (want 409) %s", c, b)
	}
	if c := post("/api/agents/patio/rearm"); c != http.StatusConflict {
		t.Fatalf("rearm netgrip: %d (want 409)", c)
	}

	// Un agente NATIVO sin kind sigue listándose como native.
	_, tokenN, _ := createAgentToken(t, ts, "living")
	payloadN := strings.Replace(strings.Replace(payload, `"patio"`, `"living"`, 1), `"kind":"netgrip",`, ``, 1)
	resN := ingest(t, ts, tokenN, "10.0.3.8", payloadN)
	resN.Body.Close()
	res = get(t, ts.URL, "/api/agents", ts.cookie)
	body = readJSON(t, res)
	agents, _ = body["agents"].([]any)
	for _, a := range agents {
		m, _ := a.(map[string]any)
		if m["slug"] == "living" && m["kind"] != "native" {
			t.Fatalf("living kind: %v (want native)", m["kind"])
		}
	}
}
