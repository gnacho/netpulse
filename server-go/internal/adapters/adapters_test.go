// adapters_test.go — port de snapshot.test.js (canon demo) + tests de
// parseo con fixtures literales (wg dump, iwinfo, leases, netdev, /proc/stat,
// puertos, radios, FDB, board.json, top_blocked, parseBytes/fmtBytes).
package adapters

import (
	"context"
	"testing"
)

// --- snapshot.test.js: el PRIMER snapshot del demo es el canon EXACTO ---

func TestDemoPrimerSnapshotEsCanon(t *testing.T) {
	d := NewDemo()
	ov, err := d.GetOverview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Escalares canónicos del flint2
	var flint *Router
	for i := range ov.Routers {
		if ov.Routers[i].ID == "flint2" {
			flint = &ov.Routers[i]
		}
	}
	if flint == nil {
		t.Fatal("flint2 ausente")
	}
	if *flint.CPU != 23 || *flint.RAM != 41 || *flint.Temp != 54 {
		t.Fatalf("flint2 cpu/ram/temp: %v/%v/%v", *flint.CPU, *flint.RAM, *flint.Temp)
	}
	if flint.Clients != 33 || flint.Health != 98 {
		t.Fatalf("flint2 clients/health: %v/%v", flint.Clients, flint.Health)
	}
	// WAN
	if ov.WAN.DownMbps != 84.2 || ov.WAN.UpMbps != 12.6 || ov.WAN.LatencyMs != 8 {
		t.Fatalf("wan: %+v", ov.WAN)
	}
	// AdGuard
	if ov.Adguard.Queries24h != 84312 || ov.Adguard.Blocked24h != 15687 || ov.Adguard.BlockedPct != 18.6 {
		t.Fatalf("adguard: %+v", ov.Adguard)
	}
	if len(ov.Adguard.TopBlocked) != 5 || ov.Adguard.TopBlocked[0].Domain != "graph.facebook.com" {
		t.Fatalf("topBlocked: %+v", ov.Adguard.TopBlocked)
	}
	// WireGuard
	if ov.Wireguard.Status != "active" || len(ov.Wireguard.Peers) != 5 {
		t.Fatalf("wireguard: %+v", ov.Wireguard)
	}
	p0 := ov.Wireguard.Peers[0]
	if p0.ID != "pixel-8-pro" || !p0.Active || p0.Rx != "1,2 GB" || p0.Tx != "214 MB" || p0.LastHandshake != "hace 38 s" {
		t.Fatalf("peer0: %+v", p0)
	}
	// DeviceTotals
	if ov.DeviceTotals != (DeviceTotals{Total: 67, Online: 59, KnownOffline: 8, NewToday: 3}) {
		t.Fatalf("deviceTotals: %+v", ov.DeviceTotals)
	}
	// Health
	if ov.Health.Score != 92 || ov.Health.Label != "Excelente" || len(ov.Health.Breakdown) != 3 {
		t.Fatalf("health: %+v", ov.Health)
	}
	// ts en segundos (no ms)
	if ov.Ts > 1e12 {
		t.Fatalf("ts debería ser epoch segundos: %v", ov.Ts)
	}
	// Alertas canónicas
	if len(ov.Alerts) != 5 || ov.UnreadAlerts != 2 {
		t.Fatalf("alerts: %d unread %d", len(ov.Alerts), ov.UnreadAlerts)
	}
}

func TestDemoDevices67YTop(t *testing.T) {
	d := NewDemo()
	devs := d.GetDevices(context.Background())
	if len(devs) != 67 {
		t.Fatalf("devices: %d", len(devs))
	}
	offline := 0
	for _, dev := range devs {
		if !dev.Online {
			offline++
		}
		if !dev.Online && dev.Sparkline == nil {
			t.Fatalf("sparkline offline debe ser [] no null: %s", dev.ID)
		}
	}
	if offline != 8 {
		t.Fatalf("offline: %d", offline)
	}
	ov, _ := d.GetOverview(context.Background())
	if len(ov.TopDevices) != 5 {
		t.Fatalf("topDevices: %d", len(ov.TopDevices))
	}
	for i := 1; i < len(ov.TopDevices); i++ {
		if ov.TopDevices[i-1].TrafficMbps < ov.TopDevices[i].TrafficMbps {
			t.Fatal("topDevices no ordenado desc")
		}
	}
	if ov.TopDevices[0].ID != "imac-salon" {
		t.Fatalf("top1: %s", ov.TopDevices[0].ID)
	}
}

