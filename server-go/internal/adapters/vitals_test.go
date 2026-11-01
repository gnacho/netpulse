package adapters

import (
	"context"
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
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

// Un pusher externo (routeros/asuswrt) declara CPU, memoria y temperatura en
// el payload: son lecturas confirmadas, no huecos que pintar a cero. Antes solo
// el sondeo SNMP ponía los flags, así que el null de #441 se llevaba las tres
// vitals del equipo alimentado por pusher.
func TestBuildRouterKeepsConfirmedPusherVitals(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	cfg := RouterConfig{ID: "crs305", Host: "192.168.2.15", Name: "CRS305", Type: "external", AgentOnly: true}
	cpu, temp := 1, 48
	si := &probe.SysInfo{Uptime: 253000}
	si.Memory.Total = 536870912
	si.Memory.Free = 446193664
	payload := &probe.Payload{
		Router: "crs305", Ts: 1, Kind: "external", Version: "0.1.1",
		Data: probe.PayloadData{System: &probe.SystemData{CPU: &cpu, Temp: &temp, SysInfo: si}},
	}

	p := l.polledFromAgent(cfg, payload)
	r := l.buildRouter(p, nil)
	if r.CPU == nil || *r.CPU != cpu {
		t.Fatalf("CPU=%v, esperaba %d", r.CPU, cpu)
	}
	if r.Temp == nil || *r.Temp != temp {
		t.Fatalf("Temp=%v, esperaba %d", r.Temp, temp)
	}
	if r.RAM == nil || *r.RAM != p.ram {
		t.Fatalf("RAM=%v, esperaba %d (Mem.Total/Free del payload)", r.RAM, p.ram)
	}
	if r.VitalsAvailable == nil || !*r.VitalsAvailable {
		t.Fatalf("VitalsAvailable=%v, esperaba true", r.VitalsAvailable)
	}

	l.lastPolled[cfg.ID] = p
	rows := l.GetMetricsRows(context.Background())
	if len(rows) != 1 {
		t.Fatalf("rows=%d, esperaba 1", len(rows))
	}
	row := rows[0]
	if row.CPU == nil || *row.CPU != float64(cpu) || row.Temp == nil || *row.Temp != float64(temp) {
		t.Fatalf("histórico: cpu=%v temp=%v", row.CPU, row.Temp)
	}
	if row.RAM == nil || *row.RAM != float64(p.ram) {
		t.Fatalf("histórico: ram=%v, esperaba %d", row.RAM, p.ram)
	}
}

// #441 sigue vivo: un beacon externo sin sección system no inventa vitals.
func TestBuildRouterPusherSinSystemSigueSinVitals(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	cfg := RouterConfig{ID: "beacon", Host: "192.168.2.99", Name: "Beacon", Type: "external", AgentOnly: true}
	p := l.polledFromAgent(cfg, &probe.Payload{Router: "beacon", Ts: 1, Kind: "external"})
	r := l.buildRouter(p, nil)
	if r.CPU != nil || r.RAM != nil || r.Temp != nil {
		t.Fatalf("sin sección system las vitals siguen nil: cpu=%v ram=%v temp=%v", r.CPU, r.RAM, r.Temp)
	}
	if r.VitalsAvailable == nil || *r.VitalsAvailable {
		t.Fatalf("VitalsAvailable=%v, esperaba false", r.VitalsAvailable)
	}
}

// extrasCache se clave solo por ID: reasignar un router de un agente nativo a
// un pusher que no trae seccion system no debe publicar las vitales que
// confirmo la fuente anterior. Sin la etiqueta de origen, #1724 las marcaria
// como lecturas propias del pusher y acabarian tambien en el historico.
func TestBuildRouterDoesNotCrossTheAgentSourceBoundary(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	cfg := RouterConfig{ID: "rt-ax92u", Host: "192.168.3.6", Name: "AX92U", Type: "external", AgentOnly: true}
	cpu, temp := 7, 61
	nativeSys := &probe.SysInfo{Uptime: 100000}
	nativeSys.Memory.Total = 536870912
	nativeSys.Memory.Free = 400000000
	l.polledFromAgent(cfg, &probe.Payload{
		Router: cfg.ID, Ts: 1, Kind: "native", Version: "3.0.30",
		Data: probe.PayloadData{System: &probe.SystemData{CPU: &cpu, Temp: &temp, SysInfo: nativeSys}},
	})

	// El router pasa a un pusher externo cuyo payload no trae seccion system
	// (REST, o un scraper que todavia no llega a esa parte).
	external := l.polledFromAgent(cfg, &probe.Payload{
		Router: cfg.ID, Ts: 2, Kind: "external", Version: "0.1.1",
	})
	if external.cpu != 0 || external.temp != 0 || external.ram != 0 {
		t.Fatalf("vitals heredadas de la otra fuente: cpu=%d temp=%d ram=%d", external.cpu, external.temp, external.ram)
	}
	if external.hasCpu || external.hasTemp || external.hasRam {
		t.Fatalf("los flags de confirmacion no pueden venir de otra fuente: %+v", external)
	}
	r := l.buildRouter(external, nil)
	if r.CPU != nil || r.Temp != nil || r.RAM != nil {
		t.Fatalf("cpu=%v temp=%v ram=%v, esperaba nil en la tarjeta del router", r.CPU, r.Temp, r.RAM)
	}
	if r.VitalsAvailable == nil || *r.VitalsAvailable {
		t.Fatalf("VitalsAvailable=%v, esperaba false", r.VitalsAvailable)
	}

	l.lastPolled[cfg.ID] = external
	rows := l.GetMetricsRows(context.Background())
	if len(rows) != 1 {
		t.Fatalf("rows=%d, esperaba 1", len(rows))
	}
	if rows[0].CPU != nil || rows[0].Temp != nil || rows[0].RAM != nil {
		t.Fatalf("historico con vitales de otra fuente: cpu=%v temp=%v ram=%v",
			rows[0].CPU, rows[0].Temp, rows[0].RAM)
	}

	// La proteccion no es "un externo nunca tiene nada": su propia seccion
	// system se publica, y cuando el propio externo confirma vitales el
	// antiparpadeo #441 sigue reciclándolas dentro de la misma fuente.
	boardOnly := l.polledFromAgent(cfg, &probe.Payload{
		Router: cfg.ID, Ts: 3, Kind: "external", Version: "0.1.1",
		Data: probe.PayloadData{System: &probe.SystemData{Board: &probe.BoardInfo{Model: "CRS305-1G-4S+"}}},
	})
	if rb := l.buildRouter(boardOnly, nil); rb.Model != "CRS305-1G-4S+" || rb.CPU != nil {
		t.Fatalf("board propio del externo: model=%q cpu=%v", rb.Model, rb.CPU)
	}
	confirmed := l.polledFromAgent(cfg, &probe.Payload{
		Router: cfg.ID, Ts: 4, Kind: "external", Version: "0.1.1",
		Data: probe.PayloadData{System: &probe.SystemData{CPU: &cpu}},
	})
	if !confirmed.hasCpu || confirmed.cpu != cpu {
		t.Fatalf("cpu=%d hasCpu=%v, el pusher confirmó 7", confirmed.cpu, confirmed.hasCpu)
	}
	recycled := l.polledFromAgent(cfg, &probe.Payload{
		Router: cfg.ID, Ts: 5, Kind: "external", Version: "0.1.1",
	})
	if !recycled.hasCpu || recycled.cpu != cpu {
		t.Fatalf("cpu=%d hasCpu=%v, dentro de la misma fuente el antiparpadeo #441 debe seguir vivo",
			recycled.cpu, recycled.hasCpu)
	}
}

// Un payload que solo trae Mem.Total, sin ninguna cifra de uso, no permite
// derivar nada: memUsagePct cae a "available = free + buffered = 0" y daria un
// 100 % de RAM inventado. Memoria desconocida, no memoria llena. Las demas
// vitales del mismo payload siguen confirmadas.
func TestBuildRouterTotalOnlyMemoryIsUnknown(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	cfg := RouterConfig{ID: "bbox", Host: "192.168.1.1", Name: "Bbox", Type: "external", AgentOnly: true}
	cpu, temp := 3, 40
	totalOnly := &probe.SysInfo{Uptime: 1000}
	totalOnly.Memory.Total = 100
	p := l.polledFromAgent(cfg, &probe.Payload{
		Router: cfg.ID, Ts: 1, Kind: "external", Version: "0.1.1",
		Data: probe.PayloadData{System: &probe.SystemData{CPU: &cpu, Temp: &temp, SysInfo: totalOnly}},
	})
	if p.ram != 0 || p.hasRam {
		t.Fatalf("ram=%d hasRam=%v, con solo el total la memoria es desconocida", p.ram, p.hasRam)
	}
	r := l.buildRouter(p, nil)
	if r.RAM != nil {
		t.Fatalf("RAM=%v, esperaba nil (no 100)", r.RAM)
	}
	if r.CPU == nil || *r.CPU != cpu || r.Temp == nil || *r.Temp != temp {
		t.Fatalf("cpu=%v temp=%v, las otras vitales confirmadas no se tocan", r.CPU, r.Temp)
	}
	if r.VitalsAvailable == nil || !*r.VitalsAvailable {
		t.Fatalf("VitalsAvailable=%v, esperaba true", r.VitalsAvailable)
	}

	l.lastPolled[cfg.ID] = p
	rows := l.GetMetricsRows(context.Background())
	if len(rows) != 1 {
		t.Fatalf("rows=%d, esperaba 1", len(rows))
	}
	if rows[0].RAM != nil {
		t.Fatalf("historico: ram=%v, esperaba NULL", *rows[0].RAM)
	}
	if rows[0].CPU == nil || rows[0].Temp == nil {
		t.Fatalf("historico: cpu=%v temp=%v, esperando los valores confirmados", rows[0].CPU, rows[0].Temp)
	}

	// Con una sola cifra de uso ya es derivable: free = 40 de 100 es 60 %.
	WithFree := &probe.SysInfo{Uptime: 1000}
	WithFree.Memory.Total = 100
	WithFree.Memory.Free = 40
	p2 := l.polledFromAgent(cfg, &probe.Payload{
		Router: cfg.ID, Ts: 2, Kind: "external", Version: "0.1.1",
		Data: probe.PayloadData{System: &probe.SystemData{SysInfo: WithFree}},
	})
	if !p2.hasRam || p2.ram != 60 {
		t.Fatalf("ram=%d hasRam=%v, esperaba 60 confirmado", p2.ram, p2.hasRam)
	}
}

func intPtr(v int) *int { return &v }

func equalOptionalInt(a, b *int) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
