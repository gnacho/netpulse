// probe_test.go — parsers sobre fixtures de salidas reales (los MISMOS
// fixtures que internal/adapters/adapters_test.go del servidor: al compartir
// package, estos tests son el test cruzado agente↔servidor) + Build del
// prober local con runner fake.
package probe

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestParseProcStat(t *testing.T) {
	s, err := ParseProcStat("cpu  4705 356 584 3699 23 0 23 0 0 0\n")
	if err != nil {
		t.Fatal(err)
	}
	idle := float64(3699 + 23)
	total := idle + float64(4705+356+584+0+23)
	if s.Total != total || s.IdleAll != idle {
		t.Fatalf("procstat: %+v", s)
	}
	// Delta → porcentaje
	prev := CPUSample{Total: 1000, IdleAll: 500}
	cur := CPUSample{Total: 2000, IdleAll: 750}
	pct := CPUPercent(prev, cur)
	if pct == nil || *pct != 75 {
		t.Fatalf("cpu%%: %v", pct)
	}
	if CPUPercent(cur, prev) != nil {
		t.Fatal("contadores reseteados → nil")
	}
	if _, err := ParseProcStat("basura"); err == nil {
		t.Fatal("procstat basura debería dar error")
	}
}

func TestParseTempC(t *testing.T) {
	if v := ParseTempC("43500\n"); v == nil || *v != 44 {
		t.Fatalf("temp: %v", v)
	}
	if ParseTempC("") != nil || ParseTempC("nan") != nil {
		t.Fatal("temp inválida → nil")
	}
}

func TestParseNetDevYBps(t *testing.T) {
	out := "  eth0: 1000000    0    0    0    0     0          0         0   500000    0\n" +
		"    lo: 9999999    0    0    0    0     0          0         0  8888888    0\n" +
		"br-lan: 7000000    0    0    0    0     0          0         0  6000000    0\n" +
		"  lan1: 2000000    0    0    0    0     0          0         0  1000000    0\n"
	rx, tx := ParseNetDev(out)
	if rx != 3000000 || tx != 1500000 { // lo, br-lan excluidos
		t.Fatalf("netdev: %v/%v", rx, tx)
	}
	rxBps, txBps := NetDevBps(1000, 2000, 3000, 6000, 2)
	if rxBps == nil || *rxBps != 8000 || txBps == nil || *txBps != 16000 {
		t.Fatalf("bps: %v/%v", rxBps, txBps)
	}
	if a, b := NetDevBps(0, 0, 0, 0, 0); a != nil || b != nil {
		t.Fatal("dt=0 → nil")
	}
}

func TestParseNetDevIfaces(t *testing.T) {
	out := "  eth0: 1000000 5 2 0 0 0 0 0 500000 7 1 0 0 0 0 0\n" +
		"  lan1: 2000000 9 0 0 0 0 0 0 1000000 4 3 0 0 0 0 0\n" +
		"basura sin formato\n"
	ifaces := ParseNetDevIfaces(out)
	if len(ifaces) != 2 {
		t.Fatalf("ifaces: %+v", ifaces)
	}
	e, ok := ifaces["eth0"]
	if !ok || e.Rx != 1000000 || e.Tx != 500000 || e.RxErr != 2 || e.TxErr != 1 {
		t.Fatalf("eth0: %+v", e)
	}
	l, ok := ifaces["lan1"]
	if !ok || l.RxErr != 0 || l.TxErr != 3 {
		t.Fatalf("lan1: %+v", l)
	}
	if ParseNetDevIfaces("") == nil {
		t.Fatal("vacío → mapa no nil")
	}
}

func TestIfRates(t *testing.T) {
	prev := map[string]IfCounters{"lan1": {Rx: 1000, Tx: 2000}, "wan": {Rx: 500, Tx: 500}}
	cur := map[string]IfCounters{"lan1": {Rx: 3000, Tx: 6000}, "wan": {Rx: 400, Tx: 900}, "lan2": {Rx: 10, Tx: 10}}
	rates := IfRates(prev, cur, 2)
	if len(rates) != 3 {
		t.Fatalf("rates: %+v", rates)
	}
	l1 := rates["lan1"]
	if l1.RxBps == nil || *l1.RxBps != 8000 || l1.TxBps == nil || *l1.TxBps != 16000 {
		t.Fatalf("lan1 rates: %+v", l1)
	}
	// Contador reseteado (cur < prev) → 0, no negativo
	w := rates["wan"]
	if w.RxBps == nil || *w.RxBps != 0 || w.TxBps == nil || *w.TxBps != 1600 {
		t.Fatalf("wan reset: %+v", w)
	}
	// Iface nueva sin previa → sin rate
	if rates["lan2"].RxBps != nil || rates["lan2"].TxBps != nil {
		t.Fatalf("lan2 nueva: %+v", rates["lan2"])
	}
	// dt <= 0 → todo sin rate
	rates = IfRates(prev, cur, 0)
	if rates["lan1"].RxBps != nil {
		t.Fatal("dt=0 → nil rates")
	}
}

func TestBuildEthPortsConIfaces(t *testing.T) {
	layout := []PortLayout{{ID: "lan1", Name: "lan1", Label: "LAN 1", Role: "lan"}}
	states := []PortState{{Name: "lan1", Up: true, Speed: "1 Gbps"}}
	rxb, txb := 40e6, 12e6
	ifaces := map[string]IfRate{
		"lan1":  {IfCounters: IfCounters{Rx: 1000, Tx: 2000, RxErr: 3}, RxBps: &rxb, TxBps: &txb},
		"wlan0": {IfCounters: IfCounters{Rx: 99}}, // no es boca: se ignora
	}
	ports := BuildEthPorts(layout, states, nil, ifaces, "")
	if len(ports) != 1 {
		t.Fatalf("ports: %+v", ports)
	}
	p := ports[0]
	if p.Iface != "lan1" || p.RxBytes != 1000 || p.TxBytes != 2000 || p.RxErrs != 3 {
		t.Fatalf("stats: %+v", p)
	}
	if p.RxBps == nil || *p.RxBps != rxb || p.TxBps == nil || *p.TxBps != txb {
		t.Fatalf("rates: %+v", p)
	}
	// Fallback (sin layout) también enriquece
	ports = BuildEthPorts(nil, []PortState{{Name: "lan1", Up: true, Speed: "1 Gbps"}}, nil, ifaces, "")
	if len(ports) != 1 || ports[0].Iface != "lan1" || ports[0].RxBytes != 1000 {
		t.Fatalf("fallback stats: %+v", ports)
	}
}

// TestFmtSpeedMbps (#847): 2500 debe ser "2.5 Gbps", no truncar a "2 Gbps".
func TestFmtSpeedMbps(t *testing.T) {
	cases := map[int]string{
		10:    "10 Mbps",
		100:   "100 Mbps",
		1000:  "1 Gbps",
		2500:  "2.5 Gbps",
		5000:  "5 Gbps",
		10000: "10 Gbps",
	}
	for mbps, want := range cases {
		if got := fmtSpeedMbps(mbps); got != want {
			t.Errorf("fmtSpeedMbps(%d) = %q, want %q", mbps, got, want)
		}
	}
}

// TestParsePortStatesDSA (#847): el 4º campo marca el conduit DSA.
func TestParsePortStatesDSA(t *testing.T) {
	states := ParsePortStates("eth1 up 2500 dsa\nlan1 up 1000 -\nsfp-wan up 2500\n")
	if len(states) != 3 {
		t.Fatalf("states: %+v", states)
	}
	if !states[0].DSA || states[0].Speed != "2.5 Gbps" {
		t.Fatalf("conduit eth1: %+v", states[0])
	}
	if states[1].DSA {
		t.Fatalf("lan1 no es conduit: %+v", states[1])
	}
	if states[2].DSA {
		t.Fatalf("línea sin 4º campo no es conduit: %+v", states[2])
	}
}

// TestBuildEthPortsSkipsDsaConduit (#847): los conduits DSA se excluyen del
// panel aunque figuren en /sys (su velocidad interna no es una boca usable).
func TestBuildEthPortsSkipsDsaConduit(t *testing.T) {
	states := []PortState{
		{Name: "lan1", Up: true, Speed: "1 Gbps"},
		{Name: "eth1", Up: true, Speed: "150 Mbps", DSA: true},
	}
	ports := BuildEthPorts(nil, states, nil, nil, "")
	if len(ports) != 1 || ports[0].ID != "lan1" {
		t.Fatalf("el conduit DSA debe excluirse: %+v", ports)
	}
}

func TestParsePingSummary(t *testing.T) {
	out := "3 packets transmitted, 3 received, 0% packet loss, time 2003ms\nrtt min/avg/max/mdev = 8.123/9.456/10.999/0.5 ms"
	lat, loss := ParsePingSummary(out)
	if lat == nil || *lat != 9 || loss == nil || *loss != 0 {
		t.Fatalf("ping: %v %v", lat, loss)
	}
	// #1281: busybox (OpenWrt) imprime min/avg/max SIN mdev.
	if lat, _ := ParsePingSummary("3 packets transmitted, 3 packets received, 0% packet loss\nround-trip min/avg/max = 6.196/6.326/6.527 ms"); lat == nil || *lat != 6 {
		t.Errorf("busybox rtt: lat = %v, want 6", lat)
	}
	if lat, loss := ParsePingSummary("round-trip min/avg/max = 10.5/20.4/30.6 ms\n3 packets transmitted, 3 received, 50% packet loss"); lat == nil || *lat != 20 || loss == nil || *loss != 50 {
		t.Errorf("busybox rtt+loss: lat=%v loss=%v", lat, loss)
	}
	if lat, _ := ParsePingSummary("2 packets transmitted, 0 received, 100% packet loss"); lat != nil {
		t.Fatalf("sin rtt → nil: %v", lat)
	}
}

