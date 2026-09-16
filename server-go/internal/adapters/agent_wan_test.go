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
