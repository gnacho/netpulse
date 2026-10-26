package adapters

import (
	"context"
	"testing"
)

// #1568: una fuente SNMP puede tener solo CPU o solo temperatura. La lectura
// presente debe llegar al contrato y marcar las vitals como disponibles; las
// ausentes siguen siendo nil para que la UI no invente valores.
func TestBuildRouterExposesPartialSnmpVitals(t *testing.T) {
	tests := []struct {
		name              string
		cpu, temp         int
		hasCPU, hasTemp   bool
		wantCPU, wantTemp *int
		wantAvailable     bool
	}{
		{name: "cpu only", cpu: 37, hasCPU: true, wantCPU: intPtr(37), wantAvailable: true},
		{name: "temp only", temp: 51, hasTemp: true, wantTemp: intPtr(51), wantAvailable: true},
		{name: "cpu and temp", cpu: 22, temp: 48, hasCPU: true, hasTemp: true, wantCPU: intPtr(22), wantTemp: intPtr(48), wantAvailable: true},
		{name: "no readings", cpu: 90, temp: 90, wantAvailable: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := NewLive(nil, nil, nil, nil)
			p := &routerPolled{
				cfg:      RouterConfig{ID: "sw1", Host: "192.168.1.10", SnmpEnabled: true},
				cpu:      tc.cpu,
				temp:     tc.temp,
				hasCpu:   tc.hasCPU,
				hasTemp:  tc.hasTemp,
				polledAt: 1000,
			}
			r := l.buildRouter(p, nil)
			if r.VitalsAvailable == nil || *r.VitalsAvailable != tc.wantAvailable {
				t.Fatalf("VitalsAvailable=%v, esperaba %v", r.VitalsAvailable, tc.wantAvailable)
			}
			if !equalOptionalInt(r.CPU, tc.wantCPU) || !equalOptionalInt(r.Temp, tc.wantTemp) || r.RAM != nil {
				t.Fatalf("vitals: cpu=%v (quiere %v) ram=%v temp=%v (quiere %v)", r.CPU, tc.wantCPU, r.RAM, r.Temp, tc.wantTemp)
			}
		})
	}
}

// #1568: el histórico usa los mismos flags que la tarjeta. Antes limpiaba CPU
// y temperatura para cualquier fuente SNMP aunque el poller sí las tuviera.
func TestMetricsRowsKeepPartialSnmpVitals(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	l.lastPolled["sw1"] = &routerPolled{
		cfg:      RouterConfig{ID: "sw1", Host: "192.168.1.10", SnmpEnabled: true},
		cpu:      37,
		temp:     51,
		hasCpu:   true,
		hasTemp:  true,
		polledAt: 1000,
	}
	l.lastPolled["sw2"] = &routerPolled{
		cfg:      RouterConfig{ID: "sw2", Host: "192.168.1.11", SnmpEnabled: true},
		temp:     49,
		hasTemp:  true,
		polledAt: 1000,
	}

	rows := l.GetMetricsRows(context.Background())
	if len(rows) != 2 {
		t.Fatalf("rows=%d, esperaba 2", len(rows))
	}
	byID := map[string]MetricsRow{}
	for _, row := range rows {
		byID[row.RouterID] = row
	}
	row := byID["sw1"]
	if row.CPU == nil || *row.CPU != 37 || row.Temp == nil || *row.Temp != 51 || row.RAM != nil {
		t.Fatalf("sw1: cpu=%v ram=%v temp=%v", row.CPU, row.RAM, row.Temp)
	}
	row = byID["sw2"]
	if row.CPU != nil || row.Temp == nil || *row.Temp != 49 || row.RAM != nil {
		t.Fatalf("sw2: cpu=%v ram=%v temp=%v", row.CPU, row.RAM, row.Temp)
	}
}

func intPtr(v int) *int { return &v }

func equalOptionalInt(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
