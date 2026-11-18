package adapters

// stale_arp_2073_test.go - recheck activo de vecinos ARP en STALE por SSH.
//
// El mismo caso que cubre el agente (#2063) pero en una unidad sondeada por
// SSH: el ping sale de la propia unidad, que es quien esta en la subred, y la
// confirmacion se lee de la relectura de `ip neigh` (la entrada pasa a
// REACHABLE cuando llega la respuesta ARP).

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
)

type staleArpPool struct {
	mu    sync.Mutex
	neigh []string
	idx   int
	calls []string
}

func (p *staleArpPool) Run(_, cmd string, _ time.Duration) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, cmd)
	if cmd == probe.CmdIpNeigh {
		if p.idx >= len(p.neigh) {
			return p.neigh[len(p.neigh)-1], nil
		}
		out := p.neigh[p.idx]
		p.idx++
		return out, nil
	}
	if strings.Contains(cmd, "ping -c 1 -W 1") {
		return "", nil
	}
	return "", errors.New("unexpected command")
}

func (p *staleArpPool) RunCtx(_ context.Context, host, cmd string, timeout time.Duration) (string, error) {
	return p.Run(host, cmd, timeout)
}

func (p *staleArpPool) pingCalls() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, c := range p.calls {
		if strings.Contains(c, "ping -c 1 -W 1") {
			out = append(out, c)
		}
	}
	return out
}

const (
	sshNeighStale = `192.168.2.10 dev br-lan lladdr AA:AA:AA:AA:AA:01 STALE
192.168.2.11 dev br-lan lladdr AA:AA:AA:AA:AA:02 STALE
192.168.2.12 dev br-lan lladdr AA:AA:AA:AA:AA:03 REACHABLE
`
	sshNeighAfterPing = `192.168.2.10 dev br-lan lladdr AA:AA:AA:AA:AA:01 REACHABLE
192.168.2.11 dev br-lan lladdr AA:AA:AA:AA:AA:02 STALE
192.168.2.12 dev br-lan lladdr AA:AA:AA:AA:AA:03 REACHABLE
`
)

func TestGetArpRechecksStaleFromTheUnit(t *testing.T) {
	pool := &staleArpPool{neigh: []string{sshNeighStale, sshNeighAfterPing}}
	c := &OpenWrtClient{Host: "192.168.2.11", pool: pool}

	arp, stale, _ := c.GetArp()

	if stale["AA:AA:AA:AA:AA:01"] {
		t.Fatalf("el vecino que respondio sigue stale: %v", stale)
	}
	if !stale["AA:AA:AA:AA:AA:02"] {
		t.Fatalf("el vecino que no respondio debe seguir stale: %v", stale)
	}
	if arp["AA:AA:AA:AA:AA:01"] != "192.168.2.10" {
		t.Fatalf("la IP confirmada se conserva: %v", arp)
	}
	pings := pool.pingCalls()
	if len(pings) != 1 {
		t.Fatalf("se esperaba un unico comando de ping, hubo %d: %v", len(pings), pings)
	}
	for _, want := range []string{"192.168.2.10", "192.168.2.11", "wait"} {
		if !strings.Contains(pings[0], want) {
			t.Fatalf("el comando de ping no lleva %q: %q", want, pings[0])
		}
	}
	if strings.Contains(pings[0], "192.168.2.12") {
		t.Fatalf("no se sondea un vecino ya confirmado: %q", pings[0])
	}
}

func TestGetArpStaleRecheckIsThrottled(t *testing.T) {
	pool := &staleArpPool{neigh: []string{sshNeighStale, sshNeighAfterPing}}
	c := &OpenWrtClient{Host: "192.168.2.11", pool: pool}

	_, _, _ = c.GetArp()
	first := len(pool.pingCalls())
	_, _, _ = c.GetArp()
	if second := len(pool.pingCalls()); second != first {
		t.Fatalf("la segunda lectura dentro del intervalo no debe pingear: %d -> %d", first, second)
	}
}
