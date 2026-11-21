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

func TestIsBatadvBackhaulPort(t *testing.T) {
	yes := []string{"bat0", "bat0.40", "bat1", "bat12.100"}
	for _, port := range yes {
		if !isBatadvBackhaulPort(port) {
			t.Errorf("%q should be treated as BATMAN backhaul port", port)
		}
	}
	no := []string{"", "lan2", "lan3", "eth0.40", "bat", "batman", "bat0x"}
	for _, port := range no {
		if isBatadvBackhaulPort(port) {
			t.Errorf("%q should not be treated as BATMAN backhaul port", port)
		}
	}
}

func TestBuildDevicesPrefersPhysicalLANOverBatadvBackhaul(t *testing.T) {
	const hueMAC = "00:17:88:11:22:33"
	const eufyMAC = "8C:85:80:44:55:66"

	for iter := 0; iter < 50; iter++ {
		l := NewLive(nil, nil, nil, nil)
		polled := map[string]*routerPolled{
			"gw": {
				cfg:   RouterConfig{ID: "gw", Name: "Gateway", Host: "192.168.1.1", IsGateway: true},
				brMac: "AA:AA:AA:00:00:01",
				arp: map[string]string{
					hueMAC:  "192.168.40.10",
					eufyMAC: "192.168.40.11",
				},
			},
			"mx1": {
				cfg:   RouterConfig{ID: "mx1", Name: "MX5300 #1", Host: "192.168.1.2"},
				brMac: "AA:AA:AA:00:00:02",
				fdb: map[string]string{
					hueMAC:  "lan3",
					eufyMAC: "lan2",
				},
			},
			"mx2": {
				cfg:   RouterConfig{ID: "mx2", Name: "MX5300 #2", Host: "192.168.1.3"},
				brMac: "AA:AA:AA:00:00:03",
				fdb: map[string]string{
					hueMAC:  "bat0.40",
					eufyMAC: "bat0.40",
				},
			},
		}
		l.updateFdbMemo(polled, 1_000_000)
		devs := l.buildDevices(polled, "gw")
		byMac := make(map[string]Device, len(devs))
		for _, d := range devs {
			byMac[d.MAC] = d
		}
		if got := byMac[hueMAC].RouterID; got != "mx1" {
			t.Fatalf("iter %d: Hue Bridge attributed to %q, want mx1", iter, got)
		}
		if got := byMac[eufyMAC].RouterID; got != "mx1" {
			t.Fatalf("iter %d: Eufy HomeBase attributed to %q, want mx1", iter, got)
		}
	}
}
