// agent_wan_test.go — #276: el estado WAN viaja en el payload del agente.
// Un router agent_only no tiene cliente SSH, así que la sonda del servidor
// no corre nunca: sin el campo del payload, el panel de conexión del
// gateway se queda con "—" aunque el uplink esté perfectamente arriba.
package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
)

func TestPolledFromAgentTomaElWanDelPayload(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "gateway", Host: "192.0.2.1", Name: "openwrt", IsGateway: true, AgentOnly: true},
	}, nil)
	payload := &probe.Payload{
		Router: "gateway", Ts: 1, Version: "2.28.24",
		Data: probe.PayloadData{Wan: &probe.WanInfo{
			Proto: "pppoe", Device: "pppoe-isp", Port: "lan1",
			IP: "203.0.113.45", Gateway: "198.51.100.1",
			DNS: []string{"198.51.100.53"},
		}},
	}
	p := l.polledFromAgent(l.routers[0], payload)
	if p.wanInfo.IP != "203.0.113.45" || p.wanInfo.Proto != "pppoe" {
		t.Fatalf("wanInfo del payload: %+v", p.wanInfo)
	}
	if p.wanInfo.Gateway != "198.51.100.1" || p.wanInfo.Port != "lan1" {
		t.Fatalf("wanInfo del payload: %+v", p.wanInfo)
	}
}

func TestPolledFromAgentSinWanQuedaVacio(t *testing.T) {
	// Agente viejo (sin el campo) o AP sin uplink: nada que ingerir, y el
	// servidor sigue pudiendo sondear por SSH si el router lo permite.
	l := NewLive(nil, nil, []RouterConfig{{ID: "ap1", Host: "192.0.2.2", Name: "ap"}}, nil)
	payload := &probe.Payload{Router: "ap1", Ts: 1, Version: "2.28.0"}
	p := l.polledFromAgent(l.routers[0], payload)
	if p.wanInfo.Proto != "" || p.wanInfo.IP != "" {
		t.Fatalf("sin wan en el payload debía quedar vacío: %+v", p.wanInfo)
	}
}

// The several-connections section travels the same way the single one
// does, and is the only source for a router polled through its agent.
func TestPolledFromAgentTakesMultiWanFromPayload(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "gateway", Host: "192.0.2.1", Name: "gateway", IsGateway: true, AgentOnly: true},
	}, nil)
	payload := &probe.Payload{
		Router: "gateway", Ts: 1, Version: "2.29.0",
		Data: probe.PayloadData{MultiWan: &probe.MultiWanInfo{
			Mode: "failover", Managed: true, Active: "cell", Primary: "fiber",
			Uplinks: []probe.WanUplink{
				{Name: "fiber", Proto: "pppoe", IP: "203.0.113.45", Up: true, Primary: true},
				{Name: "cell", Proto: "qmi", IP: "192.0.2.77", Up: true, Active: true, Metered: true},
			},
		}},
	}
	p := l.polledFromAgent(l.routers[0], payload)
	if p.multiWan == nil || p.multiWan.Active != "cell" || len(p.multiWan.Uplinks) != 2 {
		t.Fatalf("multiWan = %+v", p.multiWan)
	}
}

// An event-driven push carries no such section. Dropping it would make the
// panel blink out of the page every time a device associates.
func TestPolledFromAgentKeepsTheLastMultiWanSection(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "gateway", Host: "192.0.2.1", Name: "gateway", IsGateway: true, AgentOnly: true},
	}, nil)
	full := &probe.Payload{Router: "gateway", Ts: 1, Data: probe.PayloadData{
		MultiWan: &probe.MultiWanInfo{Mode: "balance", Uplinks: []probe.WanUplink{{Name: "a"}, {Name: "b"}}},
		Vlans:    []probe.VlanPort{{Port: "lan1"}},
	}}
	l.polledFromAgent(l.routers[0], full)

	wirelessOnly := &probe.Payload{Router: "gateway", Ts: 2, Data: probe.PayloadData{}}
	p := l.polledFromAgent(l.routers[0], wirelessOnly)
	if p.multiWan == nil || p.multiWan.Mode != "balance" {
		t.Fatalf("multiWan = %+v, want the last good section", p.multiWan)
	}
	// Same rule for the VLANs, which travel in the payload and were being
	// dropped on this path entirely.
	if len(p.vlans) != 1 {
		t.Fatalf("vlans = %+v, want the last good section", p.vlans)
	}
}

// An older agent reports nothing, and nothing is what the panel gets.
func TestPolledFromAgentWithoutMultiWanStaysNil(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "gateway", Host: "192.0.2.1", Name: "gateway", IsGateway: true, AgentOnly: true},
	}, nil)
	p := l.polledFromAgent(l.routers[0], &probe.Payload{Router: "gateway", Ts: 1, Version: "2.28.0"})
	if p.multiWan != nil {
		t.Fatalf("multiWan = %+v, want nothing", p.multiWan)
	}
}
