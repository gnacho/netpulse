package adapters

import (
	"strings"
	"testing"
)

// TestFirmwareOutdated cubre la comparación pura (issue #241): Contains
// normalizado en minúsculas sobre la descripción del firmware.
func TestFirmwareOutdated(t *testing.T) {
	cases := []struct {
		name      string
		installed string
		target    string
		want      bool
	}{
		{"sin target", "OpenWrt 23.05.5", "", false},
		{"target solo espacios", "OpenWrt 23.05.5", "   ", false},
		{"coincide exacto", "23.05.5", "23.05.5", false},
		{"coincide dentro de descripción", "OpenWrt 25.12.5 r33051-abc", "25.12.5", false},
		{"coincide sin distinguir mayúsculas", "OpenWrt 25.12.5", "OPENWRT 25.12.5", false},
		{"no coincide", "OpenWrt 23.05.5", "24.10.1", true},
		{"instalado vacío", "", "24.10.1", true},
		{"target con espacios alrededor", "OpenWrt 23.05.5", " 23.05.5 ", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := firmwareOutdated(c.installed, c.target); got != c.want {
				t.Fatalf("firmwareOutdated(%q, %q) = %v, esperaba %v", c.installed, c.target, got, c.want)
			}
		})
	}
}

// boardWithDesc construye un BoardInfo con la descripción de firmware dada.
func boardWithDesc(desc string) *BoardInfo {
	b := &BoardInfo{}
	b.Release.Description = desc
	return b
}

// TestBuildRouterFirmwareTarget cubre la integración en buildRouter: flag
// FirmwareOutdated, FirmwareTarget, penalización de salud y alerta CatSystem.
func TestBuildRouterFirmwareTarget(t *testing.T) {
	// Target no coincide → flag, health -5 y alerta no urgente.
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "r1", Host: "192.168.8.1", IsGateway: true},
	}, nil)
	p := &routerPolled{
		cfg:   RouterConfig{ID: "r1", Name: "Salón", Host: "192.168.8.2", FirmwareTarget: "25.12.5"},
		board: boardWithDesc("OpenWrt 23.05.5 r20134-abc"),
	}
	r := l.buildRouter(p, nil)
	if !r.FirmwareOutdated {
		t.Fatalf("FirmwareOutdated debería ser true: %+v", r)
	}
	if r.FirmwareTarget != "25.12.5" {
		t.Fatalf("FirmwareTarget=%q, esperaba 25.12.5", r.FirmwareTarget)
	}
	if r.Firmware != "OpenWrt 23.05.5 r20134-abc" {
		t.Fatalf("Firmware=%q", r.Firmware)
	}
	if r.Health != 95 {
		t.Fatalf("Health=%d, esperaba 95 (100-5)", r.Health)
	}
	list := l.AlertsEngine().List()
	if len(list) != 1 {
		t.Fatalf("alertas=%d, esperaba 1", len(list))
	}
	ev := list[0]
	if ev.Category != "system" || ev.Urgent || ev.Severity != "warn" || ev.RouterID != "r1" {
		t.Fatalf("alerta mal formada: %+v", ev)
	}
	if ev.Title != "Firmware desactualizado" {
		t.Fatalf("Title=%q", ev.Title)
	}
	if !strings.Contains(ev.Description, "25.12.5") || !strings.Contains(ev.Description, "23.05.5") {
		t.Fatalf("Description=%q", ev.Description)
	}

	// Target coincide → sin flag, health intacto, sin alerta nueva.
	l2 := NewLive(nil, nil, []RouterConfig{{ID: "g", Host: "192.168.8.1", IsGateway: true}}, nil)
	p2 := &routerPolled{
		cfg:   RouterConfig{ID: "r2", Name: "Estudio", Host: "192.168.8.3", FirmwareTarget: "23.05.5"},
		board: boardWithDesc("OpenWrt 23.05.5 r20134-abc"),
	}
	r2 := l2.buildRouter(p2, nil)
	if r2.FirmwareOutdated {
		t.Fatalf("FirmwareOutdated no debería marcarse: %+v", r2)
	}
	if r2.FirmwareTarget != "23.05.5" {
		t.Fatalf("FirmwareTarget=%q", r2.FirmwareTarget)
	}
	if r2.Health != 100 {
		t.Fatalf("Health=%d, esperaba 100", r2.Health)
	}
	if n := len(l2.AlertsEngine().List()); n != 0 {
		t.Fatalf("alertas=%d, esperaba 0", n)
	}

	// Sin target → sin flag y sin alerta.
	l3 := NewLive(nil, nil, []RouterConfig{{ID: "g", Host: "192.168.8.1", IsGateway: true}}, nil)
	p3 := &routerPolled{
		cfg:   RouterConfig{ID: "r3", Name: "Patio", Host: "192.168.8.4"},
		board: boardWithDesc("OpenWrt 23.05.5"),
	}
	r3 := l3.buildRouter(p3, nil)
	if r3.FirmwareOutdated || r3.FirmwareTarget != "" {
		t.Fatalf("sin target no debería marcar nada: %+v", r3)
	}
	if n := len(l3.AlertsEngine().List()); n != 0 {
		t.Fatalf("alertas=%d, esperaba 0", n)
	}
}

// TestBuildRouterFirmwareSinBoard: target sin board (sin firmware leído) no
// debe marcar outdated ni emitir (evita falsos positivos en sondeo parcial).
func TestBuildRouterFirmwareSinBoard(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{{ID: "g", Host: "192.168.8.1", IsGateway: true}}, nil)
	p := &routerPolled{
		cfg: RouterConfig{ID: "r4", Name: "Salón", Host: "192.168.8.2", FirmwareTarget: "25.12.5"},
	}
	r := l.buildRouter(p, nil)
	if r.FirmwareOutdated {
		t.Fatalf("sin board no debería marcar outdated: %+v", r)
	}
	if r.FirmwareTarget != "25.12.5" {
		t.Fatalf("FirmwareTarget debería reflejar el config aun sin board: %+v", r)
	}
	if n := len(l.AlertsEngine().List()); n != 0 {
		t.Fatalf("alertas=%d, esperaba 0", n)
	}
}
