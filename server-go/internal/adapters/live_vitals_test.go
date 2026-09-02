package adapters

import (
	"context"
	"testing"
)

// #441: los routers cuya fuente no puede reportar métricas de sistema (SNMP,
// pushers externos por beacon/scraper) se marcan sin vitals y no sirven
// cpu/ram/temp (null en el contrato) para que la UI no pinte ceros falsos.
func TestBuildRouterVitalsUnavailable(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)

	// SNMP: marcado y sin vitals.
	p := &routerPolled{
		cfg:      RouterConfig{ID: "sw1", Host: "192.168.1.10", SnmpEnabled: true},
		polledAt: 1000,
	}
	r := l.buildRouter(p, nil)
	if r.VitalsAvailable == nil || *r.VitalsAvailable {
		t.Fatalf("VitalsAvailable=%v, esperaba false (SNMP)", r.VitalsAvailable)
	}
	if r.CPU != nil || r.RAM != nil || r.Temp != nil {
		t.Fatalf("vitals deberían ser nil: cpu=%v ram=%v temp=%v", r.CPU, r.RAM, r.Temp)
	}

	// Agente externo (beacon/scraper): mismo tratamiento.
	p2 := &routerPolled{
		cfg:       RouterConfig{ID: "sw2", Host: "192.168.1.11"},
		agentKind: "external",
	}
	r2 := l.buildRouter(p2, nil)
	if r2.VitalsAvailable == nil || *r2.VitalsAvailable {
		t.Fatalf("VitalsAvailable=%v, esperaba false (external)", r2.VitalsAvailable)
	}
	if r2.CPU != nil || r2.RAM != nil || r2.Temp != nil {
		t.Fatalf("vitals deberían ser nil: cpu=%v ram=%v temp=%v", r2.CPU, r2.RAM, r2.Temp)
	}

	// Sondeo normal (SSH/agente nativo): sin marca y con vitals.
	p3 := &routerPolled{
		cfg:      RouterConfig{ID: "rt1", Host: "192.168.1.2"},
		cpu:      12,
		ram:      34,
		temp:     45,
		polledAt: 1000,
	}
	r3 := l.buildRouter(p3, nil)
	if r3.VitalsAvailable != nil {
		t.Fatalf("VitalsAvailable=%v, esperaba nil (sondeo normal)", r3.VitalsAvailable)
	}
	if r3.CPU == nil || *r3.CPU != 12 || r3.RAM == nil || *r3.RAM != 34 || r3.Temp == nil || *r3.Temp != 45 {
		t.Fatalf("vitals normales mal servidas: cpu=%v ram=%v temp=%v", r3.CPU, r3.RAM, r3.Temp)
	}
}

// #441: el poller no persiste cpu/ram/temp (nil) para routers sin vitals.
func TestMetricsRowsWithoutVitals(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	l.lastPolled["sw1"] = &routerPolled{
		cfg:      RouterConfig{ID: "sw1", Host: "192.168.1.10", SnmpEnabled: true},
		polledAt: 1000,
	}
	l.lastPolled["sw2"] = &routerPolled{
		cfg:       RouterConfig{ID: "sw2", Host: "192.168.1.11"},
		agentKind: "external",
		polledAt:  1000,
	}
	l.lastPolled["rt1"] = &routerPolled{
		cfg:      RouterConfig{ID: "rt1", Host: "192.168.1.2"},
		cpu:      12,
		ram:      34,
		temp:     45,
		polledAt: 1000,
	}

	rows := l.GetMetricsRows(context.Background())
	if len(rows) != 3 {
		t.Fatalf("rows=%d, esperaba 3", len(rows))
	}
	byID := map[string]MetricsRow{}
	for _, row := range rows {
		byID[row.RouterID] = row
	}
	for _, id := range []string{"sw1", "sw2"} {
		row := byID[id]
		if row.CPU != nil || row.RAM != nil || row.Temp != nil {
			t.Fatalf("%s: vitals deberían ser nil: %+v", id, row)
		}
	}
	row := byID["rt1"]
	if row.CPU == nil || *row.CPU != 12 {
		t.Fatalf("rt1: CPU=%v, esperaba 12", row.CPU)
	}
}