func TestDemoDetallePatioYFlint(t *testing.T) {
	d := NewDemo()
	det, err := d.GetRouterDetail(context.Background(), "patio")
	if err != nil || det == nil {
		t.Fatalf("patio: %v %v", det, err)
	}
	// Extras completos del patio
	ex, ok := det.Extras.(*demoExtras)
	if !ok {
		t.Fatalf("extras tipo: %T", det.Extras)
	}
	if ex.MAC != "C0:4A:00:9B:51:8D" || ex.RamMb != 128 || ex.Backhaul == nil || ex.Backhaul.Kind != "wireless" {
		t.Fatalf("extras patio: %+v", ex)
	}
	if len(det.Radios) != 2 || !det.Radios[0].Congested {
		t.Fatalf("radios patio: %+v", det.Radios)
	}
	if len(det.Clients) != 6 {
		t.Fatalf("clients patio: %d", len(det.Clients))
	}
	// flint2: radios null + adguard/wireguard + series + wgPeerExtras
	fdet, _ := d.GetRouterDetail(context.Background(), "flint2")
	if fdet.Radios != nil {
		t.Fatalf("radios flint2 debe ser null: %+v", fdet.Radios)
	}
	if fdet.Backhaul != nil {
		t.Fatalf("backhaul flint2 debe ser null: %+v", fdet.Backhaul)
	}
	if fdet.Adguard == nil || fdet.Wireguard == nil || fdet.AdguardSeries24h == nil ||
		fdet.WANLatency == nil || fdet.WGPeerExtras == nil || fdet.WGTotals30d == nil {
		t.Fatal("flint2 sin secciones de gateway")
	}
	if len(fdet.AdguardSeries24h) != 24 {
		t.Fatalf("adguardSeries24h: %d", len(fdet.AdguardSeries24h))
	}
	// Suma exacta del canon (84312 = permitidas + bloqueadas)
	var sumP, sumB int64
	for _, h := range fdet.AdguardSeries24h {
		sumP += h.Permitidas
		sumB += h.Bloqueadas
	}
	if sumP+sumB != 84312 || sumB != 15687 {
		t.Fatalf("series adguard: %d+%d", sumP, sumB)
	}
	// Series de rendimiento terminan en el valor actual del router
	if got := fdet.Series.H24[len(fdet.Series.H24)-1]; got.CPU != 23 || got.RAM != 41 || got.Temp != 54 {
		t.Fatalf("último punto 24h: %+v", got)
	}
	// Id desconocido → (nil, nil)
	none, err := d.GetRouterDetail(context.Background(), "nope")
	if none != nil || err != nil {
		t.Fatalf("id desconocido: %v %v", none, err)
	}
	// Dawn/AdguardClients → (nil, nil) en demo
	if dawn, err := d.GetDawn(context.Background()); dawn != nil || err != nil {
		t.Fatalf("dawn demo: %v %v", dawn, err)
	}
	if cl, err := d.GetAdguardClients(context.Background()); cl != nil || err != nil {
		t.Fatalf("adguardClients demo: %v %v", cl, err)
	}
}

