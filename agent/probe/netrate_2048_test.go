package probe

import (
	"context"
	"testing"
	"time"
)

// seqNetRunner devuelve una salida distinta de /proc/net/dev en cada llamada y
// vacío para el resto de comandos.
type seqNetRunner struct {
	netdev []string
	i      int
}

func (r *seqNetRunner) Run(_ context.Context, cmd string, _ time.Duration) (string, error) {
	if cmd != CmdNetDev {
		return "", &fakeErr{cmd}
	}
	if r.i >= len(r.netdev) {
		return r.netdev[len(r.netdev)-1], nil
	}
	out := r.netdev[r.i]
	r.i++
	return out, nil
}

// #2048: el agregado de todas las bocas cuenta la bajada dos veces (entra por
// la WAN y sale por la LAN). Cuando el equipo sabe cuál es su WAN, la tasa
// publicada es la suya.
func TestSystemRatesUseTheWanPort(t *testing.T) {
	// eth0 = LAN (reenvía la bajada), eth1 = WAN.
	first := "  eth0: 1000000 0 0 0 0 0 0 0 500000 0 0 0 0 0 0 0\n" +
		"  eth1: 1000000 0 0 0 0 0 0 0 100000 0 0 0 0 0 0 0\n"
	second := "  eth0: 2000000 0 0 0 0 0 0 0 1000000 0 0 0 0 0 0 0\n" +
		"  eth1: 3000000 0 0 0 0 0 0 0 110000 0 0 0 0 0 0 0\n"

	p := NewProber(&seqNetRunner{netdev: []string{first, second}}, Options{})
	p.lastWan = &WanInfo{Port: "eth1"}
	p.probeSystem(context.Background()) // abre la ventana
	sd := p.probeSystem(context.Background())
	if sd.RxBps == nil || sd.TxBps == nil {
		t.Fatal("sin tasas en la segunda muestra")
	}
	// WAN: rx +2e6, tx +1e4 → tx/rx = 0,005. Agregado: rx +3e6, tx +5e5 → 0,17.
	if *sd.TxBps > *sd.RxBps/50 {
		t.Fatalf("se publica el agregado: tx/rx = %v (WAN esperada ~0,005)", *sd.TxBps / *sd.RxBps)
	}
}

// Un AP sin WAN identificable sigue publicando el agregado, que ahí son dos
// direcciones reales y no un doble conteo.
func TestSystemRatesKeepTheAggregateWithoutWan(t *testing.T) {
	first := "  eth0: 1000000 0 0 0 0 0 0 0 500000 0 0 0 0 0 0 0\n"
	second := "  eth0: 2000000 0 0 0 0 0 0 0 1000000 0 0 0 0 0 0 0\n"
	p := NewProber(&seqNetRunner{netdev: []string{first, second}}, Options{})
	p.probeSystem(context.Background())
	sd := p.probeSystem(context.Background())
	if sd.RxBps == nil || sd.TxBps == nil {
		t.Fatal("sin tasas en la segunda muestra")
	}
	// Agregado de una boca: rx y tx crecen en la misma proporción (1e6 y 5e5).
	if *sd.TxBps < *sd.RxBps/4 {
		t.Fatalf("el agregado de una boca debería conservarse: tx/rx = %v", *sd.TxBps / *sd.RxBps)
	}
}

// Una WAN declarada que no está en /proc/net/dev no puede dejar la tasa a cero.
func TestSystemRatesFallBackWhenWanPortMissing(t *testing.T) {
	first := "  eth0: 1000000 0 0 0 0 0 0 0 500000 0 0 0 0 0 0 0\n"
	second := "  eth0: 2000000 0 0 0 0 0 0 0 1000000 0 0 0 0 0 0 0\n"
	p := NewProber(&seqNetRunner{netdev: []string{first, second}}, Options{})
	p.lastWan = &WanInfo{Port: "pppoe-wan"}
	p.probeSystem(context.Background())
	sd := p.probeSystem(context.Background())
	if sd.RxBps == nil || *sd.RxBps == 0 {
		t.Fatalf("sin la boca WAN debe caer al agregado, sd=%+v", sd)
	}
}
