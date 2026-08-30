// agent_version_test.go — issue #400: alerta consolidada de agente
// desactualizado (native contra el binario embebido, netgrip contra su
// release pública) + recuperación ok única.
package adapters

import (
	"strings"
	"testing"
	"time"
)

func TestLiveAgentOutdatedAlert(t *testing.T) {
	reg := NewAgentRegistry(90 * time.Second)
	l := newLiveAgentTest(t, reg)
	l.SetAgentLatestSource(func(kind string) string {
		if kind == "netgrip" {
			return "0.27.0"
		}
		if kind == "external" {
			return ""
		}
		return "2.20.0"
	})
	cfg := RouterConfig{ID: "patio", Name: "Patio", Host: "127.0.0.1", Type: "openwrt"}

	// Agente nativo 0.1.0 contra referencia 2.20.0 → warn consolidada.
	p := testPayload() // Version 0.1.0, kind native
	l.checkAgentVersion(cfg, p)
	a := findAlert(l.engine.List(), "Agente desactualizado")
	if a == nil || a.Severity != "warn" || a.RouterID != "patio" || a.Hint == "" {
		t.Fatalf("alerta warn esperada con hint: %+v", a)
	}

	// Re-check con el mismo estado → sigue UNA sola entrada (consolidada).
	l.checkAgentVersion(cfg, p)
	if n := countAlerts(l.engine.List(), "Agente desactualizado"); n != 1 {
		t.Fatalf("consolidación: %d alertas, esperaba 1", n)
	}

	// El agente actualiza → recuperación ok única y sin warn en curso.
	p2 := testPayload()
	p2.Version = "2.20.0"
	l.checkAgentVersion(cfg, p2)
	if n := countAlerts(l.engine.List(), "Agente actualizado"); n != 1 {
		t.Fatalf("recuperación: %d alertas ok, esperaba 1", n)
	}

	// Seguimos al día → sin nuevas recuperaciones.
	l.checkAgentVersion(cfg, p2)
	if n := countAlerts(l.engine.List(), "Agente actualizado"); n != 1 {
		t.Fatalf("recuperación repetida: %d, esperaba 1", n)
	}

	// NetGrip embebido desactualizado → warn con su referencia propia.
	p3 := testPayload()
	p3.Kind = "netgrip"
	p3.Version = "0.26.1"
	l.checkAgentVersion(cfg, p3)
	a = findAlert(l.engine.List(), "Agente desactualizado")
	if a == nil || !strings.Contains(a.Description, "NetGrip") || !strings.Contains(a.Description, "0.27.0") {
		t.Fatalf("alerta netgrip esperada contra 0.27.0: %+v", a)
	}

	// External nunca alerta.
	p4 := testPayload()
	p4.Kind = "external"
	l.checkAgentVersion(cfg, p4)
	if n := countAlerts(l.engine.List(), "Agente desactualizado"); n != 1 {
		t.Fatalf("external no debe alertar: %d", n)
	}
}

func TestLiveAgentOutdatedNoSource(t *testing.T) {
	reg := NewAgentRegistry(90 * time.Second)
	l := newLiveAgentTest(t, reg)
	cfg := RouterConfig{ID: "patio", Name: "Patio", Host: "127.0.0.1", Type: "openwrt"}

	// Sin fuente (check desactivado) y con referencia vacía: sin alertas.
	l.checkAgentVersion(cfg, testPayload())
	if n := countAlerts(l.engine.List(), "Agente desactualizado"); n != 0 {
		t.Fatalf("sin fuente no debe alertar: %d", n)
	}
	l.SetAgentLatestSource(func(kind string) string { return "" })
	l.checkAgentVersion(cfg, testPayload())
	if n := countAlerts(l.engine.List(), "Agente desactualizado"); n != 0 {
		t.Fatalf("referencia vacía no debe alertar: %d", n)
	}
}
