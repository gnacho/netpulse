// portstats_agent_test.go — #305: rates por boca desde el payload del agente.
// El payload trae contadores ABSOLUTOS por iface (NetIf); el server calcula
// bps con el delta entre payloads y los inyecta en las bocas (match por
// Iface, fallback por ID). Sección ausente → conserva los rates cacheados.
package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
)

func TestPolledFromAgentNetIfRates(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{{ID: "patio", Host: "192.168.8.2", Name: "Patio"}}, nil)
	cfg := l.routers[0]

	// Primer push: bocas con iface + absolutos, NetIf presente. Sin previo
	// no hay rates todavía.
	p1 := &probe.Payload{Router: "patio", Ts: 1000, Version: "t"}
	p1.Data.FDB = &probe.FDBData{
		MACs:  map[string]string{},
		Ports: []probe.EthPort{{ID: "lan1", Label: "LAN 1", Up: true, Speed: "1 Gbps", Iface: "lan1", RxBytes: 1000, TxBytes: 2000}},
	}
	p1.Data.NetIf = map[string]probe.IfCounters{"lan1": {Rx: 1000, Tx: 2000}}
	out := l.polledFromAgent(cfg, p1)
	if out.ports[0].RxBps != 0 || out.ports[0].TxBps != 0 {
		t.Fatalf("primera muestra sin rates: %+v", out.ports[0])
	}

	// Segundo push 30 s después: +3000 B rx / +6000 B tx → 800/1600 bps.
	p2 := &probe.Payload{Router: "patio", Ts: 1030, Version: "t"}
	p2.Data.FDB = &probe.FDBData{
		MACs:  map[string]string{},
		Ports: []probe.EthPort{{ID: "lan1", Label: "LAN 1", Up: true, Speed: "1 Gbps", Iface: "lan1"}},
	}
	p2.Data.NetIf = map[string]probe.IfCounters{"lan1": {Rx: 4000, Tx: 8000, RxErr: 2}}
	out = l.polledFromAgent(cfg, p2)
	p := out.ports[0]
	if p.RxBps != 800 || p.TxBps != 1600 {
		t.Fatalf("rates: %+v", p)
	}
	if p.RxBytes != 4000 || p.TxBytes != 8000 || p.RxErrs != 2 {
		t.Fatalf("absolutos: %+v", p)
	}

	// Push SIN NetIf (BuildWireless): conserva bocas cacheadas con rates.
	p3 := &probe.Payload{Router: "patio", Ts: 1060, Version: "t"}
	out = l.polledFromAgent(cfg, p3)
	if out.ports[0].RxBps != 800 || out.ports[0].RxBytes != 4000 {
		t.Fatalf("anti-parpadeo rates: %+v", out.ports[0])
	}

	// Match por ID cuando la boca no declara Iface (pusher externo).
	l2 := NewLive(nil, nil, []RouterConfig{{ID: "sw", Host: "192.168.8.9", Name: "SW"}}, nil)
	q1 := &probe.Payload{Router: "sw", Ts: 2000, Version: "t",
		Data: probe.PayloadData{
			FDB:   &probe.FDBData{MACs: map[string]string{}, Ports: []probe.EthPort{{ID: "lan1", Label: "P1", Up: true}}},
			NetIf: map[string]probe.IfCounters{"lan1": {Rx: 100, Tx: 100}},
		}}
	q2 := &probe.Payload{Router: "sw", Ts: 2010, Version: "t",
		Data: probe.PayloadData{
			FDB:   &probe.FDBData{MACs: map[string]string{}, Ports: []probe.EthPort{{ID: "lan1", Label: "P1", Up: true}}},
			NetIf: map[string]probe.IfCounters{"lan1": {Rx: 300, Tx: 300}},
		}}
	l2.polledFromAgent(l2.routers[0], q1)
	out = l2.polledFromAgent(l2.routers[0], q2)
	if out.ports[0].Iface != "lan1" || out.ports[0].RxBps != 160 || out.ports[0].TxBps != 160 {
		t.Fatalf("match por ID: %+v", out.ports[0])
	}
}