func TestParseDhcp(t *testing.T) {
	out := "2000000000 aa:bb:cc:dd:ee:ff 192.168.8.21 imac-de-marc 01:aa:bb:cc:dd:ee:ff\n" +
		"2000000001 11:22:33:44:55:66 192.168.8.34 * *\n"
	leases := ParseDhcpLeasesFile(out)
	if len(leases) != 2 || leases[0].MAC != "AA:BB:CC:DD:EE:FF" || leases[0].Hostname != "imac-de-marc" || leases[1].Hostname != "" {
		t.Fatalf("leases: %+v", leases)
	}
	if leases[0].LeaseExpiresAt == nil || *leases[0].LeaseExpiresAt != 2000000000 {
		t.Fatalf("lease expiry: %+v", leases[0].LeaseExpiresAt)
	}
	ubus := `{"lease":[{"mac":"aa:bb:cc:dd:ee:ff","ip":"192.168.8.21","hostname":"imac","expires":12345}]}`
	leases, err := ParseDhcpUbus([]byte(ubus))
	if err != nil || len(leases) != 1 || leases[0].IP != "192.168.8.21" || leases[0].LeaseExpiresAt == nil || *leases[0].LeaseExpiresAt != 12345 {
		t.Fatalf("ubus dhcp: %v %+v", err, leases)
	}
	if _, err := ParseDhcpUbus([]byte("Command failed")); err == nil {
		t.Fatal("ubus roto debería dar error")
	}
}

func TestParseGlClients(t *testing.T) {
	// Formato real del Flint2: map mac -> objeto con mac/ip/name/online.
	raw := `{"clients":{
		"AA:BB:CC:DD:EE:FF":{"mac":"aa:bb:cc:dd:ee:ff","ip":"192.168.8.21","name":"imac","online":true},
		"11:22:33:44:55:66":{"mac":"11:22:33:44:55:66","ip":"192.168.8.34","name":"","online":true},
		"22:33:44:55:66:77":{"mac":"22:33:44:55:66:77","ip":"192.168.8.40","name":"viejo","online":false},
		"33:44:55:66:77:88":{"mac":"33:44:55:66:77:88","ip":"","name":"sinip","online":true}
	}}`
	leases, err := ParseGlClients([]byte(raw))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Solo deben quedar los ONLINE con IP (2 de 4), ordenados por MAC.
	if len(leases) != 2 {
		t.Fatalf("esperadas 2, %d: %+v", len(leases), leases)
	}
	if leases[0].MAC != "11:22:33:44:55:66" || leases[0].IP != "192.168.8.34" {
		t.Fatalf("primera %+v", leases[0])
	}
	if leases[1].MAC != "AA:BB:CC:DD:EE:FF" || leases[1].IP != "192.168.8.21" || leases[1].Hostname != "imac" {
		t.Fatalf("segunda %+v", leases[1])
	}
	// Salida vacía / JSON roto no deben dar entradas.
	if got, err := ParseGlClients([]byte("")); err == nil && len(got) != 0 {
		t.Fatalf("vacío: %+v", got)
	}
	if _, err := ParseGlClients([]byte("not json")); err == nil {
		t.Fatal("JSON roto debería dar error")
	}
}

func TestParseArp(t *testing.T) {
	out := "IP address       HW type     Flags       HW address            Mask     Device\n" +
		"192.168.1.10     0x1         0x2         aa:bb:cc:dd:ee:ff     *        br-lan\n" +
		"192.168.1.11     0x1         0x0         11:22:33:44:55:66     *        br-lan\n" +
		"192.168.1.12     0x1         0x2         00:00:00:00:00:00     *        br-lan\n" +
		"invalid          0x1         0x2         22:33:44:55:66:77     *        br-lan\n"
	m := ParseArp(out)
	if len(m) != 1 || m["AA:BB:CC:DD:EE:FF"] != "192.168.1.10" {
		t.Fatalf("arp: %+v", m)
	}
	if _, ok := m["11:22:33:44:55:66"]; ok {
		t.Fatal("entrada incompleta (0x0) no debe aparecer")
	}
	if _, ok := m["00:00:00:00:00:00"]; ok {
		t.Fatal("MAC nula no debe aparecer")
	}
	if _, ok := m["22:33:44:55:66:77"]; ok {
		t.Fatal("IP inválida no debe aparecer")
	}
}

func TestParseWireless(t *testing.T) {
	out := "A4:83:E7:21:0B:3C -48 5\nEC:71:DB:44:12:8A -72 2.4\n"
	m := ParseWirelessClients(out)
	if len(m) != 2 || m["A4:83:E7:21:0B:3C"].Band != "5 GHz" || m["A4:83:E7:21:0B:3C"].SignalDbm != -48 || m["EC:71:DB:44:12:8A"].Band != "2.4 GHz" {
		t.Fatalf("wireless: %+v", m)
	}
	sta := `{"radio0":{"up":true,"interfaces":[{"ifname":"wlan0","config":{"mode":"sta"}}]}}`
	wifi, err := ParseWirelessUplink([]byte(sta))
	if err != nil || !wifi {
		t.Fatalf("sta: %v %v", wifi, err)
	}
	ap := `{"radio0":{"up":true,"interfaces":[{"ifname":"wlan0","config":{"mode":"ap"}}]}}`
	wifi, _ = ParseWirelessUplink([]byte(ap))
	if wifi {
		t.Fatal("solo AP → cable")
	}
}

// TestPortPrettyLabel (#1200): etiquetas del panel de bocas. Los patrones
// clásicos conservan formato; los netdev 25.12+ (sfp-lan/sfp-wan) y las
// bocas sin dígitos (lan en UniFi 6 Lite) muestran su nombre real.
func TestPortPrettyLabel(t *testing.T) {
	cases := map[string]string{
		"lan1":    "LAN 1",
		"lan10":   "LAN 10",
		"lan":     "LAN",
		"wan":     "WAN",
		"eth0":    "ETH 0",
		"sfp0":    "SFP 0",
		"sfp":     "SFP",
		"sfp-lan": "sfp-lan",
		"sfp-wan": "sfp-wan",
		"enp3s0":  "enp3s0",
	}
	for name, want := range cases {
		if got := portPrettyLabel(name); got != want {
			t.Errorf("portPrettyLabel(%q) = %q, want %q", name, got, want)
		}
	}
}

// TestBuildEthPortsNetdevNames (#1200): BPI-R4 con nombres openwrt,netdev-name
// (25.12+). En board.json, sfp-lan es miembro de lan; sfp-wan no está y llega
// por extras. Antes: "sfp-LAN " (Replace a ciegas) y "SFP -wan" (sufijo
// partido). Ahora: el nombre real de la interfaz.
func TestBuildEthPortsNetdevNames(t *testing.T) {
	board := `{"network":{"lan":{"ports":["lan1","lan2","lan3","sfp-lan"],"device":"br-lan"},"wan":{"device":""}}}`
	layout, err := ParsePortLayout(board)
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	labels := map[string]string{}
	for _, p := range layout {
		labels[p.ID] = p.Label
	}
	if labels["sfp-lan"] != "sfp-lan" {
		t.Fatalf("sfp-lan en layout: %q", labels["sfp-lan"])
	}
	if labels["lan1"] != "LAN 1" {
		t.Fatalf("lan1 en layout: %q", labels["lan1"])
	}
	states := ParsePortStates("lan1 up 1000\nlan2 down -1\nlan3 down -1\nsfp-lan up 10000\nsfp-wan up 2500\neth1 up 100\n")
	ports := BuildEthPorts(layout, states, nil, nil, "")
	got := map[string]string{}
	for _, p := range ports {
		got[p.ID] = p.Label
	}
	if got["sfp-lan"] != "sfp-lan" {
		t.Errorf("sfp-lan: %q", got["sfp-lan"])
	}
	if got["sfp-wan"] != "sfp-wan" {
		t.Errorf("sfp-wan: %q", got["sfp-wan"])
	}
}

// TestBuildEthPortsBareLan (#1200): UniFi 6 Lite, la boca se llama "lan" a
// secas. Antes el panel salía vacío (0 de 0): ^lan\d+$ no la cogía y phyRe
// (^lan\d+$) la excluía de extras.
func TestBuildEthPortsBareLan(t *testing.T) {
	ports := BuildEthPorts(nil, ParsePortStates("lan up 1000\n"), nil, nil, "")
	if len(ports) != 1 || ports[0].ID != "lan" || ports[0].Label != "LAN" || !ports[0].Up {
		t.Fatalf("boca lan a secas: %+v", ports)
	}
}

func TestParsePortsYLayout(t *testing.T) {
	// La boca WAN aparece en /sys aunque esté esclavizada al bridge: sin ella
	// en los estados sería un device que no existe y BuildEthPorts la
	// descartaría (ver TestBuildEthPortsUplinkNoEthernet).
	states := ParsePortStates("eth0 up 2500\nlan1 up 1000\nlan2 down -1\nwlan0 up 0\nwan up 1000\n")
	if len(states) != 5 || states[0].Speed != "2.5 Gbps" || states[2].Up || states[2].Speed != "—" {
		t.Fatalf("states: %+v", states)
	}
	board := `{"network":{"lan":{"ports":["lan1","lan2","lan3","lan4"],"device":"br-lan"},"wan":{"device":"wan","protocol":"dhcp"}}}`
	layout, err := ParsePortLayout(board)
	if err != nil || len(layout) != 5 || layout[0].ID != "wan" || layout[1].Label != "LAN 1" {
		t.Fatalf("layout: %v %+v", err, layout)
	}
	// AP en bridge: wan en br-lan → se re-etiqueta LAN 5
	ports := BuildEthPorts(layout, states, map[string]bool{"wan": true}, nil, "")
	if len(ports) != 6 || ports[0].Label != "LAN 5" {
		t.Fatalf("ethports bridge: %+v", ports)
	}
	foundBridge := map[string]bool{}
	for _, p := range ports {
		foundBridge[p.ID] = true
	}
	if !foundBridge["eth0"] {
		t.Fatalf("ethports bridge sin eth0 extra: %+v", ports)
	}
	// Sin layout: fallback heurístico + interfaces físicas no cubiertas (#413/#416)
	ports = BuildEthPorts(nil, ParsePortStates("lan2 up 1000\nlan10 up 100\nwan down -1\neth0 up 2500\n"), nil, nil, "")
	fallbackIDs := map[string]int{}
	for i, p := range ports {
		fallbackIDs[p.ID] = i
	}
	if len(fallbackIDs) != 4 {
		t.Fatalf("ethports fallback count: %+v", ports)
	}
	for _, id := range []string{"wan", "lan2", "lan10", "eth0"} {
		if _, ok := fallbackIDs[id]; !ok {
			t.Fatalf("falta puerto %s: %+v", id, ports)
		}
	}
	if fallbackIDs["wan"] != 0 {
		t.Fatalf("wan no es el primero: %+v", ports)
	}

	// Con layout: eth0 no está en board.json pero sí en /sys → se añade al final.
	ports = BuildEthPorts(layout, []PortState{
		{Name: "wan", Up: true, Speed: "1 Gbps"},
		{Name: "lan1", Up: true, Speed: "1 Gbps"},
		{Name: "lan2", Up: true, Speed: "1 Gbps"},
		{Name: "lan3", Up: true, Speed: "1 Gbps"},
		{Name: "lan4", Up: true, Speed: "1 Gbps"},
		{Name: "eth0", Up: true, Speed: "2.5 Gbps"},
		{Name: "sfp0", Up: true, Speed: "10 Gbps"},
	}, nil, nil, "")
	if len(ports) != 7 {
		t.Fatalf("layout + extras: %d ports, %+v", len(ports), ports)
	}
	found := map[string]bool{}
	for _, p := range ports {
		found[p.ID] = true
	}
	for _, id := range []string{"wan", "lan1", "lan2", "lan3", "lan4", "eth0", "sfp0"} {
		if !found[id] {
			t.Fatalf("falta puerto %s: %+v", id, ports)
		}
	}
}

