// Package rearmer — Fase 5: rearme de agentes netpulse.
//
// Dos disparadores comparten exactamente la misma lógica de rearme:
//
//  1. Manual (Plan B): POST /api/agents/{slug}/rearm desde la UI.
//  2. Automático: el Supervisor detecta agentes cuyo último push expiró
//     (TTL del registry) y los rearma solo si NETPULSE_AUTO_REARM=1.
//     Nunca rearma un slug sin token registrado y respeta un cooldown por
//     slug para no martillear un router que está roto de verdad.
package rearmer

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/alerts"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
)

const (
	// Cmd es el comando de reinicio del servicio procd del agente.
	Cmd = "/etc/init.d/netpulse-agent restart"
	// SSHWait: timeout del comando SSH de reinicio.
	SSHWait = 10 * time.Second
	// PollWait: cuánto esperar el push de vuelta tras el reinicio.
	PollWait = 30 * time.Second
	// Cooldown: anti-martilleo por slug (rearme manual, Fase 5).
	Cooldown = 60 * time.Second
	// AutoCooldownDefault: anti-martilleo del supervisor (10 min por slug:
	// si el rearme automático no lo arregla, el problema no es el proceso).
	AutoCooldownDefault = 600 * time.Second
	// SupervisorIntervalDefault: cadencia del supervisor.
	SupervisorIntervalDefault = 30 * time.Second
)

// TokenPrefix: prefijo de las claves kv que marcan un slug con agente
// registrado. Duplicado de httpapi.agentTokenKeyPrefix a propósito: httpapi
// importa rearmer (no al revés) y el supervisor necesita consultarlo.
const TokenPrefix = "agent.token."

// SSHRunner ejecuta comandos en los routers (adapters.SSHPool en
// producción; fake en tests).
type SSHRunner interface {
	Run(host, cmd string, timeout time.Duration) (string, error)
}

// AlertsEngine: mínimo del motor de alertas que necesita el rearmer.
type AlertsEngine interface {
	Emit(alerts.AlertEvent) bool
}

// Result es el resultado de un intento de rearme.
type Result struct {
	Slug      string
	Restarted bool   // el comando SSH se ejecutó
	Recovered bool   // llegó un push nuevo tras el reinicio
	Message   string
}

// ErrCooldown se devuelve cuando el slug se rearmó hace poco.
type ErrCooldown struct{ Wait time.Duration }

func (e ErrCooldown) Error() string {
	return fmt.Sprintf("rearme reciente; espera %d s", int(e.Wait.Seconds()))
}

// Errores tipificados (el handler HTTP los mapea a 404/409/503).
var (
	ErrNoToken  = fmt.Errorf("ese slug no tiene agente registrado")
	ErrNoRouter = fmt.Errorf("no hay router con ese slug en la tabla routers")
	ErrNoSSH    = fmt.Errorf("el servidor no tiene pool SSH (modo demo o clave ausente)")
	ErrNoDB     = fmt.Errorf("db no disponible")
)

// Rearmer ejecuta rearmes (compartido entre el handler manual y el
// supervisor automático).
type Rearmer struct {
	db       *sql.DB
	agents   *adapters.AgentRegistry
	pool     SSHRunner
	engine   AlertsEngine
	pollWait time.Duration

	mu        sync.Mutex
	lastRearm map[string]time.Time
}

// New construye un Rearmer. engine puede ser nil (sin alertas; tests).
// pollWait <= 0 → PollWait.
func New(db *sql.DB, agents *adapters.AgentRegistry, pool SSHRunner, engine AlertsEngine, pollWait time.Duration) *Rearmer {
	if pollWait <= 0 {
		pollWait = PollWait
	}
	return &Rearmer{
		db: db, agents: agents, pool: pool, engine: engine,
		pollWait: pollWait, lastRearm: map[string]time.Time{},
	}
}

