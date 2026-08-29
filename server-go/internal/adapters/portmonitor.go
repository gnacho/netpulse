package adapters

import (
	"fmt"
	"sync"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

const (
	flapWindow         = 10 * time.Minute
	flapThreshold      = 5
	ghostConsecutive   = 3
	ghostMinHistory   = 12
	degradedConsec     = 3
	degradedMinHistory = 12
)

type portKey struct {
	routerID string
	portID   string
}

type portState struct {
	up           bool
	transitions  []time.Time
	speedMbps    int
	speedHistory []int
	trafficTotal uint64
	zeroStreak   int
	hadTraffic   bool
	// Incidentes abiertos (issue #366): mientras la condición persiste hay
	// UNA sola alerta viva por puerto (EmitOrUpdate la refresca in situ);
	// la flag dispara la alerta de recuperación exactamente una vez.
	flapActive     bool
	ghostActive    bool
	degradedActive bool
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
		pm.updateSpeedHistory(st, p)
		pm.checkFlapping(key, st, p, now, engine)
		pm.checkGhost(key, st, p, now, engine)
		pm.checkDegraded(key, st, p, now, engine)
	}
}

func (pm *PortMonitor) updateSpeedHistory(st *portState, p EthPort) {
	spd := parseSpeedMbps(p.Speed)
	if !p.Up || spd == 0 {
		st.speedHistory = nil
		st.speedMbps = 0
		return
	}
	st.speedHistory = append(st.speedHistory, spd)
	if len(st.speedHistory) > 200 {
		st.speedHistory = st.speedHistory[len(st.speedHistory)-200:]
	}
	st.speedMbps = spd
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
		st.flapActive = true
		engine.EmitOrUpdate(alerts.AlertEvent{
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
	} else if st.flapActive {
		st.flapActive = false
		engine.Emit(alerts.AlertEvent{
			ID:          fmt.Sprintf("alert-flap-%s-%s-ok-%d", key.routerID, key.portID, now.Unix()),
			Category:    alerts.CatSystem,
			Urgent:      false,
			Severity:    "ok",
			Title:       fmt.Sprintf("Port stable again: %s on %s", p.Label, key.routerID),
			Description: fmt.Sprintf("Flapping stopped, fewer than %d transitions in %s", flapThreshold, flapWindow),
			Time:        "ahora mismo",
			RouterID:    key.routerID,
		})
	}
}

func (pm *PortMonitor) checkGhost(key portKey, st *portState, p EthPort, now time.Time, engine *alerts.Engine) {
	total := p.RxBytes + p.TxBytes
	if !p.Up {
		st.zeroStreak = 0
		st.hadTraffic = false
		st.trafficTotal = 0
		st.ghostActive = false
		return
	}
	if total < st.trafficTotal {
		// Contador reseteado (reboot del router): arranca una era nueva del
		// contador, no es un puerto muerto. Sin esto, el streak crecería
		// durante horas hasta superar el total pre-reboot (issue #365).
		st.trafficTotal = total
		st.hadTraffic = false
		st.zeroStreak = 0
		st.ghostActive = false
		return
	}
	if total > st.trafficTotal {
		st.hadTraffic = true
		st.zeroStreak = 0
	} else if st.hadTraffic {
		st.zeroStreak++
	}
	st.trafficTotal = total
	if len(st.speedHistory) < ghostMinHistory {
		return
	}
	if st.zeroStreak >= ghostConsecutive {
		st.ghostActive = true
		engine.EmitOrUpdate(alerts.AlertEvent{
			ID:          fmt.Sprintf("alert-ghost-%s-%s", key.routerID, key.portID),
			Category:    alerts.CatSystem,
			Urgent:      false,
			Severity:    "warn",
			Title:       fmt.Sprintf("Ghost port: %s went silent", p.Label),
			Description: fmt.Sprintf("Port had traffic but zero bytes for %d consecutive polls", st.zeroStreak),
			Hint:        alerts.HintFor(alerts.HintGhostPort),
			Time:        "ahora mismo",
			RouterID:    key.routerID,
		})
	} else if st.ghostActive && st.zeroStreak == 0 {
		st.ghostActive = false
		engine.Emit(alerts.AlertEvent{
			ID:          fmt.Sprintf("alert-ghost-%s-%s-ok-%d", key.routerID, key.portID, now.Unix()),
			Category:    alerts.CatSystem,
			Urgent:      false,
			Severity:    "ok",
			Title:       fmt.Sprintf("Ghost port recovered: %s on %s", p.Label, key.routerID),
			Description: "Port is moving traffic again",
			Time:        "ahora mismo",
			RouterID:    key.routerID,
		})
	}
}

func (pm *PortMonitor) checkDegraded(key portKey, st *portState, p EthPort, now time.Time, engine *alerts.Engine) {
	if len(st.speedHistory) < degradedMinHistory {
		return
	}
	prev := dominantSpeed(st.speedHistory[:len(st.speedHistory)-degradedConsec])
	if prev == 0 {
		return
	}
	recent := st.speedHistory[len(st.speedHistory)-degradedConsec:]
	allBelow := true
	for _, s := range recent {
		if s >= prev/2 {
			allBelow = false
			break
		}
	}
	if allBelow && st.speedMbps < prev {
		st.degradedActive = true
		engine.EmitOrUpdate(alerts.AlertEvent{
			ID:          fmt.Sprintf("alert-degraded-%s-%s", key.routerID, key.portID),
			Category:    alerts.CatSystem,
			Urgent:      false,
			Severity:    "warn",
			Title:       fmt.Sprintf("Degraded link: %s at %dMbps (was %dMbps)", p.Label, st.speedMbps, prev),
			Description: fmt.Sprintf("Port negotiated speed dropped from %d to %d Mbps for %d consecutive polls", prev, st.speedMbps, degradedConsec),
			Hint:        alerts.HintFor(alerts.HintDegradedLink),
			Time:        "ahora mismo",
			RouterID:    key.routerID,
		})
	} else if st.degradedActive {
		st.degradedActive = false
		engine.Emit(alerts.AlertEvent{
			ID:          fmt.Sprintf("alert-degraded-%s-%s-ok-%d", key.routerID, key.portID, now.Unix()),
			Category:    alerts.CatSystem,
			Urgent:      false,
			Severity:    "ok",
			Title:       fmt.Sprintf("Link speed recovered: %s on %s", p.Label, key.routerID),
			Description: fmt.Sprintf("Port negotiated speed is back to %d Mbps", st.speedMbps),
			Time:        "ahora mismo",
			RouterID:    key.routerID,
		})
	}
}

func dominantSpeed(history []int) int {
	counts := map[int]int{}
	for _, s := range history {
		counts[s]++
	}
	best, bestN := 0, 0
	for spd, n := range counts {
		if n > bestN || (n == bestN && spd > best) {
			best, bestN = spd, n
		}
	}
	return best
}
