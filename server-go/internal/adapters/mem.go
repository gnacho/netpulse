// mem.go — cálculo del uso de RAM de un router a partir de los campos de
// memoria (issue #513).
package adapters

import "math"

// memUsagePct devuelve el % de RAM en uso.
//
// Con Cached > 0 (el agente lo rellena desde /proc/meminfo; ubus system info
// no lo da) usa la fórmula clásica que no cuenta la caché de ficheros como
// consumida (paridad LuCI/htop): used = total - free - buffered - cached.
// Sin Cached (payloads viejos o sondeo SSH de ubus) cae a la fórmula previa
// basada en MemAvailable, que en root ubifs/overlay tiende a sobreestimar el
// uso porque el kernel cuenta poca caché como reclamable a corto plazo.
func memUsagePct(total, free, buffered, cached, available float64) int {
	if total <= 0 {
		return 0
	}
	if cached > 0 {
		used := total - free - buffered - cached
		if used < 0 {
			used = 0
		}
		return int(math.Round(used / total * 100))
	}
	avail := available
	if avail == 0 {
		avail = free + buffered
	}
	used := total - avail
	if used < 0 {
		used = 0
	}
	return int(math.Round(used / total * 100))
}