func TestParseFdbYRadios(t *testing.T) {
	fdb := ParseBridgeFdb("==PORTS==\n0x1 lan1\n0x2 lan2\n==MACS==\n1 aa:bb:cc:dd:ee:ff\n2 11:22:33:44:55:66\n")
	if len(fdb) != 2 || fdb["AA:BB:CC:DD:EE:FF"] != "lan1" || fdb["11:22:33:44:55:66"] != "lan2" {
		t.Fatalf("fdb: %+v", fdb)
	}
	radios := ParseRadios("2.4|6|HT20|20|3\n5|36|HT80|23|7\n")
	if len(radios) != 2 || radios[0].Name != "2.4 GHz" || radios[0].Clients != 3 || radios[1].WidthMhz != 80 || radios[1].PowerDbm != 23 {
		t.Fatalf("radios: %+v", radios)
	}
}

// TestParseRadiosNoiseBssid (#1213): el formato nuevo lleva BSSID y ruido de
// iwinfo; el histórico de 5 campos sigue parseando (sin ruido, sin BSSID).
func TestParseRadiosNoiseBssid(t *testing.T) {
	radios := ParseRadios("2.4|6|HT20|20|aa:bb:cc:00:00:01|-96|3\n5|36|HT80|23|aa:bb:cc:00:00:02|-91|7\n")
	if len(radios) != 2 {
		t.Fatalf("radios: %+v", radios)
	}
	if radios[0].NoiseDbm == nil || *radios[0].NoiseDbm != -96 {
		t.Fatalf("ruido 2.4: %+v", radios[0])
	}
	if radios[0].BSSID != "AA:BB:CC:00:00:01" {
		t.Fatalf("bssid 2.4: %+v", radios[0])
	}
	if radios[1].NoiseDbm == nil || *radios[1].NoiseDbm != -91 || radios[1].Clients != 7 {
		t.Fatalf("radio 5: %+v", radios[1])
	}
	// driver sin ruido ("unknown" → campo vacío) y formato viejo
	old := ParseRadios("5|36|HT80|23|aa:bb:cc:00:00:02||7\n")
	if len(old) != 1 || old[0].NoiseDbm != nil || old[0].Clients != 7 {
		t.Fatalf("radio 5 sin ruido: %+v", old)
	}
	legacy := ParseRadios("5|36|HT80|23|7\n")
	if len(legacy) != 1 || legacy[0].NoiseDbm != nil || legacy[0].BSSID != "" || legacy[0].Clients != 7 {
		t.Fatalf("formato viejo: %+v", legacy)
	}
}

// TestParseFdbBridgeFdb — #253: formato `bridge fdb show` (puerto por nombre,
// p. ej. eth0/eth1 en GLuON) y puertos ethernet fuera de lanN/wan.
func TestParseFdbBridgeFdb(t *testing.T) {
	// bridge fdb show emite "dev <ifname> <mac>" → CmdBridgeFDB lo convierte a
	// "<ifname> <mac>"; el parser debe casar por nombre contra ==PORTS==.
	fdb := ParseBridgeFdb("==PORTS==\n0x1 eth0\n0x2 eth1\n0x3 lan1\n==MACS==\neth0 aa:bb:cc:dd:ee:01\neth1 bb:cc:dd:ee:ff:02\nlan1 cc:dd:ee:ff:00:03\n")
	if len(fdb) != 3 {
		t.Fatalf("bridge fdb: esperaba 3 MACs, tengo %+v", fdb)
	}
	if fdb["AA:BB:CC:DD:EE:01"] != "eth0" || fdb["BB:CC:DD:EE:FF:02"] != "eth1" {
		t.Fatalf("bridge fdb eth*: %+v", fdb)
	}
	if fdb["CC:DD:EE:FF:00:03"] != "lan1" {
		t.Fatalf("bridge fdb lan1: %+v", fdb)
	}
}

// TestParseFdbExcluyeWireless — #253: los puertos inalámbricos (phy*-ap*,
// wlan*, bat*) no deben entrar como clientes cableados.
func TestParseLuCILabels(t *testing.T) {
	out := `config switchvlan 'port_labels'
	option lan1 'Router/Fritzbox'
	option lan2 'Garage door'
config switchvlan 'vlan_labels'
	option 1 'LAN'
	option 2 'WAN'
config language 'main'
	option lang 'es'
`
	labels := ParseLuCILabels(out)
	if labels == nil {
		t.Fatal("con etiquetas debería devolver datos")
	}
	if labels.PortLabels["lan1"] != "Router/Fritzbox" || labels.PortLabels["lan2"] != "Garage door" {
		t.Fatalf("port_labels: %+v", labels.PortLabels)
	}
	if labels.VlanLabels["1"] != "LAN" || labels.VlanLabels["2"] != "WAN" {
		t.Fatalf("vlan_labels: %+v", labels.VlanLabels)
	}
	if ParseLuCILabels("config language 'main'\n\toption lang 'es'\n") != nil {
		t.Fatal("sin port_labels/vlan_labels → nil")
	}
	if ParseLuCILabels("") != nil {
		t.Fatal("vacío → nil")
	}
}

func TestParseFdbExcluyeWireless(t *testing.T) {
	fdb := ParseBridgeFdb("==PORTS==\n0x1 lan1\n0x5 phy0-ap0\n==MACS==\n1 aa:bb:cc:dd:ee:01\n5 ff:ee:dd:cc:bb:aa\n")
	if _, ok := fdb["FF:EE:DD:CC:BB:AA"]; ok {
		t.Fatalf("phy0-ap0 no debería contar como cableado: %+v", fdb)
	}
	if fdb["AA:BB:CC:DD:EE:01"] != "lan1" {
		t.Fatalf("lan1 debería seguir: %+v", fdb)
	}
}

// TestParseFdbDenylistPuertos — #506: el allowlist histórico descartaba en
// silencio puertos físicos con nombres no enumerados (las jaulas SFP del
// BPI-R4 se llaman sfp-lan/sfp-wan; también "lan" suelto o usb0). El filtro
// invertido conserva cualquier miembro físico del bridge y solo excluye
// wireless (phy*-ap*, wlan*) y virtuales (br*, lo, veth*, wg*, túneles,
// subinterfaces VLAN).
func TestParseFdbDenylistPuertos(t *testing.T) {
	out := "==PORTS==\n" +
		"1 sfp-lan\n2 sfp-wan\n3 eth1\n4 lan\n5 usb0\n6 swp1\n7 enp2s0\n" +
		"8 phy0-ap0\n9 phy1-ap1\n10 wlan0\n11 br-lan\n12 br0\n13 lo\n" +
		"14 veth0\n15 wg0\n16 tunl0\n17 tap0\n18 pppoe-wan\n19 lan1.10\n" +
		"==MACS==\n" +
		// sfp-lan resuelto por port_no (vía brctl); el resto por nombre.
		"1 aa:00:00:00:00:01\n" +
		"sfp-wan aa:00:00:00:00:02\neth1 aa:00:00:00:00:03\nlan aa:00:00:00:00:04\n" +
		"usb0 aa:00:00:00:00:05\nswp1 aa:00:00:00:00:06\nenp2s0 aa:00:00:00:00:07\n" +
		"phy0-ap0 bb:00:00:00:00:01\nphy1-ap1 bb:00:00:00:00:02\nwlan0 bb:00:00:00:00:03\n" +
		"br-lan bb:00:00:00:00:04\nbr0 bb:00:00:00:00:05\nlo bb:00:00:00:00:06\nveth0 bb:00:00:00:00:07\n" +
		"wg0 bb:00:00:00:00:08\ntunl0 bb:00:00:00:00:09\ntap0 bb:00:00:00:00:0a\n" +
		"pppoe-wan bb:00:00:00:00:0b\nlan1.10 bb:00:00:00:00:0c\n"
	fdb := ParseBridgeFdb(out)
	want := map[string]string{
		"AA:00:00:00:00:01": "sfp-lan",
		"AA:00:00:00:00:02": "sfp-wan",
		"AA:00:00:00:00:03": "eth1",
		"AA:00:00:00:00:04": "lan",
		"AA:00:00:00:00:05": "usb0",
		"AA:00:00:00:00:06": "swp1",
		"AA:00:00:00:00:07": "enp2s0",
	}
	if len(fdb) != len(want) {
		t.Fatalf("esperaba %d MACs (solo puertos físicos), tengo %d: %+v", len(want), len(fdb), fdb)
	}
	for mac, port := range want {
		if fdb[mac] != port {
			t.Fatalf("MAC %s: esperaba puerto %s, tengo %q", mac, port, fdb[mac])
		}
	}
}

// ---------------------------------------------------------------------------
// Prober local con runner fake
// ---------------------------------------------------------------------------

