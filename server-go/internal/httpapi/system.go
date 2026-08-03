// system.go — GET /api/system/info (SPEC-65 D65-6): datos REALES del
// proceso/servidor (nunca hardcodeados), autenticado (cualquier usuario).
// Las lecturas de /proc y /etc tienen fallback amable ("" / 0) si no son
// legibles (p.ej. contenedor sin /proc completo); los parseadores son
// funciones puras para testearlas con fixtures.
package httpapi

import (
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// systemInfo es la respuesta de GET /api/system/info.
type systemInfo struct {
	Version    string `json:"version"`    // versión de la app (const Version, la de update.go)
	GoVersion  string `json:"goVersion"`  // runtime.Version()
	OS         string `json:"os"`         // runtime.GOOS
	Arch       string `json:"arch"`       // runtime.GOARCH
	Distro     string `json:"distro"`     // /etc/os-release PRETTY_NAME ("" si no legible)
	Kernel     string `json:"kernel"`     // /proc/sys/kernel/osrelease ("" si no legible)
	CPUModel   string `json:"cpuModel"`   // /proc/cpuinfo "model name" (o "Model" en ARM)
	CPUCores   int    `json:"cpuCores"`   // runtime.NumCPU()
	MemTotalMb int64  `json:"memTotalMb"` // /proc/meminfo MemTotal → MiB
	UptimeS    int64  `json:"uptimeS"`    // uptime del PROCESO netpulse, segundos
	Demo       bool   `json:"demo"`       // DEMO_MODE activo
}

// parseOsReleasePretty extrae PRETTY_NAME de un contenido de os-release
// ("" si ausente). Tolera comillas y espacios.
func parseOsReleasePretty(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "PRETTY_NAME=") {
			continue
		}
		v := strings.TrimPrefix(line, "PRETTY_NAME=")
		v = strings.TrimSpace(v)
		v = strings.Trim(v, `"`)
		return v
	}
	return ""
}

// parseCPUModel devuelve la primera entrada "model name" (x86) o "Model"
// (ARM/DeviceTree) de un contenido de /proc/cpuinfo ("" si ninguna).
func parseCPUModel(content string) string {
	for _, line := range strings.Split(content, "\n") {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(key) {
		case "model name", "Model":
			return strings.TrimSpace(val)
		}
	}
	return ""
}

// parseMemTotalMB convierte MemTotal (kB) de un contenido de /proc/meminfo a
// MiB (0 si ausente o inválido).
func parseMemTotalMB(content string) int64 {
	for _, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kb / 1024
	}
	return 0
}

// readFileOrEmpty lee un fichero de texto del sistema ("" si no legible).
func readFileOrEmpty(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// handleSystemInfo: 200 con los datos del proceso/servidor (sesión).
func (s *server) handleSystemInfo(w http.ResponseWriter, _ *http.Request) {
	started := s.started
	if started.IsZero() {
		started = time.Now() // tests sin Started: uptime ~0 en vez de negativo
	}
	writeJSON(w, http.StatusOK, systemInfo{
		Version:    Version,
		GoVersion:  runtime.Version(),
		OS:         runtime.GOOS,
		Arch:       runtime.GOARCH,
		Distro:     parseOsReleasePretty(readFileOrEmpty("/etc/os-release")),
		Kernel:     strings.TrimSpace(readFileOrEmpty("/proc/sys/kernel/osrelease")),
		CPUModel:   parseCPUModel(readFileOrEmpty("/proc/cpuinfo")),
		CPUCores:   runtime.NumCPU(),
		MemTotalMb: parseMemTotalMB(readFileOrEmpty("/proc/meminfo")),
		UptimeS:    int64(time.Since(started).Seconds()),
		Demo:       s.adapter.Mode() == "demo",
	})
}
