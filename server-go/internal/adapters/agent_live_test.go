// agent_live_test.go — Adapter live-agent (SPEC-AGENTE-PILOTO §1): payload
// fresco = estado del router sin SSH; expiración → degrade SSH + alerta
// category system urgent=false; vuelta del agente → alerta ok de recuperación.
package adapters

import (
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/alerts"
	"github.com/gnacho/netpulse/server-go/internal/sshkey"
)

func f64(v float64) *float64 { return &v }
func i(v int) *int           { return &v }

// testPayload es un push completo del AP "patio" (shapes de agent/probe).
func testPayload() *probe.Payload {
	pl := &probe.Payload{Router: "patio", Ts: time.Now().Unix(), Version: "0.1.0"}
	sys := &probe.SysInfo{Uptime: 90061}
	sys.Memory.Total = 256e6
	sys.Memory.Available = 128e6
	pl.Data.System = &probe.SystemData{
		SysInfo: sys,
		Board:   &probe.BoardInfo{Model: "TP-Link EAP225", Hostname: "patio"},
		CPU:     i(12), Temp: i(43),
		RxBps: f64(1e6), TxBps: f64(2e6),
		LatencyMs: f64(3), Backhaul: "cable",
		BridgeMAC: "94:83:C4:00:00:09",
	}
	pl.Data.Wireless = &probe.WirelessData{
		Clients: map[string]probe.WirelessClient{
			"EC:71:DB:44:12:8A": {SignalDbm: -55, Band: "2.4 GHz"},
		},
		Radios: []probe.Radio{{Name: "2.4 GHz", Channel: 6, WidthMhz: 20, PowerDbm: 20, Clients: 1}},
	}
	pl.Data.DHCP = &probe.DHCPData{
		Leases: []probe.DhcpLease{
			{MAC: "EC:71:DB:44:12:8A", IP: "192.168.8.71", Hostname: "movil"},
		},
		// gl-clients (GL.iNet, issue #5 bug 1): superset que resuelve IPs
		// sin lease — debe llegar a routerPolled.glClients por la ruta agente.
		GlClients: []probe.DhcpLease{
			{MAC: "AA:BB:CC:00:00:01", IP: "192.168.8.99"},
		},
	}
	pl.Data.FDB = &probe.FDBData{
		MACs:  map[string]string{"EC:71:DB:44:12:8A": "lan1"},
		Ports: []probe.EthPort{{ID: "lan1", Label: "LAN 1", Up: true, Speed: "1 Gbps"}},
	}
	// LuCI (issue #258): etiquetas de puertos/VLANs para la topología.
	pl.Data.LuCI = &probe.LuCILabels{
		PortLabels: map[string]string{"lan1": "Router/Fritzbox"},
		VlanLabels: map[string]string{"1": "LAN"},
	}
	return pl
}

func TestAgentRegistry(t *testing.T) {
	reg := NewAgentRegistry(90 * time.Second)
	now := time.Now()
	reg.SetClock(func() time.Time { return now })

	if _, ok := reg.Fresh("patio"); ok {
		t.Fatal("sin estado no debería haber payload fresco")
	}
	reg.Ingest(testPayload())
	p, ok := reg.Fresh("patio")
	if !ok || p.Version != "0.1.0" {
		t.Fatalf("payload fresco esperado: %v %v", ok, p)
	}
	seen, ver, _, _, ok := reg.Info("patio")
	if !ok || ver != "0.1.0" || !seen.Equal(now) {
		t.Fatalf("Info: %v %q %v", seen, ver, ok)
	}
	now = now.Add(91 * time.Second)
	if _, ok := reg.Fresh("patio"); ok {
		t.Fatal("tras el TTL no debería estar fresco")
	}
	if !reg.Expired("patio") {
		t.Fatal("tras el TTL debería estar expirado")
	}
	reg.Forget("patio")
	if reg.Expired("patio") {
		t.Fatal("Forget borra el estado")
	}
}

// newLiveAgentTest: Live con pool SSH real (clave efímera) apuntando a un
// puerto cerrado — el fallback SSH falla al instante (connection refused).
func newLiveAgentTest(t *testing.T, reg *AgentRegistry) *Live {
	t.Helper()
	keyPath := t.TempDir() + "/id_ed25519"
	if err := sshkey.EnsureKeypair(keyPath); err != nil {
		t.Fatalf("ssh key: %v", err)
	}
	pool, err := NewSSHPool(keyPath)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "patio", Name: "Patio", Host: "127.0.0.1", Type: "openwrt"},
	}, pool)
	l.SetAgents(reg)
	// Tests legacy: confirmación inmediata (sin Dead Man's Switch delay).
	l.SetAgentDownConfirm(time.Millisecond)
	return l
}

