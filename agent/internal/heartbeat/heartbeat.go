// Package heartbeat — Fase 5 (resiliencia del agente): el agente toca un
// fichero en /tmp (RAM, nada en NAND) tras cada push CONFIRMADO por el
// servidor. El watchdog cron del router (agent/deploy/watchdog.sh) usa la
// edad de ese fichero para distinguir "proceso vivo y empujando" de
// "proceso vivo pero roto" (vivo sin latido → reinicio del servicio).
//
// Diseño: stateless, sin dependencias, tolerante a cualquier fallo — un
// error de heartbeat NUNCA puede afectar al ciclo de push.
package heartbeat

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// DefaultFile: latido por defecto en tmpfs (se pierde al reiniciar el
// router, que es lo correcto: tras un reboot el watchdog ve "sin fichero" y
// no hace nada porque el agente aún no ha tenido tiempo de empujar).
const DefaultFile = "/tmp/netpulse-agent.heartbeat"

// Touch escribe el unix actual en el fichero de heartbeat. Nunca devuelve
// error: el caller (main del agente) lo ignora deliberadamente.
func Touch(path string, now time.Time) error {
	if path == "" {
		path = DefaultFile
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	return os.WriteFile(path, []byte(strconv.FormatInt(now.Unix(), 10)+"\n"), 0o644)
}

// Age: segundos desde el último latido; (0, false) si el fichero no existe
// o es ilegible (agente recién instalado / reboot reciente → el watchdog no
// debe actuar por ausencia de fichero, solo por latido viejo CON fichero).
func Age(path string, now time.Time) (time.Duration, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	ts, err := strconv.ParseInt(string(bytes.TrimSpace(data)), 10, 64)
	if err != nil || ts <= 0 {
		return 0, false
	}
	age := now.Sub(time.Unix(ts, 0))
	if age < 0 {
		age = 0
	}
	return age, true
}
