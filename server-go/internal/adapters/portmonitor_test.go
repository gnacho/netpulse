package adapters

import (
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

func TestPortMonitorFlapping(t *testing.T) {
	pm := NewPortMonitor(true)
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
	pm := NewPortMonitor(true)
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
	pm := NewPortMonitor(true)
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
	pm := NewPortMonitor(true)
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
	pm := NewPortMonitor(true)
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

func TestPortMonitorDegradedLink(t *testing.T) {
	pm := NewPortMonitor(true)
	engine := alerts.New(nil, nil)
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	pm.SetClock(func() time.Time { return now })

	port := EthPort{ID: "eth0", Label: "LAN1", Up: true, Speed: "1 Gbps", RxBytes: 100, TxBytes: 100}

	for i := 0; i < degradedMinHistory; i++ {
		port.RxBytes += 1000
		port.TxBytes += 500
		now = now.Add(45 * time.Second)
		pm.SetClock(func() time.Time { return now })
		pm.Observe("r1", []EthPort{port}, engine)
	}

	port.Speed = "100 Mbps"
	for i := 0; i < degradedConsec; i++ {
		port.RxBytes += 500
		port.TxBytes += 200
		now = now.Add(45 * time.Second)
		pm.SetClock(func() time.Time { return now })
		pm.Observe("r1", []EthPort{port}, engine)
	}

	found := false
	for _, ev := range engine.List() {
		if ev.Title == "Degraded link: LAN1 at 100Mbps (was 1000Mbps)" {
			found = true
			if ev.Category != alerts.CatSystem {
				t.Errorf("category = %q, want %q", ev.Category, alerts.CatSystem)
			}
			break
		}
	}
	if !found {
		t.Errorf("degraded link alert not emitted; alerts: %v", engine.List())
	}
}

func TestPortMonitorDegradedLinkNoAlertWhenStable(t *testing.T) {
	pm := NewPortMonitor(true)
	engine := alerts.New(nil, nil)
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	pm.SetClock(func() time.Time { return now })

	port := EthPort{ID: "eth0", Label: "LAN1", Up: true, Speed: "1 Gbps", RxBytes: 100, TxBytes: 100}

	for i := 0; i < degradedMinHistory+degradedConsec; i++ {
		port.RxBytes += 1000
		port.TxBytes += 500
		now = now.Add(45 * time.Second)
		pm.SetClock(func() time.Time { return now })
		pm.Observe("r1", []EthPort{port}, engine)
	}

	for _, ev := range engine.List() {
		if ev.Title == "Degraded link: LAN1 at 1000Mbps (was 1000Mbps)" {
			t.Error("degraded link should not fire when speed is stable")
		}
	}
}

func TestDominantSpeed(t *testing.T) {
	cases := []struct {
		in   []int
		want int
	}{
		{[]int{1000, 1000, 1000, 100}, 1000},
		{[]int{100, 100, 100, 1000}, 100},
		{[]int{}, 0},
		{[]int{1000}, 1000},
	}
	for _, tc := range cases {
		got := dominantSpeed(tc.in)
		if got != tc.want {
			t.Errorf("dominantSpeed(%v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// buildGhostPort deja el monitor listo para el umbral de ghost: historia
// suficiente y tráfico creciente. Devuelve el reloj mutable.
func buildGhostPort(t *testing.T, pm *PortMonitor, engine *alerts.Engine) (EthPort, func(time.Time)) {
	t.Helper()
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	setClock := func(n time.Time) { pm.SetClock(func() time.Time { return n }) }
	setClock(now)
	port := EthPort{ID: "eth0", Label: "LAN1", Up: true, RxBytes: 0, TxBytes: 0, Speed: "1 Gbps"}
	pm.Observe("r1", []EthPort{port}, engine)
	for i := 0; i < ghostMinHistory; i++ {
		port.RxBytes += 1000
		port.TxBytes += 500
		now = now.Add(45 * time.Second)
		setClock(now)
		pm.Observe("r1", []EthPort{port}, engine)
	}
	return port, setClock
}

func findAlerts(engine *alerts.Engine, title string) (int, alerts.AlertEvent) {
	n := 0
	var last alerts.AlertEvent
	for _, ev := range engine.List() {
		if ev.Title == title {
			n++
			last = ev
		}
	}
	return n, last
}

// issue #366: un incidente persistente refresca UNA sola alerta (EmitOrUpdate)
// en vez de colar una nueva por cada ventana de dedup.
func TestPortMonitorGhostConsolidatesIntoOneAlert(t *testing.T) {
	pm := NewPortMonitor(true)
	engine := alerts.New(nil, nil)
	port, setClock := buildGhostPort(t, pm, engine)

	now := time.Date(2026, 8, 29, 13, 0, 0, 0, time.UTC)
	for i := 0; i < 20; i++ {
		now = now.Add(6 * time.Minute)
		setClock(now)
		pm.Observe("r1", []EthPort{port}, engine)
	}
	n, last := findAlerts(engine, "Ghost port: LAN1 went silent")
	if n != 1 {
		t.Fatalf("ghost alerts = %d, want 1 (consolidated)", n)
	}
	if last.Ts < now.Unix()-120 {
		t.Errorf("consolidated alert ts %d should be recent (near %d)", last.Ts, now.Unix())
	}
	if last.ID != "alert-ghost-r1-eth0" {
		t.Errorf("alert ID = %q, want stable alert-ghost-r1-eth0", last.ID)
	}
}

// issue #365: un reset de contadores (reboot) NO es un puerto muerto.
func TestPortMonitorGhostCounterResetNoAlert(t *testing.T) {
	pm := NewPortMonitor(true)
	engine := alerts.New(nil, nil)
	port, setClock := buildGhostPort(t, pm, engine)

	// Reboot: contadores caen de ~27k a 42 y vuelven a crecer desde ahí.
	port.RxBytes, port.TxBytes = 42, 0
	now := time.Date(2026, 8, 29, 13, 0, 0, 0, time.UTC)
	for i := 0; i < ghostConsecutive+3; i++ {
		if i > 0 {
			port.RxBytes += 100
		}
		now = now.Add(45 * time.Second)
		setClock(now)
		pm.Observe("r1", []EthPort{port}, engine)
	}
	if n, _ := findAlerts(engine, "Ghost port: LAN1 went silent"); n != 0 {
		t.Fatalf("ghost alerts after counter reset = %d, want 0", n)
	}
}

// issue #366: al volver el tráfico se emite UNA alerta ok de recuperación.
func TestPortMonitorGhostRecovery(t *testing.T) {
	pm := NewPortMonitor(true)
	engine := alerts.New(nil, nil)
	port, setClock := buildGhostPort(t, pm, engine)

	now := time.Date(2026, 8, 29, 13, 0, 0, 0, time.UTC)
	for i := 0; i < ghostConsecutive; i++ {
		now = now.Add(45 * time.Second)
		setClock(now)
		pm.Observe("r1", []EthPort{port}, engine)
	}
	if n, _ := findAlerts(engine, "Ghost port: LAN1 went silent"); n != 1 {
		t.Fatalf("ghost alert not emitted before recovery")
	}
	// El tráfico vuelve y se mantiene: una única recuperación.
	for round := 0; round < 2; round++ {
		for i := 0; i < 4; i++ {
			port.RxBytes += 500
			now = now.Add(45 * time.Second)
			setClock(now)
			pm.Observe("r1", []EthPort{port}, engine)
		}
	}
	if n, _ := findAlerts(engine, "Ghost port recovered: LAN1 on r1"); n != 1 {
		t.Fatalf("recovery alerts = %d, want 1", n)
	}
}

// issue #419: cuando ghost port esta desactivado, no se emiten alertas de
// puerto fantasma ni de recuperacion.
func TestPortMonitorGhostDisabled(t *testing.T) {
	pm := NewPortMonitor(false)
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

	for i := 0; i < ghostConsecutive+2; i++ {
		now = now.Add(45 * time.Second)
		pm.SetClock(func() time.Time { return now })
		pm.Observe("r1", []EthPort{port}, engine)
	}

	for _, ev := range engine.List() {
		if strings.Contains(ev.Title, "Ghost port") {
			t.Fatalf("ghost alert emitted while disabled: %s", ev.Title)
		}
	}
}

