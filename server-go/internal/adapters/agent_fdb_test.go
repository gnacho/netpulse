// agent_fdb_test.go — #291: normalización del FDB de pushers externos.
// El scraper del KP-9000 declara el puerto como número ("1") mientras sus
// bocas usan id "lan1": polledFromAgent debe alinear las claves para que el
// enriquecimiento del detalle (MAC→nombre por boca) case.
package adapters

import (
	"context"
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
)

func TestPolledFromAgentNormalizaFDBAPuertos(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{{ID: "switch16", Host: "192.168.1.6", Name: "Switch KP-9000"}}, nil)
	payload := &probe.Payload{
		Router: "switch16", Ts: 1, Version: "scraper-2.1",
		Kind: "external", Interval: 300,
		Data: probe.PayloadData{FDB: &probe.FDBData{
			MACs: map[string]string{
				"AA:BB:CC:DD:EE:01": "1",
				"AA:BB:CC:DD:EE:02": "8",
				"AA:BB:CC:DD:EE:03": "wan",
				"AA:BB:CC:DD:EE:04": "lan3", // ya normalizado: se respeta
			},
			Ports: []probe.EthPort{
				{ID: "lan1", Label: "uplink", Up: true, Speed: "1G"},
				{ID: "lan3", Label: "rt3-entrada", Up: true, Speed: "2.5G"},
				{ID: "lan8", Label: "citadel-02", Up: true, Speed: "1G"},
			},
		}},
	}
	p := l.polledFromAgent(l.routers[0], payload)
	want := map[string]string{
		"AA:BB:CC:DD:EE:01": "lan1",
		"AA:BB:CC:DD:EE:02": "lan8",
		"AA:BB:CC:DD:EE:03": "wan",
		"AA:BB:CC:DD:EE:04": "lan3",
	}
	for mac, port := range want {
		if got := p.fdb[mac]; got != port {
			t.Fatalf("fdb[%s] = %q, want %q", mac, got, port)
		}
	}
	// Sin bocas declaradas no se toca nada (FDB de routers SSH: lanN/wan ya).
	p2 := l.polledFromAgent(l.routers[0], &probe.Payload{
		Router: "switch16", Ts: 2, Version: "scraper-2.1",
		Data: probe.PayloadData{FDB: &probe.FDBData{
			MACs: map[string]string{"AA:BB:CC:DD:EE:01": "1"},
		}},
	})
	if p2.fdb["AA:BB:CC:DD:EE:01"] != "1" {
		t.Fatalf("sin puertos el FDB no debe reescribirse: %v", p2.fdb)
	}
	_ = context.Background()
}
