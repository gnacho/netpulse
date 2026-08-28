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
		pm.checkGhost(key, st, p, engine)
		pm.checkDegraded(key, st, p, engine)
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

func (pm *PortMonitor) checkGhost(key portKey, st *portState, p EthPort, engine *alerts.Engine) {
	total := p.RxBytes + p.TxBytes
	if !p.Up {
		st.zeroStreak = 0
		st.hadTraffic = false
		st.trafficTotal = 0
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
		engine.Emit(alerts.AlertEvent{
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
	}
}

func (pm *PortMonitor) checkDegraded(key portKey, st *portState, p EthPort, engine *alerts.Engine) {
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
		engine.Emit(alerts.AlertEvent{
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
