// batadv_test.go — #1818: las tramas LOOP DETECT de BATMAN-adv llegan al FDB
// del bridge con origen BA:BE:xx:xx:xx:xx. No son hosts y no deben aparecer
// como clientes ni sostener nodos de topología.
package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
)

func TestIsBatadvBlaMAC(t *testing.T) {
	yes := []string{
		"BA:BE:11:22:33:44",
		"ba:be:aa:bb:cc:dd",
		"BA-BE-00-11-22-33",
	}
	no := []string{
		"",
		"BA:BF:11:22:33:44",
		"AA:BB:CC:DD:EE:FF",
		"BA:BE:11:22:33",    // no es una MAC de 6 octetos
		"D2:95:DE:65:C5:B0", // NMRP: localmente administrada, pero no es BLA
	}
	for _, mac := range yes {
		if !isBatadvBlaMAC(mac) {
			t.Errorf("%q debería considerarse BLA", mac)
		}
	}
	for _, mac := range no {
		if isBatadvBlaMAC(mac) {
			t.Errorf("%q no debería considerarse BLA", mac)
		}
	}
}

func TestWithoutBatadvBla(t *testing.T) {
	in := map[string]string{
		"BA:BE:11:22:33:44": "lan1",
		"AA:BB:CC:DD:EE:01": "lan1",
	}
	out := withoutBatadvBla(in)
	if _, ok := out["BA:BE:11:22:33:44"]; ok {
		t.Fatalf("la MAC de BLA sigue en el FDB: %+v", out)
	}
	if out["AA:BB:CC:DD:EE:01"] != "lan1" {
		t.Fatalf("se perdió un cliente real: %+v", out)
	}
	// El mapa original no se muta: el payload del agente se comparte.
	if _, ok := in["BA:BE:11:22:33:44"]; !ok {
		t.Fatalf("withoutBatadvBla mutó el mapa de entrada")
	}
	// Sin nada que quitar se devuelve el mismo mapa, sin asignar.
	clean := map[string]string{"AA:BB:CC:DD:EE:02": "lan2"}
	if got := withoutBatadvBla(clean); len(got) != 1 {
		t.Fatalf("FDB limpio alterado: %+v", got)
	}
}

func TestBuildDevicesSkipsBatadvBla(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	polled := map[string]*routerPolled{
		"gw": {
			cfg: RouterConfig{ID: "gw", Name: "GW", Host: "192.168.1.1"},
			fdb: map[string]string{
				"BA:BE:11:22:33:44": "lan1", // sonda del mesh
				"AA:BB:CC:DD:EE:01": "lan1", // cliente real
			},
			arp: map[string]string{"AA:BB:CC:DD:EE:01": "192.168.1.50"},
		},
	}
	devs := l.buildDevices(polled, "")
	for _, d := range devs {
		if isBatadvBlaMAC(d.MAC) {
			t.Fatalf("la MAC de BLA llegó a la lista de clientes: %+v", d)
		}
	}
	if len(devs) != 1 || devs[0].MAC != "AA:BB:CC:DD:EE:01" {
		t.Fatalf("clientes = %+v, esperaba solo AA:BB:CC:DD:EE:01", devs)
	}
}

func TestPolledFromAgentDropsBatadvBla(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	p := &probe.Payload{Data: probe.PayloadData{FDB: &probe.FDBData{
		MACs: map[string]string{
			"BA:BE:55:66:77:88": "lan1",
			"AA:BB:CC:DD:EE:02": "lan2",
		},
	}}}
	out := l.polledFromAgent(RouterConfig{ID: "sw", Type: "external"}, p)
	if _, ok := out.fdb["BA:BE:55:66:77:88"]; ok {
		t.Fatalf("el polled del agente conserva la MAC de BLA: %+v", out.fdb)
	}
	if out.fdb["AA:BB:CC:DD:EE:02"] != "lan2" {
		t.Fatalf("se perdió un cliente real: %+v", out.fdb)
	}
}