func TestDemoRandomWalkEnRangos(t *testing.T) {
	d := NewDemo()
	for i := 0; i < 20; i++ {
		if err := d.Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	ov, _ := d.GetOverview(context.Background())
	for _, r := range ov.Routers {
		if *r.CPU < 2 || *r.CPU > 95 || *r.RAM < 10 || *r.RAM > 92 || *r.Temp < 30 || *r.Temp > 82 {
			t.Fatalf("%s fuera de rango: %+v", r.ID, r)
		}
	}
	if ov.WAN.LatencyMs < 6 || ov.WAN.LatencyMs > 11 {
		t.Fatalf("latencia: %v", ov.WAN.LatencyMs)
	}
	if ov.Adguard.Queries24h < 84312 || ov.Adguard.Blocked24h < 15687 {
		t.Fatal("adguard debe ser creciente")
	}
}

// --- Parseo WG dump (fixture literal) ---

func TestParseWGDump(t *testing.T) {
	dump := "wg0\tAB private =\t(none)\t51820\n" +
		"PEERKEY1=\t(none)\t5.224.10.20:51820\t10.0.0.2/32\t1700000060\t1200000000\t214000000\t0\n" +
		"PEERKEY2=\t(none)\t(none)\t10.0.0.4/32\t0\t3100000000\t402000000\t0\n"
	peers := ParseWGDump(dump)
	if len(peers) != 2 {
		t.Fatalf("peers: %d", len(peers))
	}
	if peers[0].Endpoint == nil || *peers[0].Endpoint != "5.224.10.20:51820" {
		t.Fatalf("endpoint: %+v", peers[0])
	}
	if peers[1].Endpoint != nil || peers[1].HandshakeSec != 0 {
		t.Fatalf("peer2: %+v", peers[1])
	}
	if peers[0].RxBytes != 1200000000 || peers[0].TxBytes != 214000000 {
		t.Fatalf("bytes: %+v", peers[0])
	}
}

// --- fmtBytes / parseBytes roundtrip del canon ---

func TestFmtParseBytes(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{1.2e9, "1,2 GB"}, {214e6, "214 MB"}, {3.1e9, "3,1 GB"}, {12e9, "12 GB"},
		{402e6, "402 MB"}, {500, "500 B"}, {2500, "3 KB"}, {0, "0 B"},
	}
	for _, c := range cases {
		if got := fmtBytes(c.in); got != c.want {
			t.Fatalf("fmtBytes(%v) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := parseBytes("1,2 GB"); got != 1.2e9 {
		t.Fatalf("parseBytes: %v", got)
	}
	if got := parseBytes("1,1 TB"); got != 1.1e12 {
		t.Fatalf("parseBytes TB: %v", got)
	}
	if got := parseBytes("214 MB"); got != 214e6 {
		t.Fatalf("parseBytes MB: %v", got)
	}
	if got := parseBytes("basura"); got != 0 {
		t.Fatalf("parseBytes inválido: %v", got)
	}
}

// --- /proc/stat ---

func TestParseProcStat(t *testing.T) {
	s, err := parseProcStat("cpu  4705 356 584 3699 23 0 23 0 0 0\n")
	if err != nil {
		t.Fatal(err)
	}
	idle := float64(3699 + 23)
	total := idle + float64(4705+356+584+0+23)
	if s.total != total || s.idleAll != idle {
		t.Fatalf("procstat: %+v", s)
	}
}

// --- /proc/net/dev ---

func TestParseNetDev(t *testing.T) {
	out := "  eth0: 1000000    0    0    0    0     0          0         0   500000    0\n" +
		"    lo: 9999999    0    0    0    0     0          0         0  8888888    0\n" +
		"br-lan: 7000000    0    0    0    0     0          0         0  6000000    0\n" +
		"  lan1: 2000000    0    0    0    0     0          0         0  1000000    0\n"
	rx, tx := parseNetDev(out)
	if rx != 3000000 || tx != 1500000 { // lo, br-lan excluidos
		t.Fatalf("netdev: %v/%v", rx, tx)
	}
}

// --- dhcp.leases ---

func TestParseDhcpLeasesFile(t *testing.T) {
	out := "1700000000 aa:bb:cc:dd:ee:ff 192.168.8.21 imac-de-marc 01:aa:bb:cc:dd:ee:ff\n" +
		"1700000001 11:22:33:44:55:66 192.168.8.34 * *\n"
	leases := parseDhcpLeasesFile(out)
	if len(leases) != 2 {
		t.Fatalf("leases: %d", len(leases))
	}
	if leases[0].MAC != "AA:BB:CC:DD:EE:FF" || leases[0].Hostname != "imac-de-marc" {
		t.Fatalf("lease0: %+v", leases[0])
	}
	if leases[1].Hostname != "" {
		t.Fatalf("lease1 hostname: %+v", leases[1])
	}
}

// --- wireless (iwinfo loop) ---

func TestParseWirelessClients(t *testing.T) {
	out := "A4:83:E7:21:0B:3C -48 5\nEC:71:DB:44:12:8A -72 2.4\n"
	m := parseWirelessClients(out)
	if len(m) != 2 {
		t.Fatalf("clients: %d", len(m))
	}
	if m["A4:83:E7:21:0B:3C"].Band != "5 GHz" || m["A4:83:E7:21:0B:3C"].SignalDbm != -48 {
		t.Fatalf("c0: %+v", m["A4:83:E7:21:0B:3C"])
	}
	if m["EC:71:DB:44:12:8A"].Band != "2.4 GHz" {
		t.Fatalf("c1: %+v", m["EC:71:DB:44:12:8A"])
	}
}

// --- puertos /sys ---

func TestParsePortStates(t *testing.T) {
	out := "eth0 up 2500\nlan1 up 1000\nlan2 down -1\nwlan0 up 0\n"
	ports := parsePortStates(out)
	if len(ports) != 4 {
		t.Fatalf("ports: %d", len(ports))
	}
	if ports[0].Speed != "2 Gbps" || !ports[0].Up {
		t.Fatalf("p0: %+v", ports[0])
	}
	if ports[1].Speed != "1 Gbps" {
		t.Fatalf("p1: %+v", ports[1])
	}
	if ports[2].Up || ports[2].Speed != "—" {
		t.Fatalf("p2: %+v", ports[2])
	}
}

// --- board.json ---

func TestParsePortLayout(t *testing.T) {
	board := `{"network":{"lan":{"ports":["lan1","lan2","lan3","lan4"],"device":"br-lan"},"wan":{"device":"wan","protocol":"dhcp"}}}`
	layout, err := parsePortLayout(board)
	if err != nil {
		t.Fatal(err)
	}
	if len(layout) != 5 || layout[0].ID != "wan" || layout[0].Name != "wan" || layout[0].Role != "wan" {
		t.Fatalf("layout: %+v", layout)
	}
	if layout[1].Label != "LAN 1" {
		t.Fatalf("label: %+v", layout[1])
	}
}

// --- bridge FDB ---

func TestParseBridgeFdb(t *testing.T) {
	out := "==PORTS==\n0x1 lan1\n0x2 lan2\n==MACS==\n1 aa:bb:cc:dd:ee:ff\n2 11:22:33:44:55:66\n"
	m := parseBridgeFdb(out)
	if len(m) != 2 || m["AA:BB:CC:DD:EE:FF"] != "lan1" || m["11:22:33:44:55:66"] != "lan2" {
		t.Fatalf("fdb: %+v", m)
	}
}

// --- radios iwinfo ---

func TestParseRadios(t *testing.T) {
	out := "2.4|6|HT20|20|3\n5|36|HT80|23|7\n"
	radios := parseRadios(out)
	if len(radios) != 2 {
		t.Fatalf("radios: %d", len(radios))
	}
	if radios[0].Name != "2.4 GHz" || radios[0].Channel != 6 || radios[0].WidthMhz != 20 || radios[0].Clients != 3 {
		t.Fatalf("r0: %+v", radios[0])
	}
	if radios[1].Name != "5 GHz" || radios[1].WidthMhz != 80 || radios[1].PowerDbm != 23 {
		t.Fatalf("r1: %+v", radios[1])
	}
}

// --- top_blocked_domains: ambos formatos ---

func TestParseTopBlocked(t *testing.T) {
	nuevo := parseTopBlocked([]byte(`[{"graph.facebook.com":1204},{"adservice.google.com":986}]`), 5)
	if len(nuevo) != 2 || nuevo[0].Domain != "graph.facebook.com" || nuevo[0].Count != 1204 {
		t.Fatalf("nuevo: %+v", nuevo)
	}
	viejo := parseTopBlocked([]byte(`[["graph.facebook.com",1204],["ads.tiktok.com",448]]`), 5)
	if len(viejo) != 2 || viejo[1].Domain != "ads.tiktok.com" || viejo[1].Count != 448 {
		t.Fatalf("viejo: %+v", viejo)
	}
	if vacio := parseTopBlocked([]byte(`null`), 5); len(vacio) != 0 {
		t.Fatalf("vacio: %+v", vacio)
	}
}

// --- spark determinista (LCG del dataset) ---

func TestSparkDeterminista(t *testing.T) {
	a := spark(11, 2.1, 1.68)
	b := spark(11, 2.1, 1.68)
	if len(a) != 12 {
		t.Fatalf("len: %d", len(a))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatal("spark no determinista")
		}
	}
	if a[11] != 2.1 { // último punto = base
		t.Fatalf("último: %v", a[11])
	}
	for _, v := range a {
		if v < 0 {
			t.Fatal("spark negativo")
		}
	}
}
