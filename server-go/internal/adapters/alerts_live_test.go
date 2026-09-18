// alerts_live_test.go — taxonomía SPEC-ALERTAS §1 en la demo y en los
// eventos live nuevos (router recuperado, WAN caído, desconocido se conecta)
// y migración de los 4 sitios históricos al motor.
package adapters

import (
	"fmt"
	"strings"
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

// Issue #846: la alerta crítica de offline de un router se ELIMINA cuando el
// router vuelve a responder de forma estable; con APs que flapean no quedan
// caídas fantasma en el feed (antes, #256, solo se marcaba leída).
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
	// El router vuelve online → las caídas pendientes se eliminan del feed.
	l.mu.Lock()
	l.resolveOfflineAlerts("patio")
	l.mu.Unlock()
	if list := l.engine.List(); len(list) != 0 {
		t.Fatalf("la caída debería eliminarse tras la recuperación: %+v", list)
	}
	// Las caídas de OTROS routers no se tocan.
	l.engine.Emit(AlertEvent{
		ID: "alert-offline-living-1", Category: alerts.CatRouter, Urgent: true,
		Severity: "critical", Title: "Living offline", RouterID: "living", Time: "ahora mismo",
	})
	l.mu.Lock()
	l.resolveOfflineAlerts("patio")
	l.mu.Unlock()
	if list := l.engine.List(); len(list) != 1 || list[0].ID != "alert-offline-living-1" {
		t.Fatalf("la caída de living no debía tocarse: %+v", list)
	}
}

// Issue #846: mientras el incidente de offline está abierto no se re-emite
// la alerta aunque el estado flapee (offline → online → offline), y la
// recuperación hay que estabilizarla (offlineRecoverTicks éxitos seguidos)
// antes de cerrar el incidente (borrar la caída + emitir "recuperado").
func TestLiveRouterOfflineFlappingDoesNotRearm(t *testing.T) {
	l := liveTestLive()
	if err := l.engine.SetConfig(map[string]string{"router": "all"}); err != nil {
		t.Fatal(err)
	}
	cfg := &RouterConfig{ID: "patio", Name: "Patio", Host: "10.0.0.1"}
	errDown := fmt.Errorf("timeout")

	l.mu.Lock()
	l.trackRouterOffline(cfg, errDown, 2) // caída: primera alerta
	l.trackRouterOffline(cfg, errDown, 3) // sigue caído: NO re-emite
	l.mu.Unlock()
	if list := l.engine.List(); len(list) != 1 || !strings.HasPrefix(list[0].ID, "alert-offline-patio-") {
		t.Fatalf("un incidente = una alerta: %+v", list)
	}

	// Flap: éxitos aislados no cierran el incidente ni rearman.
	l.mu.Lock()
	l.trackRouterRecovered(cfg) // streak 1
	l.trackRouterRecovered(cfg) // streak 2
	l.trackRouterOffline(cfg, errDown, 2) // vuelve a caer: sin alerta nueva
	l.mu.Unlock()
	if list := l.engine.List(); len(list) != 1 || !strings.HasPrefix(list[0].ID, "alert-offline-patio-") {
		t.Fatalf("el flap no debía rearmar ni cerrar: %+v", list)
	}

	// Recuperación estable: offlineRecoverTicks éxitos seguidos cierran el
	// incidente: la caída se elimina y el "recuperado" queda como relevo.
	l.mu.Lock()
	l.trackRouterRecovered(cfg) // streak 3 → cierra
	l.mu.Unlock()
	list := l.engine.List()
	if len(list) != 1 || list[0].Type != alerts.TypeRouterRecovered {
		t.Fatalf("tras recuperación estable: %+v", list)
	}
	for _, ev := range list {
		if strings.HasPrefix(ev.ID, "alert-offline-") {
			t.Fatalf("la caída debía eliminarse: %+v", ev)
		}
	}
}

