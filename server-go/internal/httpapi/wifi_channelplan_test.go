// wifi_channelplan_test.go — tests de GET /api/wifi/channel-plan (#452).
package httpapi_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
)

func TestChannelPlanEndpoint(t *testing.T) {
	ts := makeTestServer(t)

	// Crear agente/token y hacer login admin.
	token, cookie := createAgentAndLogin(t, ts)

	// Construir payload con scans y empujarlo.
	payload := probe.Payload{
		Router:  "rt1",
		Ts:      time.Now().Unix(),
		Version: "test",
		Data: probe.PayloadData{
			Wireless: &probe.WirelessData{
				Clients: map[string]probe.WirelessClient{},
				Radios: []probe.Radio{
					{Name: "2.4 GHz", Channel: 6, WidthMhz: 20, PowerDbm: 20, Clients: 0},
				},
				Scans: []probe.ScanResult{
					{Iface: "wlan0", BSSID: "00:11:22:33:44:55", SSID: "vecino", Channel: 6, Freq: 2437, Signal: -50},
				},
			},
		},
	}
	res := pushPayload(t, ts, token, "10.0.9.1", payload)
	if res.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("ingest: %d %s", res.StatusCode, string(b))
	}
	res.Body.Close()

	// Consultar channel-plan.
	req, _ := http.NewRequest("GET", ts.URL+"/api/wifi/channel-plan?routerId=rt1", nil)
	req.Header.Set("Cookie", "session="+cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("channel-plan: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("channel-plan: %d %s", res.StatusCode, string(b))
	}

	var body struct {
		RouterID string                 `json:"routerId"`
		Radios   []channelplanRadioJSON `json:"radios"`
		Scans    []channelplanScanJSON  `json:"scans"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Scans) != 1 {
		t.Fatalf("esperaba 1 scan, got %d", len(body.Scans))
	}
	if len(body.Radios) != 1 {
		t.Fatalf("esperaba 1 radio, got %d", len(body.Radios))
	}
	if body.Radios[0].Recommended == 6 {
		t.Errorf("no debería recomendar canal 6 ocupado")
	}
}

type channelplanRadioJSON struct {
	Name        string `json:"name"`
	Channel     int    `json:"channel"`
	Recommended int    `json:"recommended"`
}

type channelplanScanJSON struct {
	BSSID   string `json:"bssid"`
	SSID    string `json:"ssid"`
	Channel int    `json:"channel"`
	Signal  int    `json:"signal"`
}

func createAgentAndLogin(t *testing.T, ts *testServer) (string, string) {
	t.Helper()
	token := "agent-token-rt1"
	sum := sha256.Sum256([]byte(token))
	_, err := ts.db.Exec("INSERT INTO kv (key, value) VALUES (?, ?)", "agent.token.rt1", hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatalf("insert agent token: %v", err)
	}
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	return token, cookie
}

func pushPayload(t *testing.T, ts *testServer, token, ip string, payload probe.Payload) *http.Response {
	t.Helper()
	data, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", ts.URL+"/api/ingest/agent", strings.NewReader(string(data)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", ip)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
		mac := hmac.New(sha256.New, []byte(token))
		mac.Write(data)
		req.Header.Set("X-Agent-Signature", hex.EncodeToString(mac.Sum(nil)))
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	return res
}
