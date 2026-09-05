// clientbw_test.go — ingesta del tráfico por cliente (#551): resolución de
// fuentes (nlbwmon preferente, hostapd fallback) y conversión de contadores
// absolutos a samples delta en la store client_bw_raw.
package adapters

import (
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

// TestResolveClientBwSources: nlbwmon gana cuando está disponible con hosts;
// si no, hostapd bytes por estación; sin datos → no ok.
func TestResolveClientBwSources(t *testing.T) {
	wireless := map[string]WirelessClient{
		"AA:BB:CC:DD:EE:01": {SignalDbm: -50, RxBytes: 111, TxBytes: 222},
		"AA:BB:CC:DD:EE:02": {SignalDbm: -60}, // sin bytes → no cuenta
	}
	nlbw := &probe.ClientBwData{Available: true, Hosts: map[string]probe.NlbwCounter{
		"AA:BB:CC:DD:EE:0F": {RxBytes: 1000, TxBytes: 2000},
	}}

	// nlbwmon preferente: cubre hasta MACs que no están en wireless.
	samples, source, ok := resolveClientBwSources(wireless, nlbw)
	if !ok || source != "nlbwmon" || len(samples) != 1 {
		t.Fatalf("nlbwmon: ok=%v source=%s samples=%+v", ok, source, samples)
	}
	if samples[0].mac != "AA:BB:CC:DD:EE:0F" || samples[0].rx != 1000 {
		t.Fatalf("muestra nlbwmon: %+v", samples[0])
	}

	// Sin nlbwmon → hostapd bytes (solo estaciones con contadores).
	samples, source, ok = resolveClientBwSources(wireless, nil)
	if !ok || source != "hostapd" || len(samples) != 1 {
		t.Fatalf("hostapd: ok=%v source=%s samples=%+v", ok, source, samples)
	}
	if samples[0].mac != "AA:BB:CC:DD:EE:01" || samples[0].rx != 111 || samples[0].tx != 222 {
		t.Fatalf("muestra hostapd: %+v", samples[0])
	}

	// nlbwmon instalado pero vacío (Available con Hosts {}) → cae a hostapd.
	nlbwEmpty := &probe.ClientBwData{Available: true, Hosts: map[string]probe.NlbwCounter{}}
	_, source, ok = resolveClientBwSources(wireless, nlbwEmpty)
	if !ok || source != "hostapd" {
		t.Fatalf("nlbwmon vacío: source=%s ok=%v", source, ok)
	}

	// Sin datos en ninguna fuente → no ok.
	_, _, ok = resolveClientBwSources(map[string]WirelessClient{"AA:BB:CC:DD:EE:02": {SignalDbm: -60}}, nil)
	if ok {
		t.Fatal("sin contadores debería dar ok=false")
	}
}

// TestRecordClientBwSamplesDeltas: la ingesta convierte contadores absolutos
// en deltas con bps, ignora reinicios (contador que baja) y siembra sin
// escribir en la primera observación.
func TestRecordClientBwSamplesDeltas(t *testing.T) {
	d := openLiveTestDB(t)
	l := &Live{db: d}
	t0 := time.Now()

	// Primera observación: siembra, no escribe nada.
	l.recordClientBwSamples("gw", t0, []clientBwSample{
		{mac: "AA:BB:CC:DD:EE:01", rx: 10000, tx: 5000},
	}, "hostapd")
	if n := countRawClientBW(t, d); n != 0 {
		t.Fatalf("primera observación no debe escribir: %d filas", n)
	}

	// Segunda observación 10 s después con +1000/+500 → delta 1000/500,
	// bps = bytes*8/dt = 800 y 400.
	t1 := t0.Add(10 * time.Second)
	l.recordClientBwSamples("gw", t1, []clientBwSample{
		{mac: "AA:BB:CC:DD:EE:01", rx: 11000, tx: 5500},
	}, "hostapd")
	if n := countRawClientBW(t, d); n != 1 {
		t.Fatalf("segunda observación debe escribir 1 fila: %d", n)
	}
	var rxBytes, txBytes int64
	var rxBps, txBps float64
	err := d.QueryRow("SELECT rx_bytes, tx_bytes, rx_bps, tx_bps FROM client_bw_raw").Scan(&rxBytes, &txBytes, &rxBps, &txBps)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if rxBytes != 1000 || txBytes != 500 {
		t.Fatalf("delta: rx=%d tx=%d (want 1000/500)", rxBytes, txBytes)
	}
	if rxBps != 800 || txBps != 400 {
		t.Fatalf("bps: rx=%.0f tx=%.0f (want 800/400)", rxBps, txBps)
	}
	// El rate del último intervalo queda en memoria para el TrafficMbps.
	if rrx, rtx := l.clientBwRateFor("AA:BB:CC:DD:EE:01"); rrx != 800 || rtx != 400 {
		t.Fatalf("rate en memoria: rx=%.0f tx=%.0f (want 800/400)", rrx, rtx)
	}

	// Reinicio del contador (hostapd se reinició al reasociar / nlbwmon nuevo
	// periodo): contador BAJA → re-siembra, no escribe delta gigante.
	t2 := t1.Add(10 * time.Second)
	l.recordClientBwSamples("gw", t2, []clientBwSample{
		{mac: "AA:BB:CC:DD:EE:01", rx: 100, tx: 50},
	}, "hostapd")
	if n := countRawClientBW(t, d); n != 1 {
		t.Fatalf("reinicio no debe escribir: %d filas", n)
	}

	// Tras el reinicio, siguiente delta normal sí escribe (base re-sembrada).
	t3 := t2.Add(10 * time.Second)
	l.recordClientBwSamples("gw", t3, []clientBwSample{
		{mac: "AA:BB:CC:DD:EE:01", rx: 600, tx: 300},
	}, "hostapd")
	if n := countRawClientBW(t, d); n != 2 {
		t.Fatalf("delta post-reinicio debe escribir: %d filas", n)
	}
	var rx2 int64
	if err := d.QueryRow("SELECT rx_bytes FROM client_bw_raw ORDER BY ts DESC LIMIT 1").Scan(&rx2); err != nil {
		t.Fatalf("query rx2: %v", err)
	}
	if rx2 != 500 { // 600 - 100
		t.Fatalf("delta post-reinicio: %d (want 500)", rx2)
	}

	// Sin DB → no panic, no op.
	(&Live{db: nil}).recordClientBwSamples("gw", t0, []clientBwSample{{mac: "x", rx: 1}}, "hostapd")
}

// TestRecordClientBwSamplesPorRouterYMac: los estados se segregan por router
// y por MAC (un cliente en dos routers no comparte contador).
func TestRecordClientBwSamplesPorRouterYMac(t *testing.T) {
	d := openLiveTestDB(t)
	l := &Live{db: d}
	t0 := time.Now()
	t1 := t0.Add(10 * time.Second)

	l.recordClientBwSamples("gw", t0, []clientBwSample{{mac: "AA:BB:CC:DD:EE:01", rx: 100}}, "hostapd")
	l.recordClientBwSamples("ap1", t0, []clientBwSample{{mac: "AA:BB:CC:DD:EE:01", rx: 100}}, "hostapd")
	l.recordClientBwSamples("gw", t1, []clientBwSample{{mac: "AA:BB:CC:DD:EE:01", rx: 200}}, "hostapd")
	l.recordClientBwSamples("ap1", t1, []clientBwSample{{mac: "AA:BB:CC:DD:EE:01", rx: 300}}, "hostapd")

	var n int
	if err := d.QueryRow("SELECT COUNT(*) FROM client_bw_raw").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("filas: %d (want 2, una por router)", n)
	}
	var apRx int64
	if err := d.QueryRow("SELECT rx_bytes FROM client_bw_raw WHERE router_id='ap1'").Scan(&apRx); err != nil {
		t.Fatalf("query ap1: %v", err)
	}
	if apRx != 200 { // 300-100
		t.Fatalf("delta ap1: %d (want 200)", apRx)
	}
}

