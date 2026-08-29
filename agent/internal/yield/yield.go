// Package yield: ceder el paso a NetGrip (#369). El agente STANDALONE
// (cmd/netpulse-agent) NO debe convivir con el agente embebido de NetGrip:
// si ambos corren, pushean dos veces para el mismo slug. NetGrip ya detecta
// y retira el standalone desde SU lado; este paquete es la protección
// contraria: el standalone detecta NetGrip y se retira solo.
//
// Vive en agent/internal a propósito: el runtime (agent/runtime) lo comparten
// el standalone y el agente embebido de NetGrip, y el sabor embebido NUNCA
// debe ceder. Al ser internal, solo el módulo del agente puede importarlo:
// NetGrip (otro módulo, que consume runtime) no tiene forma de llegar aquí
// aunque quiera, y runtime no lo referencia (guardado por test de imports).
package yield

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Interval por defecto del chequeo periódico.
const Interval = 60 * time.Second

// ReasonStartup / ReasonPeriodic: motivos que quedan anotados en el fichero
// de estado y en los logs.
const (
	ReasonStartup  = "netgrip detected on this router at startup"
	ReasonPeriodic = "netgrip detected on this router (periodic check)"
)

// Paths: rutas que usa la detección y la cedida (inyectables en tests).
type Paths struct {
	NetgripInit string // /etc/init.d/netgrip
	NetgripBin  string // /usr/sbin/netgrip
	SelfInit    string // /etc/init.d/netpulse-agent (propio; puede no existir)
	StateFile   string // /tmp/netpulse-agent.yielded
	ProcRoot    string // /proc (escaneo de procesos; "" = sin escanear)
	CronFile    string // /etc/crontabs/root (línea del watchdog)
}

// DefaultPaths: rutas de producción en el router.
func DefaultPaths() Paths {
	return Paths{
		NetgripInit: "/etc/init.d/netgrip",
		NetgripBin:  "/usr/sbin/netgrip",
		SelfInit:    "/etc/init.d/netpulse-agent",
		StateFile:   "/tmp/netpulse-agent.yielded",
		ProcRoot:    "/proc",
		CronFile:    "/etc/crontabs/root",
	}
}

// runCmd inyectable (tests registran los comandos sin ejecutarlos).
var runCmd = func(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		slog.Warn("[netpulse-agent] yield cmd falló", "cmd", name, "err", err, "out", strings.TrimSpace(string(out)))
	}
	return err
}

// NetgripPresent: true si hay señales de NetGrip en este router: su init.d,
// su binario o un proceso netgrip vivo. Barato: dos stat y, solo si ambos
// fallan, un escaneo de /proc (nombres de proceso, sin tocar cmdline).
func NetgripPresent(p Paths) bool {
	if fileExists(p.NetgripInit) || fileExists(p.NetgripBin) {
		return true
	}
	return procHasNetgrip(p.ProcRoot)
}

// procHasNetgrip escanea <procRoot>/*/comm buscando "netgrip" (comm trunca a
// 15 chars; "netgrip" entra entero). procRoot vacío desactiva el escaneo.
func procHasNetgrip(procRoot string) bool {
	if procRoot == "" {
		return false
	}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		comm, err := os.ReadFile(filepath.Join(procRoot, e.Name(), "comm"))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(comm)) == "netgrip" {
			return true
		}
	}
	return false
}

// Yield ejecuta la cedida (idempotente y mejor esfuerzo):
//  1. aviso claro en el log (visible en logread),
//  2. fichero de estado con motivo y ts (para NetGrip o un humano),
//  3. retira la línea del watchdog del crontab (si no, el cron relanza el
//     agente cada 2 min y la cedida no termina),
//  4. disable + stop del propio init.d. El binario NO se autodesinstala:
//     los ficheros quedan (NetGrip los retira en su siguiente recheck, y un
//     humano puede re-activar el standalone si algún día se desinstala
//     NetGrip; el servicio queda disabled hasta entonces).
//
// cancel (opcional) se invoca al final para que el bucle principal del
// agente devuelva el control si el stop de procd no nos mata antes.
func Yield(p Paths, reason string, cancel func()) {
	slog.Warn("[netpulse-agent] netgrip detected: embedded netpulse agent takes over, stopping standalone")
	writeStateFile(p.StateFile, reason)
	removeCronWatchdog(p.CronFile)
	if fileExists(p.SelfInit) {
		_ = runCmd(p.SelfInit, "disable")
		_ = runCmd(p.SelfInit, "stop")
	}
	if cancel != nil {
		cancel()
	}
}

// writeStateFile: motivo + ts en dos líneas KEY=VALUE (tmpfs: muere al
// reiniciar, que es cuando la detección volvería a evaluarse).
func writeStateFile(path, reason string) {
	if path == "" {
		return
	}
	content := "yielded=1\nts=" + time.Now().UTC().Format(time.RFC3339) + "\nreason=" + reason + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		slog.Warn("[netpulse-agent] yield: escribir estado falló", "file", path, "err", err)
	}
}

// removeCronWatchdog borra las líneas del watchdog del standalone del
// crontab conservando el resto (mismo criterio que el cleanup de NetGrip).
func removeCronWatchdog(path string) {
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "netpulse-watchdog") {
			continue
		}
		kept = append(kept, line)
	}
	out := strings.Join(kept, "\n")
	if out == string(data) {
		return
	}
	mode := os.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	if err := os.WriteFile(path, []byte(out), mode); err != nil {
		slog.Warn("[netpulse-agent] yield: limpiar cron falló", "file", path, "err", err)
	}
}

// Watch: chequeo periódico de NetgripPresent. Al primer hallazgo ejecuta la
// cedida y termina (no repite: el proceso o muere o su ctx se cancela).
// Sin NetGrip no hace nada salvo volver a mirar cada every.
func Watch(ctx context.Context, p Paths, every time.Duration, cancel func()) {
	if every <= 0 {
		every = Interval
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if NetgripPresent(p) {
				Yield(p, ReasonPeriodic, cancel)
				return
			}
		}
	}
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
