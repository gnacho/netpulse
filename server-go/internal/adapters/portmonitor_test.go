package adapters

import (
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

func TestPortMonitorFlapping(t *testing.T) {
	pm := NewPortMonitor()
	engine := alerts.New(nil, nil)
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	pm.SetClock(func() time.Time { return now })

	port := EthPort{ID: "eth0", Label: "LAN1", Up: true, Speed: "1 Gbps"}
	pm.Observe("r1", []EthPort{port}, engine)

	for i := 0; i < flapThreshold; i++ {
		port.Up = !port.Up
		now = now.Add(30 * time.Second)
		pm.SetClock(func() time.Time { return now })
		pm.Observe("r1", []EthPort{port}, engine)
	}

	found := false
	for _, ev := range engine.List() {
		if ev.Title == "Port flapping: LAN1 on r1" {
			found = true
			if ev.Category != alerts.CatSystem {
				t.Errorf("category = %q, want %q", ev.Category, alerts.CatSystem)
			}
			if ev.Hint == "" {
				t.Error("hint should not be empty")
			}
			break
		}
	}
	if !found {
		t.Error("flapping alert not emitted")
	}
}

func TestPortMonitorFlappingNoAlertBelowThreshold(t *testing.T) {
	pm := NewPortMonitor()
	engine := alerts.New(nil, nil)
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	pm.SetClock(func() time.Time { return now })

	port := EthPort{ID: "eth0", Label: "LAN1", Up: true, Speed: "1 Gbps"}
	pm.Observe("r1", []EthPort{port}, engine)

	for i := 0; i < flapThreshold-1; i++ {
		port.Up = !port.Up
		now = now.Add(30 * time.Second)
		pm.SetClock(func() time.Time { return now })
		pm.Observe("r1", []EthPort{port}, engine)
	}

	for _, ev := range engine.List() {
		if ev.Title == "Port flapping: LAN1 on r1" {
			t.Error("flapping alert should not fire below threshold")
		}
	}
}

func TestPortMonitorFlappingWindowExpiry(t *testing.T) {
	pm := NewPortMonitor()
	engine := alerts.New(nil, nil)
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	pm.SetClock(func() time.Time { return now })

	port := EthPort{ID: "eth0", Label: "LAN1", Up: true, Speed: "1 Gbps"}
	pm.Observe("r1", []EthPort{port}, engine)

	for i := 0; i < flapThreshold-1; i++ {
		port.Up = !port.Up
		now = now.Add(30 * time.Second)
		pm.SetClock(func() time.Time { return now })
		pm.Observe("r1", []EthPort{port}, engine)
	}

	now = now.Add(flapWindow + time.Minute)
	pm.SetClock(func() time.Time { return now })
	port.Up = !port.Up
	pm.Observe("r1", []EthPort{port}, engine)

	for _, ev := range engine.List() {
		if ev.Title == "Port flapping: LAN1 on r1" {
			t.Error("flapping alert should not fire after window expires")
		}
	}
}

func TestPortMonitorGhostPort(t *testing.T) {
	pm := NewPortMonitor()
	engine := alerts.New(nil, nil)
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	pm.SetClock(func() time.Time { return now })

	port := EthPort{ID: "eth0", Label: "LAN1", Up: true, RxBytes: 0, TxBytes: 0, Speed: "1 Gbps"}
	pm.Observe("r1", []EthPort{port}, engine)

	for i := 0; i < ghostMinHistory; i++ {
		port.RxBytes += 1000
		port.TxBytes += 500
		now = now.Add(45 * time.Second)
		pm.SetClock(func() time.Time { return now })
		pm.Observe("r1", []EthPort{port}, engine)
	}

	for i := 0; i < ghostConsecutive; i++ {
		now = now.Add(45 * time.Second)
		pm.SetClock(func() time.Time { return now })
		pm.Observe("r1", []EthPort{port}, engine)
	}

	found := false
	for _, ev := range engine.List() {
		if ev.Title == "Ghost port: LAN1 went silent" {
			found = true
			if ev.Category != alerts.CatSystem {
				t.Errorf("category = %q, want %q", ev.Category, alerts.CatSystem)
			}
			break
		}
	}
	if !found {
		t.Error("ghost port alert not emitted")
	}
}

func TestPortMonitorGhostPortNoAlertWithoutHistory(t *testing.T) {
	pm := NewPortMonitor()
	engine := alerts.New(nil, nil)
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	pm.SetClock(func() time.Time { return now })

	port := EthPort{ID: "eth0", Label: "LAN1", Up: true, RxBytes: 100, TxBytes: 100, Speed: "1 Gbps"}

	for i := 0; i < ghostConsecutive+2; i++ {
		now = now.Add(45 * time.Second)
		pm.SetClock(func() time.Time { return now })
		pm.Observe("r1", []EthPort{port}, engine)
	}

	for _, ev := range engine.List() {
		if ev.Title == "Ghost port: LAN1 went silent" {
			t.Error("ghost port should not fire without sufficient history")
		}
	}
}
