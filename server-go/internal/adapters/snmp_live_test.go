package adapters

import (
	"context"
	"fmt"
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
		cfg: cfg,
		cpu: 1, ram: 2,
		polledAt: 1000,
		net:      &NetDevBps{RxBps: &rx, TxBps: &tx},
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

// #930: 3 fallos seguidos con ping OK emiten UNA alerta warn "snmp-failing";
// la recuperación la resuelve y emite la ok; los contadores son correctos.
func TestSnmpPollHealthAlertAndRecovery(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	// Mock del ping: siempre alcanzable (switch vivo con SNMP roto).
	l.ping = func(host string) bool { return true }
	cfg := RouterConfig{ID: "sw1", Name: "Switch 1", Host: "192.168.1.10", SnmpEnabled: true}
	boom := fmt.Errorf("snmp ifTable: timeout")

	for i := 0; i < 3; i++ {
		l.recordSnmpFailure(cfg, boom)
	}
	n, ev := findAlerts(l.engine, "SNMP polling failing on Switch 1")
	if n != 1 {
		t.Fatalf("snmp-failing alerts = %d, want 1", n)
	}
	if ev.Type != alerts.HintSnmpFailing || ev.Vars["router"] != "Switch 1" || ev.Vars["error"] == "" {
		t.Fatalf("evento snmp-failing mal formado: %+v", ev)
	}
	// Un 4.º fallo no repite la alerta (una por incidente).
	l.recordSnmpFailure(cfg, boom)
	if n2, _ := findAlerts(l.engine, "SNMP polling failing on Switch 1"); n2 != 1 {
		t.Fatalf("snmp-failing alerts tras 4.º fallo = %d, want 1", n2)
	}

	l.mu.Lock()
	st := l.snmpPollStats["sw1"]
	l.mu.Unlock()
	if st == nil || st.Fail != 4 || st.ConsecFail != 4 || st.Ok != 0 {
		t.Fatalf("contadores tras fallos = %+v, want fail=4 consec=4 ok=0", st)
	}

	// Recuperación: resolve de la warn + alerta ok.
	l.recordSnmpSuccess(cfg)
	if n3, _ := findAlerts(l.engine, "SNMP polling failing on Switch 1"); n3 != 0 {
		t.Fatalf("snmp-failing tras recuperación = %d, want 0 (resuelta)", n3)
	}
	if n4, _ := findAlerts(l.engine, "SNMP polling recovered on Switch 1"); n4 != 1 {
		t.Fatalf("snmp-recovered alerts = %d, want 1", n4)
	}
	l.mu.Lock()
	st = l.snmpPollStats["sw1"]
	l.mu.Unlock()
	if st.Ok != 1 || st.ConsecFail != 0 || st.Fail != 4 {
		t.Fatalf("contadores tras recuperación = %+v, want ok=1 consec=0 fail=4", st)
	}
}

// #930: si el ping también falla, NO se emite la alerta SNMP (el equipo está
// caído; lo cubre el alerteo de offline). Solo log y contadores.
func TestSnmpPollHealthNoAlertWhenPingFails(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	l.ping = func(host string) bool { return false }
	cfg := RouterConfig{ID: "sw1", Name: "Switch 1", Host: "192.168.1.10", SnmpEnabled: true}
	for i := 0; i < 3; i++ {
		l.recordSnmpFailure(cfg, fmt.Errorf("snmp connect: timeout"))
	}
	if n, _ := findAlerts(l.engine, "SNMP polling failing on Switch 1"); n != 0 {
		t.Fatalf("snmp-failing alerts con ping caído = %d, want 0", n)
	}
	l.mu.Lock()
	st := l.snmpPollStats["sw1"]
	l.mu.Unlock()
	if st == nil || st.ConsecFail != 3 || st.Fail != 3 {
		t.Fatalf("contadores con ping caído = %+v, want consec=3 fail=3", st)
	}
}

// #1489: la MAC que el switch aprende en su propio puerto interno/CPU es del
// equipo, no de un cliente. Antes salía como cliente fantasma "CPU node".
func TestSnmpFdbMapDropsInternalCPUPort(t *testing.T) {
	fdb := []npsnmp.FdbEntry{
		{MAC: "AA:BB:CC:00:00:01", IfIndex: 1},
		{MAC: "08:BD:43:75:50:CD", IfIndex: 9}, // CPU MAC del propio switch
		{MAC: "AA:BB:CC:00:00:02", IfIndex: 10},
	}
	names := map[int]string{1: "1/0/1", 9: "CPU Interface:  3/1", 10: "Internal stack"}
	m := snmpFdbMap(fdb, names)
	if _, ok := m["08:BD:43:75:50:CD"]; ok {
		t.Fatalf("la MAC del puerto CPU no debe entrar en el FDB: %v", m)
	}
	if _, ok := m["AA:BB:CC:00:00:02"]; ok {
		t.Fatalf("la MAC del puerto interno no debe entrar en el FDB: %v", m)
	}
	if m["AA:BB:CC:00:00:01"] != "1/0/1" {
		t.Fatalf("el puerto normal se perdió: %v", m)
	}
}

// #1489: las MACs aprendidas en los puertos internos son identidad del propio
// switch; se absorben también cuando las ve un vecino (br-lan del gateway).
func TestSnmpSelfMacs(t *testing.T) {
	fdb := []npsnmp.FdbEntry{
		{MAC: "AA:BB:CC:00:00:01", IfIndex: 1},
		{MAC: "08:BD:43:75:50:CD", IfIndex: 9},
		{MAC: "08:BD:43:75:50:CF", IfIndex: 9},
	}
	names := map[int]string{1: "1/0/1", 9: "CPU Interface:  3/1"}
	got := snmpSelfMacs(fdb, names)
	if len(got) != 2 {
		t.Fatalf("selfMacs %v, want las dos del puerto CPU", got)
	}
	for _, mac := range got {
		if mac == "AA:BB:CC:00:00:01" {
			t.Fatalf("una MAC de puerto normal no es identidad: %v", got)
		}
	}
	// Esa identidad se absorbe aunque otro miembro (el gateway) la aprenda.
	l := NewLive(nil, nil, nil, nil)
	polled := map[string]*routerPolled{
		"sw": {cfg: RouterConfig{ID: "sw", Type: "managed-switch"}, selfMacs: []string{"08:BD:43:75:50:CF"}},
		"gw": {cfg: RouterConfig{ID: "gw", IsGateway: true}, fdb: map[string]string{
			"08:BD:43:75:50:CF": "br-lan",
			"AA:BB:CC:00:00:09": "br-lan",
		}},
	}
	devs := l.buildDevices(polled, "")
	if len(devs) != 1 || devs[0].MAC != "AA:BB:CC:00:00:09" {
		t.Fatalf("devices: %+v, want solo el cliente real", devs)
	}
}

// #1725: HOST-RESOURCES es respaldo, no segunda opinión. Donde la MIB privada
// ya leyó, la genérica no pisa; donde no leyó nada, rellena; y donde el equipo
// no publica el árbol, no escribe ni un cero (#441).
func TestApplyHostResourcesVitalsSoloRellenaHuecos(t *testing.T) {
	// Netgear FastPath ya dio CPU (suma de procesos). hrProcessorLoad diría
	// otro número midiendo otra cosa: el panel del equipo muestra el primero.
	p := &routerPolled{cpu: 7, hasCpu: true}
	applyHostResourcesVitals(p, 73, true, 40, true)
	if p.cpu != 7 || !p.hasCpu {
		t.Errorf("cpu = %d/%v, want 7/true (la lectura privada manda)", p.cpu, p.hasCpu)
	}
	if p.ram != 40 || !p.hasRam {
		t.Errorf("ram = %d/%v, want 40/true (la RAM sí era hueco)", p.ram, p.hasRam)
	}

	// RouterOS por SNMP: ninguna MIB privada, las dos entran.
	r := &routerPolled{}
	applyHostResourcesVitals(r, 1, true, 17, true)
	if r.cpu != 1 || !r.hasCpu || r.ram != 17 || !r.hasRam {
		t.Errorf("routeros = cpu %d/%v ram %d/%v, want 1/true 17/true", r.cpu, r.hasCpu, r.ram, r.hasRam)
	}

	// FN SWITCH 8 PRO: no publica HOST-RESOURCES. Sin bandera no se escribe
	// nada y la tarjeta sigue mostrando null, no un 0 %.
	f := &routerPolled{}
	applyHostResourcesVitals(f, 0, false, 0, false)
	if f.hasCpu || f.hasRam || f.cpu != 0 || f.ram != 0 {
		t.Errorf("sin árbol genérico = %+v, want sin banderas y ceros intactos", f)
	}

	applyHostResourcesVitals(nil, 5, true, 5, true) // sin payload: no panica
}

// #1725: la pregunta por HOST-RESOURCES se hace una vez por equipo y se
// recuerda TAMBIÉN cuando la respuesta fue negativa. Un agente que descarta el
// árbol sin responder cuesta DefaultTimeout×(Retries+1)=10 s por operación;
// sin cache eso se pagaría en cada poll (snmp_poll_interval, 60 s por
// defecto) de forma indefinida.
func TestHostResourcesKnownCachesBothAnswers(t *testing.T) {
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	calls := 0
	l := NewLive(nil, nil, nil, nil)
	l.now = func() time.Time { return base }
	probe := func() bool { calls++; return false }

	for i := 0; i < 5; i++ {
		if l.hostResourcesKnown("sw1", probe) {
			t.Fatal("el agente dijo que no; no puede salir true")
		}
		base = base.Add(time.Minute)
		l.now = func() time.Time { return base }
	}
	if calls != 1 {
		t.Fatalf("sondeos = %d, want 1 (la negativa se cachea igual que la afirmativa)", calls)
	}

	// Pasado el TTL se vuelve a preguntar: un firmware que añada el árbol se
	// recoge sin reiniciar el server.
	base = base.Add(snmpHrProbeTTL)
	if l.hostResourcesKnown("sw1", probe) {
		t.Fatal("sigue sin publicar el árbol")
	}
	if calls != 2 {
		t.Fatalf("sondeos tras el TTL = %d, want 2", calls)
	}

	// Es por equipo, no global.
	l.now = func() time.Time { return base }
	otras := 0
	if !l.hostResourcesKnown("sw2", func() bool { otras++; return true }) {
		t.Fatal("sw2 sí publica y se descartó")
	}
	if otras != 1 {
		t.Errorf("sondeos de sw2 = %d, want 1", otras)
	}
	if calls != 2 {
		t.Errorf("sw1 no debe repetir su sondeo (%d)", calls)
	}
}
