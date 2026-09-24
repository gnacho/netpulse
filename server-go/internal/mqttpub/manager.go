// manager.go: ciclo de vida del publisher MQTT (#838). Los Ajustes lo
// reconfiguran en caliente (cancel + rearranque) sin reiniciar NetPulse.
package mqttpub

import (
	"context"
	"log"
	"sync"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
)

// Manager posee el publisher en marcha y permite sustituirlo.
type Manager struct {
	mu       sync.Mutex
	cancel   context.CancelFunc
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
	m.cancel = cancel
	New(m.cfg, m.version, m.snapshot, m.demo).Start(ctx)
}

// Apply sustituye la configuración y rearranca el publisher cuando cambia.
func (m *Manager) Apply(cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if cfg == m.cfg {
		return
	}
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
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
