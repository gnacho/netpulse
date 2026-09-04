// meminfo.go — lectura local de /proc/meminfo en el agente (#513).
//
// ubus system info no expone la caché de ficheros; la fórmula de uso clásica
// (usado = total - free - buffers - cached) la necesita para no contar RAM
// reclamable como consumida. El agente corre EN el router: /proc/meminfo
// siempre está. Fuera de OpenWrt (hosts de desarrollo/tests) la lectura
// falla y el agente simplemente no rellena Cached (fallback del servidor).
package probe

import (
	"os"
	"strconv"
	"strings"
)

// readMeminfoCachedKB devuelve el campo Cached de /proc/meminfo en kB, o 0
// si el fichero no existe o no se puede leer.
func readMeminfoCachedKB() int64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	return parseMeminfoCachedKB(string(data))
}

// parseMeminfoCachedKB extrae "Cached: N kB" del contenido de /proc/meminfo.
func parseMeminfoCachedKB(data string) int64 {
	for _, line := range strings.Split(data, "\n") {
		if !strings.HasPrefix(line, "Cached:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			if n, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
				return n
			}
		}
	}
	return 0
}