// TestProberWirelessWithoutClients (#1092): una unidad SIN clientes
// asociados conserva la sección wireless (radios + BSSIDs propios); antes
// el flag de presencia solo consideraba clientes y el server la veía como
// "sin WiFi" (encontrado en un AP lab de NetGrip).
func TestProberWirelessWithoutClients(t *testing.T) {
	run := fakeRunner{outs: map[string]string{
		CmdUbusSystemInfo:  `{"uptime":90061,"load":[0.1,0.2,0.3],"memory":{"total":256000000,"free":100000000,"buffered":0,"available":128000000}}`,
		CmdUbusSystemBoard: `{"model":"GL.iNet Lab","hostname":"lab","release":{"version":"25.12","description":"OpenWrt 25.12"}}`,
		CmdProcStat:        "cpu  4705 356 584 3699 23 0 23 0 0 0\n",
		CmdTemp:            "43500\n",
		CmdNetDev:          "  eth0: 1000000 0 0 0 0 0 0 0 500000 0\n",
		CmdBridgeMAC:       "94:83:c4:00:00:09\n",
		// Sin clientes: ningún comando de assoclist/ubus hostapd.
		CmdRadios:   "2.4|6|HT20|20|0\n",
		CmdDhcpFile: "1700000000 ec:71:db:44:12:8a 192.168.8.71 movil *\n",
		CmdBridgeFDB: "==PORTS==\n0x1 lan1\n==MACS==\n1 ec:71:db:44:12:8a\n",
		CmdPortStates: "lan1 up 1000\nwan down -1\n",
		CmdUbusWireless: `{"radio0":{"up":true,"interfaces":[{"ifname":"wlan0","config":{"mode":"ap"}}]}}`,
		CmdIwDev: "phy#0\n\tInterface wlan0\n\t\taddr 62:e5:56:b6:94:bd\n\t\tssid temiscira\n\t\ttype AP\n",
	}}
	p := NewProber(run, Options{GwPingTarget: "192.168.8.1", ScanInterval: ScanDisabled})

	pl := p.Build(context.Background(), "lab", "3.0.7")
	if pl.Data.Wireless == nil {
		t.Fatal("sin clientes la sección wireless no debe descartarse (#1092)")
	}
	if len(pl.Data.Wireless.OwnBssids) != 1 || pl.Data.Wireless.OwnBssids[0].SSID != "temiscira" {
		t.Fatalf("ownBssids perdidos: %+v", pl.Data.Wireless.OwnBssids)
	}
	if len(pl.Data.Wireless.Radios) != 1 {
		t.Fatalf("radios perdidas: %+v", pl.Data.Wireless.Radios)
	}
}

type fakeRunner struct{ outs map[string]string }

func (f fakeRunner) Run(_ context.Context, cmd string, _ time.Duration) (string, error) {
	if out, ok := f.outs[cmd]; ok {
		return out, nil
	}
	return "", &fakeErr{cmd}
}

type fakeErr struct{ cmd string }

func (e *fakeErr) Error() string { return "no existe: " + e.cmd }

func TestProberBuild(t *testing.T) {
	run := fakeRunner{outs: map[string]string{
		CmdUbusSystemInfo:  `{"uptime":90061,"load":[0.1,0.2,0.3],"memory":{"total":256000000,"free":100000000,"buffered":0,"available":128000000}}`,
		CmdUbusSystemBoard: `{"model":"TP-Link EAP225","hostname":"patio","release":{"version":"23.05","description":"OpenWrt 23.05"}}`,
		CmdProcStat:        "cpu  4705 356 584 3699 23 0 23 0 0 0\n",
		CmdTemp:            "43500\n",
		CmdNetDev:          "  eth0: 1000000 0 0 0 0 0 0 0 500000 0\n",
		CmdBridgeMAC:       "94:83:c4:00:00:09\n",
		CmdIwinfoAssoc:     "EC:71:DB:44:12:8A -55 2.4\n",
		CmdRadios:          "2.4|6|HT20|20|1\n",
		CmdDhcpFile:        "1700000000 ec:71:db:44:12:8a 192.168.8.71 movil *\n",
		CmdBridgeFDB:       "==PORTS==\n0x1 lan1\n==MACS==\n1 ec:71:db:44:12:8a\n",
		CmdPortStates:      "lan1 up 1000\nwan down -1\n",
		CmdUbusWireless:    `{"radio0":{"up":true,"interfaces":[{"ifname":"wlan0","config":{"mode":"ap"}}]}}`,
	}}
	p := NewProber(run, Options{GwPingTarget: "192.168.8.1"})

	// Primera muestra: cpu/net sin delta (null), el resto presente
	pl := p.Build(context.Background(), "patio", "0.1.0")
	if pl.Router != "patio" || pl.Version != "0.1.0" || pl.Ts <= 0 {
		t.Fatalf("cabecera: %+v", pl)
	}
	sd := pl.Data.System
	if sd == nil || sd.SysInfo == nil || sd.SysInfo.Uptime != 90061 || sd.Board.Hostname != "patio" {
		t.Fatalf("system: %+v", sd)
	}
	if sd.CPU != nil || sd.RxBps != nil {
		t.Fatalf("primera muestra sin delta: %v %v", sd.CPU, sd.RxBps)
	}
	if sd.Temp == nil || *sd.Temp != 44 || sd.Backhaul != "cable" || sd.BridgeMAC != "94:83:C4:00:00:09" {
		t.Fatalf("temp/backhaul/mac: %+v", sd)
	}
	if pl.Data.Wireless.Clients["EC:71:DB:44:12:8A"].SignalDbm != -55 || len(pl.Data.Wireless.Radios) != 1 {
		t.Fatalf("wireless: %+v", pl.Data.Wireless)
	}
	if len(pl.Data.DHCP.Leases) != 1 || pl.Data.DHCP.Leases[0].IP != "192.168.8.71" {
		t.Fatalf("dhcp: %+v", pl.Data.DHCP)
	}
	if pl.Data.FDB.MACs["EC:71:DB:44:12:8A"] != "lan1" || len(pl.Data.FDB.Ports) == 0 {
		t.Fatalf("fdb: %+v", pl.Data.FDB)
	}

	// Segunda muestra (contadores avanzados): ya hay delta de cpu/net
	run.outs[CmdProcStat] = "cpu  4805 356 584 3799 23 0 23 0 0 0\n"
	run.outs[CmdNetDev] = "  eth0: 2000000 0 0 0 0 0 0 0 1000000 0\n"
	pl2 := p.Build(context.Background(), "patio", "0.1.0")
	if pl2.Data.System.CPU == nil || pl2.Data.System.RxBps == nil {
		t.Fatal("segunda muestra debería tener delta cpu/net")
	}
}

func TestProberNetIfOmittedWhenCmdNetDevFails(t *testing.T) {
	// Issue #408: si /proc/net/dev no se refresca en un ciclo, NetIf no debe
	// enviarse para que el servidor no calcule deltas 0 con contadores viejos.
	run := fakeRunner{outs: map[string]string{
		CmdUbusSystemInfo:  `{"uptime":90061,"load":[0.1,0.2,0.3],"memory":{"total":256000000,"free":100000000,"buffered":0,"available":128000000}}`,
		CmdUbusSystemBoard: `{"model":"TP-Link EAP225","hostname":"patio","release":{"version":"23.05","description":"OpenWrt 23.05"}}`,
		CmdProcStat:        "cpu  4705 356 584 3699 23 0 23 0 0 0\n",
		CmdTemp:            "43500\n",
		CmdNetDev:          "  eth0: 1000000 0 0 0 0 0 0 0 500000 0\n",
		CmdBridgeMAC:       "94:83:c4:00:00:09\n",
	}}
	p := NewProber(run, Options{})

	pl := p.Build(context.Background(), "patio", "0.1.0")
	if pl.Data.NetIf == nil || pl.Data.NetIf["eth0"].Rx != 1000000 {
		t.Fatalf("primera muestra debería incluir NetIf: %+v", pl.Data.NetIf)
	}

	// Segundo ciclo: /proc/net/dev falla; NetIf debe omitirse.
	delete(run.outs, CmdNetDev)
	pl2 := p.Build(context.Background(), "patio", "0.1.0")
	if pl2.Data.NetIf != nil {
		t.Fatalf("CmdNetDev falló: NetIf debería ser nil, got %+v", pl2.Data.NetIf)
	}

	// Tercer ciclo: /proc/net/dev vuelve; NetIf se reanuda.
	run.outs[CmdNetDev] = "  eth0: 2000000 0 0 0 0 0 0 0 1000000 0\n"
	pl3 := p.Build(context.Background(), "patio", "0.1.0")
	if pl3.Data.NetIf == nil || pl3.Data.NetIf["eth0"].Rx != 2000000 {
		t.Fatalf("tercera muestra debería incluir NetIf refrescado: %+v", pl3.Data.NetIf)
	}
}

func TestProberSondasFallidasSeccionAusente(t *testing.T) {
	// Equipo sin ubus/iwinfo/brctl (todo falla salvo /proc): las secciones
	// wireless/dhcp/fdb quedan ausentes (nil) → el servidor conserva lo último.
	run := fakeRunner{outs: map[string]string{
		CmdProcStat: "cpu  4705 356 584 3699 23 0 23 0 0 0\n",
		CmdTemp:     "43500\n",
		CmdNetDev:   "  eth0: 1000 0 0 0 0 0 0 0 500 0\n",
	}}
	p := NewProber(run, Options{})
	pl := p.Build(context.Background(), "patio", "0.1.0")
	if pl.Data.System == nil {
		t.Fatal("system debería existir (proc funciona)")
	}
	if pl.Data.Wireless != nil || pl.Data.DHCP != nil || pl.Data.FDB != nil {
		t.Fatalf("secciones fallidas deberían ser nil: %+v", pl.Data)
	}
	// JSON de las secciones ausentes: omitempty las omite
	if strings.Contains(payloadJSON(t, pl), `"wireless"`) {
		t.Fatal("wireless ausente no debería serializarse")
	}
}

