package httpapi

// firmware_sweep_test.go - tests del barrido semanal de versiones (#1777) y
// del retune de urgencia de emitFirmwareResult (#1776).

import (
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

// sweepFakeSSH: rebootFakeSSH + salida controlada para `owut check`.
type sweepFakeSSH struct {
	rebootFakeSSH
	checkOut string
}

func (f *sweepFakeSSH) Run(host, cmd string, d time.Duration) (string, error) {
	if strings.Contains(cmd, "owut check") {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.cmds = append(f.cmds, cmd)
		return f.checkOut, nil
	}
	return f.rebootFakeSSH.Run(host, cmd, d)
}

// captureEmitter: alertEmitter de test que guarda los eventos emitidos.
type captureEmitter struct {
	mu     sync.Mutex
	events []alerts.AlertEvent
}

func (c *captureEmitter) Emit(ev alerts.AlertEvent) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, ev)
	return true
}

func (c *captureEmitter) list() []alerts.AlertEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]alerts.AlertEvent(nil), c.events...)
}

// makeSweepServer: server de prueba con fake SSH de check, adapter que
// reporta la versión instalada y emisor capturado.
func makeSweepServer(t *testing.T, installed, checkOut string) (*server, string, *sweepFakeSSH, *captureEmitter) {
	t.Helper()
	s, rid, _ := makeRecServer(t, installed)
	fake := &sweepFakeSSH{checkOut: checkOut}
	s.pool = fake
	s.adapter = &staticBoardAdapter{version: installed}
	em := &captureEmitter{}
	s.alertEmitter = em
	return s, rid, fake, em
}

const sweepCheckOut = "Version-from   25.12.5 r33311-981c978eff\n" +
	"Version-to     25.12.6 r33311-981c978eff (kernel 6.12.112)\n" +
	"ERROR: Checks reveal errors, do not upgrade\n"

// #1777: avisa una vez por router+versión; una versión nueva vuelve a avisar.
func TestFirmwareSweepAlertsOncePerVersion(t *testing.T) {
	s, rid, _, em := makeSweepServer(t, "25.12.5", sweepCheckOut)

	s.firmwareSweepRouter(rid, "192.168.1.60")

	if got := kvGet(s.db.DB, fwSweepCheckedKey(rid)); got == "" {
		t.Fatalf("la comprobación debe quedar marcada")
	}
	evs := em.list()
	if len(evs) != 1 {
		t.Fatalf("eventos = %d, esperaba 1: %+v", len(evs), evs)
	}
	ev := evs[0]
	if ev.Type != "firmware-available" || ev.Urgent || ev.Severity != "info" {
		t.Fatalf("evento inesperado: type=%q urgent=%v severity=%q", ev.Type, ev.Urgent, ev.Severity)
	}
	if !strings.Contains(ev.Description, "25.12.5 -> 25.12.6") {
		t.Fatalf("descripción = %q", ev.Description)
	}

	// Re-check con la misma versión: dedup, sin segundo evento.
	s.firmwareSweepRouter(rid, "192.168.1.60")
	if n := len(em.list()); n != 1 {
		t.Fatalf("tras re-check de la misma versión hay %d eventos, esperaba 1", n)
	}

	// Versión nueva: avisa otra vez.
	s.adapter = &staticBoardAdapter{version: "25.12.6"}
	s.pool = &sweepFakeSSH{checkOut: strings.Replace(sweepCheckOut, "25.12.6", "25.12.7", 1)}
	s.firmwareSweepRouter(rid, "192.168.1.60")
	evs = em.list()
	if len(evs) != 2 {
		t.Fatalf("tras nueva versión hay %d eventos, esperaba 2", len(evs))
	}
	if !strings.Contains(evs[1].Description, "25.12.6 -> 25.12.7") {
		t.Fatalf("segunda descripción = %q", evs[1].Description)
	}
}

// #1777: instalada == destino no genera alerta (pero sí marca la comprobación).
func TestFirmwareSweepSkipsUpToDate(t *testing.T) {
	s, rid, _, em := makeSweepServer(t, "25.12.6", sweepCheckOut)

	s.firmwareSweepRouter(rid, "192.168.1.60")

	if len(em.list()) != 0 {
		t.Fatalf("al día no debe alertar: %+v", em.list())
	}
	if got := kvGet(s.db.DB, fwSweepCheckedKey(rid)); got == "" {
		t.Fatalf("la comprobación debe quedar marcada aunque no alerte")
	}
}

// #1777: un check sin dato fiable no marca la comprobación (reintento).
func TestFirmwareSweepNoCheckDataDoesNotMark(t *testing.T) {
	s, rid, _, em := makeSweepServer(t, "25.12.5", "")

	s.firmwareSweepRouter(rid, "192.168.1.60")

	if len(em.list()) != 0 {
		t.Fatalf("sin dato no debe alertar")
	}
	if got := kvGet(s.db.DB, fwSweepCheckedKey(rid)); got != "" {
		t.Fatalf("sin VersionTo la comprobación no debe marcarse (got %q)", got)
	}
}

// #1777: la ventana semanal manda sobre el tick horario.
func TestFirmwareSweepRateLimitDue(t *testing.T) {
	s, rid, _, _ := makeSweepServer(t, "25.12.5", sweepCheckOut)
	now := time.Now()

	if !s.fwSweepDue(rid, now) {
		t.Fatalf("sin marca previa debe estar vencido")
	}
	if err := kvSetStr(s.db.DB, fwSweepCheckedKey(rid), "no-es-un-numero"); err != nil {
		t.Fatalf("kvSetStr: %v", err)
	}
	if !s.fwSweepDue(rid, now) {
		t.Fatalf("marca corrupta debe tratarse como vencida")
	}
	if err := kvSetStr(s.db.DB, fwSweepCheckedKey(rid), strconv.FormatInt(now.Unix(), 10)); err != nil {
		t.Fatalf("kvSetStr: %v", err)
	}
	if s.fwSweepDue(rid, now) {
		t.Fatalf("marcada ahora no debe estar vencida")
	}
	old := now.Add(-8 * 24 * time.Hour).Unix()
	if err := kvSetStr(s.db.DB, fwSweepCheckedKey(rid), strconv.FormatInt(old, 10)); err != nil {
		t.Fatalf("kvSetStr: %v", err)
	}
	if !s.fwSweepDue(rid, now) {
		t.Fatalf("marca de hace 8 días debe estar vencida")
	}
}

// #1776: el resultado de un upgrade interactivo no es urgent; los fallos y
// los programados sí.
func TestEmitFirmwareResultUrgency(t *testing.T) {
	s, rid, _, em := makeSweepServer(t, "25.12.5", sweepCheckOut)

	s.emitFirmwareResult(rid, "25.12.5", "25.12.6", "manual", true, "")
	s.emitFirmwareResult(rid, "25.12.5", "25.12.6", "manual", false, "boom")
	s.emitFirmwareResult(rid, "25.12.5", "25.12.6", "scheduled", true, "")

	evs := em.list()
	if len(evs) != 3 {
		t.Fatalf("eventos = %d, esperaba 3", len(evs))
	}
	if evs[0].Urgent {
		t.Fatalf("éxito manual no debe ser urgent")
	}
	if !evs[1].Urgent {
		t.Fatalf("fallo manual debe ser urgent")
	}
	if !evs[2].Urgent {
		t.Fatalf("éxito programado debe ser urgent")
	}
}
