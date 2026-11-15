package adapters

import "github.com/gnacho/netpulse/agent/probe"

// pickRouterNet decide qué tasa se publica como tráfico del router (#2048).
//
// El agregado de /proc/net/dev suma TODAS las bocas físicas. En un router que
// enruta eso cuenta los mismos bytes dos veces, una por cada tramo de la ruta:
// la bajada entra por la WAN y sale por la LAN, así que rx y tx acaban
// parecidos y la tarjeta WAN muestra una subida que no existe (el reporte de
// #2048: 70/69 Mbps con una línea de 120/10).
//
// Cuando la unidad sabe cuál es su interfaz WAN se usan sus contadores, que es
// la única medida de la línea. El agregado queda como respaldo para las
// unidades sin WAN identificable (APs, donde todas las bocas son la misma
// dirección de tráfico y el agregado sí es honesto) y para cuando la interfaz
// WAN no aparece en /proc/net/dev.
func pickRouterNet(agg *NetDevBps, wan string, ifRates map[string]probe.IfRate) *NetDevBps {
	if wan != "" {
		if r, ok := ifRates[wan]; ok && r.RxBps != nil && r.TxBps != nil {
			return &NetDevBps{RxBps: r.RxBps, TxBps: r.TxBps}
		}
	}
	if agg == nil {
		return &NetDevBps{}
	}
	return agg
}
