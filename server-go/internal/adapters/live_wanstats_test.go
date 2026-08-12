package adapters

import (
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

// openLiveTestDB abre una BD SQLite real en un dir temporal (patrón de
// db/rollup_test.go, pero en el paquete adapters para probar wanDayStats).
func openLiveTestDB(t *testing.T) *db.DB {
	t.Helper()
	dataDir := t.TempDir()
	d, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func insertMetric(t *testing.T, d *db.DB, routerID string, ts time.Time, rxBps float64) {
	t.Helper()
	_, err := d.Exec(
		"INSERT INTO metrics (router_id, ts, cpu, ram, temp, latency_ms, rx_bps, tx_bps) VALUES (?,?,0,0,0,0,?,0)",
		routerID, ts.UnixMilli(), rxBps)
	if err != nil {
		t.Fatalf("insert metric: %v", err)
	}
}

// Reloj fijo: 12-Ago-2026 21:00 local (ventana "hoy" = 12-Ago 00:00–23:59).
func fixedNow() time.Time {
	return time.Date(2026, 8, 12, 21, 0, 0, 0, time.Local)
}

// wanDayStats calcula pico de hoy, media y total 24h desde la tabla raw del
// gateway (issue #169: solo la demo poblaba estos campos en live).
func TestWanDayStats(t *testing.T) {
	d := openLiveTestDB(t)
	l := &Live{db: d, now: fixedNow}
	now := fixedNow()

	// Ayer (fuera del rango de hoy, pero 2 muestras dentro de la ventana 24h
	// si caen >= now-24h). now-24h = 11-Ago 21:00.
	insertMetric(t, d, "gw", now.Add(-25*time.Hour), 10e6)  // 11-Ago 20:00 → fuera de 24h
	insertMetric(t, d, "gw", now.Add(-24*time.Hour), 20e6)  // 11-Ago 21:00 → dentro (borde)
	// Hoy (12-Ago): pico a las 15:00 con 90 Mbps.
	insertMetric(t, d, "gw", time.Date(2026, 8, 12, 10, 0, 0, 0, time.Local), 30e6)
	insertMetric(t, d, "gw", time.Date(2026, 8, 12, 15, 0, 0, 0, time.Local), 90e6)
	insertMetric(t, d, "gw", time.Date(2026, 8, 12, 18, 0, 0, 0, time.Local), 50e6)
	// Otro router: no debe contaminar las stats del gateway.
	insertMetric(t, d, "rt2", time.Date(2026, 8, 12, 15, 0, 0, 0, time.Local), 999e6)

	res := l.wanDayStats("gw")
	if res.peakMbps != 90 {
		t.Fatalf("peakMbps=%v, esperaba 90", res.peakMbps)
	}
	if res.peakTime != "15:00" {
		t.Fatalf("peakTime=%q, esperaba 15:00", res.peakTime)
	}
	// Media de los 4 puntos del gateway dentro de 24h: (20+30+90+50)/4 = 47.5.
	if res.avgMbps != 47.5 {
		t.Fatalf("avgMbps=%v, esperaba 47.5", res.avgMbps)
	}
	// Total 24h: SUM(rx_bps)×dt/8 con dt = 86400/4 = 21600s.
	// (20+30+90+50)e6 × 21600 / 8 = 190e6×21600/8 = 5.13e11 bytes = 513 GB.
	if res.totalStr != "513 GB" {
		t.Fatalf("totalStr=%q, esperaba 513 GB", res.totalStr)
	}
}

// wanDayStats con gateway sin datos: devuelve los marcadores "—"/0 de partida
// (no pánico, no datos inventados).
func TestWanDayStatsSinDatos(t *testing.T) {
	d := openLiveTestDB(t)
	l := &Live{db: d, now: fixedNow}
	res := l.wanDayStats("gw")
	if res.peakMbps != 0 || res.avgMbps != 0 {
		t.Fatalf("stats sin datos deberían ser 0: %+v", res)
	}
	if res.peakTime != "—" || res.totalStr != "—" {
		t.Fatalf("marcadores sin datos deberían ser '—': %+v", res)
	}
}

// wanDayStats sin BD (db nil, p. ej. modo demo): cero y "—" sin pánico.
func TestWanDayStatsSinBD(t *testing.T) {
	l := &Live{db: nil}
	res := l.wanDayStats("gw")
	if res.peakMbps != 0 || res.avgMbps != 0 || res.totalStr != "—" {
		t.Fatalf("sin BD debería devolver zeros/'—': %+v", res)
	}
}