func findAlert(list []AlertEvent, titlePart string) *AlertEvent {
	for i, a := range list {
		if strings.Contains(a.Title, titlePart) {
			return &list[i]
		}
	}
	return nil
}

func TestLiveAgentFreshSkipsSSH(t *testing.T) {
	reg := NewAgentRegistry(90 * time.Second)
	l := newLiveAgentTest(t, reg)
	reg.Ingest(testPayload())

	p, err := l.pollRouter(t.Context(), RouterConfig{ID: "patio", Name: "Patio", Host: "127.0.0.1"})
	if err != nil {
		t.Fatalf("pollRouter con agente fresco: %v", err)
	}
	// Datos del payload, no de SSH (el host es inalcanzable por SSH)
	if p.cpu != 12 || p.temp != 43 || p.ram != 50 || p.uptimeSec != 90061 {
		t.Fatalf("system: cpu=%d temp=%d ram=%d uptime=%v", p.cpu, p.temp, p.ram, p.uptimeSec)
	}
	if p.net == nil || p.net.RxBps == nil || *p.net.RxBps != 1e6 {
		t.Fatalf("net: %+v", p.net)
	}
	if len(p.leases) != 1 || p.leases[0].Hostname != "movil" {
		t.Fatalf("leases: %+v", p.leases)
	}
	// gl-clients del agente deben propagarse a routerPolled (issue #5 bug 1)
	if len(p.glClients) != 1 || p.glClients[0].IP != "192.168.8.99" {
		t.Fatalf("glClients vía agente: %+v", p.glClients)
	}
	if p.wireless["EC:71:DB:44:12:8A"].SignalDbm != -55 {
		t.Fatalf("wireless: %+v", p.wireless)
	}
	if p.fdb["EC:71:DB:44:12:8A"] != "lan1" || len(p.ports) != 1 || p.ports[0].Speed != "1 Gbps" {
		t.Fatalf("fdb/ports: %+v %+v", p.fdb, p.ports)
	}
	// LuCI del agente (issue #258): etiquetas propagadas al routerPolled.
	if p.luci == nil || p.luci.PortLabels["lan1"] != "Router/Fritzbox" || p.luci.VlanLabels["1"] != "LAN" {
		t.Fatalf("luci vía agente: %+v", p.luci)
	}
	if len(p.radios) != 1 || p.radios[0].Name != "2.4 GHz" || p.brMac != "94:83:C4:00:00:09" || p.backhaul != "cable" {
		t.Fatalf("radios/brMac/backhaul: %+v %q %q", p.radios, p.brMac, p.backhaul)
	}
	if p.board == nil || p.board.Model != "TP-Link EAP225" || p.latencyMs == nil || *p.latencyMs != 3 {
		t.Fatalf("board/latency: %+v %v", p.board, p.latencyMs)
	}
}

