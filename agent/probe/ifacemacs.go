// ifacemacs.go — identidades MAC de las interfaces del propio equipo (#1765).
//
// Un equipo de la flota puede responder por varias MACs: la del bridge, la de
// cada VLAN, la del puerto CPU de un switch DSA y la de cada radio. Algunas de
// ellas no son la MAC del router que se guarda en la flota, así que un vecino
// las aprende en su FDB y las publica como clientes desconocidos sin nombre ni
// IP. El equipo es la única fuente fiable de esa lista: nadie mejor que él sabe
// qué MACs son suyas.
//
// El campo es opcional y aditivo: un agente o pusher que no lo envíe sigue
// funcionando igual, y el servidor no inventa identidades que no recibe.
package probe

import (
	"sort"
	"strings"
)

// ParseIfaceMACs convierte la salida de CmdIfaceMACs ("iface mac" por línea)
// en la lista de MACs propias, en mayúsculas, sin duplicados y ordenada.
// Descarta la MAC nula (loopback) y cualquier línea que no traiga una MAC
// ethernet válida.
func ParseIfaceMACs(out string) []string {
	seen := make(map[string]bool, 8)
	macs := make([]string, 0, 8)
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		mac := normalizeIfaceMAC(fields[len(fields)-1])
		if mac == "" || seen[mac] {
			continue
		}
		seen[mac] = true
		macs = append(macs, mac)
	}
	sort.Strings(macs)
	return macs
}

// normalizeIfaceMAC acepta solo MAC ethernet de 6 octetos en hex. La MAC nula
// es la de loopback y no identifica a nadie.
func normalizeIfaceMAC(raw string) string {
	mac := strings.ToUpper(strings.TrimSpace(raw))
	if len(mac) != 17 {
		return ""
	}
	for i := 0; i < 17; i++ {
		c := mac[i]
		if i%3 == 2 {
			if c != ':' && c != '-' {
				return ""
			}
			continue
		}
		isHex := (c >= '0' && c <= '9') || (c >= 'A' && c <= 'F')
		if !isHex {
			return ""
		}
	}
	mac = strings.ReplaceAll(mac, "-", ":")
	if mac == "00:00:00:00:00:00" {
		return ""
	}
	return mac
}
