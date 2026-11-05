package adapters

import (
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
)

// #1871: las integraciones que empujan no mandan el campo kind, así que su
// polled quedaba con agentKind vacío y la lente de 24 h se apagaba aunque el
// histórico de puertos estuviera lleno. El registro ya sabe que la unidad no
// corre el agente nativo: se sella ahí.
func TestPusherRowSealsExternalKind(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)

	p := &probe.Payload{
		Router: "crs305", Ts: time.Now().Unix(),
		Data: probe.PayloadData{FDB: &probe.FDBData{Ports: []probe.EthPort{{
			ID: "sfp-sfpplus1", Up: true, RxBps: fptr(1e6), TxBps: fptr(2e6),
		}}}},
	}
	out := l.polledFromAgent(RouterConfig{ID: "crs305", Type: "external"}, p)
	if out.agentKind != "external" {
		t.Fatalf("agentKind = %q, esperaba external para una fila external sin kind", out.agentKind)
	}

	// La consecuencia que reportó el issue: el flag que habilita el respaldo
	// desde port_series y las vitals que se publican como desconocidas.
	r := l.buildRouter(out, nil)
	if !r.sparklineFromPorts {
		t.Fatalf("sparklineFromPorts = false, la línea de 24 h se quedaría vacía")
	}
	if r.VitalsAvailable == nil || *r.VitalsAvailable {
		t.Fatalf("VitalsAvailable = %v, esperaba false (no hay system en el payload)", r.VitalsAvailable)
	}
}

// Un agente nativo en OpenWrt o GL.iNet no cambia de kind: sigue con vitals
// propias y con la comprobación de versión de su binario.
func TestNativeRowsKeepEmptyKind(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	cpu, temp := 12, 48
	for _, typ := range []string{"", "openwrt", "glinet"} {
		p := &probe.Payload{
			Router: "rt1", Ts: time.Now().Unix(),
			Data: probe.PayloadData{System: &probe.SystemData{CPU: &cpu, Temp: &temp}},
		}
		out := l.polledFromAgent(RouterConfig{ID: "rt1", Type: typ}, p)
		if out.agentKind != "" {
			t.Fatalf("tipo %q: agentKind = %q, esperaba vacío", typ, out.agentKind)
		}
		r := l.buildRouter(out, nil)
		if r.sparklineFromPorts {
			t.Fatalf("tipo %q: sparklineFromPorts = true, el agente nativo tiene serie propia", typ)
		}
		if r.CPU == nil || *r.CPU != cpu || r.Temp == nil || *r.Temp != temp {
			t.Fatalf("tipo %q: vitals cpu=%v temp=%v, esperaba %d y %d", typ, r.CPU, r.Temp, cpu, temp)
		}
	}
}

// El sello no pisa un kind declarado por el payload: un pusher que se anuncia
// como netgrip conserva su etiqueta y su referencia de versión.
func TestExplicitKindIsNotOverwritten(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	p := &probe.Payload{Router: "ng1", Ts: time.Now().Unix(), Kind: "netgrip"}
	out := l.polledFromAgent(RouterConfig{ID: "ng1", Type: "external"}, p)
	if out.agentKind != "netgrip" {
		t.Fatalf("agentKind = %q, esperaba netgrip", out.agentKind)
	}
}
