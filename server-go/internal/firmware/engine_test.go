// engine_test.go — motor compartido de upgrades (#494). Usa fakes: un sender
// que registra los comandos y NUNCA toca un router real ni firmware real.
package firmware

import (
	"errors"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/sse"
)

// fakeSender implementa Sender registrando los comandos enviados. `ok` fija el
// resultado de SendPrivileged (false = agente no conectado) y `err` permite
// simular un transporte inseguro.
type fakeSender struct {
	sent []struct {
		slug  string
		event string
		data  any
	}
	ok  bool
	err error
}

func (f *fakeSender) SendPrivileged(slug, event string, data any) error {
	f.sent = append(f.sent, struct {
		slug  string
		event string
		data  any
	}{slug, event, data})
	if f.err != nil {
		return f.err
	}
	if !f.ok {
		return errors.New("agent not connected")
	}
	return nil
}

func (f *fakeSender) count() int { return len(f.sent) }

const (
	testTargetURL = "https://x/image.bin"
	testChecksum  = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func setTarget(t *testing.T, s *Store, routerID string) {
	t.Helper()
	if err := s.SetTarget(Target{
		RouterID: routerID, Model: "m", CurrentVersion: "v1",
		TargetVersion: "v2", TargetURL: testTargetURL, Checksum: testChecksum,
	}); err != nil {
		t.Fatalf("set target: %v", err)
	}
}

// TestEngineStartUpgradeNoTarget: sin target devuelve ErrNoTarget.
func TestEngineStartUpgradeNoTarget(t *testing.T) {
	s := open(t)
	e := NewEngine(s, &fakeSender{ok: true})
	if _, err := e.StartUpgrade("rt1"); !errors.Is(err, ErrNoTarget) {
		t.Fatalf("esperaba ErrNoTarget, got %v", err)
	}
}

// TestEngineStartUpgradeInProgress: un upgrade activo rechaza con
// ErrUpgradeInProgress.
func TestEngineStartUpgradeInProgress(t *testing.T) {
	s := open(t)
	setTarget(t, s, "rt1")
	if _, err := s.BeginUpgrade("rt1", "v2", "http://x", "abc"); err != nil {
		t.Fatalf("begin: %v", err)
	}
	e := NewEngine(s, &fakeSender{ok: true})
	if _, err := e.StartUpgrade("rt1"); !errors.Is(err, ErrUpgradeInProgress) {
		t.Fatalf("esperaba ErrUpgradeInProgress, got %v", err)
	}
}

// TestEngineStartUpgradeSendsCommand: el flujo manual crea la fila requested
// y envía el comando firmware_upgrade con el payload correcto.
func TestEngineStartUpgradeRejectsInvalidTarget(t *testing.T) {
	cases := []struct {
		name     string
		url      string
		checksum string
	}{
		{"http", "http://x/image.bin", testChecksum},
		{"sin checksum", testTargetURL, ""},
		{"checksum corto", testTargetURL, "abc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := open(t)
			if err := s.SetTarget(Target{
				RouterID: "rt1", Model: "m", CurrentVersion: "v1",
				TargetVersion: "v2", TargetURL: tc.url, Checksum: tc.checksum,
			}); err != nil {
				t.Fatalf("set target: %v", err)
			}
			send := &fakeSender{ok: true}
			e := NewEngine(s, send)
			if _, err := e.StartUpgrade("rt1"); !errors.Is(err, ErrInvalidTarget) {
				t.Fatalf("esperaba ErrInvalidTarget, got %v", err)
			}
			if send.count() != 0 {
				t.Fatalf("no deberia enviar comando: %+v", send.sent)
			}
		})
	}
}

