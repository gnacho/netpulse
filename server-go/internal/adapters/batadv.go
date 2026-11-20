package adapters

import "strings"

// isBatadvBlaMAC: BATMAN-adv bridge loop avoidance emite tramas LOOP DETECT
// desde una dirección localmente administrada cuyos dos primeros octetos son
// BA:BE, con los cuatro restantes aleatorios y rotados cada cierto número de
// periodos (net/batman-adv/bridge_loop_avoidance.c fija loopdetect_addr a
// 0xba 0xbe). No es un host: es una sonda del mesh. Pero el FDB de cada
// unidad la aprende, así que sin este filtro una red con batman-adv llena la
// lista de clientes de fantasmas (#1818).
func isBatadvBlaMAC(mac string) bool {
	m := strings.TrimSpace(mac)
	if len(m) == 12 { // 12 hex sin separador
		return strings.EqualFold(m[:4], "BABE")
	}
	if len(m) != 17 {
		return false
	}
	sep := m[2]
	if sep != ':' && sep != '-' {
		return false
	}
	if m[5] != sep || m[8] != sep || m[11] != sep || m[14] != sep {
		return false
	}
	return strings.EqualFold(m[:2], "BA") && strings.EqualFold(m[3:5], "BE")
}

// withoutBatadvBla devuelve el FDB sin las direcciones de sonda de BLA. Si no
// hay nada que quitar devuelve el MISMO mapa, así el camino normal no paga
// nada y nunca se muta el mapa del payload en sitio.
func withoutBatadvBla(fdb map[string]string) map[string]string {
	dropped := 0
	for mac := range fdb {
		if isBatadvBlaMAC(mac) {
			dropped++
		}
	}
	if dropped == 0 {
		return fdb
	}
	out := make(map[string]string, len(fdb)-dropped)
	for mac, port := range fdb {
		if !isBatadvBlaMAC(mac) {
			out[mac] = port
		}
	}
	return out
}
