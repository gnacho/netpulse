// agent_cadence_test.go — #288: frescura por cadencia declarada.
// Un pusher externo que declara Interval=300 (scraper cada 5 min) debe
// seguir fresco más allá del TTL base de 90 s y su confirmación de caída
// escala a 3x su cadencia.
package adapters

import (
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
)

func TestRegistryExternalCadenceStaysFresh(t *testing.T) {
	reg := NewAgentRegistry(0)
	now := time.Now()
	reg.SetClock(func() time.Time { return now })

	reg.Ingest(&probe.Payload{
		Router: "switch16", Ts: now.Unix(), Version: "scraper-2.0",
		Kind: "external", Interval: 300,
	})

	// A 200 s del último push: fuera del TTL base (90 s) pero dentro de
	// 3x300 s → sigue fresco (#288).
	now = now.Add(200 * time.Second)
	if _, ok := reg.Fresh("switch16"); !ok {
		t.Fatal("pusher externo con interval=300 debería seguir fresco a los 200 s")
	}
	if reg.Expired("switch16") {
		t.Fatal("no debería estar expirado a los 200 s")
	}

	// A 3x interval + 1 s: expirado.
	now = now.Add(15*time.Minute + time.Second)
	if _, ok := reg.Fresh("switch16"); ok {
		t.Fatal("debería expirar pasados 3x interval")
	}
}

func TestRegistryNativeDefaultUnchanged(t *testing.T) {
	reg := NewAgentRegistry(0)
	now := time.Now()
	reg.SetClock(func() time.Time { return now })

	reg.Ingest(&probe.Payload{Router: "patio", Ts: now.Unix(), Version: "0.1.3"})

	now = now.Add(91 * time.Second)
	if _, ok := reg.Fresh("patio"); ok {
		t.Fatal("agente nativo sin interval declarado debe expirar al TTL base")
	}
}

func TestRegistryExternalDownConfirmScales(t *testing.T) {
	reg := NewAgentRegistry(0)
	now := time.Now()
	reg.SetClock(func() time.Time { return now })

	reg.Ingest(&probe.Payload{
		Router: "switch16", Ts: now.Unix(), Version: "scraper-2.0",
		Kind: "external", Interval: 300,
	})

	got := reg.ExternalDownConfirm("switch16", 3*time.Minute)
	if got != 15*time.Minute {
		t.Fatalf("confirm para externo de 5 min: got %v want 15m", got)
	}
	// Sin interval declarado (nativo): la base se respeta tal cual.
	reg.Ingest(&probe.Payload{Router: "patio", Ts: now.Unix(), Version: "0.1.3"})
	if got := reg.ExternalDownConfirm("patio", 3*time.Minute); got != 3*time.Minute {
		t.Fatalf("confirm para nativo: got %v want 3m", got)
	}
}

func TestRegistryInfoExposesKindAndInterval(t *testing.T) {
	reg := NewAgentRegistry(0)
	now := time.Now()
	reg.SetClock(func() time.Time { return now })

	reg.Ingest(&probe.Payload{
		Router: "switch16", Ts: now.Unix(), Version: "scraper-2.0",
		Kind: "external", Interval: 300,
	})
	_, _, kind, interval, ok := reg.Info("switch16")
	if !ok || kind != "external" || interval != 300 {
		t.Fatalf("Info: ok=%v kind=%q interval=%d", ok, kind, interval)
	}
}
