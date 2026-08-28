package adapters

import (
	"fmt"
	"sync"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

const (
	flapWindow    = 10 * time.Minute
	flapThreshold = 5
)

type portKey struct {
	routerID string
	portID   string
}

type portState struct {
	up          bool
	transitions []time.Time
	speedMbps   int
	speedHistory []int
	trafficTotal uint64
	zeroStreak   int
	hadTraffic   bool
}

type PortMonitor struct {
	mu     sync.Mutex
	states map[portKey]*portState
	now    func() time.Time
}

func NewPortMonitor() *PortMonitor {
	return &PortMonitor{
		states: map[portKey]*portState{},
		now:    time.Now,
	}
}

func (pm *PortMonitor) SetClock(f func() time.Time) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.now = f
}

func (pm *PortMonitor) Observe(routerID string, ports []EthPort, engine *alerts.Engine) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	now := pm.now()
	for _, p := range ports {
		if p.ID == "" {
			continue
		}
		key := portKey{routerID, p.ID}
		st, ok := pm.states[key]
		if !ok {
			st = &portState{up: p.Up}
			pm.states[key] = st
		}
		pm.checkFlapping(key, st, p, now, engine)
		pm.checkGhost(key, st, p, engine)
		pm.checkDegraded(key, st, p, engine)
	}
}

func (pm *PortMonitor) checkFlapping(key portKey, st *portState, p EthPort, now time.Time, engine *alerts.Engine) {
	if p.Up != st.up {
		st.transitions = append(st.transitions, now)
		st.up = p.Up
	}
	cutoff := now.Add(-flapWindow)
	kept := st.transitions[:0]
	for _, t := range st.transitions {
		if !t.Before(cutoff) {
			kept = append(kept, t)
		}
	}
	st.transitions = kept
	if len(st.transitions) >= flapThreshold {
		engine.Emit(alerts.AlertEvent{
			ID:          fmt.Sprintf("alert-flap-%s-%s", key.routerID, key.portID),
			Category:    alerts.CatSystem,
			Urgent:      false,
			Severity:    "warn",
			Title:       fmt.Sprintf("Port flapping: %s on %s", p.Label, key.routerID),
			Description: fmt.Sprintf("%d transitions in %s", len(st.transitions), flapWindow),
			Hint:        alerts.HintFor(alerts.HintPortFlapping),
			Time:        "ahora mismo",
			RouterID:    key.routerID,
		})
	}
}

func (pm *PortMonitor) checkGhost(_ portKey, _ *portState, _ EthPort, _ *alerts.Engine) {
}

func (pm *PortMonitor) checkDegraded(_ portKey, _ *portState, _ EthPort, _ *alerts.Engine) {
}
