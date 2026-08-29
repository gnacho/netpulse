// yield_test.go: detección, cedida, idempotencia y garantía de que el sabor
// embebido (runtime) nunca depende de este paquete (#369).
package yield

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeProc monta un /proc falso con un proceso por nombre.
func fakeProc(t *testing.T, names map[int]string) string {
	t.Helper()
	root := t.TempDir()
	for pid, name := range names {
		dir := filepath.Join(root, strconv.Itoa(pid))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "comm"), []byte(name+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// TestNetgripPresentFiles: init.d o binario de netgrip cuentan como presencia.
func TestNetgripPresentFiles(t *testing.T) {
	dir := t.TempDir()
	p := Paths{
		NetgripInit: filepath.Join(dir, "init.d", "netgrip"),
		NetgripBin:  filepath.Join(dir, "sbin", "netgrip"),
		ProcRoot:    "", // sin /proc: aislado del host de CI
	}
	if NetgripPresent(p) {
		t.Fatal("sin ficheros ni /proc no debería detectar")
	}
	touch(t, p.NetgripBin)
	if !NetgripPresent(p) {
		t.Fatal("el binario de netgrip debería detectarse")
	}
	os.Remove(p.NetgripBin)
	touch(t, p.NetgripInit)
	if !NetgripPresent(p) {
		t.Fatal("el init.d de netgrip debería detectarse")
	}
}

// TestNetgripPresentProc: proceso vivo por comm (ficheros ausentes).
func TestNetgripPresentProc(t *testing.T) {
	proc := fakeProc(t, map[int]string{1: "procd", 42: "netgrip", 99: "netpulse-agent"})
	p := Paths{ProcRoot: proc}
	if !NetgripPresent(p) {
		t.Fatal("el proceso netgrip debería detectarse por comm")
	}
	p.ProcRoot = fakeProc(t, map[int]string{1: "procd", 99: "netpulse-agent"})
	if NetgripPresent(p) {
		t.Fatal("sin netgrip en /proc no debería detectar")
	}
}

// TestYieldFull: fichero de estado, orden disable->stop del init propio y
// limpieza de la línea del watchdog del cron.
func TestYieldFull(t *testing.T) {
	dir := t.TempDir()
	selfInit := filepath.Join(dir, "init.d", "netpulse-agent")
	touch(t, selfInit)
	cron := filepath.Join(dir, "crontabs", "root")
	touch(t, cron)
	if err := os.WriteFile(cron, []byte("*/5 * * * * /usr/sbin/algo\n*/2 * * * * /usr/sbin/netpulse-watchdog\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := Paths{
		SelfInit:  selfInit,
		StateFile: filepath.Join(dir, "netpulse-agent.yielded"),
		ProcRoot:  "",
		CronFile:  cron,
	}

	var cmds [][]string
	origRun := runCmd
	runCmd = func(name string, args ...string) error {
		cmds = append(cmds, append([]string{name}, args...))
		return nil
	}
	cancelled := false
	defer func() { runCmd = origRun }()

	Yield(p, "prueba", func() { cancelled = true })

	if !cancelled {
		t.Fatal("cancel debería invocarse al final de la cedida")
	}
	data, err := os.ReadFile(p.StateFile)
	if err != nil {
		t.Fatal("el fichero de estado debería existir tras la cedida")
	}
	if !strings.HasPrefix(string(data), "yielded=1\n") || !strings.Contains(string(data), "reason=prueba") {
		t.Fatalf("contenido inesperado del estado: %q", data)
	}
	// orden exacto: disable antes que stop (si stop mata el proceso, el
	// disable ya quedó aplicado)
	if len(cmds) != 2 || cmds[0][1] != "disable" || cmds[1][1] != "stop" {
		t.Fatalf("comandos inesperados: %v", cmds)
	}
	cronData, _ := os.ReadFile(cron)
	if strings.Contains(string(cronData), "netpulse-watchdog") {
		t.Fatalf("la línea del watchdog debería desaparecer del cron: %q", cronData)
	}
	if !strings.Contains(string(cronData), "/usr/sbin/algo") {
		t.Fatal("el resto del cron debería conservarse")
	}
}

// TestYieldIdempotente: una segunda cedida no rompe nada (estado se
// reescribe, comandos se repiten pero todo es mejor esfuerzo).
func TestYieldIdempotente(t *testing.T) {
	dir := t.TempDir()
	selfInit := filepath.Join(dir, "init.d", "netpulse-agent")
	touch(t, selfInit)
	p := Paths{SelfInit: selfInit, StateFile: filepath.Join(dir, "y"), ProcRoot: "", CronFile: ""}
	var n int
	origRun := runCmd
	runCmd = func(name string, args ...string) error { n++; return nil }
	defer func() { runCmd = origRun }()

	Yield(p, "uno", nil)
	Yield(p, "dos", nil)
	if n != 4 {
		t.Fatalf("dos cedidas = 4 comandos, hubo %d", n)
	}
	data, _ := os.ReadFile(p.StateFile)
	if !strings.Contains(string(data), "reason=dos") {
		t.Fatal("la segunda cedida debería reescribir el motivo")
	}
}

// TestYieldSinInit: agente manual (sin init.d propio): no ejecuta comandos,
// pero sí escribe estado y cancela.
func TestYieldSinInit(t *testing.T) {
	dir := t.TempDir()
	p := Paths{SelfInit: filepath.Join(dir, "no-existe"), StateFile: filepath.Join(dir, "y"), ProcRoot: "", CronFile: ""}
	var n int
	origRun := runCmd
	runCmd = func(name string, args ...string) error { n++; return nil }
	defer func() { runCmd = origRun }()
	cancelled := false
	Yield(p, "manual", func() { cancelled = true })
	if n != 0 {
		t.Fatal("sin init.d propio no debería lanzar comandos")
	}
	if !cancelled || !fileExists(p.StateFile) {
		t.Fatal("debería escribir estado y cancelar igualmente")
	}
}

// TestWatchSinNetgripNuncaCede: sin señales de netgrip, Watch termina solo
// por ctx y NO ejecuta la cedida.
func TestWatchSinNetgripNuncaCede(t *testing.T) {
	dir := t.TempDir()
	p := Paths{
		NetgripInit: filepath.Join(dir, "ninguna"),
		NetgripBin:  filepath.Join(dir, "ninguno"),
		StateFile:   filepath.Join(dir, "y"),
		ProcRoot:    "",
		CronFile:    "",
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(150 * time.Millisecond); cancel() }()
	start := time.Now()
	Watch(ctx, p, 30*time.Millisecond, func() { t.Error("cancel no debería invocarse sin netgrip") })
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Watch debería terminar con el ctx, tardó %s", elapsed)
	}
	if fileExists(p.StateFile) {
		t.Fatal("sin netgrip no debería escribirse el estado de cedida")
	}
}

// TestWatchCedeAlDetectar: netgrip aparece a mitad de ejecución -> cedida.
func TestWatchCedeAlDetectar(t *testing.T) {
	dir := t.TempDir()
	netgripBin := filepath.Join(dir, "sbin", "netgrip")
	p := Paths{
		NetgripInit: filepath.Join(dir, "init.d", "netgrip"),
		NetgripBin:  netgripBin,
		StateFile:   filepath.Join(dir, "y"),
		ProcRoot:    "",
		CronFile:    "",
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { Watch(ctx, p, 30*time.Millisecond, cancel); close(done) }()

	time.Sleep(60 * time.Millisecond) // un par de ticks sin netgrip
	if fileExists(p.StateFile) {
		t.Fatal("aún no hay netgrip: no debería haber cedido")
	}
	touch(t, netgripBin) // netgrip se instala en caliente
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Watch debería ceder tras detectar netgrip")
	}
	data, err := os.ReadFile(p.StateFile)
	if err != nil {
		t.Fatal("el estado de cedida debería existir")
	}
	if !strings.Contains(string(data), ReasonPeriodic) {
		t.Fatalf("motivo inesperado: %q", data)
	}
	if ctx.Err() == nil {
		t.Fatal("el ctx del agente debería quedar cancelado tras la cedida")
	}
}
