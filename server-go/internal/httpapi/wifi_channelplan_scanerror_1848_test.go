// wifi_channelplan_scanerror_1848_test.go — #1848: el plan de canales debe
// decir por que un radio no tiene medidas, no solo que no las tiene.
package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
)

func TestChannelPlanSurfacesScanError(t *testing.T) {
	ts := makeTestServer(t)
	token, cookie := createAgentAndLogin(t, ts)

	const motivo = "command failed: Operation not supported (-95)"
	payload := probe.Payload{
		Router:  "rt1",
		Ts:      time.Now().Unix(),
		Version: "test",
		Data: probe.PayloadData{
			Wireless: &probe.WirelessData{
				Clients: map[string]probe.WirelessClient{},
				Radios: []probe.Radio{
					{Name: "5 GHz", Channel: 48, WidthMhz: 80, Section: "eth6"},
				},
				ScanOutcomes: []probe.ScanOutcome{
					{Iface: "eth6", Phy: 1, BSS: 0, Error: motivo},
				},
			},
		},
	}
	res := pushPayload(t, ts, token, "10.0.9.2", payload)
	if res.StatusCode != http.StatusAccepted {
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Fatalf("ingest: %d %s", res.StatusCode, string(b))
	}
	res.Body.Close()

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
		Radios []struct {
			Iface     string `json:"iface"`
			ScanError string `json:"scanError"`
		} `json:"radios"`
		ScanOutcomes []struct {
			Iface string `json:"iface"`
			BSS   int    `json:"bss"`
			Error string `json:"error"`
		} `json:"scanOutcomes"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Radios) != 1 {
		t.Fatalf("esperaba 1 radio, got %d", len(body.Radios))
	}
	if body.Radios[0].ScanError != motivo {
		t.Errorf("el radio debe llevar el motivo del rechazo: %q", body.Radios[0].ScanError)
	}
	if len(body.ScanOutcomes) != 1 {
		t.Fatalf("esperaba 1 outcome, got %d", len(body.ScanOutcomes))
	}
	if body.ScanOutcomes[0].Iface != "eth6" || body.ScanOutcomes[0].Error != motivo {
		t.Errorf("outcome inesperado: %+v", body.ScanOutcomes[0])
	}
}
