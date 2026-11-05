// vlans_client_1757_test.go — VLAN de cada cliente desde el FDB del bridge
// (#1757). El PVID de un puerto dice por dónde entra una trama sin etiquetar,
// no en qué VLAN vive el cliente: el dato bueno es la VLAN con la que el
// bridge aprendió su MAC.
package adapters

import "testing"

func TestBuildDevicesVlanDesdeFdb(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	polled := map[string]*routerPolled{
		"rutto": {
			cfg: RouterConfig{ID: "rutto", Name: "RUTTO", Host: "192.168.1.3"},
			fdb: map[string]string{
				"AA:BB:CC:DD:EE:01": "eth0.999",
				"AA:BB:CC:DD:EE:02": "eth0.777",
			},
			fdbVlans: map[string]int{
				"AA:BB:CC:DD:EE:01": 999,
				"AA:BB:CC:DD:EE:02": 777,
			},
		},
	}
	devs := l.buildDevices(polled, "")
	got := map[string]int{}
	for _, d := range devs {
		got[d.MAC] = d.Vlan
	}
	if got["AA:BB:CC:DD:EE:01"] != 999 {
		t.Fatalf("cliente de eth0.999 esperado en VLAN 999, got %d (%+v)", got["AA:BB:CC:DD:EE:01"], devs)
	}
	if got["AA:BB:CC:DD:EE:02"] != 777 {
		t.Fatalf("cliente de eth0.777 esperado en VLAN 777, got %d (%+v)", got["AA:BB:CC:DD:EE:02"], devs)
	}
}

// Un FDB sin campo vlan (bridge sin VLAN filtering) NO inventa VLAN: el
// cliente se queda sin dato y la UI no pinta ninguna.
func TestBuildDevicesSinVlanEnFdbNoInventa(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	polled := map[string]*routerPolled{
		"rutto": {
			cfg: RouterConfig{ID: "rutto", Name: "RUTTO"},
			fdb: map[string]string{"AA:BB:CC:DD:EE:03": "lan1"},
		},
	}
	devs := l.buildDevices(polled, "")
	if len(devs) != 1 {
		t.Fatalf("esperado 1 dispositivo, got %+v", devs)
	}
	if devs[0].Vlan != 0 {
		t.Fatalf("sin dato de VLAN el dispositivo no debe llevar ninguna, got %d", devs[0].Vlan)
	}
}

// La VLAN del FDB es de SU router: una MAC que otro router declara en una VLAN
// no se le pega al dispositivo del primero.
func TestBuildDevicesVlanNoCruzaRouters(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	polled := map[string]*routerPolled{
		"rutto": {
			cfg:  RouterConfig{ID: "rutto", Name: "RUTTO"},
			fdb:  map[string]string{"AA:BB:CC:DD:EE:04": "eth0"},
			arp:  map[string]string{"AA:BB:CC:DD:EE:04": "192.168.1.40"},
		},
		"ap": {
			cfg:      RouterConfig{ID: "ap", Name: "AP"},
			fdbVlans: map[string]int{"AA:BB:CC:DD:EE:04": 888},
		},
	}
	devs := l.buildDevices(polled, "")
	for _, d := range devs {
		if d.MAC == "AA:BB:CC:DD:EE:04" && d.Vlan == 888 {
			t.Fatalf("la VLAN del FDB de otro router no debe aplicarse: %+v", d)
		}
	}
}
