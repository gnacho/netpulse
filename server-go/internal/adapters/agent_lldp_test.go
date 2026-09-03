// agent_lldp_test.go — #489: consumo de la sección lldp del payload del
// agente en polledFromAgent, con el anti-parpadeo de pushes event-driven y
// sondas fallidas (mismo patrón que system #441).
package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
)

func lldpAgentPayload(ts int64, lldp *probe.LldpData) *probe.Payload {
	return &probe.Payload{
		Router: "gateway", Ts: ts, Version: "test",
		Data: probe.PayloadData{
			System: &probe.SystemData{BridgeMAC: "94:83:c4:00:00:01"},
			Lldp:   lldp,
		},
	}
}

var lldpGoodNeighbors = []probe.LldpNeighbor{
	{Port: "lan3", Chassis: "GS308E", ChassisMac: "28:C6:8E:1D:90:44", Mgmt: "192.168.1.13", Caps: []string{"Bridge"}, PortDesc: "ge5"},
}

func TestPolledFromAgentLldp(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{{ID: "gateway", Host: "192.168.1.1", Name: "Gateway"}}, nil)
	cfg := l.routers[0]

	// 1. Push completo con vecinos: lldp poblado, disponible.
	p := l.polledFromAgent(cfg, lldpAgentPayload(1, &probe.LldpData{Available: true, Neighbors: lldpGoodNeighbors}))
	if len(p.lldp) != 1 || p.lldp[0].Chassis != "GS308E" || p.lldp[0].ChassisMac != "28:C6:8E:1D:90:44" {
		t.Fatalf("lldp: %+v", p.lldp)
	}
	if p.lldpUnavailable {
		t.Fatal("no debe marcar unavailable con vecinos")
	}

	// 2. Push event-driven SIN sección: conserva los vecinos (anti-parpadeo).
	p = l.polledFromAgent(cfg, lldpAgentPayload(2, nil))
	if len(p.lldp) != 1 || p.lldp[0].Chassis != "GS308E" {
		t.Fatalf("event-driven perdió lldp: %+v", p.lldp)
	}

	// 3. Sonda fallida (Available=true, Neighbors nil): conserva el último bueno.
	p = l.polledFromAgent(cfg, lldpAgentPayload(3, &probe.LldpData{Available: true}))
	if len(p.lldp) != 1 {
		t.Fatalf("sonda fallida perdió lldp: %+v", p.lldp)
	}

	// 4. Cero vecinos honesto (Available=true, []): pisa lo cacheado.
	p = l.polledFromAgent(cfg, lldpAgentPayload(4, &probe.LldpData{Available: true, Neighbors: []probe.LldpNeighbor{}}))
	if p.lldp == nil || len(p.lldp) != 0 {
		t.Fatalf("vacío honesto: %+v", p.lldp)
	}

	// 5. lldpd desinstalado (Available=false): sin vecinos + hint de UI, y
	// persiste en un push event-driven posterior.
	p = l.polledFromAgent(cfg, lldpAgentPayload(5, &probe.LldpData{Available: false}))
	if p.lldp != nil || !p.lldpUnavailable {
		t.Fatalf("unavailable: lldp=%v flag=%v", p.lldp, p.lldpUnavailable)
	}
	p = l.polledFromAgent(cfg, lldpAgentPayload(6, nil))
	if !p.lldpUnavailable {
		t.Fatal("unavailable debe persistir sin sección")
	}

	// 6. Reinstalado con vecinos: vuelve a poblar.
	p = l.polledFromAgent(cfg, lldpAgentPayload(7, &probe.LldpData{Available: true, Neighbors: lldpGoodNeighbors}))
	if len(p.lldp) != 1 || p.lldpUnavailable {
		t.Fatalf("reinstalado: %+v flag=%v", p.lldp, p.lldpUnavailable)
	}
}
