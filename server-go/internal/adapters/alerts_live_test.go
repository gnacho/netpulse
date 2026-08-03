// alerts_live_test.go — taxonomía SPEC-ALERTAS §1 en la demo y en los
// eventos live nuevos (router recuperado, WAN caído, desconocido se conecta)
// y migración de los 4 sitios históricos al motor.
package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

// Las 5 alertas canon pasan por el motor con config default y sobreviven
// (SPEC-ALERTAS §5), con Category/Urgent/Ts correctos.
func TestDemoCanonAlertsThroughEngine(t *testing.T) {
	d := NewDemo()
	list := d.GetAlerts(t.Context())
	if len(list) != 5 {
		t.Fatalf("canon: %d alertas, esperaba 5", len(list))
	}
	want := map[string]struct {
		cat    string
		urgent bool
		sev    string
		read   bool
	}{
		"alert-temp-patio":       {alerts.CatRouter, true, "warn", false},
		"alert-firmware-estudio": {alerts.CatSystem, false, "warn", false},
		"alert-nuevo-tab":        {alerts.CatClients, false, "info", true},
		"alert-handshake-wg":     {alerts.CatVPN, false, "info", true},
		"alert-backup-adguard":   {alerts.CatSystem, false, "ok", true},
	}
	for _, a := range list {
		w, ok := want[a.ID]
		if !ok {
			t.Fatalf("alerta inesperada: %s", a.ID)
		}
		if a.Category != w.cat || a.Urgent != w.urgent || a.Severity != w.sev {
			t.Fatalf("%s taxonomía: (%s,%v,%s), esperaba (%s,%v,%s)",
				a.ID, a.Category, a.Urgent, a.Severity, w.cat, w.urgent, w.sev)
		}
		if a.Read != w.read {
			t.Fatalf("%s read: %v, esperaba %v", a.ID, a.Read, w.read)
		}
		if a.Ts <= 0 {
			t.Fatalf("%s sin Ts", a.ID)
		}
		if a.Time == "" {
			t.Fatalf("%s sin Time legado", a.ID)
		}
	}
	// El orden canónico se conserva (seed en inverso + prepend).
	order := []string{"alert-temp-patio", "alert-firmware-estudio", "alert-nuevo-tab", "alert-handshake-wg", "alert-backup-adguard"}
	for i, id := range order {
		if list[i].ID != id {
			t.Fatalf("orden canon: posición %d = %s, esperaba %s", i, list[i].ID, id)
		}
	}
	// UnreadAlerts del overview = no leídas del motor (2 en el canon).
	ov, err := d.GetOverview(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if ov.UnreadAlerts != 2 {
		t.Fatalf("unreadAlerts: %d, esperaba 2", ov.UnreadAlerts)
	}
}

// liveTestLive: Live mínimo con motor en memoria para probar las emisiones.
func liveTestLive() *Live {
	return &Live{
		engine:     alerts.New(nil, nil),
		lastStatus: map[string]string{},
		wanDown:    map[string]int{},
		onlineMacs: map[string]bool{},
	}
}

func TestLiveRouterRecoveredTaxonomy(t *testing.T) {
	l := liveTestLive()
	// Con el default router:urgent el evento (urgent=false) se filtra EN
	// CREACIÓN (semántica SPEC §2): es lo esperado.
	l.mu.Lock()
	l.emitRouterRecovered("patio", "Patio")
	l.mu.Unlock()
	if len(l.engine.List()) != 0 {
		t.Fatal("con router:urgent, 'recuperado' (no urgente) debía filtrarse")
	}
	// Con router:all pasa, con la taxonomía exacta del SPEC §1.
	if err := l.engine.SetConfig(map[string]string{"router": "all"}); err != nil {
		t.Fatal(err)
	}
	l.mu.Lock()
	l.emitRouterRecovered("patio", "Patio")
	l.mu.Unlock()
	list := l.engine.List()
	if len(list) != 1 {
		t.Fatalf("alertas: %d", len(list))
	}
	a := list[0]
	if a.Category != alerts.CatRouter || a.Urgent || a.Severity != "ok" {
		t.Fatalf("recuperado: (%s,%v,%s), esperaba (router,false,ok)", a.Category, a.Urgent, a.Severity)
	}
	if a.RouterID != "patio" || a.Ts == 0 {
		t.Fatalf("recuperado: %+v", a)
	}
}

func TestLiveWanDownTaxonomyAndDebounce(t *testing.T) {
	l := liveTestLive()
	cfg := &RouterConfig{ID: "flint2", Name: "Flint 2", Host: "192.168.8.1"}
	loss0, loss100 := 0.0, 100.0
	// 1er sondeo con 100 % pérdida: aún no alerta (debounce 2 como offline)
	l.mu.Lock()
	l.trackWanDown(cfg, &routerPolled{lossPct: &loss100})
	l.mu.Unlock()
	if len(l.engine.List()) != 0 {
		t.Fatal("WAN down no debía alertar al primer sondeo")
	}
	// 2º seguido: alerta internet/urgent/critical
	l.mu.Lock()
	l.trackWanDown(cfg, &routerPolled{lossPct: &loss100})
	l.mu.Unlock()
	list := l.engine.List()
	if len(list) != 1 {
		t.Fatalf("WAN down: %d alertas", len(list))
	}
	a := list[0]
	if a.Category != alerts.CatInternet || !a.Urgent || a.Severity != "critical" {
		t.Fatalf("WAN down: (%s,%v,%s), esperaba (internet,true,critical)", a.Category, a.Urgent, a.Severity)
	}
	// 3er sondeo: no re-alerta (estado ya "down"); el dedup también lo evita
	l.mu.Lock()
	l.trackWanDown(cfg, &routerPolled{lossPct: &loss100})
	l.mu.Unlock()
	if len(l.engine.List()) != 1 {
		t.Fatal("WAN down re-alertó sin recuperación")
	}
	// Recuperación: pérdida < 100 resetea (próxima caída vuelve a alertar)
	l.mu.Lock()
	l.trackWanDown(cfg, &routerPolled{lossPct: &loss0})
	l.mu.Unlock()
	if l.lastStatus["flint2:wan"] != "up" || l.wanDown["flint2"] != 0 {
		t.Fatalf("reset WAN: %v", l.lastStatus)
	}
}

func TestLiveUnknownDeviceTaxonomy(t *testing.T) {
	l := liveTestLive()
	l.mu.Lock()
	l.emitUnknownDevice(Device{MAC: "AA:BB:CC:DD:EE:FF", Name: "AA:BB:CC:DD:EE:FF", RouterID: "living", Online: true})
	l.mu.Unlock()
	list := l.engine.List()
	if len(list) != 1 {
		t.Fatalf("desconocido: %d alertas", len(list))
	}
	a := list[0]
	if a.Category != alerts.CatClients || !a.Urgent || a.Severity != "warn" {
		t.Fatalf("desconocido: (%s,%v,%s), esperaba (clients,true,warn)", a.Category, a.Urgent, a.Severity)
	}
}

func TestLiveTrackUnknownDevices(t *testing.T) {
	l := liveTestLive()
	unknown := Device{MAC: "11:22:33:44:55:66", Name: "11:22:33:44:55:66", RouterID: "living", Online: true}
	named := Device{MAC: "77:88:99:AA:BB:CC", Name: "Galaxy Tab", RouterID: "living", Online: true}

	// Primer ciclo: siembra de base, NUNCA alerta (anti-avalancha de arranque)
	l.trackUnknownDevices([]Device{unknown, named})
	if len(l.engine.List()) != 0 {
		t.Fatal("primer ciclo no debía alertar")
	}
	// Sigue online: no re-alerta
	l.trackUnknownDevices([]Device{unknown, named})
	if len(l.engine.List()) != 0 {
		t.Fatal("reconexión sin caída no debía alertar")
	}
	// Se desconecta y vuelve: AHORA sí alerta (sin nombre → desconocido)
	l.trackUnknownDevices([]Device{named})
	l.trackUnknownDevices([]Device{unknown, named})
	list := l.engine.List()
	if len(list) != 1 {
		t.Fatalf("reconexión de desconocido: %d alertas", len(list))
	}
	if list[0].Category != alerts.CatClients || !list[0].Urgent {
		t.Fatalf("taxonomía: %+v", list[0])
	}
	// El dispositivo CON nombre nunca alerta aunque se reconecte
	l.trackUnknownDevices([]Device{unknown})
	l.trackUnknownDevices([]Device{unknown, named})
	if len(l.engine.List()) != 1 {
		t.Fatal("dispositivo conocido no debía alertar")
	}
}
