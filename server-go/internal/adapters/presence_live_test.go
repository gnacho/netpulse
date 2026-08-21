package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

// testLivePresence: Live con DB real y estado de presencia inicializado.
func testLivePresence(t *testing.T) *Live {
	t.Helper()
	d := openLiveTestDB(t)
	return &Live{
		db:                  d,
		devicePresence:      map[string]bool{},
		presenceMisses:      map[string]int{},
		presenceOfflineAfter: 3,
	}
}

func countDeviceEvents(t *testing.T, l *Live) int {
	t.Helper()
	var n int
	if err := l.db.QueryRow("SELECT COUNT(*) FROM device_events").Scan(&n); err != nil {
		t.Fatalf("count device_events: %v", err)
	}
	return n
}

// TestTrackDevicePresenceAntiStartup: el primer ciclo NO emite nada (solo
// puebla el estado) — evita la avalancha de arranque.
func TestTrackDevicePresenceAntiStartup(t *testing.T) {
	l := testLivePresence(t)
	d := Device{MAC: "AA:BB:CC:DD:EE:01", RouterID: "rt1", Online: true, Band: "5 GHz"}
	sig := -45
	d.SignalDbm = &sig

	l.trackDevicePresence([]Device{d}, 1000)
	if n := countDeviceEvents(t, l); n != 0 {
		t.Fatalf("primer ciclo: %d eventos, esperaba 0", n)
	}
}

// TestTrackDevicePresenceOfflineTransition: online → (3 ticks sin ver) →
// offline emite EXACTAMENTE un evento; el tick perdido aislado NO emite.
func TestTrackDevicePresenceOfflineTransition(t *testing.T) {
	l := testLivePresence(t)
	d := Device{MAC: "AA:BB:CC:DD:EE:01", RouterID: "rt1", Online: true, Band: "5 GHz"}
	sig := -45
	d.SignalDbm = &sig

	// Ciclo 1 (siembra): no emite.
	l.trackDevicePresence([]Device{d}, 1000)
	// Ciclo 2 (online sigue): no emite.
	l.trackDevicePresence([]Device{d}, 5000)
	if n := countDeviceEvents(t, l); n != 0 {
		t.Fatalf("2 ciclos online: %d eventos, esperaba 0", n)
	}

	// Un solo tick sin ver (poller lento): NO dispara offline.
	l.trackDevicePresence([]Device{}, 9000)
	if n := countDeviceEvents(t, l); n != 0 {
		t.Fatalf("un tick perdido: %d eventos, esperaba 0", n)
	}

	// Vuelve a verse: el tick perdido no cuenta, sigue online sin eventos.
	l.trackDevicePresence([]Device{d}, 13000)
	if n := countDeviceEvents(t, l); n != 0 {
		t.Fatalf("recuperación tras 1 tick: %d eventos, esperaba 0", n)
	}

	// 3 ticks seguidos sin ver → offline.
	l.trackDevicePresence([]Device{}, 17000)
	l.trackDevicePresence([]Device{}, 21000)
	l.trackDevicePresence([]Device{}, 25000)
	if n := countDeviceEvents(t, l); n != 1 {
		t.Fatalf("tras 3 ticks sin ver: %d eventos, esperaba 1", n)
	}
}

// TestTrackDevicePresenceOnlineAfterOffline: tras el offline, cuando la MAC
// reaparece se emite device_online.
func TestTrackDevicePresenceOnlineAfterOffline(t *testing.T) {
	l := testLivePresence(t)
	d := Device{MAC: "AA:BB:CC:DD:EE:01", RouterID: "rt1", Online: true, Band: "2.4 GHz"}
	sig := -50
	d.SignalDbm = &sig

	l.trackDevicePresence([]Device{d}, 1000) // siembra
	l.trackDevicePresence([]Device{}, 5000)
	l.trackDevicePresence([]Device{}, 9000)
	l.trackDevicePresence([]Device{}, 13000) // → offline
	if n := countDeviceEvents(t, l); n != 1 {
		t.Fatalf("offline: %d eventos, esperaba 1", n)
	}

	// Reaparece → online. Mismo ciclo de recuperación ya puede emitir online
	// porque la transición es offline→online (devicePresence[mac]=false).
	l.trackDevicePresence([]Device{d}, 17000)
	if n := countDeviceEvents(t, l); n != 2 {
		t.Fatalf("tras reaparecer: %d eventos, esperaba 2 (offline+online)", n)
	}
}

