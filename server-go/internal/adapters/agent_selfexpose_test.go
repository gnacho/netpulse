package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
)

func TestAgentRegistrySelfExpose(t *testing.T) {
	r := NewAgentRegistry(0)
	if _, _, ok := r.SelfExpose("rt3"); ok {
		t.Fatal("unknown slug must report ok=false")
	}

	r.Ingest(&probe.Payload{Router: "rt3", Data: probe.PayloadData{
		MQTT: &probe.MQTTData{Enabled: true, Node: "rt3"},
	}})
	if en, node, ok := r.SelfExpose("rt3"); !ok || !en || node != "rt3" {
		t.Fatalf("got (%v, %q, %v), want (true, rt3, true)", en, node, ok)
	}

	// Informado pero apagado: ok con enabled=false.
	r.Ingest(&probe.Payload{Router: "rt3", Data: probe.PayloadData{
		MQTT: &probe.MQTTData{Enabled: false},
	}})
	if en, _, ok := r.SelfExpose("rt3"); !ok || en {
		t.Fatalf("got (%v, %v), want (false, true)", en, ok)
	}

	// Sin sección mqtt (agente standalone): ok=false.
	r.Ingest(&probe.Payload{Router: "rt4"})
	if _, _, ok := r.SelfExpose("rt4"); ok {
		t.Fatal("payload without mqtt section must report ok=false")
	}
}
