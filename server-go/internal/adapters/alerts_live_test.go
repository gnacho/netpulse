// alerts_live_test.go — taxonomía SPEC-ALERTAS §1 en la demo y en los
// eventos live nuevos (router recuperado, WAN caído, desconocido se conecta)
// y migración de los 4 sitios históricos al motor.
package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
	"github.com/gnacho/netpulse/server-go/internal/db"
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
		engine:          alerts.New(nil, nil),
		lastStatus:      map[string]string{},
		wanDown:         map[string]int{},
		onlineMacs:      map[string]bool{},
		unknownGrace:    map[string]int{},
		unknownAlerted:  map[string]bool{},
		unknownGraceNum: 3,
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

// Issue #256: la alerta crítica de offline de un router se RESUELVE (marca
// leída) cuando el router vuelve a responder; con APs que flapean no quedan
// caídas fantasma "no leídas para siempre".
func TestLiveRouterOfflineAlertResolvedOnRecovery(t *testing.T) {
	l := liveTestLive()
	offlineID := "alert-offline-patio-1"
	l.engine.Emit(AlertEvent{
		ID: offlineID, Category: alerts.CatRouter, Urgent: true,
		Severity: "critical", Title: "Patio offline", RouterID: "patio", Time: "ahora mismo",
	})
	if len(l.engine.List()) != 1 {
		t.Fatalf("alerta de caída: %d", len(l.engine.List()))
	}
	// El router vuelve online → las caídas pendientes se resuelven.
	l.mu.Lock()
	l.resolveOfflineAlerts("patio")
	l.mu.Unlock()
	list := l.engine.List()
	if len(list) != 1 || !list[0].Read {
		t.Fatalf("la caída debería quedar leída tras la recuperación: %+v", list)
	}
	// Las caídas de OTROS routers no se tocan.
	l.engine.Emit(AlertEvent{
		ID: "alert-offline-living-1", Category: alerts.CatRouter, Urgent: true,
		Severity: "critical", Title: "Living offline", RouterID: "living", Time: "ahora mismo",
	})
	l.mu.Lock()
	l.resolveOfflineAlerts("patio")
	l.mu.Unlock()
	for _, ev := range l.engine.List() {
		if ev.ID == "alert-offline-living-1" && ev.Read {
			t.Fatal("la caída de living no debía resolverse")
		}
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
	// issue #196: la alerta de desconocido NO es urgente (warn informativa).
	if a.Category != alerts.CatClients || a.Urgent || a.Severity != "warn" {
		t.Fatalf("desconocido: (%s,%v,%s), esperaba (clients,false,warn)", a.Category, a.Urgent, a.Severity)
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
	// Se desconecta y vuelve: la gracia de N ticks (issue #234) evita alertar
	// mientras el lease aún no se resuelve tras reconectar.
	l.trackUnknownDevices([]Device{named})
	l.trackUnknownDevices([]Device{unknown, named})
	if len(l.engine.List()) != 0 {
		t.Fatal("1 tick nameless no debía alertar (gracia)")
	}
	l.trackUnknownDevices([]Device{unknown, named})
	if len(l.engine.List()) != 0 {
		t.Fatal("2 ticks nameless no debían alertar (gracia)")
	}
	// Al tercer tick consecutivo online+sin nombre, el desconocido real alerta
	l.trackUnknownDevices([]Device{unknown, named})
	list := l.engine.List()
	if len(list) != 1 {
		t.Fatalf("desconocido tras gracia: %d alertas", len(list))
	}
	if list[0].Category != alerts.CatClients || list[0].Urgent {
		t.Fatalf("taxonomía: %+v", list[0])
	}
	// Issue #248: la reconexión posterior de la MISMA MAC ya no alerta
	l.trackUnknownDevices([]Device{named})
	l.trackUnknownDevices([]Device{unknown, named})
	l.trackUnknownDevices([]Device{unknown, named})
	l.trackUnknownDevices([]Device{unknown, named})
	if len(l.engine.List()) != 1 {
		t.Fatal("memoria per-MAC (#248): no debía re-alertar")
	}
	// El dispositivo CON nombre nunca alerta aunque se reconecte
	l.trackUnknownDevices([]Device{unknown})
	l.trackUnknownDevices([]Device{unknown, named})
	if len(l.engine.List()) != 1 {
		t.Fatal("dispositivo conocido no debía alertar")
	}
}

// Issue #234: un dispositivo conocido cuyo lease DHCP tarda un par de ticks
// en resolverse tras reconectar NO dispara la alerta de desconocido.
func TestLiveTrackUnknownDevicesGraciaConocido(t *testing.T) {
	l := liveTestLive()
	d := Device{MAC: "77:88:99:AA:BB:CC", Name: "77:88:99:AA:BB:CC", RouterID: "living", Online: true}
	// Siembra; desconexión; reconexión SIN lease resuelto durante 2 ticks…
	l.trackUnknownDevices([]Device{d})
	l.trackUnknownDevices([]Device{})
	l.trackUnknownDevices([]Device{d}) // tick 1 sin nombre
	l.trackUnknownDevices([]Device{d}) // tick 2 sin nombre
	if len(l.engine.List()) != 0 {
		t.Fatal("no debía alertar antes de resolver el lease")
	}
	// …y al tercer tick el lease ya está (Name != MAC) → nunca llega al umbral
	d.Name = "Galaxy Tab"
	l.trackUnknownDevices([]Device{d})
	if len(l.engine.List()) != 0 {
		t.Fatal("con hostname resuelto no debía alertar")
	}
	// La gracia se reseteó al ver el nombre: una reconexión futura sin nombre
	// parte de cero y vuelve a necesitar los N ticks completos (no 1).
	l.trackUnknownDevices([]Device{})
	l.trackUnknownDevices([]Device{{Name: d.MAC, MAC: d.MAC, RouterID: "living", Online: true}})
	if len(l.engine.List()) != 0 {
		t.Fatal("tras reset, tick 1 no debía alertar")
	}
	l.trackUnknownDevices([]Device{{Name: d.MAC, MAC: d.MAC, RouterID: "living", Online: true}})
	if len(l.engine.List()) != 0 {
		t.Fatal("tras reset, tick 2 no debía alertar")
	}
	l.trackUnknownDevices([]Device{{Name: d.MAC, MAC: d.MAC, RouterID: "living", Online: true}})
	if len(l.engine.List()) != 1 {
		t.Fatalf("tras reset y gracia completa: %d alertas", len(l.engine.List()))
	}
}

// Issue #248: la memoria per-MAC es PERSISTENTE (kv) y sobrevive a un reinicio
// del servidor: una MAC que ya alertó no vuelve a alertar en un proceso nuevo.
func TestLiveUnknownDeviceMemoryPersisted(t *testing.T) {
	d := openLiveTestDB(t)
	l := NewLive(nil, d, nil, nil)
	mac := "AA:BB:CC:DD:EE:0F"
	unknown := Device{MAC: mac, Name: mac, RouterID: "living", Online: true}
	// Reconexión simulada: siembra, caída, vuelve → tras la gracia alerta.
	l.trackUnknownDevices([]Device{unknown})
	l.trackUnknownDevices([]Device{})
	l.trackUnknownDevices([]Device{unknown})
	l.trackUnknownDevices([]Device{unknown})
	l.trackUnknownDevices([]Device{unknown})
	if len(l.engine.List()) != 1 {
		t.Fatalf("primera alerta: %d", len(l.engine.List()))
	}
	// "Reinicio": un Live nuevo sobre la MISMA BD carga la memoria persistida.
	l2 := NewLive(nil, d, nil, nil)
	if !l2.unknownAlerted[mac] {
		t.Fatal("la memoria per-MAC no se cargó desde kv")
	}
	l2.trackUnknownDevices([]Device{unknown})
	l2.trackUnknownDevices([]Device{})
	l2.trackUnknownDevices([]Device{unknown})
	l2.trackUnknownDevices([]Device{unknown})
	l2.trackUnknownDevices([]Device{unknown})
	if len(l2.engine.List()) != 0 {
		t.Fatal("tras reinicio no debía re-alertar (memoria persistida)")
	}
}

func TestLiveTrackUnknownDevicesTrustedAllowlist(t *testing.T) {
	l := liveTestLive()
	l.db = openLiveTestDB(t) // allowlist en BD real
	trusted := Device{MAC: "A4:7E:FA:65:0C:AA", Name: "A4:7E:FA:65:0C:AA", RouterID: "living", Online: true}
	if err := l.db.UpsertKnownMac(db.KnownMac{MAC: "A4:7E:FA:65:0C:AA", Name: "Withings"}); err != nil {
		t.Fatal(err)
	}
	// Primer ciclo siembra; desconexión; reconexión: la MAC de la allowlist
	// NUNCA alerta, aunque siga sin nombre/lease (issue #196).
	l.trackUnknownDevices([]Device{trusted})
	l.trackUnknownDevices([]Device{})
	l.trackUnknownDevices([]Device{trusted})
	if len(l.engine.List()) != 0 {
		t.Fatalf("MAC confiable alertó: %d", len(l.engine.List()))
	}
}
