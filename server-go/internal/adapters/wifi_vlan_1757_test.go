package adapters

import "testing"

// #1757: la interfaz AP y el SSID de una estacion viajan hasta el Device. La
// topologia los cruza con el bridge VLAN filtering del router para etiquetar
// la VLAN del cliente inalambrico; sin estos campos solo se podia etiquetar
// el cableado.
func TestBuildDevicesCarriesWifiIfaceAndSSID(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	polled := map[string]*routerPolled{
		"gw": {
			cfg: RouterConfig{ID: "gw", IsGateway: true},
			wireless: map[string]WirelessClient{
				"AA:BB:CC:DD:EE:01": {SignalDbm: -55, Band: "5 GHz", Iface: "wlan0", SSID: "IoT"},
			},
			arp:      map[string]string{},
			arpStale: map[string]bool{},
		},
	}
	devs := l.buildDevices(polled, "")
	if len(devs) != 1 {
		t.Fatalf("devices = %d, want 1: %+v", len(devs), devs)
	}
	d := devs[0]
	if d.WifiIface != "wlan0" || d.SSID != "IoT" {
		t.Errorf("iface=%q ssid=%q, want wlan0 / IoT", d.WifiIface, d.SSID)
	}
	if d.Band != "5 GHz" || d.SignalDbm == nil || *d.SignalDbm != -55 {
		t.Errorf("band=%q signal=%v, want 5 GHz / -55", d.Band, d.SignalDbm)
	}
}
