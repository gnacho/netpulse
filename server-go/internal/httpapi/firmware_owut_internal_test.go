// firmware_owut_internal_test.go - tests internos (package httpapi) del
// disparo recurrente: idempotencia cuando la versión instalada coincide con
// el target, y autodesactivación con config inválida (#761).
package httpapi

import (
	"strings"
	"sync"
	"testing"
	"time"

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
