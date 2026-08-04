// rearmer_test.go — Fase 5: Rearmer compartido + Supervisor de
// auto-rearme. Casos: rearme completo (SSH ejecutado), supervisor solo
// rearma slugs con token + push expirado (nunca sin evidencia previa),
// cooldown largo del supervisor, alerta de fallo sin recuperación.
package rearmer_test

import (
	"sync"
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/alerts"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/rearmer"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
)

// fakeSSH registra los comandos ejecutados.
type fakeSSH struct {
	mu   sync.Mutex
	cmds []string
}

func (f *fakeSSH) Run(host, cmd string, _ time.Duration) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cmds = append(f.cmds, host+":"+cmd)
	return "", nil
}

func (f *fakeSSH) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.cmds)
}

// fakeEngine captura las alertas emitidas.
type fakeEngine struct {
	mu  sync.Mutex
	evs []alerts.AlertEvent
}

func (f *fakeEngine) Emit(ev alerts.AlertEvent) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.evs = append(f.evs, ev)
	return true
}

func (f *fakeEngine) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.evs)
}

func (f *fakeEngine) events() []alerts.AlertEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]alerts.AlertEvent, len(f.evs))
	copy(out, f.evs)
	return out
}

// makeRearmEnv: db + registry con reloj controlable + router "patio" en la
// tabla routers + token registrado para el slug que se pase.
type rearmEnv struct {
	d       *db.DB
	reg     *adapters.AgentRegistry
	ssh     *fakeSSH
	engine  *fakeEngine
	arm     *rearmer.Rearmer
	now     time.Time
	setNow  func(time.Time)
}

func makeRearmEnv(t *testing.T, slugsConToken ...string) *rearmEnv {
	t.Helper()
	dataDir := t.TempDir()
	d, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := routerstore.AddRouter(d.DB, routerstore.AddInput{
		Name: "patio", Host: "192.168.1.4", Type: "openwrt",
	}); err != nil {
		t.Fatalf("AddRouter: %v", err)
	}
	for _, slug := range slugsConToken {
		if _, err := d.Exec("INSERT INTO kv (key, value) VALUES (?, ?)",
			rearmer.TokenPrefix+slug, "fakehash"); err != nil {
			t.Fatalf("token kv: %v", err)
		}
	}
	reg := adapters.NewAgentRegistry(90 * time.Second)
	base := time.Now()
	env := &rearmEnv{d: d, reg: reg, ssh: &fakeSSH{}, engine: &fakeEngine{}, now: base}
	env.setNow = func(t time.Time) { env.now = t }
	reg.SetClock(func() time.Time { return env.now })
	env.arm = rearmer.New(d.DB, reg, env.ssh, env.engine, 0)
	return env
}

// pushViejo simula un push del agente en el instante actual del reloj fake.
func (e *rearmEnv) pushViejo(slug string) {
	e.reg.Ingest(&probe.Payload{Router: slug, Ts: e.now.Unix(), Version: "2.3.0"})
}

// expira avanza el reloj fake más allá del TTL (90 s).
func (e *rearmEnv) expira(extra time.Duration) {
	e.setNow(e.now.Add(90*time.Second + extra))
}

func TestSupervisorRearmaAgenteExpirado(t *testing.T) {
	env := makeRearmEnv(t, "patio")
	env.pushViejo("patio")
	env.expira(time.Minute)

	// pollWait corto para no dormir el test; supervisor con cooldown largo.
	env.arm.SetPollWait(100 * time.Millisecond)
	sup := rearmer.NewSupervisor(env.arm, env.reg, env.d.DB, env.engine, time.Hour, time.Hour)
	sup.CheckOnce()

	if env.ssh.count() != 1 {
		t.Fatalf("quiero 1 comando SSH de rearme, tuve %d", env.ssh.count())
	}
}

func TestSupervisorNoRearmaSinToken(t *testing.T) {
	env := makeRearmEnv(t) // sin tokens registrados
	env.pushViejo("patio")
	env.expira(time.Minute)

	env.arm.SetPollWait(100 * time.Millisecond)
	sup := rearmer.NewSupervisor(env.arm, env.reg, env.d.DB, env.engine, time.Hour, time.Hour)
	sup.CheckOnce()

	if env.ssh.count() != 0 {
		t.Fatalf("sin token no se rearma: tuve %d comandos", env.ssh.count())
	}
}

