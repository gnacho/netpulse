// mem.go — cálculo del uso de RAM de un router a partir de los campos de
// memoria (issue #513).
package adapters

import "math"

// memUsagePct devuelve el % de RAM en uso.
//
// Prioridad: si el agente reporta `usedProc` (suma de VmRSS de procesos) lo
// usamos directamente, porque es lo que el usuario percibe como uso real.
// Si no llega, con Cached > 0 usa la fórmula clásica que no cuenta la caché de
// ficheros como consumida (paridad LuCI/htop): used = total - free - buffered
// - cached. Sin Cached cae a la fórmula previa basada en MemAvailable, que en
// root ubifs/overlay tiende a sobreestimar el uso (#513).
func memUsagePct(total, free, buffered, cached, available, usedProc float64) int {
	if total <= 0 {
		return 0
	}
	if usedProc > 0 && usedProc < total {
		return int(math.Round(usedProc / total * 100))
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

// memUsageKnown: hay alguna cifra de uso de la que derivar el porcentaje.
// memUsagePct no puede distinguirlas de un payload que solo trae el total: con
// free/buffered/cached/available/used todos a 0 cae a "available = 0 →
// used = total" y devuelve 100. Para un pusher parcial o malformado eso es
// memoria desconocida, no memoria llena, y publicarlo como confirmado (ver
// polledFromAgent) además la persiste en el histórico.
func memUsageKnown(free, buffered, cached, available, used float64) bool {
	return free > 0 || buffered > 0 || cached > 0 || available > 0 || used > 0
}

// memUsedBytes: mismos criterios que memUsagePct pero en bytes, para que el
// server envíe la cifra de uso autoritativa (#1501): derivarla en el front
// desde el porcentaje redondeado (pct × total) acumula hasta ~2% de error
// visible en el texto "X MB used of Y MB".
func memUsedBytes(total, free, buffered, cached, available, usedProc float64) float64 {
	if total <= 0 {
		return 0
	}
	if usedProc > 0 && usedProc < total {
		return usedProc
	}
	if cached > 0 {
		used := total - free - buffered - cached
		if used < 0 {
			used = 0
		}
		return used
	}
	avail := available
	if avail == 0 {
		avail = free + buffered
	}
	used := total - avail
	if used < 0 {
		used = 0
	}
	return used
}