func countRawClientBW(t *testing.T, d *db.DB) int {
	t.Helper()
	var n int
	if err := d.QueryRow("SELECT COUNT(*) FROM client_bw_raw").Scan(&n); err != nil {
		t.Fatalf("count raw: %v", err)
	}
	return n
}

// TestClientBwRateForPriorizaFuente: cuando una MAC es reportada por nlbwmon
// (router que enruta) y por hostapd a la vez, el rate de la lista usa el de
// nlbwmon (el del gateway ya contabiliza todo el tráfico; sumar doblaría).
func TestClientBwRateForPriorizaFuente(t *testing.T) {
	d := openLiveTestDB(t)
	l := &Live{db: d}
	t0 := time.Now()
	t1 := t0.Add(10 * time.Second)

	// hostapd en ap1 y nlbwmon en gw reportan la misma MAC.
	l.recordClientBwSamples("ap1", t0, []clientBwSample{{mac: "AA:BB:CC:DD:EE:01", rx: 10000}}, "hostapd")
	l.recordClientBwSamples("gw", t0, []clientBwSample{{mac: "AA:BB:CC:DD:EE:01", rx: 20000}}, "nlbwmon")
	l.recordClientBwSamples("ap1", t1, []clientBwSample{{mac: "AA:BB:CC:DD:EE:01", rx: 10100}}, "hostapd")
	l.recordClientBwSamples("gw", t1, []clientBwSample{{mac: "AA:BB:CC:DD:EE:01", rx: 20200}}, "nlbwmon")

	rx, tx := l.clientBwRateFor("AA:BB:CC:DD:EE:01")
	// 200 bytes en 10 s → 160 bps (nlbwmon), no 80 del hostapd.
	if rx != 160 {
		t.Fatalf("rate rx: %.0f (want 160 del nlbwmon)", rx)
	}
	if tx != 0 {
		t.Fatalf("rate tx: %.0f (want 0)", tx)
	}
}
