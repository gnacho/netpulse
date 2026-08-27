// rearmer_test.go — Fase 5: Rearmer compartido + Supervisor de
// auto-rearme. Casos: rearme completo (SSH ejecutado), supervisor solo
// rearma slugs con token + push expirado (nunca sin evidencia previa),
// cooldown largo del supervisor, alerta de fallo sin recuperación.
package rearmer_test

import (
	"errors"
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

func (f *fakeEngine) EmitOrUpdate(ev alerts.AlertEvent) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.evs {
		if f.evs[i].ID == ev.ID {
			f.evs[i] = ev
			return true
		}
	}
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
	d      *db.DB
	reg    *adapters.AgentRegistry
	ssh    *fakeSSH
	engine *fakeEngine
	arm    *rearmer.Rearmer
	now    time.Time
	setNow func(time.Time)
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

// TestSupervisorConsolidaFallosRepetidos (#271): N reintentos fallidos del
// mismo slug → UNA sola alerta (mismo ID, actualizada), no N copias.
func TestSupervisorConsolidaFallosRepetidos(t *testing.T) {
	env := makeRearmEnv(t, "patio")
	env.pushViejo("patio")
	env.expira(time.Minute)

	// pollWait corto para no dormir; cooldowns cortos para poder reintentar
	// en el mismo test (en prod supervisor 600 s y Rearmer 60 s).
	env.arm.SetPollWait(50 * time.Millisecond)
	env.arm.SetCooldown(5 * time.Millisecond)
	sup := rearmer.NewSupervisor(env.arm, env.reg, env.d.DB, env.engine, time.Hour, 20*time.Millisecond)

	for i := 0; i < 3; i++ {
		sup.CheckOnce()
		time.Sleep(40 * time.Millisecond) // deja pasar el cooldown del slot
	}

	if got := env.ssh.count(); got != 3 {
		t.Fatalf("quiero 3 reintentos SSH, tuve %d", got)
	}
	evs := env.engine.events()
	fails := []alerts.AlertEvent{}
	for _, ev := range evs {
		if ev.Title == "Auto-rearme sin recuperación en patio" {
			fails = append(fails, ev)
		}
	}
	if len(fails) != 1 {
		t.Fatalf("3 fallos debían consolidarse en 1 alerta de fallo, hay %d", len(fails))
	}
	if len(evs) > 0 && evs[0].Title == "Auto-rearme sin recuperación en patio" && evs[0].ID != fails[0].ID {
		t.Fatalf("el ID de la alerta consolidada debe ser el del primer fallo: %s", fails[0].ID)
	}
}

// TestSupervisorRecuperadoCierraIncidente (#271): al recuperarse el agente
// (vuelve a empujar) se cierra el incidente; un fallo posterior crea una
// alerta NUEVA (ID distinto), no actualiza la vieja.
func TestSupervisorRecuperadoCierraIncidente(t *testing.T) {
	env := makeRearmEnv(t, "patio")
	env.pushViejo("patio")
	env.expira(time.Minute)

	env.arm.SetPollWait(50 * time.Millisecond)
	env.arm.SetCooldown(5 * time.Millisecond)
	sup := rearmer.NewSupervisor(env.arm, env.reg, env.d.DB, env.engine, time.Hour, 20*time.Millisecond)

	// Incidente 1: fallo → 1 alerta.
	sup.CheckOnce()
	first := env.engine.events()
	if len(first) != 1 || first[0].Title != "Auto-rearme sin recuperación en patio" {
		t.Fatalf("incidente 1: quiero 1 alerta de fallo, hay %+v", first)
	}

	// El agente se recupera: vuelve a empujar (dentro del TTL).
	env.reg.Ingest(&probe.Payload{Router: "patio", Ts: env.now.Unix(), Version: "2.3.0"})
	sup.CheckOnce() // este tick ve el slug sano → cierra el incidente

	// Incidente 2: el agente vuelve a expirar y falla → alerta NUEVA.
	env.expira(time.Minute)
	time.Sleep(40 * time.Millisecond)
	sup.CheckOnce()

	evs := env.engine.events()
	fails := []alerts.AlertEvent{}
	for _, ev := range evs {
		if ev.Title == "Auto-rearme sin recuperación en patio" {
			fails = append(fails, ev)
		}
	}
	if len(fails) != 2 {
		t.Fatalf("dos incidentes → 2 alertas de fallo (una por incidente), hay %d", len(fails))
	}
	if fails[0].ID == fails[1].ID {
		t.Fatalf("incidentes distintos deben tener IDs distintos: %s == %s", fails[0].ID, fails[1].ID)
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

// Un slug cuyo router es de tipo EXTERNO (managed-switch por beacon) no se
// rearmera: Rearm devuelve ErrExternalAgent (sin SSH) y el supervisor lo
// ignora (solo alerta el Dead Man's Switch, que vive en el adapter live).
func TestRearmExternalAgentRejected(t *testing.T) {
	env := makeRearmEnv(t, "switch16")
	// el env añade "patio" openwrt; añadir switch16 como managed-switch
	if _, err := routerstore.AddRouter(env.d.DB, routerstore.AddInput{
		Name: "switch16", Host: "192.168.1.6", Type: "managed-switch",
	}); err != nil {
		t.Fatalf("AddRouter switch16: %v", err)
	}
	if _, err := env.arm.Rearm("switch16"); !errors.Is(err, rearmer.ErrExternalAgent) {
		t.Fatalf("switch externo: got %v want ErrExternalAgent", err)
	}
	// El nativo "patio" sigue pasando (su error no debe ser ErrExternalAgent).
	_, err := env.arm.Rearm("patio")
	if errors.Is(err, rearmer.ErrExternalAgent) {
		t.Fatalf("patio (openwrt) no debe rechazarse como externo")
	}
}
