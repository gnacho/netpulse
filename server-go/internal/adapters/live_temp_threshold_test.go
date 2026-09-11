// live_temp_threshold_test.go — issue #716: umbral de temperatura alta por
// router. El umbral configurado se usa en el estado hot del router, en la
// salud global y en el emisor de alerta, cayendo al default 65 cuando no hay.
package adapters

import (
	"strings"
	"testing"
)

func TestRouterConfigTempThresholdValue(t *testing.T) {
	if got := (RouterConfig{}).TempThresholdValue(); got != DefaultTempThreshold {
		t.Fatalf("sin umbral: %d, esperaba %d", got, DefaultTempThreshold)
	}
	if got := (RouterConfig{TempThreshold: iptr(70)}).TempThresholdValue(); got != 70 {
		t.Fatalf("umbral 70: %d, esperaba 70", got)
	}
	if got := (RouterConfig{TempThreshold: iptr(0)}).TempThresholdValue(); got != DefaultTempThreshold {
		t.Fatalf("umbral 0: %d, esperaba default %d", got, DefaultTempThreshold)
	}
}

func TestBuildRouterUsesPerRouterTempThreshold(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)

	// Umbral 70 y 68 °C → por debajo: online y sin hotMetric.
	p := &routerPolled{
		cfg:      RouterConfig{ID: "rt1", Name: "RT1", Host: "192.168.1.2", TempThreshold: iptr(70)},
		temp:     68,
		polledAt: 1000,
	}
	r := l.buildRouter(p, nil)
	if r.Status != "online" {
		t.Fatalf("68 °C con umbral 70: status=%q, esperaba online", r.Status)
	}
	if r.HotMetric != "" {
		t.Fatalf("68 °C con umbral 70: hotMetric=%q, esperaba vacío", r.HotMetric)
	}
	if r.TempThreshold != 70 {
		t.Fatalf("TempThreshold resuelto=%d, esperaba 70", r.TempThreshold)
	}

	// 72 °C supera el umbral 70 → warn + hotMetric.
	p2 := &routerPolled{
		cfg:      RouterConfig{ID: "rt1", Name: "RT1", Host: "192.168.1.2", TempThreshold: iptr(70)},
		temp:     72,
		polledAt: 1000,
	}
	r2 := l.buildRouter(p2, nil)
	if r2.Status != "warn" {
		t.Fatalf("72 °C con umbral 70: status=%q, esperaba warn", r2.Status)
	}
	if r2.HotMetric != "temp" {
		t.Fatalf("72 °C con umbral 70: hotMetric=%q, esperaba temp", r2.HotMetric)
	}
}

func TestBuildRouterTempThresholdFallsBackToDefault(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	// Sin umbral configurado → 65. 66 °C dispara warn (comportamiento previo).
	p := &routerPolled{
		cfg:      RouterConfig{ID: "rt1", Name: "RT1", Host: "192.168.1.2"},
		temp:     66,
		polledAt: 1000,
	}
	r := l.buildRouter(p, nil)
	if r.Status != "warn" || r.HotMetric != "temp" {
		t.Fatalf("66 °C sin umbral: status=%q hotMetric=%q, esperaba warn/temp", r.Status, r.HotMetric)
	}
	if r.TempThreshold != DefaultTempThreshold {
		t.Fatalf("TempThreshold resuelto=%d, esperaba %d", r.TempThreshold, DefaultTempThreshold)
	}
}

func TestEmitTempAlertUsesPerRouterThreshold(t *testing.T) {
	l := liveTestLive()
	cfg := RouterConfig{ID: "patio", Name: "Patio", TempThreshold: iptr(70)}
	router := Router{Name: "Patio", Temp: iptr(72), TempThreshold: 70}

	l.mu.Lock()
	l.emitTempAlert(cfg, router)
	l.mu.Unlock()

	list := l.engine.List()
	if len(list) != 1 {
		t.Fatalf("72 °C con umbral 70: %d alertas, esperaba 1", len(list))
	}
	if !strings.Contains(list[0].Description, "70 °C") {
		t.Fatalf("la descripción no refleja el umbral del router: %q", list[0].Description)
	}
}

func TestEmitTempAlertNoAlertBelowThreshold(t *testing.T) {
	l := liveTestLive()
	cfg := RouterConfig{ID: "patio", Name: "Patio", TempThreshold: iptr(70)}
	router := Router{Name: "Patio", Temp: iptr(68), TempThreshold: 70}

	l.mu.Lock()
	l.emitTempAlert(cfg, router)
	l.mu.Unlock()

	if n := len(l.engine.List()); n != 0 {
		t.Fatalf("68 °C con umbral 70: %d alertas, esperaba 0", n)
	}
}

func TestEmitTempAlertFallsBackToDefault(t *testing.T) {
	l := liveTestLive()
	cfg := RouterConfig{ID: "patio", Name: "Patio"} // sin umbral → default 65
	router := Router{Name: "Patio", Temp: iptr(66), TempThreshold: DefaultTempThreshold}

	l.mu.Lock()
	l.emitTempAlert(cfg, router)
	l.mu.Unlock()

	list := l.engine.List()
	if len(list) != 1 {
		t.Fatalf("66 °C sin umbral: %d alertas, esperaba 1", len(list))
	}
	if !strings.Contains(list[0].Description, "65 °C") {
		t.Fatalf("la descripción no usa el default 65: %q", list[0].Description)
	}
}

func TestComputeHealthUsesPerRouterTempThreshold(t *testing.T) {
	// 72 °C con umbral 70 penaliza (-8); 68 °C con umbral 70 no.
	routers := []Router{
		{Name: "hot", Status: "online", Temp: iptr(72), TempThreshold: 70},
		{Name: "cool", Status: "online", Temp: iptr(68), TempThreshold: 70},
	}
	h := computeHealth(routers, nil)
	if h.Score != 92 {
		t.Fatalf("score=%d, esperaba 92", h.Score)
	}
}
