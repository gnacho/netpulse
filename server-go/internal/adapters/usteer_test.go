package adapters

import (
	"encoding/json"
	"testing"
)

// TestBuildUsteerAP parsea una salida sintética de `ubus call usteer
// local_info` (shapes verificados en rt3) a UsteerAP, comprobando band/freq/
// load/n_assoc/local/bssid uppercase.
func TestBuildUsteerAP(t *testing.T) {
	const localInfo = `{
  "hostapd.phy0-ap0": {
    "bssid": "9c:9d:7e:1b:ea:b3",
    "ssid": "temiscira",
    "freq": 5260,
    "n_assoc": 0,
    "noise": -108,
    "load": 3,
    "max_assoc": 0,
    "roam_events": { "source": 0, "target": 0 },
    "rrm_nr": ["9c:9d:7e:1b:ea:b3"]
  },
  "hostapd.phy1-ap0": {
    "bssid": "9c:9d:7e:1b:ea:b2",
    "ssid": "temiscira",
    "freq": 2442,
    "n_assoc": 2,
    "load": 20
  }
}`
	var local map[string]usteerAPRaw
	if err := json.Unmarshal([]byte(localInfo), &local); err != nil {
		t.Fatalf("unmarshal local_info: %v", err)
	}
	if len(local) != 2 {
		t.Fatalf("esperaba 2 APs, got %d", len(local))
	}

	ap := buildUsteerAP("hostapd.phy0-ap0", local["hostapd.phy0-ap0"], "rt3", true)
	if ap.SSID != "temiscira" || ap.BSSID != "9C:9D:7E:1B:EA:B3" {
		t.Errorf("ap = %+v", ap)
	}
	if ap.Band != "5 GHz" || ap.Freq != 5260 {
		t.Errorf("band/freq = %q/%d, want 5 GHz/5260", ap.Band, ap.Freq)
	}
	if ap.UtilizationPct != 3 || ap.ClientCount != 0 {
		t.Errorf("load/n_assoc = %d/%d, want 3/0", ap.UtilizationPct, ap.ClientCount)
	}
	if !ap.Local || ap.Hostname != "rt3" {
		t.Errorf("local/hostname = %v/%q", ap.Local, ap.Hostname)
	}
	if ap.Clients == nil {
		t.Error("Clients debería ser slice vacío, no nil")
	}

	ap2 := buildUsteerAP("hostapd.phy1-ap0", local["hostapd.phy1-ap0"], "rt3", true)
	if ap2.Band != "2.4 GHz" || ap2.Freq != 2442 || ap2.ClientCount != 2 {
		t.Errorf("ap2 = %+v", ap2)
	}
}

// TestBuildUsteerAPWithClients verifica que connected_clients se asocia al AP
// por iface y se ordena por MAC.
func TestBuildUsteerAPWithClients(t *testing.T) {
	raw := usteerAPRaw{
		BSSID:  "9c:9d:7e:1b:ea:b2",
		SSID:   "temiscira",
		Freq:   2462,
		NAssoc: 2,
		Load:   39,
	}
	clients := map[string]usteerClientRaw{
		"40:44:f7:38:32:28": {Signal: -83},
		"d8:d6:68:32:7c:46": {Signal: -53},
		"00:00:00:00:00:00": {Signal: 0}, // se ignora
	}
	ap := buildUsteerAPWithClients("hostapd.phy1-ap0", raw, clients, "rt3", true)
	if len(ap.Clients) != 2 {
		t.Fatalf("esperaba 2 clientes, got %d", len(ap.Clients))
	}
	if ap.Clients[0].MAC != "40:44:F7:38:32:28" || ap.Clients[0].Signal != -83 {
		t.Errorf("client[0] = %+v", ap.Clients[0])
	}
	if ap.Clients[1].MAC != "D8:D6:68:32:7C:46" || ap.Clients[1].Signal != -53 {
		t.Errorf("client[1] = %+v", ap.Clients[1])
	}
}

// TestBuildUsteerAPVacio: entrada sin ssid/bssid se descarta en GetUsteer.
func TestBuildUsteerAPVacio(t *testing.T) {
	ap := buildUsteerAP("hostapd.phy0-ap0", usteerAPRaw{}, "rt3", true)
	if ap.SSID != "" || ap.BSSID != "" {
		t.Errorf("AP vacío debería tener ssid/bssid vacío: %+v", ap)
	}
	if ap.Band != "2.4 GHz" {
		t.Errorf("freq 0 debería caer en 2.4 GHz: %q", ap.Band)
	}
}