func payloadJSON(t *testing.T, pl *Payload) string {
	t.Helper()
	data, err := json.Marshal(pl)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestParseWanStatus(t *testing.T) {
	// Fixture real de `ubus call network.interface.wan status` (PPPoE Digi).
	raw := `{"up":true,"proto":"pppoe","l3_device":"pppoe-wan","device":"eth1.20",` +
		`"ipv4-address":[{"address":"79.112.56.116","mask":32,"ptpaddress":"10.0.28.237"}],` +
		`"route":[{"target":"0.0.0.0","mask":0,"nexthop":"10.0.28.237","source":"0.0.0.0/0"}],` +
		`"dns-server":["100.90.1.1","100.100.1.1"]}`
	info := ParseWanStatus([]byte(raw))
	if info.Proto != "pppoe" {
		t.Fatalf("proto=%q, esperaba pppoe", info.Proto)
	}
	if info.Device != "pppoe-wan" {
		t.Fatalf("device=%q, esperaba pppoe-wan", info.Device)
	}
	if info.IP != "79.112.56.116" {
		t.Fatalf("ip=%q, esperaba 79.112.56.116", info.IP)
	}
	if info.Gateway != "10.0.28.237" {
		t.Fatalf("gateway=%q, esperaba 10.0.28.237", info.Gateway)
	}
	if len(info.DNS) != 2 || info.DNS[0] != "100.90.1.1" || info.DNS[1] != "100.100.1.1" {
		t.Fatalf("dns=%v, esperaba [100.90.1.1 100.100.1.1]", info.DNS)
	}
}

func TestParseWanStatusVacioYMalFormado(t *testing.T) {
	// Router sin interfaz wan (AP) → JSON sin ipv4-address ni rutas.
	raw := `{"up":true,"proto":"dhcp"}`
	info := ParseWanStatus([]byte(raw))
	if info.IP != "" || info.Gateway != "" || len(info.DNS) != 0 {
		t.Fatalf("AP sin wan debía quedar vacío: %+v", info)
	}
	// Malformado → vacío sin error.
	if got := ParseWanStatus([]byte("no json")); got.IP != "" || got.Proto != "" {
		t.Fatalf("JSON inválido debía quedar vacío: %+v", got)
	}
}

func TestParseBridgeVlan(t *testing.T) {
	// Fixture típico de OpenWrt con bridge vlan filtering (VLAN 1 PVID +
	// VLANs 10/20 tagged en wan, 1 untagged en todos los LAN).
	out := `port              vlan-id
br-lan            1 PVID Egress Untagged

lan1              1 PVID Egress Untagged

lan2              1 PVID Egress Untagged

lan3              1 PVID Egress Untagged

lan4              1 PVID Egress Untagged

wan               1 PVID Egress Untagged
                  10
                  20
`
	ports := ParseBridgeVlan(out)
	if len(ports) != 6 {
		t.Fatalf("esperaba 6 puertos, tengo %d: %+v", len(ports), ports)
	}
	// br-lan: 1 untagged + PVID
	br := ports[0]
	if br.Port != "br-lan" || len(br.Vlans) != 1 {
		t.Fatalf("br-lan: %+v", br)
	}
	if br.Vlans[0].ID != 1 || br.Vlans[0].Tagged || !br.Vlans[0].PVID {
		t.Fatalf("br-lan vlan: %+v", br.Vlans[0])
	}
	// wan: 3 VLANs (1 untagged+PVID, 10 tagged, 20 tagged)
	wan := ports[5]
	if wan.Port != "wan" || len(wan.Vlans) != 3 {
		t.Fatalf("wan: %+v", wan)
	}
	if wan.Vlans[0].ID != 1 || wan.Vlans[0].Tagged || !wan.Vlans[0].PVID {
		t.Fatalf("wan vlan[0]: %+v", wan.Vlans[0])
	}
	if wan.Vlans[1].ID != 10 || !wan.Vlans[1].Tagged || wan.Vlans[1].PVID {
		t.Fatalf("wan vlan[1]: %+v", wan.Vlans[1])
	}
	if wan.Vlans[2].ID != 20 || !wan.Vlans[2].Tagged || wan.Vlans[2].PVID {
		t.Fatalf("wan vlan[2]: %+v", wan.Vlans[2])
	}
}

func TestParseBridgeVlanVacio(t *testing.T) {
	// Router sin bridge vlan filtering → salida vacía.
	if got := ParseBridgeVlan(""); len(got) != 0 {
		t.Fatalf("vacío esperaba [], tengo %+v", got)
	}
	if got := ParseBridgeVlan("port              vlan-id\n"); len(got) != 0 {
		t.Fatalf("solo header esperaba [], tengo %+v", got)
	}
}

func TestParseBridgeVlanSoloTagged(t *testing.T) {
	// Puerto trunk sin PVID (solo tagged).
	out := `port              vlan-id
eth0              100
                  200
                  300
`
	ports := ParseBridgeVlan(out)
	if len(ports) != 1 || ports[0].Port != "eth0" || len(ports[0].Vlans) != 3 {
		t.Fatalf("trunk: %+v", ports)
	}
	for _, v := range ports[0].Vlans {
		if !v.Tagged || v.PVID {
			t.Fatalf("trunk vlan debía ser tagged sin PVID: %+v", v)
		}
	}
}

func TestParseEthtoolSFP(t *testing.T) {
	// Salida realista de ethtool -m con un SFP monomodo.
	out := `	Identifier                                : 0x03 (SFP)
	Extended identifier                       : 0x04 (GBIC/SFP defined by 2-wire interface ID)
	Connector                                 : 0x07 (LC)
	Transceiver codes                         : 0x00 0x00 0x00 0x01 0x00 0x00 0x00 0x00 0x00
	Vendor Name                               : FS.COM
	Vendor Part Number                        : SFP-GE-BX
	Vendor Rev                                :
	Vendor SN                                 : F2305060072
	Module temperature                        : 34.5 degrees C / 94.1 degrees F
	Module voltage                            : 3.2950 Volts
	Alarm/warning flags implemented           : Yes
	Laser output power                        : 0.5230 mW / -2.82 dBm
	Laser receiver power                      : 0.0501 mW / -13.00 dBm
`
	sfp := ParseEthtoolSFP(out)
	if sfp == nil {
		t.Fatal("esperaba SfpInfo no nil")
	}
	if !sfp.Present {
		t.Fatal("esperaba Present=true")
	}
	if sfp.Temperature != 34.5 {
		t.Fatalf("temp=%.1f, esperaba 34.5", sfp.Temperature)
	}
	if sfp.Voltage != 3.2950 {
		t.Fatalf("volt=%.4f, esperaba 3.2950", sfp.Voltage)
	}
	if sfp.TxPower != -2.82 {
		t.Fatalf("txp=%.2f, esperaba -2.82", sfp.TxPower)
	}
	if sfp.RxPower != -13.00 {
		t.Fatalf("rxp=%.2f, esperaba -13.00", sfp.RxPower)
	}
	if sfp.Vendor != "FS.COM" {
		t.Fatalf("vendor=%q, esperaba FS.COM", sfp.Vendor)
	}
	if sfp.PartNumber != "SFP-GE-BX" {
		t.Fatalf("pn=%q, esperaba SFP-GE-BX", sfp.PartNumber)
	}

	// Sin módulo SFP: salida vacía → nil.
	if got := ParseEthtoolSFP(""); got != nil {
		t.Fatalf("vacío debía dar nil: %+v", got)
	}
	// Salida sin datos DOM (solo identifier, sin temp/power) → nil.
	if got := ParseEthtoolSFP("Identifier : 0x03 (SFP)\n"); got != nil {
		t.Fatalf("sin DOM debía dar nil: %+v", got)
	}
}

// TestIsRandomizedMAC (#338): detects locally-administered MACs.
func TestIsRandomizedMAC(t *testing.T) {
	cases := map[string]bool{
		"AA:BB:CC:DD:EE:FF": true,  // 0xAA = 10101010, bit 1 set
		"02:11:22:33:44:55": true,  // 0x02 = 00000010, bit 1 set
		"F6:A1:B2:C3:D4:E5": true,  // 0xF6 = 11110110, bit 1 set
		"00:11:22:33:44:55": false, // 0x00 = 00000000, bit 1 clear
		"DC:A6:32:XX:YY:ZZ": false, // 0xDC = 11011100, bit 1 clear (Raspberry Pi)
		"B8:27:EB:11:22:33": false, // 0xB8 = 10111000, bit 1 clear
		"B2:AA:BB:CC:DD:EE": true,  // 0xB2 = 10110010, bit 1 set (Apple private)
	}
	for mac, want := range cases {
		got := IsRandomizedMAC(mac)
		if got != want {
			t.Errorf("IsRandomizedMAC(%q) = %v, want %v", mac, got, want)
		}
	}
	// Edge cases
	if IsRandomizedMAC("") {
		t.Error("empty should be false")
	}
	if IsRandomizedMAC("X") {
		t.Error("single char should be false")
	}
}

// TestParseMdnsBrowse (#338): parses umdns browse output.
func TestParseMdnsBrowse(t *testing.T) {
	// Empty input
	if got := ParseMdnsBrowse(nil); got != nil {
		t.Fatalf("nil should return nil, got %+v", got)
	}
	if got := ParseMdnsBrowse([]byte("{}")); got != nil {
		t.Fatalf("empty object should return nil, got %+v", got)
	}

	// Real umdns browse output (simplified)
	raw := `{
		"Apple-TV._airplay._tcp.local": {"port": 7000, "ipv4": "192.168.1.50"},
		"Apple-TV._raop._tcp.local": {"port": 7000, "ipv4": "192.168.1.50"},
		"Printer._ipp._tcp.local": {"port": 631, "ipv4": "192.168.1.60"}
	}`
	dd := ParseMdnsBrowse([]byte(raw))
	if dd == nil {
		t.Fatal("should parse valid browse output")
	}
	// Check services
	if svcs, ok := dd.Services["Apple-TV"]; !ok || len(svcs) != 2 {
		t.Errorf("Apple-TV services: %v", dd.Services)
	}
	if svcs, ok := dd.Services["Printer"]; !ok || len(svcs) != 1 {
		t.Errorf("Printer services: %v", dd.Services)
	}
	// Check IP mapping
	if host, ok := dd.HostByIP["192.168.1.50"]; !ok || host != "Apple-TV" {
		t.Errorf("HostByIP[192.168.1.50] = %q", host)
	}
	if host, ok := dd.HostByIP["192.168.1.60"]; !ok || host != "Printer" {
		t.Errorf("HostByIP[192.168.1.60] = %q", host)
	}
}

func TestParseUsteer(t *testing.T) {
	// Fixtures con los shapes reales verificados en rt3 (local_info) y
	// Flint2 (remote_info / connected_clients).
	localInfo := `{
  "hostapd.phy0-ap0": {
    "bssid": "9c:9d:7e:1b:ea:b3",
    "ssid": "temiscira",
    "freq": 5260,
    "n_assoc": 0,
    "noise": -108,
    "load": 3,
    "max_assoc": 0,
    "roam_events": { "source": 0, "target": 0 },
    "rrm_nr": ["9c:9d:7e:1b:ea:b3", "temiscira", "9c9d7e1beab3ff5900008034090603023a00"]
  },
  "hostapd.phy1-ap0": {
    "bssid": "9c:9d:7e:1b:ea:b2",
    "ssid": "temiscira",
    "freq": 2442,
    "n_assoc": 1,
    "load": 20
  }
}`

	remoteInfo := `{
  "192.168.1.3#hostapd.phy0-ap0": {
    "bssid": "9c:9d:7e:1b:ea:b3",
    "ssid": "temiscira",
    "freq": 5260,
    "n_assoc": 0,
    "load": 3
  },
  "192.168.1.4#hostapd.phy0-ap0": {
    "bssid": "aa:bb:cc:dd:ee:01",
    "ssid": "temiscira",
    "freq": 5260,
    "n_assoc": 2,
    "load": 40
  }
}`

	connectedClients := `{
  "hostapd.wlan0": {
    "aa:bb:cc:dd:ee:ff": { "signal": -39 }
  }
}`

	// 1. Sin remote_info: los APs locales se agrupan por SSID.
	d := ParseUsteer(localInfo, "", "")
	if d == nil {
		t.Fatal("ParseUsteer(local) devolvió nil")
	}
	s, ok := d.SSIDs["temiscira"]
	if !ok {
		t.Fatalf("falta SSID temiscira: %v", d.SSIDs)
	}
	if len(s.APs) != 2 {
		t.Fatalf("esperaba 2 APs locales, obtuve %d", len(s.APs))
	}
	if !s.APs[0].Local || !s.APs[1].Local {
		t.Error("los APs locales deberían tener Local=true")
	}
	// Los BSSID deben ir en mayúsculas. Sin asumir orden (ParseUsteer itera
	// un mapa): comprobar el conjunto de los dos APs locales.
	got := map[string]bool{}
	for _, ap := range s.APs {
		got[ap.BSSID] = true
	}
	for _, want := range []string{"9C:9D:7E:1B:EA:B2", "9C:9D:7E:1B:EA:B3"} {
		if !got[want] {
			t.Errorf("falta el AP local %s (BSSID en mayúsculas): %v", want, s.APs)
		}
	}

	// 2. APs remotos: hostname = IP de la clave, Local=false.
	d = ParseUsteer("", remoteInfo, "")
	if d == nil {
		t.Fatal("ParseUsteer(remote) devolvió nil")
	}
	rs := d.SSIDs["temiscira"]
	if len(rs.APs) != 2 {
		t.Fatalf("esperaba 2 APs remotos, obtuve %d", len(rs.APs))
	}
	if rs.APs[0].Local {
		t.Error("el AP remoto debería tener Local=false")
	}
	if rs.APs[0].Hostname != "192.168.1.3" && rs.APs[1].Hostname != "192.168.1.3" {
		t.Errorf("falta hostname IP en remotos: %+v", rs.APs)
	}

	// 3. connected_clients: si el iface casa con local_info, el cliente se
	// agrupa por SSID con su señal.
	d = ParseUsteer(localInfo, "", `{ "hostapd.phy0-ap0": { "aa:bb:cc:dd:ee:ff": { "signal": -39 } } }`)
	if d == nil {
		t.Fatal("ParseUsteer con clientes devolvió nil")
	}
	cs := d.SSIDs["temiscira"]
	if c, ok := cs.Clients["AA:BB:CC:DD:EE:FF"]; !ok || c.Signal != -39 {
		t.Errorf("cliente no agrupado: %v", cs.Clients)
	}

	// 4. iface sin casar (wlan0 vs phy0-ap0): no se asigna SSID erróneo.
	_ = connectedClients
	d = ParseUsteer(localInfo, "", connectedClients)
	if d != nil && len(d.SSIDs["temiscira"].Clients) != 0 {
		t.Errorf("cliente con iface no resuelto debería omitirse: %v", d.SSIDs["temiscira"].Clients)
	}

	// 5. Sin datos: nil.
	if ParseUsteer("", "", "") != nil {
		t.Error("ParseUsteer vacío debería devolver nil")
	}
	if ParseUsteer("{", "", "") != nil {
		t.Error("ParseUsteer con JSON inválido debería devolver nil")
	}
}

// TestParseScanExtraeVecinos (#452): parsea la salida de `iw dev` scan.
func TestParseScanWidthMhz(t *testing.T) {
	// Bloques HT/VHT reales de `iw dev wlan0 scan` (OpenWrt).
	out := `==IFACE==wlan0
BSS 11:22:33:44:55:66(on wlan0)
	freq: 2437
	signal: -55.00 dBm
	SSID: ochenta
	HT operation:
		 * primary channel: 6
		 * secondary channel offset: below
	VHT operation:
		 * channel width: 1 (80 MHz)
BSS aa:bb:cc:dd:ee:ff(on wlan0)
	freq: 2462
	signal: -70.00 dBm
	SSID: cuarenta
	HT operation:
		 * primary channel: 11
		 * secondary channel offset: above
BSS 22:33:44:55:66:77(on wlan0)
	freq: 2412
	signal: -80.00 dBm
	SSID: veinte
	HT operation:
		 * primary channel: 1
		 * secondary channel offset: no secondary
BSS 33:44:55:66:77:88(on wlan0)
	freq: 5500
	signal: -60.00 dBm
	SSID: ciento60
	HT operation:
		 * primary channel: 100
		 * secondary channel offset: below
	VHT operation:
		 * channel width: 2 (160 MHz)
`
	got := ParseScan(out)
	widths := map[string]int{}
	for _, s := range got {
		widths[s.SSID] = s.WidthMhz
	}
	if widths["ochenta"] != 80 || widths["cuarenta"] != 40 || widths["veinte"] != 20 || widths["ciento60"] != 160 {
		t.Fatalf("anchos inesperados: %+v de %+v", widths, got)
	}
}

func TestParseIwDev(t *testing.T) {
	out := `phy#0
	Interface wlan0
		ifindex 12
		wdev 0x200000000
		addr 62:e5:56:b6:94:bd
		ssid temiscira
		type AP
		channel 6 (2437 MHz), width: 80 MHz, center1: 2437 MHz
		txpower 20.00 dBm
	Interface wlan0-1
		ifindex 13
		wdev 0x200000001
		addr 6a:e5:56:b6:94:bd
		ssid temiscira-guest
		type AP
		channel 6 (2437 MHz), width: 20 MHz, center1: 2437 MHz
	Interface wlan1
		ifindex 14
		wdev 0x200000002
		addr 62:e5:56:b6:94:be
		ssid temiscira
		type AP
		channel 36 (5180 MHz), width: 80 MHz, center1: 5210 MHz
phy#1
	Interface uap0
		ifindex 20
		addr 62:e5:56:b6:94:bf
		type managed
`
	got := ParseIwDev(out)
	if len(got) != 3 {
		t.Fatalf("esperaba 3 APs (uap0 managed fuera): %+v", got)
	}
	if got[0].BSSID != "62:E5:56:B6:94:BD" || got[0].SSID != "temiscira" || got[0].Iface != "wlan0" {
		t.Fatalf("bloque 0 mal parseado: %+v", got[0])
	}
	if got[1].SSID != "temiscira-guest" {
		t.Fatalf("guest sin capturar: %+v", got[1])
	}
}

func TestParseScanExtraeVecinos(t *testing.T) {
	out := `==IFACE==wlan0
BSS 00:11:22:33:44:55(on wlan0)
	TSF: 12345
	freq: 2437
	beacon interval: 100 TUs
	capability: ESS (0x0001)
	signal: -62.00 dBm
	last seen: 0 ms
	SSID: vecino-2g
BSS aa:bb:cc:dd:ee:ff(on wlan0)
	freq: 2462
	signal: -80.00 dBm
	SSID: 
==IFACE==wlan1
BSS 11:22:33:44:55:66(on wlan1)
	freq: 5180
	signal: -55.00 dBm
	SSID: vecino-5g
`
	got := ParseScan(out)
	if len(got) != 3 {
		t.Fatalf("esperaba 3 resultados, got %d: %+v", len(got), got)
	}
	if got[0].Iface != "wlan0" || got[0].BSSID != "00:11:22:33:44:55" || got[0].SSID != "vecino-2g" || got[0].Freq != 2437 || got[0].Channel != 6 || got[0].Signal != -62 {
		t.Errorf("primer scan incorrecto: %+v", got[0])
	}
	if got[1].Channel != 11 {
		t.Errorf("segundo scan channel incorrecto: %+v", got[1])
	}
	if got[2].Iface != "wlan1" || got[2].Channel != 36 || got[2].Signal != -55 {
		t.Errorf("tercer scan incorrecto: %+v", got[2])
	}
}

// TestParseScanFreqFloat (#475): iw real imprime "freq: 5260.0"; con Atoi el
// error se tragaba, Freq quedaba 0 y el flush descartaba todos los BSS.
func TestParseScanFreqFloat(t *testing.T) {
	out := `==IFACE==wlan1
BSS 9c:9d:7e:1b:ea:b3(on wlan1)
	last seen: 945720.058s [boottime]
	TSF: 419753678847 usec (4d, 20:35:53)
	freq: 5260.0
	signal: -61.00 dBm
	SSID: vecino-dfs
BSS 8c:de:f9:33:71:59(on wlan1)
	freq: 5180.0
	signal: -72.00 dBm
	SSID: 
`
	got := ParseScan(out)
	if len(got) != 2 {
		t.Fatalf("esperaba 2 resultados, got %d: %+v", len(got), got)
	}
	if got[0].Freq != 5260 || got[0].Channel != 52 || got[0].Signal != -61 {
		t.Errorf("primer scan incorrecto: %+v", got[0])
	}
	if got[1].Freq != 5180 || got[1].Channel != 36 || got[1].SSID != "" {
		t.Errorf("segundo scan incorrecto: %+v", got[1])
	}
}

func TestParseScanVacio(t *testing.T) {
	if got := ParseScan(""); len(got) != 0 {
		t.Fatalf("esperaba [] vacío, got %d", len(got))
	}
}

func TestFreqToChannel(t *testing.T) {
	cases := []struct{ freq, want int }{
		{2412, 1}, {2437, 6}, {2462, 11}, {2484, 14},
		{5180, 36}, {5260, 52}, {5500, 100}, {5955, 1},
	}
	for _, c := range cases {
		if got := FreqToChannel(c.freq); got != c.want {
			t.Errorf("FreqToChannel(%d) = %d, want %d", c.freq, got, c.want)
		}
	}
}

// TestBoardInfoParsesASUFields: captura real de `ubus call system board` en un
// Redmi AX6 (rt4). board_name y release.target son los dos valores que ASU
// necesita para localizar la imagen (#477).
func TestBoardInfoParsesASUFields(t *testing.T) {
	raw := `{"kernel":"6.12.94","hostname":"rt4","system":"ARMv8 Processor rev 4","model":"Redmi AX6","board_name":"redmi,ax6","rootfs_type":"squashfs","release":{"distribution":"OpenWrt","version":"25.12.5","firmware_url":"https://downloads.openwrt.org/","revision":"r33051-f5dae5ece4","target":"qualcommax/ipq807x","description":"OpenWrt 25.12.5 r33051-f5dae5ece4"}}`
	var b BoardInfo
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		t.Fatalf("unmarshal board: %v", err)
	}
	if b.BoardName != "redmi,ax6" {
		t.Errorf("BoardName = %q, want redmi,ax6", b.BoardName)
	}
	if b.Release.Target != "qualcommax/ipq807x" {
		t.Errorf("Release.Target = %q, want qualcommax/ipq807x", b.Release.Target)
	}
	if b.Model != "Redmi AX6" || b.Hostname != "rt4" || b.Release.Version != "25.12.5" {
		t.Errorf("campos básicos corruptos: %+v", b)
	}
}

