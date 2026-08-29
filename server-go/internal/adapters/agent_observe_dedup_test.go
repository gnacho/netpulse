// agent_observe_dedup_test.go — issue #365: entre pushes del agente, cada
// rebuild del overview re-deriva el sondeo con el MISMO payload cacheado.
// Re-observar contadores congelados no debe alimentar el PortMonitor ni las
// series por puerto (falsa alerta de puerto fantasma).
package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
)

func ghostPayload(ts int64, rx, tx uint64) *probe.Payload {
	p := &probe.Payload{Router: "patio", Ts: ts, Version: "t"}
	p.Data.FDB = &probe.FDBData{
		MACs:  map[string]string{},
		Ports: []probe.EthPort{{ID: "lan1", Label: "LAN 1", Up: true, Speed: "1 Gbps", Iface: "lan1", RxBytes: rx, TxBytes: tx}},
	}
	return p
}

func TestPolledFromAgentDuplicatePayloadDoesNotObserve(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{{ID: "patio", Host: "192.168.8.2", Name: "Patio"}}, nil)
	cfg := l.routers[0]

	// Historia sana: 12 payloads nuevos con tráfico creciente.
	var rx uint64 = 1000
	ts := int64(1000)
	for i := 0; i < 12; i++ {
		rx += 1000
		ts += 30
		l.polledFromAgent(cfg, ghostPayload(ts, rx, 500))
	}

	// El MISMO payload (ts congelado) re-derivado muchas veces: ninguna
	// observación nueva, el streak no puede crecer.
	frozen := ghostPayload(ts, rx, 500)
	for i := 0; i < 10; i++ {
		l.polledFromAgent(cfg, frozen)
	}
	for _, ev := range l.engine.List() {
		if ev.Title == "Ghost port: LAN 1 went silent" {
			t.Fatal("cached duplicate observations must not raise a ghost alert")
		}
	}

	// 3 payloads NUEVOS con contadores congelados (silencio real) sí alertan,
	// una única vez aunque lleguen más.
	for i := 0; i < 3; i++ {
		ts += 30
		l.polledFromAgent(cfg, ghostPayload(ts, rx, 500))
	}
	n := 0
	for _, ev := range l.engine.List() {
		if ev.Title == "Ghost port: LAN 1 went silent" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("ghost alerts = %d, want 1", n)
	}
}
