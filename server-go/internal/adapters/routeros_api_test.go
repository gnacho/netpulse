package adapters

import (
	"bufio"
	"bytes"
	"net"
	"testing"
	"time"
)

func TestRouterOSLenRoundTrip(t *testing.T) {
	for _, l := range []int{0, 1, 0x7F, 0x80, 0x3FFF, 0x4000, 0x1FFFFF, 0x200000, 0x0FFFFFFF, 0x10000000, 5_000_000} {
		var buf bytes.Buffer
		if err := writeRouterOSLen(&buf, l); err != nil {
			t.Fatalf("writeRouterOSLen(%d): %v", l, err)
		}
		got, err := readRouterOSLen(bufio.NewReader(&buf))
		if err != nil {
			t.Fatalf("readRouterOSLen(%d): %v", l, err)
		}
		if got != l {
			t.Errorf("roundtrip(%d) = %d", l, got)
		}
	}
}

func TestRouterOSWordRoundTrip(t *testing.T) {
	words := []string{"", "/system/resource/print", "=cpu-load=6", "!re"}
	for _, w := range words {
		var buf bytes.Buffer
		if err := writeRouterOSWord(&buf, w); err != nil {
			t.Fatalf("writeRouterOSWord(%q): %v", w, err)
		}
		got, err := readRouterOSWord(bufio.NewReader(&buf))
		if err != nil {
			t.Fatalf("readRouterOSWord(%q): %v", w, err)
		}
		if got != w {
			t.Errorf("roundtrip(%q) = %q", w, got)
		}
	}
}

func TestRouterOSPorts(t *testing.T) {
	rows := []map[string]string{
		{"name": "ether1", "type": "ether", "running": "true"},
		{"name": "ether2", "type": "ether", "running": "false"},
		{"name": "bridge1", "type": "bridge", "running": "true"}, // no es "ether" → excluida
	}
	ports := routerOSPorts(rows)
	if len(ports) != 2 {
		t.Fatalf("len(ports) = %d, want 2 (bridge excluded): %+v", len(ports), ports)
	}
	if ports[0].ID != "ether1" || !ports[0].Up {
		t.Errorf("ports[0] = %+v", ports[0])
	}
	if ports[1].ID != "ether2" || ports[1].Up {
		t.Errorf("ports[1] = %+v", ports[1])
	}
}

func TestRouterOSLeases(t *testing.T) {
	rows := []map[string]string{
		{"address": "192.168.50.10", "mac-address": "aa:bb:cc:dd:ee:ff", "host-name": "laptop"},
		{"address": "192.168.50.11", "mac-address": "", "host-name": "no-mac"}, // sin MAC → excluida
	}
	leases := routerOSLeases(rows)
	if len(leases) != 1 {
		t.Fatalf("len(leases) = %d, want 1: %+v", len(leases), leases)
	}
	if leases[0].MAC != "AA:BB:CC:DD:EE:FF" || leases[0].IP != "192.168.50.10" || leases[0].Hostname != "laptop" {
		t.Errorf("leases[0] = %+v", leases[0])
	}
}

func TestRouterOSWANInterface(t *testing.T) {
	t.Run("explicit interface field", func(t *testing.T) {
		rows := []map[string]string{
			{"dst-address": "192.168.50.0/24", "interface": "bridge-lan"},
			{"dst-address": "0.0.0.0/0", "interface": "ether1"},
		}
		if got := routerOSWANInterface(rows); got != "ether1" {
			t.Errorf("routerOSWANInterface = %q, want ether1", got)
		}
	})
	t.Run("PPPoE: gateway field IS the interface name", func(t *testing.T) {
		// Caso real: RouterOS con WAN PPPoE no expone "interface" en la ruta
		// por defecto, solo "gateway" con el nombre del interfaz PPPoE.
		rows := []map[string]string{
			{"dst-address": "0.0.0.0/0", "gateway": "telekom-pppoe", "immediate-gw": "telekom-pppoe"},
		}
		if got := routerOSWANInterface(rows); got != "telekom-pppoe" {
			t.Errorf("routerOSWANInterface = %q, want telekom-pppoe", got)
		}
	})
	t.Run("plain IP gateway: no resolvable interface", func(t *testing.T) {
		rows := []map[string]string{
			{"dst-address": "0.0.0.0/0", "gateway": "192.168.1.1"},
		}
		if got := routerOSWANInterface(rows); got != "" {
			t.Errorf("routerOSWANInterface = %q, want empty (IP gateway, not an interface name)", got)
		}
	})
	if got := routerOSWANInterface(nil); got != "" {
		t.Errorf("routerOSWANInterface(nil) = %q, want empty", got)
	}
}

