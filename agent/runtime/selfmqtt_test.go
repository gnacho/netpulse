package runtime

import (
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
)

func TestWithMetaSelfMQTT(t *testing.T) {
	// Sin hook: no se toca nada.
	p := &probe.Payload{}
	withMeta(p, Options{})
	if p.Kind != "" || p.Data.MQTT != nil {
		t.Fatalf("unexpected meta: %+v", p)
	}

	// Hook activo: se informa enabled y node.
	p = &probe.Payload{}
	withMeta(p, Options{
		Kind:     "netgrip",
		SelfMQTT: func() (bool, string) { return true, "rt3" },
	})
	if p.Kind != "netgrip" {
		t.Fatalf("kind = %q, want netgrip", p.Kind)
	}
	if p.Data.MQTT == nil || !p.Data.MQTT.Enabled || p.Data.MQTT.Node != "rt3" {
		t.Fatalf("mqtt = %+v, want {true rt3}", p.Data.MQTT)
	}

	// Hook apagado: se informa Enabled=false para que el servidor vea el cambio.
	p = &probe.Payload{}
	withMeta(p, Options{SelfMQTT: func() (bool, string) { return false, "rt3" }})
	if p.Data.MQTT == nil || p.Data.MQTT.Enabled {
		t.Fatalf("mqtt = %+v, want {false}", p.Data.MQTT)
	}
}
