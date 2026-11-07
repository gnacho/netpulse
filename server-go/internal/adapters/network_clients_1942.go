package adapters

import "strings"

// Atribución de clientes a redes (#1942). Hasta ahora la columna de clientes
// del panel de redes solo se llenaba cuando el cliente traía VLAN, así que en
// un br-lan sin VLAN filtering, que es el montaje normal en casa, quedaba
// vacía. Aquí se cruza lo que ya sabe el servidor: el puerto del FDB, la VLAN
// observada y el mapa wifi-iface → red que declara `uci show wireless`.
//
// Reglas: un cliente aparece en exactamente UNA red o en ninguna. Nunca se
// adivina: un puerto trunk presente en varias redes sin VLAN conocida no
// atribuye, y un cliente sin puerto, sin VLAN y sin wifi-iface mapeado tampoco.

// wifiNetworkMap: ifname inalámbrico (wlan0, phy1-ap0...) → nombre de red UCI
// (lan, guest...), desde las secciones wifi-iface ya parseadas (#541).
func wifiNetworkMap(ifaces []Dot11rIface) map[string]string {
	out := map[string]string{}
	for _, ic := range ifaces {
		if ic.Ifname == "" || ic.Network == "" {
			continue
		}
		out[ic.Ifname] = ic.Network
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// netPortBase: nombre base de un miembro de red: eth0.999 y eth0 son el mismo
// puerto físico, y el FDB aprende siempre sobre el base.
func netPortBase(port string) string {
	if i := strings.IndexByte(port, '.'); i > 0 {
		return port[:i]
	}
	return port
}

// attributeNetworkClients devuelve las redes con ClientMacs relleno. El slice
// de entrada no se modifica. Una red sin ningún cliente atribuible queda sin
// el campo (la UI muestra "-" como antes), no una lista vacía inventada.
func attributeNetworkClients(networks []NetworkInfo, clients []Device, wifiNet map[string]string) []NetworkInfo {
	if len(networks) == 0 {
		return networks
	}
	out := make([]NetworkInfo, len(networks))
	copy(out, networks)
	for _, c := range clients {
		candidates := make([]int, 0, len(out))
		// La config wireless es autoritativa: la iface está declarada en una
		// red concreta, sin la ambigüedad de un puerto trunk.
		if netName, ok := wifiNet[c.WifiIface]; ok {
			for i, n := range out {
				if n.Name == netName {
					candidates = append(candidates, i)
				}
			}
		} else if c.Port != "" {
			base := netPortBase(c.Port)
			for i, n := range out {
				for _, p := range n.Ports {
					if netPortBase(p) == base {
						candidates = append(candidates, i)
						break
					}
				}
			}
		}
		// La VLAN observada desempata un puerto presente en varias redes y,
		// sin puerto ni iface, es la única pista disponible (comportamiento
		// que ya tenía la UI).
		if c.Vlan > 0 {
			byVlan := make([]int, 0, len(out))
			for _, i := range candidates {
				if out[i].Vlan == c.Vlan {
					byVlan = append(byVlan, i)
				}
			}
			if len(candidates) == 0 {
				for i, n := range out {
					if n.Vlan == c.Vlan {
						byVlan = append(byVlan, i)
					}
				}
			}
			candidates = byVlan
		}
		if len(candidates) != 1 || c.MAC == "" {
			continue
		}
		i := candidates[0]
		out[i].ClientMacs = append(out[i].ClientMacs, c.MAC)
	}
	return out
}
