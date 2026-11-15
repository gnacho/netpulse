package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
)

func rate(rx, tx float64) probe.IfRate {
	return probe.IfRate{RxBps: netFptr(rx), TxBps: netFptr(tx)}
}

// #2048: el agregado de todas las bocas cuenta la bajada dos veces (entra por
// la WAN y sale por la LAN). Con la WAN identificada se publica la línea.
func TestPickRouterNetPrefersTheWan(t *testing.T) {
	agg := &NetDevBps{RxBps: netFptr(70.6e6), TxBps: netFptr(69.4e6)}
	rates := map[string]probe.IfRate{
		"eth1": rate(70.6e6, 8.1e6),  // WAN: 70 de bajada, 8 de subida
		"eth0": rate(69.4e6, 70.5e6), // LAN: lo que el router reenvía
	}
	got := pickRouterNet(agg, "eth1", rates)
	if got.RxBps == nil || got.TxBps == nil {
		t.Fatal("sin tasas")
	}
	if *got.RxBps != 70.6e6 || *got.TxBps != 8.1e6 {
		t.Fatalf("tasa = (%v,%v); want la de la WAN (70.6,8.1) Mbps", *got.RxBps, *got.TxBps)
	}
	if *got.TxBps == *agg.TxBps {
		t.Fatal("se está publicando la subida del agregado, que es el doble conteo")
	}
}

// Una WAN declarada que no aparece en /proc/net/dev (pppoe-wan, túnel) no
// puede publicar ceros: se cae al agregado, que es lo que había antes.
func TestPickRouterNetFallsBackWithoutWanRates(t *testing.T) {
	agg := &NetDevBps{RxBps: netFptr(10), TxBps: netFptr(2)}
	if got := pickRouterNet(agg, "pppoe-wan", map[string]probe.IfRate{}); got != agg {
		t.Fatal("sin tasas de la WAN debe devolver el agregado")
	}
	// Rates presentes pero sin bps calculados (primera muestra).
	rates := map[string]probe.IfRate{"eth1": {}}
	if got := pickRouterNet(agg, "eth1", rates); got != agg {
		t.Fatal("una primera muestra sin bps debe caer al agregado")
	}
	// Solo un lado calculado: tampoco sirve.
	half := map[string]probe.IfRate{"eth1": {RxBps: netFptr(5)}}
	if got := pickRouterNet(agg, "eth1", half); got != agg {
		t.Fatal("con un solo lado no se publica media tasa")
	}
}

// AP y unidades sin WAN: el agregado sigue siendo el dato bueno.
func TestPickRouterNetWithoutWan(t *testing.T) {
	agg := &NetDevBps{RxBps: netFptr(3), TxBps: netFptr(1)}
	if got := pickRouterNet(agg, "", map[string]probe.IfRate{"eth0": rate(9, 9)}); got != agg {
		t.Fatal("sin WAN identificable se mantiene el agregado")
	}
	if got := pickRouterNet(nil, "", nil); got == nil {
		t.Fatal("nunca se devuelve nil")
	}
	// Agregado ausente pero WAN con tasas: se publica la WAN igualmente.
	got := pickRouterNet(nil, "eth1", map[string]probe.IfRate{"eth1": rate(7, 1)})
	if got.RxBps == nil || *got.RxBps != 7 {
		t.Fatalf("sin agregado y con WAN válida = %+v; want la WAN", got)
	}
}

func netFptr(v float64) *float64 { return &v }