func TestParseRadioSections(t *testing.T) {
	out := `wireless.radio0=wifi-device
wireless.radio0.type='mac80211'
wireless.radio0.band='2g'
wireless.radio0.channel='1'
wireless.default_radio0=wifi-iface
wireless.radio1=wifi-device
wireless.radio1.band='5g'
wireless.radio1.channel='44'
wireless.default_radio1=wifi-iface
`
	got := ParseRadioSections(out)
	if len(got) != 2 {
		t.Fatalf("want 2 sections, got %d: %v", len(got), got)
	}
	if got["2.4 GHz"] != "radio0" || got["5 GHz"] != "radio1" {
		t.Errorf("wrong mapping: %v", got)
	}
}

func TestParseRadioSectionsLegacyHwmode(t *testing.T) {
	out := `wireless.wifi0=wifi-device
wireless.wifi0.hwmode='11g'
wireless.wifi1=wifi-device
wireless.wifi1.hwmode='11a'
`
	got := ParseRadioSections(out)
	if got["2.4 GHz"] != "wifi0" || got["5 GHz"] != "wifi1" {
		t.Errorf("wrong legacy hwmode mapping: %v", got)
	}
}

func TestParseRadioSectionsEmpty(t *testing.T) {
	if got := ParseRadioSections(""); len(got) != 0 {
		t.Errorf("want empty map, got %v", got)
	}
	if got := ParseRadioSections("network.lan=interface\n"); len(got) != 0 {
		t.Errorf("want empty map for unrelated config, got %v", got)
	}
}

