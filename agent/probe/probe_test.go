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
	ports := BuildEthPorts(layout, states, nil, ifaces)
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
	ports = BuildEthPorts(nil, []PortState{{Name: "lan1", Up: true, Speed: "1 Gbps"}}, nil, ifaces)
	if len(ports) != 1 || ports[0].Iface != "lan1" || ports[0].RxBytes != 1000 {
		t.Fatalf("fallback stats: %+v", ports)
	}
}

func TestParsePingSummary(t *testing.T) {
	out := "3 packets transmitted, 3 received, 0% packet loss, time 2003ms\nrtt min/avg/max/mdev = 8.123/9.456/10.999/0.5 ms"
	lat, loss := ParsePingSummary(out)
	if lat == nil || *lat != 9 || loss == nil || *loss != 0 {
		t.Fatalf("ping: %v %v", lat, loss)
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

func TestParsePortsYLayout(t *testing.T) {
	states := ParsePortStates("eth0 up 2500\nlan1 up 1000\nlan2 down -1\nwlan0 up 0\n")
	if len(states) != 4 || states[0].Speed != "2 Gbps" || states[2].Up || states[2].Speed != "—" {
		t.Fatalf("states: %+v", states)
	}
	board := `{"network":{"lan":{"ports":["lan1","lan2","lan3","lan4"],"device":"br-lan"},"wan":{"device":"wan","protocol":"dhcp"}}}`
	layout, err := ParsePortLayout(board)
	if err != nil || len(layout) != 5 || layout[0].ID != "wan" || layout[1].Label != "LAN 1" {
		t.Fatalf("layout: %v %+v", err, layout)
	}
	// AP en bridge: wan en br-lan → se re-etiqueta LAN 5
	ports := BuildEthPorts(layout, states, map[string]bool{"wan": true}, nil)
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
	ports = BuildEthPorts(nil, ParsePortStates("lan2 up 1000\nlan10 up 100\nwan down -1\neth0 up 2500\n"), nil, nil)
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
	}, nil, nil)
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

// ---------------------------------------------------------------------------
// Prober local con runner fake
// ---------------------------------------------------------------------------

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
	if !s.APs[0].Local {
		t.Error("el AP local debería tener Local=true")
	}
	if s.APs[0].BSSID != "9C:9D:7E:1B:EA:B3" {
		t.Errorf("BSSID local = %q (esperaba uppercased)", s.APs[0].BSSID)
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