func TestRouterOSNetRate(t *testing.T) {
	l := &Live{roNetPrev: map[string]netByteSample{}}
	ifRows := []map[string]string{
		{"name": "ether1", "rx-byte": "1000000", "tx-byte": "500000"},
	}

	// Primera muestra: sin delta posible → nil.
	if got := l.routerOSNetRate("gw", ifRows, "ether1"); got != nil {
		t.Fatalf("first sample = %+v, want nil", got)
	}

	// Simular 1 segundo transcurrido con 125000 bytes rx / 62500 bytes tx más
	// (=1 Mbps / 0.5 Mbps) forzando el timestamp cacheado hacia el pasado.
	l.mu.Lock()
	prev := l.roNetPrev["gw"]
	prev.at = prev.at.Add(-1 * time.Second)
	l.roNetPrev["gw"] = prev
	l.mu.Unlock()
	ifRows2 := []map[string]string{
		{"name": "ether1", "rx-byte": "1125000", "tx-byte": "562500"},
	}
	got := l.routerOSNetRate("gw", ifRows2, "ether1")
	if got == nil || got.RxBps == nil || got.TxBps == nil {
		t.Fatalf("second sample = %+v, want non-nil rates", got)
	}
	if *got.RxBps < 990000 || *got.RxBps > 1010000 {
		t.Errorf("RxBps = %v, want ~1000000", *got.RxBps)
	}

	// Contador reiniciado (reboot del router) → nil, no un pico negativo falso.
	l.mu.Lock()
	prev = l.roNetPrev["gw"]
	prev.at = prev.at.Add(-1 * time.Second)
	l.roNetPrev["gw"] = prev
	l.mu.Unlock()
	ifRowsReset := []map[string]string{
		{"name": "ether1", "rx-byte": "100", "tx-byte": "50"},
	}
	if got := l.routerOSNetRate("gw", ifRowsReset, "ether1"); got != nil {
		t.Errorf("reset counters = %+v, want nil", got)
	}
}

// fakeRouterOSServer simula el suficiente protocolo API para probar
// login+query de punta a punta sin hardware real: acepta cualquier login y
// responde /system/resource/print con una fila fija.
func fakeRouterOSServer(t *testing.T) (addr string, closeFn func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		c := &routerOSAPIConn{conn: conn, r: bufio.NewReader(conn)}

		// /login
		if _, err := c.readSentence(); err != nil {
			return
		}
		_ = c.writeSentence("!done")

		// /system/resource/print
		if _, err := c.readSentence(); err != nil {
			return
		}
		_ = c.writeSentence("!re", "=cpu-load=6", "=free-memory=892813312", "=total-memory=1073741824",
			"=uptime=1d02h03m04s", "=version=7.23.3 (stable)", "=board-name=RB5009UG+S+", "=architecture-name=arm64")
		_ = c.writeSentence("!done")

		// /system/identity/print
		if _, err := c.readSentence(); err != nil {
			return
		}
		_ = c.writeSentence("!re", "=name=RB5009UG")
		_ = c.writeSentence("!done")
	}()
	return ln.Addr().String(), func() { ln.Close() }
}

func TestRouterOSAPILoginAndQuery(t *testing.T) {
	addr, closeFn := fakeRouterOSServer(t)
	defer closeFn()
	host, port, _ := net.SplitHostPort(addr)
	_ = port // dialRouterOSAPI hardcodes 8728; dial the fake server directly instead

	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatalf("dial fake server: %v", err)
	}
	c := &routerOSAPIConn{conn: conn, r: bufio.NewReader(conn)}
	defer c.Close()

	if err := c.writeSentence("/login", "=name=admin", "=password=test"); err != nil {
		t.Fatalf("write login: %v", err)
	}
	reply, err := c.readSentence()
	if err != nil {
		t.Fatalf("read login reply: %v", err)
	}
	if len(reply) == 0 || reply[0] != "!done" {
		t.Fatalf("login reply = %v, want !done", reply)
	}

	rows, err := c.query("/system/resource/print")
	if err != nil {
		t.Fatalf("query resource: %v", err)
	}
	if len(rows) != 1 || rows[0]["cpu-load"] != "6" || rows[0]["board-name"] != "RB5009UG+S+" {
		t.Fatalf("resource rows = %+v", rows)
	}

	idRows, err := c.query("/system/identity/print")
	if err != nil {
		t.Fatalf("query identity: %v", err)
	}
	if len(idRows) != 1 || idRows[0]["name"] != "RB5009UG" {
		t.Fatalf("identity rows = %+v", idRows)
	}

	res := routerOSResource{
		CPULoad: rows[0]["cpu-load"], FreeMemory: rows[0]["free-memory"], TotalMemory: rows[0]["total-memory"],
		Uptime: rows[0]["uptime"], Version: rows[0]["version"], BoardName: rows[0]["board-name"], Architecture: rows[0]["architecture-name"],
	}
	p, err := buildRouterOSPoll(RouterConfig{ID: "gw", Host: host}, res, routerOSIdentity{Name: idRows[0]["name"]})
	if err != nil {
		t.Fatalf("buildRouterOSPoll: %v", err)
	}
	if p.cpu != 6 || p.board.Hostname != "RB5009UG" || p.board.Release.Version != "7.23.3 (stable)" {
		t.Errorf("polled = %+v board=%+v", p, p.board)
	}
}
