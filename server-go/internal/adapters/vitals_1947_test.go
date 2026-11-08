// vitals_1947_test.go - una lectura no confirmada no vale cero: la grafica de
// temperatura del WNDR3700v2 salia plana a 0 C porque el poll guardaba ceros
// de relleno cuando el sensor no existia (#1947).
package adapters

import (
	"context"
	"testing"
)

func TestBuildRouterDropsUnconfirmedTemp(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "rt1", Name: "test-gw", Host: "192.168.1.50"},
	}, nil)

	// Unidad sin sensor: temp 0 y hasTemp false.
	p := &routerPolled{cfg: RouterConfig{ID: "rt1", Name: "test-gw"}, temp: 0, hasTemp: false}
	r := l.buildRouter(p, nil)
	if r.Temp != nil {
		t.Fatalf("Temp = %v, esperaba nil sin lectura confirmada", *r.Temp)
	}

	// Sensor real: se conserva el valor.
	p2 := &routerPolled{cfg: RouterConfig{ID: "rt1", Name: "test-gw"}, temp: 43, hasTemp: true}
	r2 := l.buildRouter(p2, nil)
	if r2.Temp == nil || *r2.Temp != 43 {
		t.Fatalf("Temp = %v, esperaba 43", r2.Temp)
	}
}

func TestMetricsRowsDropUnconfirmedVitals(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	l.lastPolled = map[string]*routerPolled{
		"no-sensor": {cfg: RouterConfig{ID: "no-sensor"}, cpu: 0, ram: 0, temp: 0},
		"with-sensor": {cfg: RouterConfig{ID: "with-sensor"},
			cpu: 12, hasCpu: true, ram: 40, hasRam: true, temp: 43, hasTemp: true},
	}

	rows := l.GetMetricsRows(context.Background())
	if len(rows) != 2 {
		t.Fatalf("filas: %d", len(rows))
	}
	for _, row := range rows {
		switch row.RouterID {
		case "no-sensor":
			if row.Temp != nil || row.CPU != nil || row.RAM != nil {
				t.Fatalf("sin sensor se persistieron ceros: %+v", row)
			}
		case "with-sensor":
			if row.Temp == nil || *row.Temp != 43 {
				t.Fatalf("temp confirmada: %+v", row.Temp)
			}
			if row.CPU == nil || *row.CPU != 12 || row.RAM == nil || *row.RAM != 40 {
				t.Fatalf("vitals confirmadas: %+v", row)
			}
		}
	}
}
