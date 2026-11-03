package adapters

import "testing"

// #1818: la MAC del NMRP de Netgear (D2:...) es localmente administrada y el
// servidor debe reconocerlo sin depender del escaneo de discovery.
func TestIsLocallyAdministeredMAC(t *testing.T) {
	tests := []struct {
		mac  string
		want bool
	}{
		{"D2:95:DE:65:C5:B0", true}, // NMRP de Netgear (el del issue)
		{"9E:11:22:33:44:55", true}, // bit 0x02 puesto
		{"02:00:00:00:00:01", true}, // mínimo del rango local
		{"d2:95:de:65:c5:b0", true}, // minúsculas
		{"  D2:95:DE:65:C5:B0  ", true},
		{"04:F4:1C:F3:46:65", false}, // OUI de fabricante (MikroTik)
		{"94:83:C4:00:00:02", false}, // GL.iNet
		{"BC:24:11:0A:0B:0C", false}, // OUI registrado de Proxmox
		{"00:00:00:00:00:00", false},
		{"", false},
		{"ZZ:95:DE:65:C5:B0", false}, // no parseable
		{"D", false},
	}
	for _, tt := range tests {
		if got := isLocallyAdministeredMAC(tt.mac); got != tt.want {
			t.Errorf("isLocallyAdministeredMAC(%q) = %v, esperaba %v", tt.mac, got, tt.want)
		}
	}
}

// #1818: una MAC efímera vista solo en el FDB sale marcada como RandomMAC,
// aunque el agente no la haya reportado en su escaneo de discovery.
func TestBuildDevicesMarksLocallyAdministeredFromFDB(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	polled := map[string]*routerPolled{
		"gw": {
			cfg:      RouterConfig{ID: "gw", IsGateway: true},
			fdb:      map[string]string{"D2:95:DE:65:C5:B0": "lan2", "04:F4:1C:F3:46:65": "lan3"},
			arp:      map[string]string{},
			arpStale: map[string]bool{},
		},
	}
	byMAC := map[string]Device{}
	for _, d := range l.buildDevices(polled, "") {
		byMAC[d.MAC] = d
	}
	if d, ok := byMAC["D2:95:DE:65:C5:B0"]; !ok {
		t.Fatal("la MAC del NMRP no aparece como dispositivo")
	} else if !d.RandomMAC {
		t.Error("la MAC localmente administrada no quedó marcada como RandomMAC")
	}
	if d, ok := byMAC["04:F4:1C:F3:46:65"]; ok && d.RandomMAC {
		t.Error("una MAC de fabricante quedó marcada como RandomMAC")
	}
}

// #1818: la MAC local sin IP ni lease no se recuerda; con cualquiera de los
// dos, o si es de fabricante, sí.
func TestPersistLocalMAC(t *testing.T) {
	cases := []struct {
		name     string
		mac      string
		ip       string
		hasLease bool
		wasKnown bool
		want     bool
	}{
		{"NMRP sin IP ni lease", "D2:95:DE:65:C5:B0", "", false, false, false},
		{"local con IP", "D2:95:DE:65:C5:B0", "192.168.1.61", false, false, true},
		{"local con lease", "D2:95:DE:65:C5:B0", "", true, false, true},
		{"local ya conocida", "D2:95:DE:65:C5:B0", "", false, true, true},
		{"fabricante sin nada", "04:F4:1C:F3:46:65", "", false, false, true},
	}
	for _, c := range cases {
		if got := persistLocalMAC(c.mac, c.ip, c.hasLease, c.wasKnown); got != c.want {
			t.Errorf("%s: persistLocalMAC = %v, esperaba %v", c.name, got, c.want)
		}
	}
}