// SetPollWait ajusta la espera del push de vuelta (solo tests).
func (r *Rearmer) SetPollWait(d time.Duration) {
	if d > 0 {
		r.pollWait = d
	}
}

// Rearm ejecuta un rearme completo del slug. Errores tipificados
// (ErrNoDB, ErrNoToken, ErrNoRouter, ErrNoSSH, ErrCooldown) para que el
// handler HTTP los mapee a su status; cualquier otro error es fallo SSH.
func (r *Rearmer) Rearm(slug string) (Result, error) {
	if r.db == nil {
		return Result{}, ErrNoDB
	}
	// El slug debe tener token registrado (si no, 404 como DELETE).
	var exists int
	if err := r.db.QueryRow("SELECT COUNT(*) FROM kv WHERE key = ?", TokenPrefix+slug).Scan(&exists); err != nil || exists == 0 {
		return Result{}, ErrNoToken
	}
	// Resolver host del router (tabla routers; misma fuente que el handler
	// original: routerstore.ListRouters).
	host := ""
	for _, rc := range routerstore.ListRouters(r.db) {
		if rc.ID == slug {
			host = rc.Host
			break
		}
	}
	if host == "" {
		return Result{}, ErrNoRouter
	}
	if r.pool == nil {
		return Result{}, ErrNoSSH
	}

	// Anti-martilleo por slug (manual).
	r.mu.Lock()
	if last, ok := r.lastRearm[slug]; ok && time.Since(last) < Cooldown {
		wait := Cooldown - time.Since(last)
		r.mu.Unlock()
		return Result{}, ErrCooldown{Wait: wait}
	}
	r.lastRearm[slug] = time.Now()
	r.mu.Unlock()

	before := time.Now()
	// Marcar el lastSeen previo para saber si el push que llega es NUEVO.
	var prevSeen time.Time
	if r.agents != nil {
		if seen, _, ok := r.agents.Info(slug); ok {
			prevSeen = seen
		}
	}

	if _, err := r.pool.Run(host, Cmd, SSHWait); err != nil {
		return Result{}, fmt.Errorf("no pude reiniciar el servicio en %s: %v", host, err)
	}

	// Esperar a que el agente vuelva a empujar (poll cada 2 s hasta pollWait).
	recovered := false
	deadline := time.Now().Add(r.pollWait)
	pollInterval := 2 * time.Second
	if r.pollWait < pollInterval {
		pollInterval = 50 * time.Millisecond
	}
	for time.Now().Before(deadline) {
		time.Sleep(pollInterval)
		if r.agents == nil {
			break
		}
		if seen, _, ok := r.agents.Info(slug); ok && seen.After(prevSeen) && seen.After(before.Add(-5*time.Second)) {
			recovered = true
			break
		}
	}

	res := Result{Slug: slug, Restarted: true, Recovered: recovered}
	if recovered {
		res.Message = "servicio reiniciado y el agente volvió a empujar"
		if r.engine != nil {
			r.engine.Emit(alerts.AlertEvent{
				ID:       fmt.Sprintf("alert-agent-rearm-%s-%d", slug, time.Now().UnixMilli()),
				Category: alerts.CatSystem, Urgent: false, Severity: "info",
				Title:       fmt.Sprintf("Agente rearmado en %s", slug),
				Description: "Reinicio del servicio netpulse-agent desde el servidor — el agente vuelve a empujar",
				Time:        "ahora mismo", RouterID: slug,
			})
		}
	} else {
		res.Message = fmt.Sprintf("servicio reiniciado, pero el agente aún no ha empujado en %d s", int(r.pollWait.Seconds()))
	}
	return res, nil
}

// ---------------------------------------------------------------------------
// Supervisor: auto-rearme tras expiración del TTL (NETPULSE_AUTO_REARM=1)
// ---------------------------------------------------------------------------

