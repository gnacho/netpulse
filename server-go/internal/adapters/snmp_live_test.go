package adapters

import (
	"context"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
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