// TestTrackDevicePresenceNoDoubleOffline: una MAC ya declarada offline no
// re-emite offline en ciclos posteriores mientras siga sin verse.
func TestTrackDevicePresenceNoDoubleOffline(t *testing.T) {
	l := testLivePresence(t)
	d := Device{MAC: "AA:BB:CC:DD:EE:01", RouterID: "rt1", Online: true, Band: "5 GHz"}
	sig := -45
	d.SignalDbm = &sig

	l.trackDevicePresence([]Device{d}, 1000) // siembra
	l.trackDevicePresence([]Device{}, 5000)
	l.trackDevicePresence([]Device{}, 9000)
	l.trackDevicePresence([]Device{}, 13000) // → offline (1)
	l.trackDevicePresence([]Device{}, 17000) // sigue sin verse → NO re-emite
	l.trackDevicePresence([]Device{}, 21000) // sigue sin verse → NO re-emite
	if n := countDeviceEvents(t, l); n != 1 {
		t.Fatalf("offline repetido: %d eventos, esperaba 1", n)
	}
}

// TestTrackDevicePresenceCableSkip: los dispositivos cableados (FDB) no se
// rastrean — su presencia no genera eventos de presencia.
func TestTrackDevicePresenceCableSkip(t *testing.T) {
	l := testLivePresence(t)
	d := Device{MAC: "AA:BB:CC:DD:EE:02", RouterID: "rt1", Online: true, Band: "cable"}

	l.trackDevicePresence([]Device{d}, 1000)
	l.trackDevicePresence([]Device{}, 5000)
	l.trackDevicePresence([]Device{}, 9000)
	l.trackDevicePresence([]Device{}, 13000)
	if n := countDeviceEvents(t, l); n != 0 {
		t.Fatalf("cableado: %d eventos, esperaba 0", n)
	}
}

// TestTrackDevicePresencePrune reproduce el bug #206: los mapas de presencia
// crecían sin límite con cada MAC wireless histórica. Tras superar
// presencePruneAfter ticks sin verse, la MAC debe eliminarse de ambos mapas.
func TestTrackDevicePresencePrune(t *testing.T) {
	l := testLivePresence(t)
	l.presencePruneAfter = 5 // bajar el umbral para el test
	d := Device{MAC: "AA:BB:CC:DD:EE:03", RouterID: "rt1", Online: true, Band: "5 GHz"}

	l.trackDevicePresence([]Device{d}, 1000) // siembra (entra en el mapa)
	if _, ok := l.devicePresence[d.MAC]; !ok {
		t.Fatal("la MAC debería estar en devicePresence tras sembrar")
	}

	// Ticks vacíos: supera offlineAfter (3) → offline, y luego pruneAfter (5).
	for i := 1; i <= 5; i++ {
		l.trackDevicePresence([]Device{}, int64(i)*1000+1000)
	}

	l.mu.Lock()
	_, inPresence := l.devicePresence[d.MAC]
	_, inMisses := l.presenceMisses[d.MAC]
	l.mu.Unlock()
	if inPresence || inMisses {
		t.Fatalf("tras la poda la MAC debería haber desaparecido de los mapas (presence=%v misses=%v)", inPresence, inMisses)
	}
}

// TestMaintenancePurgaDeviceAttrib: la pasada de mantenimiento borra los
// atributos de dispositivos sin verse desde hace > AttribRetentionMS.
func TestMaintenancePurgaDeviceAttrib(t *testing.T) {
	d := openLiveTestDB(t)
	_, _ = d.Exec("INSERT INTO device_attrib (mac, router_id, band, signal_dbm, last_seen) VALUES (?, ?, ?, ?, ?)",
		"AA:BB:CC:DD:EE:04", "rt1", "5 GHz", -40, db.NowMS()-db.AttribRetentionMS-1000)
	_, _ = d.Exec("INSERT INTO device_attrib (mac, router_id, band, signal_dbm, last_seen) VALUES (?, ?, ?, ?, ?)",
		"AA:BB:CC:DD:EE:05", "rt1", "5 GHz", -40, db.NowMS()-1000)

	d.Maintenance()

	var n int
	if err := d.QueryRow("SELECT COUNT(*) FROM device_attrib").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Fatalf("device_attrib tras maintenance: %d filas, esperaba 1 (solo la reciente)", n)
	}
}
