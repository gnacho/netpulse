// stale_arp6_2109_test.go — #2109 en el camino SSH: el mismo recheck de #2073
// sobre `ip -6 neigh`. Lo lanza la unidad (está en la subred), confirma la
// pareja MAC-dirección de una link-local con `-I <iface>` y la suma al mapa que
// alimenta la lista de clientes.
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

type staleArp6Pool struct {
	mu    sync.Mutex
	v4    []string
	v4i   int
	v6    []string
	v6i   int
	calls []string
}

func (p *staleArp6Pool) Run(_, cmd string, _ time.Duration) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, cmd)
	switch cmd {
	case probe.CmdIpNeigh:
		if p.v4i >= len(p.v4) {
			return p.v4[len(p.v4)-1], nil
		}
		out := p.v4[p.v4i]
		p.v4i++
		return out, nil
	case probe.CmdIpNeigh6:
		if p.v6i >= len(p.v6) {
			return p.v6[len(p.v6)-1], nil
		}
		out := p.v6[p.v6i]
		p.v6i++
		return out, nil
	}
	if strings.Contains(cmd, "ping -6") || strings.HasPrefix(cmd, "ping6") {
		return "", nil
	}
	return "", errors.New("comando inesperado: " + cmd)
}

func (p *staleArp6Pool) RunCtx(_ context.Context, host, cmd string, timeout time.Duration) (string, error) {
	return p.Run(host, cmd, timeout)
}

func (p *staleArp6Pool) ping6Calls() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, c := range p.calls {
		if strings.Contains(c, "ping -6") || strings.Contains(c, "ping6") {
			out = append(out, c)
		}
	}
	return out
}

func TestGetArpConfirmsStaleIPv6FromTheUnit(t *testing.T) {
	pool := &staleArp6Pool{
		v4: []string{"192.168.2.12 dev br-lan lladdr AA:AA:AA:AA:AA:03 REACHABLE\n"},
		v6: []string{
			"fe80::1 dev br-lan lladdr CC:CC:CC:00:00:77 STALE\n",
			"fe80::1 dev br-lan lladdr CC:CC:CC:00:00:77 REACHABLE\n",
		},
	}
	c := &OpenWrtClient{Host: "192.168.2.11", pool: pool}

	arp, stale, _ := c.GetArp()
	if arp["CC:CC:CC:00:00:77"] != "fe80::1" {
		t.Fatalf("pareja IPv6 no confirmada: %+v", arp)
	}
	if stale["CC:CC:CC:00:00:77"] {
		t.Fatalf("una entrada confirmada no puede quedar stale: %v", stale)
	}
	pings := pool.ping6Calls()
	if len(pings) != 1 {
		t.Fatalf("comandos v6: %d, esperaba 1 (%v)", len(pings), pings)
	}
	for _, want := range []string{"-I br-lan", "fe80::1", "ping6", "wait"} {
		if !strings.Contains(pings[0], want) {
			t.Fatalf("el comando v6 no lleva %q: %q", want, pings[0])
		}
	}
}

// Una tabla v6 sin STALE no lanza ningún echo.
func TestGetArpNoStaleIPv6NoPing(t *testing.T) {
	pool := &staleArp6Pool{
		v4: []string{"192.168.2.12 dev br-lan lladdr AA:AA:AA:AA:AA:03 REACHABLE\n"},
		v6: []string{"fe80::9 dev br-lan lladdr CC:CC:CC:00:00:88 REACHABLE\n"},
	}
	c := &OpenWrtClient{Host: "192.168.2.11", pool: pool}
	_, _, _ = c.GetArp()
	if pings := pool.ping6Calls(); len(pings) != 0 {
		t.Fatalf("no debe pingear sin STALE v6: %v", pings)
	}
}
