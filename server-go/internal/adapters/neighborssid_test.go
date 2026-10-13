package adapters

// neighborssid_test.go — #1359: new-neighbor-ssid con dwell y one-shot.

import (
	"testing"
	"time"
)

func newSsidAlerts(l *Live) []AlertEvent {
	var out []AlertEvent
	for _, a := range l.engine.List() {
		if a.Type == "new-neighbor-ssid" {
			out = append(out, a)
		}
	}
	return out
}

func TestNewSsidSeedingDoesNotAlert(t *testing.T) {
	l, _ := newImpostorLive(t) // flota: "temiscira" propio
	now := time.Now()
	// Vecino de siempre (lleva 10h): en la primera evaluación se siembra,
	// no alerta (anti-avalancha de arranque).
	insertScan(t, l, "ap1", "11:22:33:44:55:01", "vecino-historico", now.Add(-10*time.Hour))
	insertScan(t, l, "ap1", "11:22:33:44:55:01", "vecino-historico", now)
	l.EvalNewNeighborSsids()
	if got := newSsidAlerts(l); len(got) != 0 {
		t.Fatalf("la siembra no debe alertar: %+v", got)
	}
	var seeded int
	_ = l.db.QueryRow(`SELECT COUNT(1) FROM kv WHERE key='ssid_known_seeded'`).Scan(&seeded)
	if seeded != 1 {
		t.Fatal("kv ssid_known_seeded no escrito")
	}
	_ = l.db.QueryRow(`SELECT COUNT(1) FROM kv WHERE key='ssid_known:vecino-historico'`).Scan(&seeded)
	if seeded != 1 {
		t.Fatal("vecino histórico no quedó sembrado como conocido")
	}
}

func TestNewSsidTransientStaysQuiet(t *testing.T) {
	l, _ := newImpostorLive(t)
	now := time.Now()
	// Siembra con la flota sola.
	l.EvalNewNeighborSsids()
	// Hotspot de paso: 2 sightings separados 1h (< dwell 4h), ya no activo.
	insertScan(t, l, "ap1", "11:22:33:44:55:02", "hotspot-movil", now.Add(-90*time.Minute))
	insertScan(t, l, "ap1", "11:22:33:44:55:02", "hotspot-movil", now.Add(-30*time.Minute))
	l.EvalNewNeighborSsids()
	if got := newSsidAlerts(l); len(got) != 0 {
		t.Fatalf("transitorio no debe alertar: %+v", got)
	}
}

func TestNewSsidAlertsAfterDwellOnce(t *testing.T) {
	l, _ := newImpostorLive(t)
	now := time.Now()
	l.EvalNewNeighborSsids() // siembra
	// SSID nuevo que persiste 5h y sigue activo.
	insertScan(t, l, "ap1", "11:22:33:44:55:03", "iot-pairing", now.Add(-5*time.Hour))
	insertScan(t, l, "ap1", "11:22:33:44:55:03", "iot-pairing", now.Add(-3*time.Hour))
	l.EvalNewNeighborSsids()
	if got := newSsidAlerts(l); len(got) != 0 {
		t.Fatalf("a los 3h de dwell no debe alertar aún... espera, esto ya son 5h")
	}
	insertScan(t, l, "ap1", "11:22:33:44:55:03", "iot-pairing", now)
	l.EvalNewNeighborSsids()
	got := newSsidAlerts(l)
	if len(got) != 1 {
		t.Fatalf("dwell cumplido y activo debe alertar: %d", len(got))
	}
	a := got[0]
	if a.Urgent || a.Severity != "info" || a.Category != "clients" {
		t.Fatalf("info/no urgente/clients: %+v", a)
	}
	if a.Vars["ssid"] != "iot-pairing" || a.Vars["router"] != "AP Uno" || a.Vars["signal"] != "-55" {
		t.Fatalf("vars: %+v", a.Vars)
	}
	// One-shot: re-evaluar con más sightings no repite.
	insertScan(t, l, "ap1", "11:22:33:44:55:03", "iot-pairing", now.Add(1*time.Minute))
	l.EvalNewNeighborSsids()
	if got := newSsidAlerts(l); len(got) != 1 {
		t.Fatalf("one-shot: no debe repetir; alertas: %d", len(got))
	}
	// Persistido en kv (sobrevive a la poda de 48h y a reinicios).
	var n int
	_ = l.db.QueryRow(`SELECT COUNT(1) FROM kv WHERE key='ssid_alerted:iot-pairing'`).Scan(&n)
	if n != 1 {
		t.Fatal("ssid_alerted no persistido en kv")
	}
}

func TestNewSsidFleetAndEmptyIgnored(t *testing.T) {
	l, _ := newImpostorLive(t)
	now := time.Now()
	l.EvalNewNeighborSsids() // siembra
	// SSID propio de la flota y SSID vacío (red oculta): nunca alertan.
	insertScan(t, l, "ap1", "AA:BB:CC:00:00:09", "temiscira", now.Add(-6*time.Hour))
	insertScan(t, l, "ap1", "AA:BB:CC:00:00:09", "temiscira", now)
	insertScan(t, l, "ap1", "11:22:33:44:55:04", "", now.Add(-6*time.Hour))
	insertScan(t, l, "ap1", "11:22:33:44:55:04", "", now)
	l.EvalNewNeighborSsids()
	if got := newSsidAlerts(l); len(got) != 0 {
		t.Fatalf("flota/vacío no alertan: %+v", got)
	}
}