// Issue #846: cerrar un incidente y reabrirlo DENTRO de la ventana de dedup
// (5 min, misma key cat|título|routerId) debe emitir la alerta nueva igual
// (EmitNoDedup): al cerrar, la alerta anterior se retiró del feed y el dedup
// dejaría el incidente nuevo invisible.
func TestLiveRouterOfflineReopenWithinDedupWindow(t *testing.T) {
	l := liveTestLive()
	if err := l.engine.SetConfig(map[string]string{"router": "all"}); err != nil {
		t.Fatal(err)
	}
	cfg := &RouterConfig{ID: "patio", Name: "Patio", Host: "10.0.0.1"}
	errDown := fmt.Errorf("timeout")

	l.mu.Lock()
	l.trackRouterOffline(cfg, errDown, 2) // incidente A
	l.trackRouterRecovered(cfg)           // streak 1
	l.trackRouterRecovered(cfg)           // streak 2
	l.trackRouterRecovered(cfg)           // streak 3 → cierra A
	l.mu.Unlock()
	if list := l.engine.List(); len(list) != 1 || list[0].Type != alerts.TypeRouterRecovered {
		t.Fatalf("tras el cierre solo queda el recuperado: %+v", list)
	}

	l.mu.Lock()
	l.trackRouterOffline(cfg, errDown, 2) // incidente B, misma dedup-key <5 min
	l.mu.Unlock()
	list := l.engine.List()
	offline := 0
	for _, ev := range list {
		if strings.HasPrefix(ev.ID, "alert-offline-") {
			offline++
		}
	}
	if offline != 1 {
		t.Fatalf("el incidente B debe tener exactamente 1 alerta offline, hay %d: %+v", offline, list)
	}
}

// Issue #846: tras un reinicio del servidor las alertas de offline de un
// proceso anterior quedan huérfanas (el incidente vivía en memoria). Los
// routers que responden SIN incidente abierto se reconcilian cada ciclo: la
// huérfana se retira SIN evento "recuperado" (no se observó recuperación:
// el servidor estaba caído, no el router). Los routers caídos conservan la
// suya; los de incidente abierto también.
func TestLiveRouterReconcileRemovesOrphan(t *testing.T) {
	l := liveTestLive()
	if err := l.engine.SetConfig(map[string]string{"router": "all"}); err != nil {
		t.Fatal(err)
	}
	// Huérfana "del proceso anterior" (misma shape que emite trackRouterOffline)
	// y una de OTRO router que no responde en este ciclo.
	l.engine.Emit(alerts.AlertEvent{
		ID: "alert-offline-patio-111", Category: alerts.CatRouter, Urgent: true,
		Severity: "critical", Title: "Patio offline", Type: alerts.HintDeviceOffline,
		RouterID: "patio",
	})
	l.engine.Emit(alerts.AlertEvent{
		ID: "alert-offline-living-222", Category: alerts.CatRouter, Urgent: true,
		Severity: "critical", Title: "Living offline", Type: alerts.HintDeviceOffline,
		RouterID: "living",
	})
	if n := len(l.engine.List()); n != 2 {
		t.Fatalf("previo: %d alertas", n)
	}
	l.mu.Lock()
	// Patio responde y SIN incidente abierto → su huérfana se retira.
	l.reconcileOfflineOrphans(map[string]*routerPolled{"patio": {}})
	l.mu.Unlock()
	list := l.engine.List()
	if len(list) != 1 || list[0].RouterID != "living" {
		t.Fatalf("solo la huérfana de living debe sobrevivir: %+v", list)
	}
	// Con incidente abierto NO se toca aunque responda: la alerta es la del
	// incidente vivo, no una huérfana.
	l.mu.Lock()
	if l.offlineOpen == nil {
		l.offlineOpen = map[string]bool{}
	}
	l.offlineOpen["living"] = true
	l.reconcileOfflineOrphans(map[string]*routerPolled{"living": {}})
	l.mu.Unlock()
	if list := l.engine.List(); len(list) != 1 || list[0].RouterID != "living" {
		t.Fatalf("con incidente abierto no se debe tocar: %+v", list)
	}
}

