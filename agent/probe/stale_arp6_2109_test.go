// stale_arp6_2109_test.go — #2109: la misma comprobación activa que #2073 pero
// sobre la tabla NDP de IPv6. Un host solo-IPv6 no aparece en `ip -4 neigh`,
// así que sin esta pasada no hay forma de confirmar que sigue ahí. La
// link-local necesita `-I <iface>` y el binario puede ser `ping -6` o `ping6`.
package probe

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type staleArp6Runner struct {
	mu      sync.Mutex
	v4      []string
	v4idx   int
	v6      []string
	v6idx   int
	pings   []string
	noReply map[string]bool // dirección que no contesta
}

func (r *staleArp6Runner) Run(_ context.Context, cmd string, _ time.Duration) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch cmd {
	case CmdIpNeigh:
		if r.v4idx >= len(r.v4) {
			return r.v4[len(r.v4)-1], nil
		}
		out := r.v4[r.v4idx]
		r.v4idx++
		return out, nil
	case CmdIpNeigh6:
		if r.v6idx >= len(r.v6) {
			return r.v6[len(r.v6)-1], nil
		}
		out := r.v6[r.v6idx]
		r.v6idx++
		return out, nil
	}
	if strings.Contains(cmd, "ping -6") || strings.HasPrefix(cmd, "ping6") {
		r.pings = append(r.pings, cmd)
		for ip := range r.noReply {
			if strings.Contains(cmd, ip) {
				return "", errors.New("sin respuesta")
			}
		}
		return "", nil
	}
	return "", errors.New("comando inesperado: " + cmd)
}

func (r *staleArp6Runner) pinged() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.pings...)
}

const v4Confirmada = `192.168.1.12 dev br-lan lladdr AA:AA:AA:AA:AA:03 REACHABLE
`

// Un vecino link-local STALE que responde queda confirmado, y el echo lleva la
// interfaz: sin `-I` el kernel no sabe por dónde mandar una fe80::.
func TestProbeArpConfirmsStaleIPv6LinkLocal(t *testing.T) {
	run := &staleArp6Runner{
		v4: []string{v4Confirmada},
		v6: []string{
			"fe80::1 dev br-lan lladdr BB:BB:BB:00:00:77 STALE\n",
			"fe80::1 dev br-lan lladdr BB:BB:BB:00:00:77 REACHABLE\n",
		},
	}
	p := NewProber(run, Options{})
	arp, _, _ := p.probeArp(context.Background())

	if arp["BB:BB:BB:00:00:77"] != "fe80::1" {
		t.Fatalf("la pareja IPv6 no se confirmó: %+v", arp)
	}
	pings := run.pinged()
	if len(pings) != 1 {
		t.Fatalf("pings: %d, esperaba 1 (%v)", len(pings), pings)
	}
	if !strings.Contains(pings[0], "-I br-lan") {
		t.Fatalf("link-local sin -I: %q", pings[0])
	}
	if !strings.Contains(pings[0], "ping6") {
		t.Fatalf("falta el fallback a ping6 para busybox sin -6: %q", pings[0])
	}
}

// Una dirección global no necesita -I y también se confirma.
func TestProbeArpConfirmsStaleIPv6Global(t *testing.T) {
	run := &staleArp6Runner{
		v4: []string{v4Confirmada},
		v6: []string{
			"2001:db8::5 dev br-lan lladdr BB:BB:BB:00:00:88 STALE\n",
			"2001:db8::5 dev br-lan lladdr BB:BB:BB:00:00:88 REACHABLE\n",
		},
	}
	p := NewProber(run, Options{})
	arp, _, _ := p.probeArp(context.Background())
	if arp["BB:BB:BB:00:00:88"] != "2001:db8::5" {
		t.Fatalf("global IPv6 no confirmada: %+v", arp)
	}
	if pings := run.pinged(); len(pings) != 1 || strings.Contains(pings[0], "-I ") {
		t.Fatalf("una global no debe llevar -I: %v", pings)
	}
}

// Si no contesta, no se inventa presencia: la entrada sigue sin confirmar y no
// entra en el mapa que alimenta la lista de clientes.
func TestProbeArpDoesNotConfirmSilentIPv6(t *testing.T) {
	run := &staleArp6Runner{
		v4:      []string{v4Confirmada},
		v6:      []string{"fe80::2 dev br-lan lladdr BB:BB:BB:00:00:99 STALE\n"},
		noReply: map[string]bool{"fe80::2": true},
	}
	p := NewProber(run, Options{})
	arp, _, _ := p.probeArp(context.Background())
	if _, ok := arp["BB:BB:BB:00:00:99"]; ok {
		t.Fatalf("un vecino que no responde no se confirma: %+v", arp)
	}
}

// El throttle es uno solo: la primera ronda sondea v4 y v6, la segunda dentro
// del intervalo no sondea ninguna de las dos.
func TestProbeArpIPv6SharesTheThrottle(t *testing.T) {
	run := &staleArp6Runner{
		v4: []string{v4Confirmada},
		v6: []string{
			"fe80::3 dev br-lan lladdr BB:BB:BB:00:00:AA STALE\n",
			"fe80::3 dev br-lan lladdr BB:BB:BB:00:00:AA REACHABLE\n",
		},
	}
	p := NewProber(run, Options{})
	_, _, _ = p.probeArp(context.Background())
	first := len(run.pinged())
	if first != 1 {
		t.Fatalf("primera ronda: %d pings, esperaba 1", first)
	}
	_, _, _ = p.probeArp(context.Background())
	if second := len(run.pinged()); second != first {
		t.Fatalf("la segunda ronda no debe pingear: %d -> %d", first, second)
	}
}
