// dot11r_agent.go - vista 802.11r alimentada por el payload del agente
// (#1450). En flotas agent-only (router.AgentOnly, el patrón NetGrip con el
// agente embebido) no hay sondeo SSH y la pestaña 802.11r respondía 503
// aunque la config estuviera puesta: el único camino leía `uci show wireless`
// por SSH. El agente manda ese volcado crudo en wireless.uciWirelessRaw y
// aquí se parsea con la MISMA función que el camino SSH (parseUciWireless):
// no hay segundo parser, ni en el agente ni en este fichero.
package adapters

import (
	"github.com/gnacho/netpulse/agent/probe"
)

// dot11rIfacesFromPayload devuelve las wifi-iface del último payload del
// agente (nil si no hay payload, no trae sección wireless o el agente es
// anterior al campo). Un campo vacío es el caso normal de un agente antiguo
// y de un equipo donde `uci` no está: se degrada a "802.11r no disponible",
// nunca a error.
func dot11rIfacesFromPayload(p *probe.Payload) []Dot11rIface {
	if p == nil || p.Data.Wireless == nil {
		return nil
	}
	raw := p.Data.Wireless.UciWirelessRaw
	if raw == "" {
		return nil
	}
	return parseUciWireless(raw)
}
