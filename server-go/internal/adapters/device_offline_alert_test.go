// device_offline_alert_test.go - issue #1354: alerta opt-in cuando un
// dispositivo marcado se desconecta (volátil, ID estable, gracia de 3 ticks).
package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

func offlineAlertTestLive() *Live {
	return &Live{
		engine:          alerts.New(nil, nil),
		routers:         []RouterConfig{{ID: "rt1", Name: "Salón"}},
		offlineOnline:   map[string]bool{},
		offlineMisses:   map[string]int{},
		offlineAlerted:  map[string]bool{},
		offlineAlertNum: 3,
	}
}

func offlineMarkedDevice() Device {
	return Device{MAC: "AA:BB:CC:DD:EE:01", Name: "TV Salón", RouterID: "rt1", NotifyOffline: true, Online: true}
}

// (a) Dispositivo marcado offline N ciclos: una única emisión con ID estable.
func TestTrackOfflineDevicesEmitsOnceAfterGrace(t *testing.T) {
	l := offlineAlertTestLive()
	d := offlineMarkedDevice()
	l.trackOfflineDevices([]Device{d}) // línea base online
	d.Online = false
	l.trackOfflineDevices([]Device{d}) // miss 1
	l.trackOfflineDevices([]Device{d}) // miss 2
	if n := len(l.engine.List()); n != 0 {
		t.Fatalf("aún en gracia: %d alertas", n)
	}
	l.trackOfflineDevices([]Device{d}) // miss 3: emite
	list := l.engine.List()
	if len(list) != 1 {
		t.Fatalf("esperaba 1 alerta, got %d", len(list))
	}
	ev := list[0]
	if ev.ID != "alert-device-offline-"+d.MAC {
		t.Fatalf("ID=%q", ev.ID)
	}
	if ev.Type != alerts.TypeDeviceOffline {
		t.Fatalf("Type=%q (want %q)", ev.Type, alerts.TypeDeviceOffline)
	}
	if ev.Category != alerts.CatClients || !ev.Urgent || ev.Severity != "warn" {
		t.Fatalf("taxonomía: (%s,%v,%s)", ev.Category, ev.Urgent, ev.Severity)
	}
	if ev.Vars["device"] != "TV Salón" || ev.Vars["router"] != "Salón" {
		t.Fatalf("Vars=%+v", ev.Vars)
	}
	// Un tick más offline: el ID estable no re-emite (sigue 1 entrada).
	l.trackOfflineDevices([]Device{d})
	if len(l.engine.List()) != 1 {
		t.Fatalf("re-emitió: %d alertas", len(l.engine.List()))
	}
}

// (b) Vuelve online: la entrada volátil desaparece del feed.
func TestTrackOfflineDevicesResolvesOnRecovery(t *testing.T) {
	l := offlineAlertTestLive()
	d := offlineMarkedDevice()
	l.trackOfflineDevices([]Device{d})
	d.Online = false
	l.trackOfflineDevices([]Device{d})
	l.trackOfflineDevices([]Device{d})
	l.trackOfflineDevices([]Device{d})
	if len(l.engine.List()) != 1 {
		t.Fatalf("previo: %d alertas", len(l.engine.List()))
	}
	d.Online = true
	l.trackOfflineDevices([]Device{d})
	if n := len(l.engine.List()); n != 0 {
		t.Fatalf("la alerta debió retirarse al volver online, quedaron %d: %+v", n, l.engine.List())
	}
}

// (c) Dispositivo SIN marca: ninguna emisión.
func TestTrackOfflineDevicesUnmarkedNeverEmits(t *testing.T) {
	l := offlineAlertTestLive()
	d := offlineMarkedDevice()
	d.NotifyOffline = false
	l.trackOfflineDevices([]Device{d})
	d.Online = false
	for i := 0; i < 6; i++ {
		l.trackOfflineDevices([]Device{d})
	}
	if n := len(l.engine.List()); n != 0 {
		t.Fatalf("sin marca no debía emitir: %+v", l.engine.List())
	}
}

// (d) Ya offline al arrancar: sin transición online->offline, nada.
func TestTrackOfflineDevicesAlreadyOfflineAtBootNeverEmits(t *testing.T) {
	l := offlineAlertTestLive()
	d := offlineMarkedDevice()
	d.Online = false
	for i := 0; i < 6; i++ {
		l.trackOfflineDevices([]Device{d})
	}
	if n := len(l.engine.List()); n != 0 {
		t.Fatalf("una MAC offline desde el arranque no debía emitir: %+v", l.engine.List())
	}
}

// (e) Offline 2 ciclos y vuelve (dentro de la gracia): nada.
func TestTrackOfflineDevicesFlapWithinGraceNoEmit(t *testing.T) {
	l := offlineAlertTestLive()
	d := offlineMarkedDevice()
	l.trackOfflineDevices([]Device{d}) // línea base online
	d.Online = false
	l.trackOfflineDevices([]Device{d}) // miss 1
	l.trackOfflineDevices([]Device{d}) // miss 2
	d.Online = true
	l.trackOfflineDevices([]Device{d}) // vuelve dentro de la gracia
	if n := len(l.engine.List()); n != 0 {
		t.Fatalf("un flap dentro de la gracia no debía emitir: %+v", l.engine.List())
	}
	l.trackOfflineDevices([]Device{d})
	if n := len(l.engine.List()); n != 0 {
		t.Fatalf("restos tras la recuperación: %+v", l.engine.List())
	}
}
