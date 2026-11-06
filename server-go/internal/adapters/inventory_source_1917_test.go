package adapters

import (
	"testing"
	"time"
)

// #1917: el inventario de una unidad puede venir de cuatro vías distintas y
// hasta ahora el servidor no lo decía. La tarjeta solo llevaba los datos, así
// que un operador no podía distinguir un push del agente de hace un minuto de
// una lectura por SSH de hace horas.
func TestInventorySourceNamesThePath(t *testing.T) {
	cases := []struct {
		name   string
		kind   string
		snmp   bool
		expect string
	}{
		{"native agent", "native", false, "agent"},
		{"netgrip agent", "netgrip", false, "agent"},
		{"ssh fallback", "", false, "ssh"},
		{"snmp collector", "", true, "snmp"},
		{"external pusher", "external", false, "external"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := &routerPolled{agentKind: c.kind, cfg: RouterConfig{SnmpEnabled: c.snmp}}
			if got := p.inventorySource(); got != c.expect {
				t.Fatalf("inventorySource() = %q, esperaba %q", got, c.expect)
			}
		})
	}
}

// La tarjeta tiene que llevar la fuente y la edad juntas: la fuente sola no
// sirve si el dato es de ayer.
func TestBuildRouterPublishesInventoryOrigin(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	polled := time.Now().Add(-7 * time.Minute).UnixMilli()
	p := &routerPolled{
		cfg:       RouterConfig{ID: "rt2", Name: "rt2", Host: "192.168.1.2", InventoryMode: "ssh"},
		agentKind: "native",
		polledAt:  polled,
	}

	r := l.buildRouter(p, nil)

	if r.InventorySource != "agent" {
		t.Fatalf("InventorySource = %q, esperaba agent", r.InventorySource)
	}
	if r.InventoryAt != polled {
		t.Fatalf("InventoryAt = %d, esperaba %d", r.InventoryAt, polled)
	}
	// El modo configurado viaja con la tarjeta: la UI lo necesita para no
	// ofrecer acciones de agente en una unidad que se inventaria por SSH.
	if r.InventoryMode != "ssh" {
		t.Fatalf("InventoryMode = %q, esperaba ssh", r.InventoryMode)
	}
}