func TestEngineStartUpgradeSendsCommand(t *testing.T) {
	s := open(t)
	setTarget(t, s, "rt1")
	send := &fakeSender{ok: true}
	e := NewEngine(s, send)
	id, err := e.StartUpgrade("rt1")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	up, _ := s.GetUpgradeByID(id)
	if up == nil || up.Status != "requested" {
		t.Fatalf("esperaba requested, got %+v", up)
	}
	if send.count() != 1 || send.sent[0].event != "firmware_upgrade" || send.sent[0].slug != "rt1" {
		t.Fatalf("comando no enviado correctamente: %+v", send.sent)
	}
	cmd := send.sent[0].data.(map[string]any)
	if cmd["upgradeId"] != id || cmd["targetUrl"] != testTargetURL || cmd["checksum"] != testChecksum {
		t.Fatalf("payload incorrecto: %+v", cmd)
	}
}

// TestEngineStartUpgradeAgentOffline: si el agente no responde, la fila queda
// failed y devuelve ErrAgentNotConnected.
func TestEngineStartUpgradeAgentOffline(t *testing.T) {
	s := open(t)
	setTarget(t, s, "rt1")
	e := NewEngine(s, &fakeSender{ok: false})
	if _, err := e.StartUpgrade("rt1"); !errors.Is(err, ErrAgentNotConnected) {
		t.Fatalf("esperaba ErrAgentNotConnected, got %v", err)
	}
	up, _ := s.LatestUpgrade("rt1")
	if up == nil || up.Status != "failed" {
		t.Fatalf("esperaba failed, got %+v", up)
	}
}

// TestEngineStartUpgradeInsecureTransport: un agente que sigue en HTTP deja la
// fila failed y devuelve ErrInsecureTransport, sin confundirlo con offline.
func TestEngineStartUpgradeInsecureTransport(t *testing.T) {
	s := open(t)
	setTarget(t, s, "rt1")
	e := NewEngine(s, &fakeSender{err: sse.ErrInsecureAgentTransport})
	if _, err := e.StartUpgrade("rt1"); !errors.Is(err, ErrInsecureTransport) {
		t.Fatalf("esperaba ErrInsecureTransport, got %v", err)
	}
	up, _ := s.LatestUpgrade("rt1")
	if up == nil || up.Status != "failed" || up.Error != "insecure agent transport" {
		t.Fatalf("esperaba failed con insecure agent transport, got %+v", up)
	}
}

// TestEngineLaunchScheduled: una programación vencida se lanza por el motor
// (transición requested + comando enviado).
func TestEngineLaunchScheduled(t *testing.T) {
	s := open(t)
	id, err := s.ScheduleUpgrade("rt1", "v2", testTargetURL, testChecksum, 1000)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	send := &fakeSender{ok: true}
	e := NewEngine(s, send)
	u, _ := s.GetUpgradeByID(id)
	if err := e.LaunchScheduled(*u); err != nil {
		t.Fatalf("launch: %v", err)
	}
	up, _ := s.GetUpgradeByID(id)
	if up == nil || up.Status != "requested" {
		t.Fatalf("esperaba requested, got %+v", up)
	}
	if send.count() != 1 || send.sent[0].slug != "rt1" {
		t.Fatalf("comando no enviado: %+v", send.sent)
	}
}

// TestEngineLaunchScheduledAgentOffline: si el agente está caído al disparar,
// la fila pasa a failed (sin tocar ningún dispositivo).
func TestEngineLaunchScheduledAgentOffline(t *testing.T) {
	s := open(t)
	id, err := s.ScheduleUpgrade("rt1", "v2", testTargetURL, testChecksum, 1000)
	if err != nil {
		t.Fatalf("schedule: %v", err)
	}
	e := NewEngine(s, &fakeSender{ok: false})
	u, _ := s.GetUpgradeByID(id)
	if err := e.LaunchScheduled(*u); !errors.Is(err, ErrAgentNotConnected) {
		t.Fatalf("esperaba ErrAgentNotConnected, got %v", err)
	}
	up, _ := s.GetUpgradeByID(id)
	if up == nil || up.Status != "failed" {
		t.Fatalf("esperaba failed, got %+v", up)
	}
}