// Issue #846: al abrir un incidente con huérfanas previas (reinicio durante
// la caída), la alerta nueva las sustituye: nunca dos alertas offline del
// mismo router.
func TestLiveRouterOfflineOpenReplacesOrphans(t *testing.T) {
	l := liveTestLive()
	if err := l.engine.SetConfig(map[string]string{"router": "all"}); err != nil {
		t.Fatal(err)
	}
	l.engine.Emit(alerts.AlertEvent{
		ID: "alert-offline-patio-111", Category: alerts.CatRouter, Urgent: true,
		Severity: "critical", Title: "Patio offline", Type: alerts.HintDeviceOffline,
		RouterID: "patio",
	})
	// Segunda huérfana con la misma key de dedup: solo entra sin dedup (es el
	// shape real tras reinicios seguidos durante una caída larga).
	l.engine.EmitNoDedup(alerts.AlertEvent{
		ID: "alert-offline-patio-222", Category: alerts.CatRouter, Urgent: true,
		Severity: "critical", Title: "Patio offline", Type: alerts.HintDeviceOffline,
		RouterID: "patio",
	})
	cfg := &RouterConfig{ID: "patio", Name: "Patio", Host: "10.0.0.1"}
	l.mu.Lock()
	l.trackRouterOffline(cfg, fmt.Errorf("timeout"), 2)
	l.mu.Unlock()
	list := l.engine.List()
	if len(list) != 1 {
		t.Fatalf("una sola alerta tras abrir el incidente: %+v", list)
	}
	if list[0].ID == "alert-offline-patio-111" || list[0].ID == "alert-offline-patio-222" {
		t.Fatalf("la alerta debe ser la del incidente nuevo, no una huérfana: %+v", list[0])
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
	l.emitUnknownDevice(Device{MAC: "AA:BB:CC:DD:EE:FF", Name: "AA:BB:CC:DD:EE:FF", RouterID: "living", Online: true}, nil, nil)
	l.mu.Unlock()
	list := l.engine.List()
	if len(list) != 1 {
		t.Fatalf("desconocido: %d alertas", len(list))
	}
	a := list[0]
	// #772: la alerta de desconocido ES urgente para cruzar al Notifier
	// (push/ntfy/telegram); el nivel por categoría en Ajustes sigue
	// pudiendo silenciarla (clients=none). Antes de #772 era warn no urgente.
	if a.Category != alerts.CatClients || !a.Urgent || a.Severity != "warn" {
		t.Fatalf("desconocido: (%s,%v,%s), esperaba (clients,true,warn)", a.Category, a.Urgent, a.Severity)
	}
	if a.Vars["mac"] != "AA:BB:CC:DD:EE:FF" || a.Vars["router"] != "living" {
		t.Fatalf("vars incompletas: %+v", a.Vars)
	}
}

func TestLiveTrackUnknownDevices(t *testing.T) {
	l := liveTestLive()
	unknown := Device{MAC: "11:22:33:44:55:66", Name: "11:22:33:44:55:66", RouterID: "living", Online: true}
	named := Device{MAC: "77:88:99:AA:BB:CC", Name: "Galaxy Tab", RouterID: "living", Online: true}

	// Primer ciclo: siembra de base, NUNCA alerta (anti-avalancha de arranque)
	l.trackUnknownDevices([]Device{unknown, named}, nil)
	if len(l.engine.List()) != 0 {
		t.Fatal("primer ciclo no debía alertar")
	}
	// Sigue online: no re-alerta
	l.trackUnknownDevices([]Device{unknown, named}, nil)
	if len(l.engine.List()) != 0 {
		t.Fatal("reconexión sin caída no debía alertar")
	}
	// Se desconecta y vuelve: la gracia de N ticks (issue #234) evita alertar
	// mientras el lease aún no se resuelve tras reconectar.
	l.trackUnknownDevices([]Device{named}, nil)
	l.trackUnknownDevices([]Device{unknown, named}, nil)
	if len(l.engine.List()) != 0 {
		t.Fatal("1 tick nameless no debía alertar (gracia)")
	}
	l.trackUnknownDevices([]Device{unknown, named}, nil)
	if len(l.engine.List()) != 0 {
		t.Fatal("2 ticks nameless no debían alertar (gracia)")
	}
	// Al tercer tick consecutivo online+sin nombre, el desconocido real alerta
	l.trackUnknownDevices([]Device{unknown, named}, nil)
	list := l.engine.List()
	if len(list) != 1 {
		t.Fatalf("desconocido tras gracia: %d alertas", len(list))
	}
	if list[0].Category != alerts.CatClients || !list[0].Urgent {
		t.Fatalf("taxonomía: %+v", list[0])
	}
	// Issue #248: la reconexión posterior de la MISMA MAC ya no alerta
	l.trackUnknownDevices([]Device{named}, nil)
	l.trackUnknownDevices([]Device{unknown, named}, nil)
	l.trackUnknownDevices([]Device{unknown, named}, nil)
	l.trackUnknownDevices([]Device{unknown, named}, nil)
	if len(l.engine.List()) != 1 {
		t.Fatal("memoria per-MAC (#248): no debía re-alertar")
	}
	// El dispositivo CON nombre nunca alerta aunque se reconecte
	l.trackUnknownDevices([]Device{unknown}, nil)
	l.trackUnknownDevices([]Device{unknown, named}, nil)
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
	l.trackUnknownDevices([]Device{d}, nil)
	l.trackUnknownDevices([]Device{}, nil)
	l.trackUnknownDevices([]Device{d}, nil) // tick 1 sin nombre
	l.trackUnknownDevices([]Device{d}, nil) // tick 2 sin nombre
	if len(l.engine.List()) != 0 {
		t.Fatal("no debía alertar antes de resolver el lease")
	}
	// …y al tercer tick el lease ya está (Name != MAC) → nunca llega al umbral
	d.Name = "Galaxy Tab"
	l.trackUnknownDevices([]Device{d}, nil)
	if len(l.engine.List()) != 0 {
		t.Fatal("con hostname resuelto no debía alertar")
	}
	// La gracia se reseteó al ver el nombre: una reconexión futura sin nombre
	// parte de cero y vuelve a necesitar los N ticks completos (no 1).
	l.trackUnknownDevices([]Device{}, nil)
	l.trackUnknownDevices([]Device{{Name: d.MAC, MAC: d.MAC, RouterID: "living", Online: true}}, nil)
	if len(l.engine.List()) != 0 {
		t.Fatal("tras reset, tick 1 no debía alertar")
	}
	l.trackUnknownDevices([]Device{{Name: d.MAC, MAC: d.MAC, RouterID: "living", Online: true}}, nil)
	if len(l.engine.List()) != 0 {
		t.Fatal("tras reset, tick 2 no debía alertar")
	}
	l.trackUnknownDevices([]Device{{Name: d.MAC, MAC: d.MAC, RouterID: "living", Online: true}}, nil)
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
	l.trackUnknownDevices([]Device{unknown}, nil)
	l.trackUnknownDevices([]Device{}, nil)
	l.trackUnknownDevices([]Device{unknown}, nil)
	l.trackUnknownDevices([]Device{unknown}, nil)
	l.trackUnknownDevices([]Device{unknown}, nil)
	if len(l.engine.List()) != 1 {
		t.Fatalf("primera alerta: %d", len(l.engine.List()))
	}
	// "Reinicio": un Live nuevo sobre la MISMA BD carga la memoria persistida.
	l2 := NewLive(nil, d, nil, nil)
	if !l2.unknownAlerted[mac] {
		t.Fatal("la memoria per-MAC no se cargó desde kv")
	}
	l2.trackUnknownDevices([]Device{unknown}, nil)
	l2.trackUnknownDevices([]Device{}, nil)
	l2.trackUnknownDevices([]Device{unknown}, nil)
	l2.trackUnknownDevices([]Device{unknown}, nil)
	l2.trackUnknownDevices([]Device{unknown}, nil)
	// #798: el log de alertas sobrevive al reinicio, así que la alerta
	// histórica sigue visible; lo que no debe pasar es que se EMITA una nueva.
	if len(l2.engine.List()) != 1 {
		t.Fatalf("tras reinicio no debía re-alertar (memoria persistida); historial=%d", len(l2.engine.List()))
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
	l.trackUnknownDevices([]Device{trusted}, nil)
	l.trackUnknownDevices([]Device{}, nil)
	l.trackUnknownDevices([]Device{trusted}, nil)
	if len(l.engine.List()) != 0 {
		t.Fatalf("MAC confiable alertó: %d", len(l.engine.List()))
	}
}

// #772: dismiss ("dejar como anónimo") silencia la alerta de desconocido para
// la MAC: aunque complete la gracia de ticks, nunca alerta.
func TestLiveDismissUnknownDevice(t *testing.T) {
	l := liveTestLive()
	unknown := Device{MAC: "11:22:33:44:55:66", Name: "11:22:33:44:55:66", RouterID: "living", Online: true}
	l.DismissUnknownDevice("11:22:33:44:55:66")
	for i := 0; i < 5; i++ {
		l.trackUnknownDevices([]Device{unknown}, nil)
	}
	if len(l.engine.List()) != 0 {
		t.Fatal("dismiss (#772): la MAC no debía alertar")
	}
	// MAC inválida: no-op sin pánico.
	l.DismissUnknownDevice("no-es-una-mac")
}