// portStatesDSASwitch es la salida de CmdPortStates en una placa con switch DSA
// (ipq4019, switch DSA): dos bocas de verdad (lan1, lan2) colgando del puerto
// de CPU eth0, el bridge con sus VLANs, y un módem celular en wwan0.
const portStatesDSASwitch = `br-lan up 1000 - 1 lan2
br-lan.10 up 1000 - 1 br-lan
br-lan.16 up 1000 - 1 br-lan
eth0 up 1000 dsa 1 -
lan1 up 1000 - 1 eth0
lan2 up 1000 - 1 eth0
lo unknown -1 - 772 -
phy2-ap0 up -1 - 1 -
pppoe-isp unknown -1 - 512 -
wg0 unknown -1 - 65534 -
wwan0 unknown -1 - 65534 -`

// boardWanIsAModem: board.json del mismo router. El WAN no es una boca, es el
// device del módem QMI.
const boardWanIsAModem = `{"network":{"lan":{"ports":["lan1","lan2"],"protocol":"static"},` +
	`"wan":{"device":"/dev/cdc-wdm0","protocol":"qmi"}}}`

func TestParsePortStatesTipoYConduit(t *testing.T) {
	states := ParsePortStates(portStatesDSASwitch)
	byName := map[string]PortState{}
	for _, s := range states {
		byName[s.Name] = s
	}
	if got := byName["lan1"]; got.Conduit != "eth0" || got.Type != ARPHRDEther {
		t.Fatalf("lan1: %+v", got)
	}
	if got := byName["eth0"]; got.Conduit != "" {
		t.Fatalf("eth0 no cuelga de nadie: %+v", got)
	}
	if got := byName["wwan0"]; got.Type == ARPHRDEther {
		t.Fatalf("wwan0 no es ethernet: %+v", got)
	}
	// Forma antigua de tres campos: sin tipo ni conduit, y nada se descarta.
	old := ParsePortStates("lan1 up 1000\neth0 up 2500\n")
	if len(old) != 2 || old[0].Type != 0 || old[0].Conduit != "" {
		t.Fatalf("tres campos: %+v", old)
	}
	if ports := BuildEthPorts(nil, old, nil, nil, ""); len(ports) != 2 {
		t.Fatalf("tres campos no debe esconder bocas: %+v", ports)
	}
}

func TestBuildEthPortsSoloLasBocasReales(t *testing.T) {
	layout, err := ParsePortLayout(boardWanIsAModem)
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	ports := BuildEthPorts(layout, ParsePortStates(portStatesDSASwitch), nil, nil, "")
	ids := []string{}
	for _, p := range ports {
		ids = append(ids, p.ID)
	}
	// Antes salían cuatro: WAN (el módem, nunca conectada), LAN 1, LAN 2 y
	// ETH 0 (el puerto de CPU del switch).
	if len(ids) != 2 || ids[0] != "lan1" || ids[1] != "lan2" {
		t.Fatalf("bocas: %v", ids)
	}
}

func TestBuildEthPortsUplinkNoEthernet(t *testing.T) {
	// board.json puede nombrar como WAN una interfaz que existe pero no es
	// ethernet (el netdev del módem); tampoco es una boca.
	layout := []PortLayout{
		{ID: "wan", Name: "wwan0", Label: "WAN", Role: "wan"},
		{ID: "lan1", Name: "lan1", Label: "LAN 1", Role: "lan"},
	}
	ports := BuildEthPorts(layout, ParsePortStates(portStatesDSASwitch), nil, nil, "")
	ids := map[string]bool{}
	for _, p := range ports {
		ids[p.ID] = true
	}
	if ids["wan"] || ids["wwan0"] {
		t.Fatalf("el módem no es una boca: %+v", ports)
	}
	// lan2 no está en el layout pero sí en /sys: entra como extra.
	if len(ids) != 2 || !ids["lan1"] || !ids["lan2"] {
		t.Fatalf("bocas: %+v", ports)
	}
}

func TestBuildEthPortsMantieneEthSinSwitch(t *testing.T) {
	// Caja sin switch DSA (x86, BPI-R4): nadie declara eth0 como puerto de
	// CPU, así que sigue siendo una boca. Sin layout, el fallback toma eth1
	// como WAN (comportamiento previo, aquí solo interesa que eth0 siga).
	states := ParsePortStates("eth0 up 1000 - 1 -\neth1 up 2500 - 1 -\nlo unknown -1 - 772 -\n")
	ports := BuildEthPorts(nil, states, nil, nil, "")
	ids := map[string]bool{}
	for _, p := range ports {
		ids[p.ID] = true
	}
	if len(ids) != 2 || !ids["eth0"] {
		t.Fatalf("eth0 sin switch debe seguir siendo boca: %+v", ports)
	}
}

// dumpUplinkNotNamedWan: forma de `ubus call network.interface dump` en una placa cuyo enlace
// recortada a lo que mira el parser y con la IP pública cambiada. El uplink
// vivo es un PPPoE llamado "isp" sobre la boca lan1; la interfaz que SÍ se
// llama "wan" es el módem celular, ocioso pero con up=true y sin ruta.
const dumpUplinkNotNamedWan = `{"interface":[
 {"interface":"lan","up":true,"proto":"static","l3_device":"br-lan.10","device":"br-lan.10",
  "ipv4-address":[{"address":"192.0.2.1","mask":24}],"route":[]},
 {"interface":"wan","up":true,"proto":"qmi","l3_device":"wwan0",
  "ipv4-address":[],"route":[],"dns-server":[]},
 {"interface":"isp","up":true,"proto":"pppoe","l3_device":"pppoe-isp","device":"lan1",
  "ipv4-address":[{"address":"203.0.113.45","mask":32,"ptpaddress":"198.51.100.1"}],
  "route":[{"target":"0.0.0.0","mask":0,"nexthop":"198.51.100.1"}],
  "dns-server":["198.51.100.53","198.51.100.54"]}]}`

