package adapters

import (
	"context"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
	"github.com/gnacho/netpulse/server-go/internal/db"
	npsnmp "github.com/gnacho/netpulse/server-go/internal/snmp"
)

// issue #414: cuando no ha pasado el intervalo SNMP configurado, pollRouterSNMP
// devuelve el snapshot cacheado sin tocar la red.
func TestSNMPCachesWithinInterval(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	l.now = time.Now
	cfg := RouterConfig{ID: "sw1", Host: "192.168.1.10", SnmpEnabled: true, SnmpPollInterval: 60}

	cached := &routerPolled{
		cfg:       cfg,
		uptimeSec: 123,
		ports:     []EthPort{{ID: "snmp-1", Label: "1", Up: true}},
		polledAt:  time.Now().Add(-30 * time.Second).UnixMilli(),
	}
	l.lastPolled[cfg.ID] = cached
	l.snmpLastPoll[cfg.ID] = time.Now().Add(-30 * time.Second)

	p, err := l.pollRouterSNMP(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != cached {
		t.Fatal("expected cached routerPolled pointer")
	}
}

// issue #414: un router SNMP cacheado solo genera UNA fila de métricas por
// poll real; los ticks intermedios se omiten.
func TestSNMPMetricsRowsDedup(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	cfg := RouterConfig{ID: "sw1", Host: "192.168.1.10", SnmpEnabled: true}
	rx := 1e6
	tx := 2e6
	l.lastPolled["sw1"] = &routerPolled{
		cfg:       cfg,
		cpu:       1, ram: 2,
		polledAt:  1000,
		net:       &NetDevBps{RxBps: &rx, TxBps: &tx},
	}

	rows1 := l.GetMetricsRows(context.Background())
	if len(rows1) != 1 {
		t.Fatalf("first call rows = %d, want 1", len(rows1))
	}

	rows2 := l.GetMetricsRows(context.Background())
	if len(rows2) != 0 {
		t.Fatalf("second call rows = %d, want 0 (dedup)", len(rows2))
	}
}

// issue #414: los puertos SNMP requieren un tiempo mínimo de silencio antes de
// declarar ghost-port, aunque el contador de polls consecutivos sin tráfico ya
// supere el umbral normal.
func TestPortMonitorGhostSnmpHysteresis(t *testing.T) {
	pm := NewPortMonitor(true)
	engine := alerts.New(nil, nil)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	pm.SetClock(func() time.Time { return now })

	port := EthPort{ID: "snmp-1", Label: "Port1", Up: true, Speed: "1 Gbps", Snmp: true}
	pm.Observe("r1", []EthPort{port}, engine)

	// Historia suficiente con tráfico creciente.
	for i := 0; i < ghostMinHistory; i++ {
		port.RxBytes += 1000
		port.TxBytes += 500
		now = now.Add(60 * time.Second)
		pm.SetClock(func() time.Time { return now })
		pm.Observe("r1", []EthPort{port}, engine)
	}

	// Tráfico cero durante 4 minutos: el streak ya supera ghostConsecutive,
	// pero el puerto es SNMP y aún no han pasado 5 minutos de silencio.
	for i := 0; i < 4; i++ {
		now = now.Add(60 * time.Second)
		pm.SetClock(func() time.Time { return now })
		pm.Observe("r1", []EthPort{port}, engine)
	}

	if n, _ := findAlerts(engine, "Ghost port: Port1 went silent"); n != 0 {
		t.Fatalf("ghost alerts before min silence = %d, want 0", n)
	}

	// 70 segundos más de silencio: ahora sí se cumple la histeresis temporal.
	now = now.Add(70 * time.Second)
	pm.SetClock(func() time.Time { return now })
	pm.Observe("r1", []EthPort{port}, engine)

	if n, _ := findAlerts(engine, "Ghost port: Port1 went silent"); n != 1 {
		t.Fatalf("ghost alerts after min silence = %d, want 1", n)
	}
}

// #661: el mapeo FDB no debe descartar MACs cuyo ifIndex no resuelve a un
// puerto conocido (antes dejaba el switch con 0 dispositivos conectados).
func TestSnmpFdbMapNoDrop(t *testing.T) {
	portIdxToName := map[int]string{3: "lan3"}
	fdb := []npsnmp.FdbEntry{
		{MAC: "aa:bb:cc:dd:ee:01", IfIndex: 3, BridgePortIndex: 3},
		{MAC: "aa:bb:cc:dd:ee:02", IfIndex: 99, BridgePortIndex: 4}, // ifIndex sin puerto → fallback
		{MAC: "aa:bb:cc:dd:ee:03", IfIndex: 0, BridgePortIndex: 5},  // nada resuelve → nombre genérico
	}
	m := snmpFdbMap(fdb, portIdxToName)
	if len(m) != 3 {
		t.Fatalf("fdbMap len = %d, want 3 (no drop)", len(m))
	}
	if got := m["aa:bb:cc:dd:ee:01"]; got != "lan3" {
		t.Errorf("ee:01 = %q, want lan3", got)
	}
	if got := m["aa:bb:cc:dd:ee:02"]; got != "port-99" {
		t.Errorf("ee:02 = %q, want port-99 (fallback ifIndex)", got)
	}
	if got := m["aa:bb:cc:dd:ee:03"]; got != "port-0" {
		t.Errorf("ee:03 = %q, want port-0 (generic)", got)
	}
}

// #661: el agregado de red del switch SNMP es la suma de la tasa de todos los
// puertos; nil si todo es 0 (primera muestra sin delta).
func TestSnmpAggregateNet(t *testing.T) {
	ports := []EthPort{{RxBps: 100, TxBps: 50}, {RxBps: 25, TxBps: 10}}
	n := snmpAggregateNet(ports)
	if n == nil {
		t.Fatal("expected non-nil aggregate")
	}
	if *n.RxBps != 125 || *n.TxBps != 60 {
		t.Errorf("aggregate = (%.0f, %.0f), want (125, 60)", *n.RxBps, *n.TxBps)
	}
	if got := snmpAggregateNet([]EthPort{{RxBps: 0, TxBps: 0}}); got != nil {
		t.Error("expected nil when all ports at 0")
	}
}

// #661: recordPortSamples persiste los contadores de bytes de un puerto SNMP
// (la ruta que antes NO se llamaba en el path SNMP → historial vacío).
func TestRecordPortSamplesPersistsBytes(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	l := NewLive(nil, d, nil, nil)
	now := time.Now()
	l.recordPortSamples("sw1", []EthPort{
		{ID: "snmp-1", RxBytes: 1000, TxBytes: 2000, RxBps: 10, TxBps: 20, Speed: "1 Gbps"},
	})

	pts, err := d.PortSeries.GetSeries("sw1", "snmp-1", now.Add(-time.Minute), now.Add(time.Minute), "raw")
	if err != nil {
		t.Fatalf("GetSeries: %v", err)
	}
	if len(pts) != 1 {
		t.Fatalf("points = %d, want 1", len(pts))
	}
	if pts[0].RxBytes != 1000 || pts[0].TxBytes != 2000 {
		t.Errorf("bytes = (%d, %d), want (1000, 2000)", pts[0].RxBytes, pts[0].TxBytes)
	}
	if pts[0].RxBps != 10 || pts[0].TxBps != 20 {
		t.Errorf("bps = (%.1f, %.1f), want (10, 20)", pts[0].RxBps, pts[0].TxBps)
	}
}

// #661: el DTO del router refleja SnmpEnabled para que la UI elija bps (no fps).
func TestBuildRouterExposeSnmpEnabled(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	p := &routerPolled{cfg: RouterConfig{ID: "sw1", Host: "192.168.1.10", SnmpEnabled: true}}
	r := l.buildRouter(p, nil)
	if !r.SnmpEnabled {
		t.Error("expected SnmpEnabled=true in built Router")
	}
}
