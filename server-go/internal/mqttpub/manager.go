// manager.go: ciclo de vida del publisher MQTT (#838). Los Ajustes lo
// reconfiguran en caliente (cancel + rearranque) sin reiniciar NetPulse.

package mqttpub

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
)

// shutdownWait: cuánto espera Apply a que el publisher viejo termine su
// cierre (offline retenido + DISCONNECT) antes de arrancar el nuevo (#1158).
const shutdownWait = 5 * time.Second

// Manager posee el publisher en marcha y permite sustituirlo.
type Manager struct {
	mu       sync.Mutex
	cancel   context.CancelFunc
	done     chan struct{} // se cierra cuando el goroutine del publisher sale
	cfg      Config
	version  string
	snapshot func() *adapters.Overview
	demo     bool
}

// NewManager prepara el manager sin arrancar el publisher: hay que llamar a
// Start cuando el poller ya está en marcha (así el primer estado no sale vacío).
func NewManager(cfg Config, version string, snapshot func() *adapters.Overview, demo bool) *Manager {
	return &Manager{cfg: cfg, version: version, snapshot: snapshot, demo: demo}
}

// Start arranca el publisher si no lo estaba ya.
func (m *Manager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		return
	}
	m.startLocked()
}

// startLocked arranca el publisher si la config lo permite (sin bloquear).
func (m *Manager) startLocked() {
	if !m.cfg.Enabled || m.demo || m.snapshot == nil {
		if m.cfg.Enabled && m.demo {
			log.Printf("[mqtt] disabled in demo mode")
		}
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	m.cancel = cancel
	m.done = done
	pub := New(m.cfg, m.version, m.snapshot, m.demo)
	go func() {
		defer close(done)
		pub.Run(ctx)
	}()
}

// stopLocked cancela el publisher en marcha y espera (acotado) a que su
// goroutine termine, de modo que su offline retenido SIEMPRE aterriza antes
// de que el siguiente publisher publique online (#1158): si el offline del
// viejo llegase después del online del nuevo, el broker retendría offline y
// las entidades de HA quedarían unavailable con datos fluyendo.
func (m *Manager) stopLocked() {
	if m.cancel == nil {
		return
	}
	m.cancel()
	m.cancel = nil
	if m.done != nil {
		select {
		case <-m.done:
		case <-time.After(shutdownWait):
			log.Printf("[mqtt] publisher shutdown timed out after %s; starting the new one anyway", shutdownWait)
		}
		m.done = nil
	}
}

// Apply sustituye la configuración y rearranca el publisher cuando cambia.
func (m *Manager) Apply(cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg == m.cfg {
		return
	}
	m.stopLocked()
	m.cfg = cfg
	m.startLocked()
}

// Config devuelve la configuración en uso.
func (m *Manager) Config() Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}

// Running informa de si el publisher está activo ahora mismo.
func (m *Manager) Running() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cancel != nil
}