// Supervisor vigila el registry de agentes: slug con token registrado cuyo
// último push expiró (TTL) → Rearm con cooldown largo + alerta de fallo si
// no se recupera. Coherente con la regla Fase 10: solo actúa si el usuario
// lo activó explícitamente (flag en .env).
type Supervisor struct {
	rearmer  *Rearmer
	registry *adapters.AgentRegistry
	db       *sql.DB
	engine   AlertsEngine
	interval time.Duration
	cooldown time.Duration

	mu    sync.Mutex
	slots map[string]time.Time

	stop chan struct{}
	done chan struct{}
}

// NewSupervisor crea el supervisor. interval <= 0 → 30 s; cooldown <= 0 →
// AutoCooldownDefault.
func NewSupervisor(r *Rearmer, registry *adapters.AgentRegistry, db *sql.DB, engine AlertsEngine, interval, cooldown time.Duration) *Supervisor {
	if interval <= 0 {
		interval = SupervisorIntervalDefault
	}
	if cooldown <= 0 {
		cooldown = AutoCooldownDefault
	}
	return &Supervisor{
		rearmer: r, registry: registry, db: db, engine: engine,
		interval: interval, cooldown: cooldown,
		slots: map[string]time.Time{},
		stop:  make(chan struct{}), done: make(chan struct{}),
	}
}

// Start lanza el loop en una goroutine (no bloquea).
func (s *Supervisor) Start() {
	go s.run()
}

// Stop detiene el loop y espera a que salga.
func (s *Supervisor) Stop() {
	close(s.stop)
	<-s.done
}

func (s *Supervisor) run() {
	defer close(s.done)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.CheckOnce()
		}
	}
}

// CheckOnce: una pasada del supervisor (los tests la llaman directamente).
func (s *Supervisor) CheckOnce() {
	if s.registry == nil || s.db == nil || s.rearmer == nil {
		return
	}
	for _, slug := range s.registeredSlugs() {
		// Solo rearma si el agente EXPIRÓ (TTL): conocido y último push
		// fuera del TTL. Un slug sin push nunca visto no se rearma (no hay
		// evidencia de que el servicio haya estado vivo).
		if !s.registry.Expired(slug) {
			continue
		}
		if !s.slotFree(slug) {
			continue
		}
		res, err := s.rearmer.Rearm(slug)
		if err != nil {
			// ErrCooldown (rearme manual reciente), SSH caído, etc.:
			// no consume el slot; se reintenta en el próximo tick.
			continue
		}
		s.markSlot(slug)
		if !res.Recovered && s.engine != nil {
			s.engine.Emit(alerts.AlertEvent{
				ID:       fmt.Sprintf("alert-agent-autorearm-fail-%s-%d", slug, time.Now().UnixMilli()),
				Category: alerts.CatSystem, Urgent: false, Severity: "warn",
				Title:       fmt.Sprintf("Auto-rearme sin recuperación en %s", slug),
				Description: fmt.Sprintf("El supervisor reinició netpulse-agent en %s pero el agente no ha vuelto a empujar; próximo intento en %d s", slug, int(s.cooldown.Seconds())),
				Time:        "ahora mismo", RouterID: slug,
			})
		}
	}
}

// registeredSlugs: slugs con token en kv (agent.token.*).
func (s *Supervisor) registeredSlugs() []string {
	rows, err := s.db.Query("SELECT key FROM kv WHERE key LIKE ?", TokenPrefix+"%")
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var key string
		if rows.Scan(&key) == nil {
			out = append(out, key[len(TokenPrefix):])
		}
	}
	return out
}

// slotFree: ¿pasó el cooldown largo desde el último auto-rearme del slug?
func (s *Supervisor) slotFree(slug string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	last, ok := s.slots[slug]
	return !ok || time.Since(last) >= s.cooldown
}

// markSlot registra el auto-rearme recién ejecutado.
func (s *Supervisor) markSlot(slug string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.slots[slug] = time.Now()
}
