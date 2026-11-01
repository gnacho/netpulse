// firmware_owut_internal_test.go - tests internos (package httpapi) del
// disparo recurrente: idempotencia cuando la versión instalada coincide con
// el target, y autodesactivación con config inválida (#761).
package httpapi

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/firmware"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
	"github.com/gnacho/netpulse/server-go/internal/sse"
)

// recFakeSSH registra comandos y responde owut instalado (para el camino
// owut del disparo) sin tocar ningún router real.
type recFakeSSH struct {
	mu   sync.Mutex
	cmds []string
}

func (f *recFakeSSH) Run(host, cmd string, _ time.Duration) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cmds = append(f.cmds, cmd)
	if strings.Contains(cmd, "command -v owut") {
		return "__owut__\n", nil
	}
	if strings.Contains(cmd, "owut upgrade") {
		return "There are no changes to upgrade\n__owut_exit__=0\n", nil
	}
	return "", nil
}

func (f *recFakeSSH) saw(substr string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.cmds {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}

// makeRecServer monta un server mínimo (sin HTTP) con la BD, el store de
// firmware, el pool fake y el hook de versión instalada.
func makeRecServer(t *testing.T, installed string) (*server, string, *recFakeSSH) {
	t.Helper()
	dataDir := t.TempDir()
	cfg, err := config.Load(map[string]string{
		"AUTH_USER": "admin", "AUTH_PASS": "test123456",
		"DEMO_MODE": "0", "DATA_DIR": dataDir, "NODE_ENV": "test",
	}, dataDir)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	d, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	secret, err := auth.EnsureSessionSecret(d, cfg)
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	if err := auth.EnsureUsers(d, cfg); err != nil {
		t.Fatalf("users: %v", err)
	}
	r, err := routerstore.AddRouter(d.DB, routerstore.AddInput{
		Name: "RecRouter", Host: "192.168.1.60", Type: "openwrt",
	})
	if err != nil {
		t.Fatalf("add router: %v", err)
	}
	pool := &recFakeSSH{}
	s := &server{
		cfg: cfg, db: d, adapter: nil,
		hub:    sse.NewHub(d, cfg.MaxSSEClients, func() any { return nil }),
		secret: secret, pool: pool,
		firmware:       firmware.NewStore(d.DB),
		firmwareEngine: firmware.NewEngine(firmware.NewStore(d.DB), nil),
	}
	s.boardVersionFn = func(string) string { return installed }
	return s, r.ID, pool
}

func TestRunRecurrenceShotNoOpWhenUpToDate(t *testing.T) {
	s, rid, pool := makeRecServer(t, "25.12.5")
	if err := s.firmware.SetTarget(firmware.Target{
		RouterID: rid, Model: "x", CurrentVersion: "25.12.5",
		TargetVersion: "25.12.5", TargetURL: "http://x/i.bin",
	}); err != nil {
		t.Fatalf("target: %v", err)
	}
	if err := firmware.SaveRecurrence(s.db.DB, rid, firmware.Recurrence{
		Enabled: true, Kind: firmware.RecurrenceWeekly, DayOfWeek: func() *int { v := 0; return &v }(), Time: "00:00",
	}); err != nil {
		t.Fatalf("recurrence: %v", err)
	}

	s.runRecurrenceShot(rid)

	if pool.saw("owut upgrade") {
		t.Fatalf("al día no debe flashear: %v", pool.cmds)
	}
	if got := firmware.LoadRecurrence(s.db.DB, rid).LastRunMs; got == 0 {
		t.Fatalf("lastRun debe quedar marcado tras el disparo")
	}
}

func TestRunRecurrenceShotDisablesWithoutTarget(t *testing.T) {
	s, rid, _ := makeRecServer(t, "25.12.5")
	if err := firmware.SaveRecurrence(s.db.DB, rid, firmware.Recurrence{
		Enabled: true, Kind: firmware.RecurrenceWeekly, DayOfWeek: func() *int { v := 1; return &v }(), Time: "00:00",
	}); err != nil {
		t.Fatalf("recurrence: %v", err)
	}

	s.runRecurrenceShot(rid)

	if firmware.LoadRecurrence(s.db.DB, rid).Enabled {
		t.Fatalf("sin target guardado la recurrencia debe autodesactivarse")
	}
}

func TestRunRecurrenceShotLaunchesOwutWhenOutdated(t *testing.T) {
	s, rid, pool := makeRecServer(t, "25.12.5")
	if err := s.firmware.SetTarget(firmware.Target{
		RouterID: rid, Model: "x", CurrentVersion: "25.12.5",
		TargetVersion: "25.12.7", TargetURL: "",
	}); err != nil {
		t.Fatalf("target: %v", err)
	}
	if err := firmware.SaveRecurrence(s.db.DB, rid, firmware.Recurrence{
		Enabled: true, Kind: firmware.RecurrenceWeekly, DayOfWeek: func() *int { v := 2; return &v }(), Time: "00:00",
	}); err != nil {
		t.Fatalf("recurrence: %v", err)
	}

	s.runRecurrenceShot(rid)

	// El upgrade corre en goroutine: esperar a que el comando llegue al fake.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pool.saw("owut upgrade -q -V '25.12.7'") {
			if got := firmware.LoadRecurrence(s.db.DB, rid).LastRunMs; got == 0 {
				t.Fatalf("lastRun debe quedar marcado tras el disparo")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("desfasado debe lanzar owut upgrade -V 25.12.7: %v", pool.cmds)
}

// rebootFakeSSH (#1771): variante del fake que simula el corte de sesión SSH
// de un flash real: el comando muere con error dejando la salida de owut a
// medias. El resto de comandos (detección, preserve-stack) responde vacío.
type rebootFakeSSH struct {
	recFakeSSH
	installOut string
	installErr error
	upgradeOut string
	upgradeErr error
}

func (f *rebootFakeSSH) Run(host, cmd string, _ time.Duration) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cmds = append(f.cmds, cmd)
	switch {
	case strings.Contains(cmd, "owut install"):
		return f.installOut, f.installErr
	case strings.Contains(cmd, "owut upgrade"):
		if f.upgradeOut != "" || f.upgradeErr != nil {
			return f.upgradeOut, f.upgradeErr
		}
		return "There are no changes to upgrade\n__owut_exit__=0\n", nil
	case strings.Contains(cmd, "command -v owut"):
		return "__owut__\n", nil
	}
	return "", nil
}

// staticBoardAdapter: adapter mínimo que reporta siempre la misma versión de
// board info (lo que haría el router tras completar el flash). El embedding
// de Demo cubre el resto del interfaz Snapshotter: estos tests solo llegan a
// BoardInfoFor vía detectedFirmwareVersion.
type staticBoardAdapter struct {
	*adapters.Demo
	version string
}

func (a *staticBoardAdapter) BoardInfoFor(string) *adapters.BoardInfo {
	return &adapters.BoardInfo{Release: struct {
		Version     string `json:"version"`
		Description string `json:"description"`
		Target      string `json:"target"`
	}{Version: a.version}}
}

// shortenOwutRebootTimers: gracia y sondeo cortos para no alargar los tests.
func shortenOwutRebootTimers(t *testing.T) {
	t.Helper()
	oldGrace, oldPoll := owutRebootGrace, owutRebootPoll
	owutRebootGrace = 2 * time.Second
	owutRebootPoll = 50 * time.Millisecond
	t.Cleanup(func() { owutRebootGrace, owutRebootPoll = oldGrace, oldPoll })
}

// waitUpgradeStatus sondea el store hasta que el upgrade llega a uno de los
// estados pedidos (el flash corre en goroutine en el flujo real; aquí las
// llamadas son directas, pero awaitReboot sondea con su timer).
func waitUpgradeStatus(t *testing.T, s *server, id int64, want ...string) *firmware.Upgrade {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if up, err := s.firmware.GetUpgradeByID(id); err == nil && up != nil {
			for _, w := range want {
				if up.Status == w {
					return up
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	up, _ := s.firmware.GetUpgradeByID(id)
	t.Fatalf("upgrade %d no llegó a %v: %+v", id, want, up)
	return nil
}

// #1771: "Installing ... and rebooting..." con la sesión SSH muerta termina
// en done, no en failed, aunque el flash muera antes de owutSSHMinRun
// (install por pasos).
func TestOwutInstallRebootLineGoesToDone(t *testing.T) {
	shortenOwutRebootTimers(t)
	s, rid, _ := makeRecServer(t, "25.12.5")
	re := &rebootFakeSSH{
		installOut: "Installing /tmp/firmware.bin and rebooting...\n",
		installErr: errors.New("ssh: session closed"),
	}
	s.pool = re
	s.adapter = &staticBoardAdapter{version: "25.12.6"}

	id, err := s.firmware.BeginUpgradeIfIdle(rid, "25.12.6", "", "")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	s.runOwutInstallImage(id, rid, "192.168.1.60", "25.12.6")

	up := waitUpgradeStatus(t, s, id, "done")
	if up.Status != "done" {
		t.Fatalf("status = %q (%q), esperaba done", up.Status, up.Error)
	}
	if !re.saw("owut install") {
		t.Fatalf("el fake no vio owut install: %v", re.cmds)
	}
}

// #1771: mismo caso en el one-shot `owut upgrade`.
func TestOwutUpgradeRebootLineGoesToDone(t *testing.T) {
	shortenOwutRebootTimers(t)
	s, rid, _ := makeRecServer(t, "25.12.5")
	re := &rebootFakeSSH{
		upgradeOut: "Installing /tmp/firmware.bin and rebooting...\n",
		upgradeErr: errors.New("ssh: session closed"),
	}
	s.pool = re
	s.adapter = &staticBoardAdapter{version: "25.12.6"}

	id, err := s.firmware.BeginUpgradeIfIdle(rid, "25.12.6", "", "")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	s.runOwutUpgrade(id, rid, "192.168.1.60", "25.12.6", "manual", nil)

	waitUpgradeStatus(t, s, id, "done")
}

// #1771: al reiniciar, la sesión SSH puede cerrar LIMPIA (err nil) y sin el
// marcador __owut_exit__ (el shell muere antes del printf): la línea de
// reboot debe clasificar flash en curso, no "terminó sin flashear".
func TestOwutInstallCleanCloseGoesToDone(t *testing.T) {
	shortenOwutRebootTimers(t)
	s, rid, _ := makeRecServer(t, "25.12.5")
	re := &rebootFakeSSH{
		installOut: "Installing /tmp/firmware.bin and rebooting...\n",
	}
	s.pool = re
	s.adapter = &staticBoardAdapter{version: "25.12.6"}

	id, err := s.firmware.BeginUpgradeIfIdle(rid, "25.12.6", "", "")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	s.runOwutInstallImage(id, rid, "192.168.1.60", "25.12.6")

	up := waitUpgradeStatus(t, s, id, "done")
	if up.Status != "done" {
		t.Fatalf("status = %q (%q), esperaba done", up.Status, up.Error)
	}
}

// El caso opuesto (sesión limpia, sin línea de reboot) sigue siendo el
// no-op "terminó sin flashear".
func TestOwutInstallCleanCloseWithoutRebootStillNoOp(t *testing.T) {
	shortenOwutRebootTimers(t)
	s, rid, _ := makeRecServer(t, "25.12.5")
	re := &rebootFakeSSH{
		installOut: "Image verifier: /tmp/firmware.bin OK\n",
	}
	s.pool = re
	s.adapter = &staticBoardAdapter{version: "25.12.5"}

	id, err := s.firmware.BeginUpgradeIfIdle(rid, "25.12.6", "", "")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	s.runOwutInstallImage(id, rid, "192.168.1.60", "25.12.6")

	up := waitUpgradeStatus(t, s, id, "failed")
	if !strings.Contains(up.Error, "Image verifier") {
		t.Fatalf("error = %q, esperaba la salida cruda del no-op", up.Error)
	}
}

// Un error real (sin línea de reboot) sigue clasificando failed, con el
// texto accionable de #1769 cuando el motivo es la imagen ausente.
func TestOwutInstallRealErrorStillFailed(t *testing.T) {
	shortenOwutRebootTimers(t)
	s, rid, _ := makeRecServer(t, "25.12.5")
	re := &rebootFakeSSH{
		installOut: "sysupgrade: ERROR: Image file '/tmp/firmware.bin' does not exist\n",
		installErr: errors.New("exit status 1"),
	}
	s.pool = re
	s.adapter = &staticBoardAdapter{version: "25.12.5"}

	id, err := s.firmware.BeginUpgradeIfIdle(rid, "25.12.6", "", "")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	s.runOwutInstallImage(id, rid, "192.168.1.60", "25.12.6")

	up := waitUpgradeStatus(t, s, id, "failed")
	if !strings.Contains(up.Error, "No hay imagen descargada") {
		t.Fatalf("error = %q, esperaba el texto accionable de #1769", up.Error)
	}
}

// #1771: sin target en memoria, el cambio de versión reportado por el board
// info también confirma el flash (v != from).
func TestOwutInstallNoTargetStillDoneWhenVersionChanges(t *testing.T) {
	shortenOwutRebootTimers(t)
	s, rid, _ := makeRecServer(t, "25.12.5")
	re := &rebootFakeSSH{
		installOut: "Installing /tmp/firmware.bin and rebooting...\n",
		installErr: errors.New("ssh: session closed"),
	}
	s.pool = re
	s.adapter = &staticBoardAdapter{version: "25.12.6"}

	id, err := s.firmware.BeginUpgradeIfIdle(rid, "25.12.6", "", "")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	s.runOwutInstallImage(id, rid, "192.168.1.60", "")

	waitUpgradeStatus(t, s, id, "done")
}
