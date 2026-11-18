package probe

// stale_arp_2073_test.go - recheck activo de vecinos ARP en STALE.
//
// #2063: un host cableado con IP estatica que lleva rato en silencio caduca de
// la FDB y su entrada ARP queda STALE, asi que NetPulse lo marcaba offline
// estando arriba. ParseIPNeigh solo acepta estados confirmados (#507), y eso
// no se toca: lo que se anyade es una comprobacion activa ANTES de dar el
// vecino por fantasma. El kernel pasa la entrada a REACHABLE cuando llega la
// respuesta ARP, asi que se confirma la pareja IP-MAC.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

type staleArpRunner struct {
	mu    sync.Mutex
	neigh []string
	idx   int
	pings []string
	fail  string // IP que no responde (el ping devuelve error)
}

func (r *staleArpRunner) Run(_ context.Context, cmd string, _ time.Duration) (string, error) {
	if cmd == CmdIpNeigh {
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.idx >= len(r.neigh) {
			return r.neigh[len(r.neigh)-1], nil
		}
		out := r.neigh[r.idx]
		r.idx++
		return out, nil
	}
	if strings.HasPrefix(cmd, "ping -c 1 -W 1 ") {
		ip := strings.TrimPrefix(cmd, "ping -c 1 -W 1 ")
		ip = strings.Fields(ip)[0]
		r.mu.Lock()
		r.pings = append(r.pings, ip)
		fail := r.fail == ip
		r.mu.Unlock()
		if fail {
			return "", &fakeErr{cmd}
		}
		return "", nil
	}
	return "", &fakeErr{cmd}
}

func (r *staleArpRunner) pinged() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.pings))
	copy(out, r.pings)
	return out
}

const (
	neighStale = `192.168.1.10 dev br-lan lladdr AA:AA:AA:AA:AA:01 STALE
192.168.1.11 dev br-lan lladdr AA:AA:AA:AA:AA:02 STALE
192.168.1.12 dev br-lan lladdr AA:AA:AA:AA:AA:03 REACHABLE
`
	neighAfterPing = `192.168.1.10 dev br-lan lladdr AA:AA:AA:AA:AA:01 REACHABLE
192.168.1.11 dev br-lan lladdr AA:AA:AA:AA:AA:02 STALE
192.168.1.12 dev br-lan lladdr AA:AA:AA:AA:AA:03 REACHABLE
`
)

// El vecino que responde sale del set stale (queda confirmado); el que no
// responde sigue stale y no se inventa presencia.
func TestProbeArpConfirmsStaleNeighbourThatAnswers(t *testing.T) {
	run := &staleArpRunner{neigh: []string{neighStale, neighAfterPing}, fail: "192.168.1.11"}
	p := NewProber(run, Options{})

	arp, stale, _ := p.probeArp(context.Background())

	got := map[string]bool{}
	for _, mac := range stale {
		got[mac] = true
	}
	if got["AA:AA:AA:AA:AA:01"] {
		t.Fatalf("el vecino que respondio sigue stale: %v", stale)
	}
	if !got["AA:AA:AA:AA:AA:02"] {
		t.Fatalf("el vecino que no respondio debe seguir stale: %v", stale)
	}
	if arp["AA:AA:AA:AA:AA:01"] != "192.168.1.10" {
		t.Fatalf("la IP confirmada se conserva: %v", arp)
	}
	pings := run.pinged()
	if len(pings) != 2 {
		t.Fatalf("solo se sondean los STALE (2), se pingearon %v", pings)
	}
}

// El recheck no corre en cada sondeo: el sondeo del agente es cada ~30 s.
func TestProbeArpStalePingIsThrottled(t *testing.T) {
	run := &staleArpRunner{neigh: []string{neighStale, neighAfterPing}}
	p := NewProber(run, Options{})

	_, _, _ = p.probeArp(context.Background())
	first := len(run.pinged())
	_, _, _ = p.probeArp(context.Background())
	if second := len(run.pinged()); second != first {
		t.Fatalf("la segunda ronda dentro del intervalo no debe pingear: %d -> %d", first, second)
	}
}

// Tope por ronda: nunca un barrido, aunque la tabla tenga muchos STALE.
func TestProbeArpStalePingIsCapped(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "10.0.0.%d dev br-lan lladdr AA:BB:CC:DD:EE:%02X STALE\n", i+1, i+1)
	}
	out := b.String()
	run := &staleArpRunner{neigh: []string{out, out}}
	p := NewProber(run, Options{})
	_, stale, _ := p.probeArp(context.Background())
	if len(stale) != 40 {
		t.Fatalf("el fixture debe dar 40 STALE, dio %d", len(stale))
	}
	if got := len(run.pinged()); got != StalePingMax {
		t.Fatalf("se pingearon %d vecinos, se esperaban %d", got, StalePingMax)
	}
}

// Sin vecinos STALE no se lanza ni un ping.
func TestProbeArpNoStaleNoPing(t *testing.T) {
	out := "192.168.1.12 dev br-lan lladdr AA:AA:AA:AA:AA:03 REACHABLE\n"
	run := &staleArpRunner{neigh: []string{out, out}}
	p := NewProber(run, Options{})
	_, stale, _ := p.probeArp(context.Background())
	if len(stale) != 0 {
		t.Fatalf("stale inesperado: %v", stale)
	}
	if got := run.pinged(); len(got) != 0 {
		t.Fatalf("no debe pingear sin STALE: %v", got)
	}
}
