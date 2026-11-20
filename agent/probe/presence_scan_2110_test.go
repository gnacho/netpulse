// presence_scan_2110_test.go — #2110: barrido activo de las subredes que la
// unidad enruta para ver a los hosts que no dejan entrada de vecino.
package probe

import (
	"context"
	"errors"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

type scanRunner struct {
	mu       sync.Mutex
	forward  string
	addrs    string
	commands []string
}

func (r *scanRunner) Run(_ context.Context, cmd string, _ time.Duration) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = append(r.commands, cmd)
	switch {
	case strings.HasPrefix(cmd, "cat /proc/sys/net/ipv4/ip_forward"):
		if r.forward == "" {
			return "", errors.New("sin fichero")
		}
		return r.forward + "\n", nil
	case strings.HasPrefix(cmd, "ip -4 -o addr show scope global"):
		return r.addrs, nil
	}
	return "", errors.New("comando inesperado: " + cmd)
}

func captureScan(t *testing.T) *[]string {
	t.Helper()
	var mu sync.Mutex
	var got []string
	prev := scanProbe
	scanProbe = func(_ context.Context, ip string) {
		mu.Lock()
		got = append(got, ip)
		mu.Unlock()
	}
	t.Cleanup(func() { scanProbe = prev })
	return &got
}

const twoSubnets = `2: br-lan    inet 192.168.10.1/30 brd 192.168.10.3 scope global br-lan
3: br-iot    inet 10.0.0.1/30 brd 10.0.0.3 scope global br-iot
`

func TestScanPresenceSweepsRoutedSubnets(t *testing.T) {
	got := captureScan(t)
	run := &scanRunner{forward: "1", addrs: twoSubnets}
	p := NewProber(run, Options{})
	p.scanPresence(context.Background())

	sort.Strings(*got)
	want := []string{"10.0.0.1", "10.0.0.2", "192.168.10.1", "192.168.10.2"}
	if strings.Join(*got, ",") != strings.Join(want, ",") {
		t.Fatalf("direcciones sondeadas = %v, esperaba %v", *got, want)
	}
}

func TestScanPresenceSkipsUnitThatDoesNotRoute(t *testing.T) {
	got := captureScan(t)
	// Dos subredes pero forwarding a 0: es un AP, no un router.
	p := NewProber(&scanRunner{forward: "0", addrs: twoSubnets}, Options{})
	p.scanPresence(context.Background())
	if len(*got) != 0 {
		t.Fatalf("una unidad que no enruta no debe barrer: %v", *got)
	}
}

func TestScanPresenceSkipsSingleSubnet(t *testing.T) {
	got := captureScan(t)
	addrs := "2: br-lan inet 192.168.10.1/30 brd 192.168.10.3 scope global br-lan\n"
	p := NewProber(&scanRunner{forward: "1", addrs: addrs}, Options{})
	p.scanPresence(context.Background())
	if len(*got) != 0 {
		t.Fatalf("con una sola subred no se barre: %v", *got)
	}
}

// La válvula de escape del operador: `on` fuerza en una unidad que no enruta,
// `off` apaga el barrido aunque enrute.
func TestScanPresenceEnvironmentOverride(t *testing.T) {
	got := captureScan(t)
	addrs := "2: br-lan inet 192.168.10.1/30 brd 192.168.10.3 scope global br-lan\n"

	t.Setenv(presenceScanEnv, "on")
	p := NewProber(&scanRunner{forward: "0", addrs: addrs}, Options{})
	p.scanPresence(context.Background())
	if len(*got) != 2 {
		t.Fatalf("con =on debe barrer la unica subred: %v", *got)
	}

	*got = nil
	t.Setenv(presenceScanEnv, "off")
	p2 := NewProber(&scanRunner{forward: "1", addrs: twoSubnets}, Options{})
	p2.scanPresence(context.Background())
	if len(*got) != 0 {
		t.Fatalf("con =off no debe barrer: %v", *got)
	}
}

func TestScanPresenceIsThrottled(t *testing.T) {
	got := captureScan(t)
	p := NewProber(&scanRunner{forward: "1", addrs: twoSubnets}, Options{})
	p.scanPresence(context.Background())
	first := len(*got)
	if first == 0 {
		t.Fatal("la primera ronda debía sondear")
	}
	p.scanPresence(context.Background())
	if second := len(*got); second != first {
		t.Fatalf("la segunda ronda dentro del intervalo no debe sondear: %d -> %d", first, second)
	}
}

func TestHostAddrs(t *testing.T) {
	cases := []struct {
		prefix string
		want   int
		first  string
		last   string
	}{
		{"192.168.1.0/30", 2, "192.168.1.1", "192.168.1.2"},
		{"192.168.1.0/24", 254, "192.168.1.1", "192.168.1.254"},
		{"192.168.1.0/31", 0, "", ""},
		{"192.168.1.7/32", 0, "", ""},
	}
	for _, c := range cases {
		pf := mustPrefix(t, c.prefix)
		got := HostAddrs(pf, PresenceScanMaxHosts)
		if len(got) != c.want {
			t.Errorf("%s: %d hosts, esperaba %d", c.prefix, len(got), c.want)
			continue
		}
		if c.want > 0 && (got[0] != c.first || got[len(got)-1] != c.last) {
			t.Errorf("%s: %s..%s, esperaba %s..%s", c.prefix, got[0], got[len(got)-1], c.first, c.last)
		}
	}
	// Con tope, nunca más de lo pedido.
	if got := HostAddrs(mustPrefix(t, "10.0.0.0/16"), 50); len(got) != 50 {
		t.Errorf("/16 con tope 50: %d direcciones", len(got))
	}
}

func mustPrefix(t *testing.T, s string) netip.Prefix {
	t.Helper()
	pf, err := netip.ParsePrefix(s)
	if err != nil {
		t.Fatalf("prefix %q: %v", s, err)
	}
	return pf
}

// El barrido va enganchado al sondeo de vecinos: sin la llamada, un host sin
// entrada de vecino nunca se vería.
func TestProbeArpRunsThePresenceScan(t *testing.T) {
	got := captureScan(t)
	run := &scanRunner{forward: "1", addrs: twoSubnets}
	p := NewProber(run, Options{})
	_, _, _ = p.probeArp(context.Background())
	if len(*got) == 0 {
		t.Fatal("probeArp debe barrer antes de leer la tabla de vecinos")
	}
}