func TestParseWanStatusDumpEligeElUplinkVivo(t *testing.T) {
	info := ParseWanStatus([]byte(dumpUplinkNotNamedWan))
	if info.Proto != "pppoe" || info.Device != "pppoe-isp" {
		t.Fatalf("no eligió el PPPoE vivo: %+v", info)
	}
	if info.Port != "lan1" {
		t.Fatalf("boca del uplink: %q, esperaba lan1", info.Port)
	}
	if info.IP != "203.0.113.45" || info.Gateway != "198.51.100.1" {
		t.Fatalf("ip/gateway: %+v", info)
	}
	if len(info.DNS) != 2 {
		t.Fatalf("dns: %+v", info.DNS)
	}
}

func TestParseWanStatusDumpCaeALaLlamadaWan(t *testing.T) {
	// Router normal con el enlace caído: ninguna tiene ruta por defecto, así
	// que manda la que se llama "wan" (WAN caída, no router sin WAN).
	dump := `{"interface":[{"interface":"lan","up":true,"proto":"static","route":[]},
	 {"interface":"wan","up":false,"proto":"dhcp","l3_device":"eth1","device":"eth1","route":[]}]}`
	info := ParseWanStatus([]byte(dump))
	if info.Proto != "dhcp" || info.Port != "eth1" {
		t.Fatalf("fallback a la llamada wan: %+v", info)
	}
	// Sin ninguna interfaz utilizable (AP puro) → vacío.
	if got := ParseWanStatus([]byte(`{"interface":[{"interface":"lan","up":true,"route":[]}]}`)); got.Proto != "" || got.Port != "" {
		t.Fatalf("AP sin wan: %+v", got)
	}
}

func TestBuildEthPortsMarcaLaBocaDelUplink(t *testing.T) {
	layout, err := ParsePortLayout(boardWanIsAModem)
	if err != nil {
		t.Fatalf("layout: %v", err)
	}
	uplink := ParseWanStatus([]byte(dumpUplinkNotNamedWan)).Port
	ports := BuildEthPorts(layout, ParsePortStates(portStatesDSASwitch), nil, nil, uplink)
	if len(ports) != 2 {
		t.Fatalf("bocas: %+v", ports)
	}
	// lan1 lleva el PPPoE: sale como WAN sin moverse de sitio.
	if ports[0].ID != "wan" || ports[0].Label != "WAN" {
		t.Fatalf("la boca del uplink debía salir como WAN: %+v", ports[0])
	}
	if ports[1].ID != "lan2" {
		t.Fatalf("el resto no se toca: %+v", ports[1])
	}
}

func TestBuildEthPortsNoPisaUnaWanDelLayout(t *testing.T) {
	// Router con boca WAN dedicada: el layout ya la trae y el uplink no
	// debe promover ninguna otra.
	layout := []PortLayout{
		{ID: "wan", Name: "wan", Label: "WAN", Role: "wan"},
		{ID: "lan1", Name: "lan1", Label: "LAN 1", Role: "lan"},
	}
	states := ParsePortStates("wan up 1000 - 1 -\nlan1 up 1000 - 1 -\n")
	ports := BuildEthPorts(layout, states, nil, nil, "lan1")
	if len(ports) != 2 || ports[0].ID != "wan" || ports[1].ID != "lan1" {
		t.Fatalf("bocas: %+v", ports)
	}
}

func TestBuildEthPortsUplinkEtiquetado(t *testing.T) {
	// Uplink sobre VLAN ("lan1.7"): la boca es la de debajo.
	layout := []PortLayout{{ID: "lan1", Name: "lan1", Label: "LAN 1", Role: "lan"}}
	states := ParsePortStates("lan1 up 1000 - 1 -\n")
	ports := BuildEthPorts(layout, states, nil, nil, "lan1.7")
	if len(ports) != 1 || ports[0].ID != "wan" {
		t.Fatalf("bocas: %+v", ports)
	}
}

// A neighbour table with every state that matters: what the kernel has
// confirmed, and what it merely remembers.
func TestParseIPNeighSeparatesConfirmedFromRemembered(t *testing.T) {
	out := strings.Join([]string{
		"192.0.2.10 dev br-lan lladdr 02:00:00:00:00:10 REACHABLE",
		"192.0.2.11 dev br-lan lladdr 02:00:00:00:00:11 STALE",
		"192.0.2.12 dev br-lan lladdr 02:00:00:00:00:12 DELAY",
		"192.0.2.13 dev br-lan lladdr 02:00:00:00:00:13 PERMANENT",
		"192.0.2.14 dev br-lan  FAILED",
		"192.0.2.15 dev br-lan lladdr 02:00:00:00:00:15 FAILED",
		"192.0.2.16 dev br-lan lladdr 00:00:00:00:00:00 REACHABLE",
		"not-an-ip dev br-lan lladdr 02:00:00:00:00:17 REACHABLE",
	}, "\n")
	arp, stale := ParseIPNeigh(out)

	if len(arp) != 5 {
		t.Fatalf("arp: %+v", arp)
	}
	if arp["02:00:00:00:00:10"] != "192.0.2.10" || arp["02:00:00:00:00:11"] != "192.0.2.11" {
		t.Fatalf("addresses: %+v", arp)
	}
	// A stale entry still resolves an IP; it just proves nothing.
	for _, mac := range []string{"02:00:00:00:00:11", "02:00:00:00:00:15"} {
		if !stale[mac] {
			t.Fatalf("%s should be stale: %+v", mac, stale)
		}
	}
	for _, mac := range []string{"02:00:00:00:00:10", "02:00:00:00:00:12", "02:00:00:00:00:13"} {
		if stale[mac] {
			t.Fatalf("%s is confirmed: %+v", mac, stale)
		}
	}
	// An entry with no lladdr, a null MAC or a broken address is not a host.
	if _, ok := arp["00:00:00:00:00:00"]; ok {
		t.Fatalf("null MAC: %+v", arp)
	}
	if _, ok := arp["02:00:00:00:00:17"]; ok {
		t.Fatalf("invalid IP: %+v", arp)
	}
}

// One confirmed address is enough for a host with several of them, whatever
// order the table lists them in.
func TestParseIPNeighConfirmedWinsOverStale(t *testing.T) {
	for _, out := range []string{
		"192.0.2.20 dev br-lan lladdr 02:00:00:00:00:20 STALE\n" +
			"192.0.2.21 dev br-lan lladdr 02:00:00:00:00:20 REACHABLE",
		"192.0.2.21 dev br-lan lladdr 02:00:00:00:00:20 REACHABLE\n" +
			"192.0.2.20 dev br-lan lladdr 02:00:00:00:00:20 STALE",
	} {
		arp, stale := ParseIPNeigh(out)
		if stale["02:00:00:00:00:20"] {
			t.Fatalf("a confirmed address must settle the host: %+v", stale)
		}
		if arp["02:00:00:00:00:20"] != "192.0.2.21" {
			t.Fatalf("the confirmed address should be the one kept: %+v", arp)
		}
	}
}

func TestParseIPNeighOnGarbage(t *testing.T) {
	arp, stale := ParseIPNeigh("ip: command not found\n\n")
	if len(arp) != 0 || len(stale) != 0 {
		t.Fatalf("garbage: %+v %+v", arp, stale)
	}
}

// uciDhcpHosts: the shape `uci show dhcp` prints for host entries, with an
// anonymous section, a named one, a list of MACs (GL firmware) and an entry
// with no name. The grep in CmdDhcpReservations also lets through options of
// other sections, so one is included to prove they are ignored.
const uciDhcpHosts = `dhcp.@dnsmasq[0].domain='lan'
dhcp.@host[0]=host
dhcp.@host[0].name='printer'
dhcp.@host[0].mac='02:00:00:00:00:01'
dhcp.@host[0].ip='192.0.2.10'
dhcp.cfg0abc12=host
dhcp.cfg0abc12.name='nas'
dhcp.cfg0abc12.mac='02:00:00:00:00:02' '02:00:00:00:00:03'
dhcp.cfg0abc12.ip='192.0.2.20'
dhcp.@host[2]=host
dhcp.@host[2].mac='02:00:00:00:00:04'
dhcp.lan=dhcp
dhcp.lan.interface='lan'`

func TestParseDhcpReservations(t *testing.T) {
	res := ParseDhcpReservations(uciDhcpHosts)
	if len(res) != 4 {
		t.Fatalf("reservations: %+v", res)
	}
	// Sorted by MAC, uppercase like the leases.
	if res[0].MAC != "02:00:00:00:00:01" || res[0].Name != "printer" || res[0].IP != "192.0.2.10" {
		t.Fatalf("first: %+v", res[0])
	}
	// Both MACs of the list entry share its name and address.
	if res[1].Name != "nas" || res[2].Name != "nas" || res[1].IP != "192.0.2.20" {
		t.Fatalf("mac list: %+v %+v", res[1], res[2])
	}
	// An entry without a name is still a reservation (it pins an address).
	if res[3].MAC != "02:00:00:00:00:04" || res[3].Name != "" {
		t.Fatalf("nameless: %+v", res[3])
	}
	if got := ParseDhcpReservations(""); len(got) != 0 {
		t.Fatalf("empty output: %+v", got)
	}
}

func TestParseMdnsHosts(t *testing.T) {
	// Shape of `ubus call umdns hosts`: the ".local" name keys an object
	// with the addresses it resolved to.
	raw := `{"laptop-01.local":{"ipv4":"192.0.2.51","ipv6":"fe80::1"},
	         "printer.local":{"ipv4":"192.0.2.10"},
	         "no-address.local":{"ipv6":"fe80::2"}}`
	hosts := ParseMdnsHosts([]byte(raw))
	if len(hosts) != 2 {
		t.Fatalf("hosts: %+v", hosts)
	}
	// Keyed by IP, and the mDNS suffix is dropped.
	if hosts["192.0.2.51"] != "laptop-01" || hosts["192.0.2.10"] != "printer" {
		t.Fatalf("hosts: %+v", hosts)
	}
	// An entry with no IPv4 cannot name anything.
	for _, name := range hosts {
		if name == "no-address" {
			t.Fatalf("entry without ipv4 should be skipped: %+v", hosts)
		}
	}
	if got := ParseMdnsHosts([]byte("{}")); len(got) != 0 {
		t.Fatalf("empty: %+v", got)
	}
	if got := ParseMdnsHosts([]byte("not json")); len(got) != 0 {
		t.Fatalf("broken json must not panic: %+v", got)
	}
}