func TestSupervisorNoRearmaPushFresco(t *testing.T) {
	env := makeRearmEnv(t, "patio")
	env.pushViejo("patio") // push reciente: dentro del TTL

	env.arm.SetPollWait(100 * time.Millisecond)
	sup := rearmer.NewSupervisor(env.arm, env.reg, env.d.DB, env.engine, time.Hour, time.Hour)
	sup.CheckOnce()

	if env.ssh.count() != 0 {
		t.Fatalf("push fresco no se rearma: tuve %d comandos", env.ssh.count())
	}
}

func TestSupervisorNoRearmaSlugNuncaVisto(t *testing.T) {
	env := makeRearmEnv(t, "patio") // token sí, pero ningún push en la vida

	env.arm.SetPollWait(100 * time.Millisecond)
	sup := rearmer.NewSupervisor(env.arm, env.reg, env.d.DB, env.engine, time.Hour, time.Hour)
	sup.CheckOnce()

	if env.ssh.count() != 0 {
		t.Fatalf("slug sin push previo no se rearma: tuve %d comandos", env.ssh.count())
	}
}

func TestSupervisorCooldownLargo(t *testing.T) {
	env := makeRearmEnv(t, "patio")
	env.pushViejo("patio")
	env.expira(time.Minute)

	env.arm.SetPollWait(50 * time.Millisecond)
	sup := rearmer.NewSupervisor(env.arm, env.reg, env.d.DB, env.engine, time.Hour, time.Hour)
	sup.CheckOnce()
	sup.CheckOnce() // segundo tick inmediato: cooldown largo lo frena

	if env.ssh.count() != 1 {
		t.Fatalf("cooldown del supervisor: quiero 1 rearme, tuve %d", env.ssh.count())
	}
}

func TestSupervisorAlertaSinRecuperacion(t *testing.T) {
	env := makeRearmEnv(t, "patio")
	env.pushViejo("patio")
	env.expira(time.Minute)

	env.arm.SetPollWait(50 * time.Millisecond)
	sup := rearmer.NewSupervisor(env.arm, env.reg, env.d.DB, env.engine, time.Hour, time.Hour)
	sup.CheckOnce()

	if env.engine.count() == 0 {
		t.Fatalf("sin recuperación debe emitir alerta de fallo")
	}
}

func TestSupervisorRecuperadoSinAlertaFallo(t *testing.T) {
	env := makeRearmEnv(t, "patio")
	env.pushViejo("patio")
	env.expira(time.Minute)

	env.arm.SetPollWait(600 * time.Millisecond)
	sup := rearmer.NewSupervisor(env.arm, env.reg, env.d.DB, env.engine, time.Hour, time.Hour)
	// El agente "vuelve a empujar" durante la espera del rearme.
	go func() {
		time.Sleep(100 * time.Millisecond)
		env.reg.Ingest(&probe.Payload{Router: "patio", Ts: env.now.Unix(), Version: "2.3.0"})
	}()
	sup.CheckOnce()

	if env.ssh.count() != 1 {
		t.Fatalf("quiero 1 rearme, tuve %d", env.ssh.count())
	}
	// Recuperado: puede haber alerta de éxito (info) pero NUNCA de fallo.
	for _, ev := range env.engine.events() {
		if ev.Severity == "warn" {
			t.Fatalf("recuperado: no debe haber alerta de fallo (%s)", ev.Title)
		}
	}
}

func TestRearmerManualSigueFuncionando(t *testing.T) {
	// El Rearmer compartido responde igual que el handler original: 60 s de
	// cooldown manual.
	env := makeRearmEnv(t, "patio")
	env.arm.SetPollWait(50 * time.Millisecond)

	res, err := env.arm.Rearm("patio")
	if err != nil || !res.Restarted {
		t.Fatalf("rearme manual: %v %+v", err, res)
	}
	if _, err := env.arm.Rearm("patio"); err == nil {
		t.Fatalf("segundo rearme inmediato debe fallar por cooldown")
	}
	if _, err := env.arm.Rearm("noexiste"); err == nil {
		t.Fatalf("slug sin token debe dar ErrNoToken")
	}
}
