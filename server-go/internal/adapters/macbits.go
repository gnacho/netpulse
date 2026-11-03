// macbits.go — clasificación de una MAC por sus bits de control.
package adapters

import (
	"strconv"
	"strings"
)

// isLocallyAdministeredMAC informa de si la MAC tiene el bit localmente
// administrado (0x02 del primer octeto). Esas direcciones no las asigna un
// fabricante: las genera el propio equipo (privacidad de iOS y Android, MACs
// de VM) o un firmware para una función temporal, como el NMRP de Netgear
// mientras reinicia para actualizar (#1818).
//
// Una MAC mal formada devuelve false: no se clasifica lo que no se entiende.
func isLocallyAdministeredMAC(mac string) bool {
	m := strings.TrimSpace(mac)
	if len(m) < 2 {
		return false
	}
	first, err := strconv.ParseUint(m[:2], 16, 8)
	if err != nil {
		return false
	}
	return first&0x02 != 0
}
