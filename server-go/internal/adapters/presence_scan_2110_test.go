// presence_scan_2110_test.go — #2110 en el camino SSH: la unidad sin agente
// barre sus subredes enrutadas con `ping` (busybox) antes de leer la tabla,
// para que los hosts que no dejan entrada de vecino aparezcan.
package adapters

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
)

type scanPool struct {
	mu      sync.Mutex
	forward string
	addrs   string
	neigh   string
	calls   []string
}

func (p *scanPool) Run(_, cmd string, _ time.Duration) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, cmd)
	switch {
	case strings.HasPrefix(cmd, "cat /proc/sys/net/ipv4/ip_forward"):
		return p.forward, nil
	case strings.HasPrefix(cmd, "ip -4 -o addr show scope global"):
		return p.addrs, nil
	case strings.HasPrefix(cmd, "for ip in "):
		return "", nil
	case cmd == probe.CmdIpNeigh:
		return p.neigh, nil
	case cmd == probe.CmdIpNeigh6:
		return "", nil
	}
	return "", errors.New("comando inesperado: " + cmd)
}

func (p *scanPool) RunCtx(_ context.Context, host, cmd string, timeout time.Duration) (string, error) {
	return p.Run(host, cmd, timeout)
}

func (p *scanPool) sweeps() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, c := range p.calls {
		if strings.HasPrefix(c, "for ip in ") {
			out = append(out, c)
		}
	}
	return out
}

const scanNeigh = "192.168.10.12 dev br-lan lladdr AA:AA:AA:AA:AA:03 REACHABLE\n"

func TestGetArpSweepsRoutedSubnetsOverSsh(t *testing.T) {
	pool := &scanPool{
		forward: "1\n",
		addrs: `2: br-lan inet 192.168.10.1/30 brd 192.168.10.3 scope global br-lan
3: br-iot inet 10.0.0.1/30 brd 10.0.0.3 scope global br-iot
`,
		neigh: scanNeigh,
	}
	c := &OpenWrtClient{Host: "192.168.10.2", pool: pool}
	_, _, _ = c.GetArp()

	sweeps := pool.sweeps()
	if len(sweeps) != 1 {
		t.Fatalf("barridos: %d, esperaba 1 (%v)", len(sweeps), sweeps)
	}
	for _, want := range []string{"192.168.10.1", "192.168.10.2", "10.0.0.1", "10.0.0.2", "wait"} {
		if !strings.Contains(sweeps[0], want) {
			t.Fatalf("el barrido no lleva %q: %q", want, sweeps[0])
		}
	}
	// Tandas de como mucho 32 direcciones por `wait`.
	if n := strings.Count(sweeps[0], "wait"); n < 2 {
		t.Fatalf("esperaba una tanda por subred: %q", sweeps[0])
	}
}

func TestGetArpSkipsSweepWhenUnitDoesNotRoute(t *testing.T) {
	pool := &scanPool{
		forward: "0\n",
		addrs:   "2: br-lan inet 192.168.10.1/30 brd 192.168.10.3 scope global br-lan\n",
		neigh:   scanNeigh,
	}
	c := &OpenWrtClient{Host: "192.168.10.2", pool: pool}
	_, _, _ = c.GetArp()
	if sweeps := pool.sweeps(); len(sweeps) != 0 {
		t.Fatalf("una unidad que no enruta no barre: %v", sweeps)
	}
}

func TestGetArpSweepIsThrottled(t *testing.T) {
	pool := &scanPool{
		forward: "1\n",
		addrs:   "2: br-lan inet 192.168.10.1/30 brd 192.168.10.3 scope global br-lan\n3: br-iot inet 10.0.0.1/30 brd 10.0.0.3 scope global br-iot\n",
		neigh:   scanNeigh,
	}
	c := &OpenWrtClient{Host: "192.168.10.2", pool: pool}
	_, _, _ = c.GetArp()
	first := len(pool.sweeps())
	if first != 1 {
		t.Fatalf("primera lectura: %d barridos", first)
	}
	_, _, _ = c.GetArp()
	if second := len(pool.sweeps()); second != first {
		t.Fatalf("la segunda lectura dentro del intervalo no debe barrer: %d -> %d", first, second)
	}
}
