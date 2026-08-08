package adapters

import (
	"encoding/json"
	"testing"
)

// TestDawnAPsFromNetwork parsea una salida sintética de `ubus call dawn
// get_network` (mismo formato que producción) y verifica APs + hearing map.
func TestDawnAPsFromNetwork(t *testing.T) {
	// Un SSID con dos APs y clientes vistos por cada uno (hearing map). El
	// mismo cliente (AA:BB:...) aparece bajo dos BSSIDs: DAWN lo reporta desde
	// cada AP que lo ve. Un segundo SSID sin clientes. Un objeto sin "channel"
	// (ruido) que debe ignorarse.
	raw := `{
		"home": {
			"AA:11:BB:22:CC:01": {
				"channel": 1,
				"freq": 2412,
				"channel_utilization": 70,
				"num_sta": 2,
				"ht_support": true,
				"vht_support": false,
				"local": true,
				"iface": "wlan0",
				"hostname": "ap-living",
				"AA:BB:CC:DD:00:01": {"signal": -40, "ht": true, "vht": false},
				"AA:BB:CC:DD:00:02": {"signal": -81, "ht": false, "vht": false}
			},
			"AA:11:BB:22:CC:02": {
				"channel": 36,
				"freq": 5180,
				"channel_utilization": 6,
				"num_sta": 1,
				"ht_support": true,
				"vht_support": true,
				"local": false,
				"iface": "wlan1",
				"hostname": "ap-living",
				"AA:BB:CC:DD:00:01": {"signal": -55, "ht": true, "vht": true}
			}
		},
		"guest": {
			"AA:11:BB:22:CC:03": {
				"channel": 6,
				"freq": 2437,
				"channel_utilization": 20,
				"num_sta": 0,
				"local": true,
				"iface": "phy1-ap1",
				"hostname": "ap-guest"
			}
		}
	}`
	var data map[string]map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}

	aps := dawnAPsFromNetwork(data)
	if len(aps) != 3 {
		t.Fatalf("esperaba 3 APs, obtuvo %d", len(aps))
	}

	// Orden: hostname asc, luego banda. ap-guest (2.4) < ap-living (2.4) < ap-living (5).
	want := []struct {
		host, band string
		ch         int
		util       float64
		count      int
		nClients   int
	}{
		{"ap-guest", "2.4 GHz", 6, 20, 0, 0},
		{"ap-living", "2.4 GHz", 1, 70, 2, 2},
		{"ap-living", "5 GHz", 36, 6, 1, 1},
	}
	for i, w := range want {
		ap := aps[i]
		if ap.Hostname != w.host || ap.Band != w.band {
			t.Errorf("AP[%d] = %s %s, queria %s %s", i, ap.Hostname, ap.Band, w.host, w.band)
		}
		if ap.Channel != w.ch {
			t.Errorf("AP[%d] channel = %d, queria %d", i, ap.Channel, w.ch)
		}
		if ap.UtilizationPct != w.util {
			t.Errorf("AP[%d] util = %v, queria %v", i, ap.UtilizationPct, w.util)
		}
		if ap.ClientCount != w.count {
			t.Errorf("AP[%d] clientCount = %d, queria %d", i, ap.ClientCount, w.count)
		}
		if len(ap.Clients) != w.nClients {
			t.Errorf("AP[%d] clients = %d, queria %d", i, len(ap.Clients), w.nClients)
		}
	}

	// El cliente AA:BB:CC:DD:00:01 aparece bajo dos APs (hearing map): señal
	// -40 desde el 2.4 del living y -55 desde el 5.
	living24 := aps[1]
	if living24.SSID != "home" || living24.BSSID != "AA:11:BB:22:CC:01" {
		t.Fatalf("AP living 2.4 inesperado: %+v", living24)
	}
	sig := map[string]int{}
	for _, c := range living24.Clients {
		sig[c.MAC] = c.Signal
	}
	if sig["AA:BB:CC:DD:00:01"] != -40 {
		t.Errorf("señal cliente 01 desde living 2.4 = %d, queria -40", sig["AA:BB:CC:DD:00:01"])
	}
	if sig["AA:BB:CC:DD:00:02"] != -81 {
		t.Errorf("señal cliente 02 desde living 2.4 = %d, queria -81", sig["AA:BB:CC:DD:00:02"])
	}
	living5 := aps[2]
	sig5 := map[string]int{}
	for _, c := range living5.Clients {
		sig5[c.MAC] = c.Signal
	}
	if sig5["AA:BB:CC:DD:00:01"] != -55 {
		t.Errorf("señal cliente 01 desde living 5 = %d, queria -55", sig5["AA:BB:CC:DD:00:01"])
	}

	// MAC normalizada a mayúsculas.
	for _, c := range living24.Clients {
		if c.MAC != upperMAC(c.MAC) {
			t.Errorf("MAC no normalizada: %s", c.MAC)
		}
	}
}

func upperMAC(s string) string {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c -= 32
		}
		out[i] = c
	}
	return string(out)
}

// TestDawnAPsFromNetworkVacio: sin datos → slice vacío (no nil en JSON).
func TestDawnAPsFromNetworkVacio(t *testing.T) {
	aps := dawnAPsFromNetwork(map[string]map[string]json.RawMessage{})
	if len(aps) != 0 {
		t.Fatalf("esperaba 0 APs, obtuvo %d", len(aps))
	}
}
