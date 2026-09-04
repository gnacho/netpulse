// scheduler.go — scheduler de upgrades programados (#494).
//
// Bucle de tick corto (30 s) que recoge las programaciones vencidas
// (status 'scheduled' y scheduled_for <= now) y las lanza a través del motor
// compartido (Engine.LaunchScheduled). Single-flight por fila: StartScheduled
// transiciona guarded, así un segundo tick o un cancelado no relanzan.
package firmware

import (
	"log"
	"sync"
	"time"
)

// tickEvery: cadencia del barrido de programaciones (dominio fijo; igual que
// speedtest.tickEvery). Un cambio de hora de programación se aplica en < 30 s.
const tickEvery = 30 * time.Second

// Launcher lanza una programación vencida (lo implementa *Engine).
type Launcher interface {
	LaunchScheduled(Upgrade) error
}

// Scheduler orquesta el barrido periódico de programaciones vencidas.
type Scheduler struct {
	store    *Store
	launcher Launcher

	now  func() time.Time
	logf func(format string, args ...any)

	stop chan struct{}
	wg   sync.WaitGroup
}

// NewScheduler construye el scheduler con su store y el launcher compartido.
func NewScheduler(store *Store, launcher Launcher) *Scheduler {
	return &Scheduler{
		store:    store,
		launcher: launcher,
		now:      time.Now,
		logf:     func(f string, a ...any) { log.Printf("[firmware] "+f, a...) },
		stop:     make(chan struct{}),
	}
}

// Start lanza el bucle periódico (daemon; no retorna hasta Stop).
func (s *Scheduler) Start() {
	s.wg.Add(1)
	defer s.wg.Done()
	ticker := time.NewTicker(tickEvery)
	defer ticker.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.Tick()
		}
	}
}

// Stop detiene el bucle y espera a que termine la pasada en curso.
func (s *Scheduler) Stop() {
	close(s.stop)
	s.wg.Wait()
}

// Tick recoge las programaciones vencidas y las lanza una a una. Los errores
// de lanzamiento se registran (un agente caído marca la fila como failed) sin
// interrumpir el resto del barrido.
func (s *Scheduler) Tick() {
	due, err := s.store.DueScheduled(s.now().UnixMilli())
	if err != nil {
		s.logf("listado de programaciones vencidas: %v", err)
		return
	}
	for _, u := range due {
		if err := s.launcher.LaunchScheduled(u); err != nil {
			s.logf("programación %s (id %d): %v", u.RouterID, u.ID, err)
		}
	}
}