func TestLiveAgentFallbackAndRecovery(t *testing.T) {
	reg := NewAgentRegistry(50 * time.Millisecond)
	l := newLiveAgentTest(t, reg)
	cfg := RouterConfig{ID: "patio", Name: "Patio", Host: "127.0.0.1"}
	reg.Ingest(testPayload())

	if _, err := l.pollRouter(t.Context(), cfg); err != nil {
		t.Fatalf("agente fresco: %v", err)
	}
	// Expira el agente → degrade a SSH (falla: host inalcanzable) + alerta
	time.Sleep(60 * time.Millisecond)
	if _, err := l.pollRouter(t.Context(), cfg); err == nil {
		t.Fatal("con agente expirado debería intentar SSH y fallar (127.0.0.1 sin sshd)")
	}
	down := findAlert(l.engine.List(), "Agente caído en Patio")
	if down == nil {
		t.Fatalf("alerta de caída esperada: %+v", l.engine.List())
	}
	if down.Category != alerts.CatSystem || down.Urgent || !strings.Contains(down.Title, "volviendo a SSH") {
		t.Fatalf("alerta caída: %+v", down)
	}
	// La alerta se emite UNA vez (no cada tick)
	if _, err := l.pollRouter(t.Context(), cfg); err == nil {
		t.Fatal("SSH sigue fallando")
	}
	if n := 0; true {
		for _, a := range l.engine.List() {
			if strings.Contains(a.Title, "Agente caído") {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("alerta de caída duplicada: %d", n)
		}
	}
	// Vuelve el agente → se retoma sin SSH y hay alerta ok de recuperación
	reg.Ingest(testPayload())
	if _, err := l.pollRouter(t.Context(), cfg); err != nil {
		t.Fatalf("agente recuperado: %v", err)
	}
	ok := findAlert(l.engine.List(), "Agente recuperado en Patio")
	if ok == nil || ok.Category != alerts.CatSystem || ok.Urgent || ok.Severity != "ok" {
		t.Fatalf("alerta recuperación: %+v", ok)
	}
}

func TestLiveAgentAntiFlicker(t *testing.T) {
	reg := NewAgentRegistry(90 * time.Second)
	l := newLiveAgentTest(t, reg)
	cfg := RouterConfig{ID: "patio", Name: "Patio", Host: "127.0.0.1"}
	reg.Ingest(testPayload())
	if _, err := l.pollRouter(t.Context(), cfg); err != nil {
		t.Fatalf("primer push: %v", err)
	}
	// Segundo push con secciones fallidas (nil) → conserva el último dato bueno
	p2 := &probe.Payload{Router: "patio", Ts: time.Now().Unix(), Version: "0.1.0"}
	p2.Data.System = testPayload().Data.System
	reg.Ingest(p2)
	p, err := l.pollRouter(t.Context(), cfg)
	if err != nil {
		t.Fatalf("segundo push: %v", err)
	}
	if len(p.wireless) != 1 || len(p.radios) != 1 || len(p.ports) != 1 || len(p.fdb) != 1 {
		t.Fatalf("anti-parpadeo: %+v %+v %+v %+v", p.wireless, p.radios, p.ports, p.fdb)
	}
}

// TestDeadMansSwitch verifica que la alerta de caída NO se dispara
// inmediatamente cuando el agente expira, sino solo tras el periodo de
// confirmación (agentDownConfirm). Un blip breve (< confirm) no genera
// alerta; el agente degrada a SSH silenciosamente y se recupera sin alerta.
func TestDeadMansSwitch(t *testing.T) {
	reg := NewAgentRegistry(50 * time.Millisecond)
	now := time.Now()
	reg.SetClock(func() time.Time { return now })

	l := newLiveAgentTest(t, reg)
	l.SetAgentDownConfirm(200 * time.Millisecond)
	cfg := RouterConfig{ID: "patio", Name: "Patio", Host: "127.0.0.1"}

	// Push inicial: agente fresco.
	reg.Ingest(testPayload())
	if _, err := l.pollRouter(t.Context(), cfg); err != nil {
		t.Fatalf("agente fresco: %v", err)
	}

	// Avanzar 60ms: agente expirado (TTL=50ms) pero NO confirmado (< 200ms).
	now = now.Add(60 * time.Millisecond)
	if _, err := l.pollRouter(t.Context(), cfg); err == nil {
		t.Fatal("con agente expirado debería intentar SSH y fallar")
	}
	if a := findAlert(l.engine.List(), "Agente caído"); a != nil {
		t.Fatalf("NO debería haber alerta de caída tras blip breve: %+v", a)
	}

	// Avanzar otros 200ms (total 260ms > 200ms confirm): ahora SÍ alerta.
	now = now.Add(200 * time.Millisecond)
	if _, err := l.pollRouter(t.Context(), cfg); err == nil {
		t.Fatal("SSH sigue fallando")
	}
	down := findAlert(l.engine.List(), "Agente caído en Patio")
	if down == nil {
		t.Fatal("debería haber alerta de caída tras confirmación")
	}

	// Recuperación: el agente vuelve a empujar → alerta ok.
	reg.Ingest(testPayload())
	if _, err := l.pollRouter(t.Context(), cfg); err != nil {
		t.Fatalf("agente recuperado: %v", err)
	}
	ok := findAlert(l.engine.List(), "Agente recuperado en Patio")
	if ok == nil {
		t.Fatal("debería haber alerta de recuperación")
	}
}

// TestDeadMansSwitchBlipNoAlert verifica que un blip breve (expira pero
// recupera antes del confirm) NO genera NINGUNA alerta.
func TestDeadMansSwitchBlipNoAlert(t *testing.T) {
	reg := NewAgentRegistry(50 * time.Millisecond)
	now := time.Now()
	reg.SetClock(func() time.Time { return now })

	l := newLiveAgentTest(t, reg)
	l.SetAgentDownConfirm(200 * time.Millisecond)
	cfg := RouterConfig{ID: "patio", Name: "Patio", Host: "127.0.0.1"}

	reg.Ingest(testPayload())
	l.pollRouter(t.Context(), cfg)

	// Blip: expira (60ms) pero recupera antes del confirm (200ms).
	now = now.Add(60 * time.Millisecond)
	l.pollRouter(t.Context(), cfg)
	reg.Ingest(testPayload())
	l.pollRouter(t.Context(), cfg)

	if a := findAlert(l.engine.List(), "Agente caído"); a != nil {
		t.Fatalf("blip breve no debería generar alerta de caída: %+v", a)
	}
	if a := findAlert(l.engine.List(), "Agente recuperado"); a != nil {
		t.Fatalf("blip breve no debería generar alerta de recuperación: %+v", a)
	}
}
