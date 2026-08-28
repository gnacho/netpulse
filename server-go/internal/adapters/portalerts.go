// portalerts.go — Alertas inteligentes por puerto (#303, #307, #308).
// Reglas deterministas sobre EthPort + FDB; todas son category system,
// urgent=false y deduplicadas por ID.
package adapters

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

const (
	// flapping: N transiciones dentro de la ventana.
	portFlapWindow      = 5 * time.Minute
	portFlapThreshold   = 4
	portFlapCooldown    = 5 * time.Minute
	ghostBaselineTicks  = 6
	ghostSilentTicks    = 6
	degradedThreshold   = 3
	trunkMacThreshold   = 5
	portAlertPruneEvery = 15 * time.Minute
)

// PortAlerts emite alertas por puerto a partir de snapshots sucesivos.
type PortAlerts struct {
	mu     sync.Mutex
	engine *alerts.Engine
	now    func() time.Time

	// último estado Up conocido por router/puerto (para detectar transiciones).
	lastUp map[string]map[string]bool
	// historial de transiciones con timestamp para flapping.
	flapHist map[string]map[string][]time.Time
	// flapAlerted evita repetir dentro del cooldown.
	flapAlerted map[string]time.Time

	// ghostBaseline: puerto con tráfico sostenido (baseline establecida).
	ghostBaseline map[string]map[string]bool
	// ghostCounters: ticks consecutivos con/sin tráfico.
	ghostCounters map[string]map[string]*ghostCounters
	ghostAlerted  map[string]bool

	// speedHist: máxima velocidad histórica por puerto (Mbps).
	speedHist map[string]map[string]float64
	// speedDownTicks: ticks seguidos a velocidad menor que el máximo.
	speedDownTicks map[string]map[string]int
	speedAlerted   map[string]bool

	// trunkMacs: MACs ya vistas en cada puerto (para alertar una sola vez).
	trunkMacs     map[string]map[string]map[string]bool
	trunkMacAlert map[string]bool

	lastPrune time.Time
}

type ghostCounters struct {
	nonZero int
	silent  int
}

// NewPortAlerts crea el rastreador de alertas por puerto.
func NewPortAlerts(engine *alerts.Engine) *PortAlerts {
	return &PortAlerts{
		engine:         engine,
		now:            time.Now,
		lastUp:         map[string]map[string]bool{},
		flapHist:       map[string]map[string][]time.Time{},
		flapAlerted:    map[string]time.Time{},
		ghostBaseline:  map[string]map[string]bool{},
		ghostCounters:  map[string]map[string]*ghostCounters{},
		ghostAlerted:   map[string]bool{},
		speedHist:      map[string]map[string]float64{},
		speedDownTicks: map[string]map[string]int{},
		speedAlerted:   map[string]bool{},
		trunkMacs:      map[string]map[string]map[string]bool{},
		trunkMacAlert:  map[string]bool{},
		lastPrune:      time.Now(),
	}
}

// SetClock permite inyectar el reloj en tests.
func (pa *PortAlerts) SetClock(f func() time.Time) { pa.now = f }

// Track analiza un snapshot de puertos de un router.
func (pa *PortAlerts) Track(routerID string, ports []EthPort, fdb map[string]string) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	now := pa.now()
	for i := range ports {
		port := &ports[i]
		pa.detectFlapping(routerID, port, now)
		pa.detectGhost(routerID, port, now)
		pa.detectDegraded(routerID, port, now)
	}
	if fdb != nil {
		pa.detectUnknownMacOnTrunk(routerID, fdb, now)
	}
	if now.Sub(pa.lastPrune) > portAlertPruneEvery {
		pa.prune(now)
		pa.lastPrune = now
	}
}

func (pa *PortAlerts) key(routerID, portID string) string { return routerID + "::" + portID }

func (pa *PortAlerts) detectFlapping(routerID string, port *EthPort, now time.Time) {
	key := pa.key(routerID, port.ID)
	prevUp, hadPrev := pa.lastUp[routerID][port.ID]
	if pa.lastUp[routerID] == nil {
		pa.lastUp[routerID] = map[string]bool{}
	}
	pa.lastUp[routerID][port.ID] = port.Up
	if !hadPrev {
		return
	}
	if prevUp == port.Up {
		return
	}
	if pa.flapHist[routerID] == nil {
		pa.flapHist[routerID] = map[string][]time.Time{}
	}
	pa.flapHist[routerID][port.ID] = append(pa.flapHist[routerID][port.ID], now)
	// Podar transiciones antiguas.
	hist := pa.flapHist[routerID][port.ID]
	cutoff := now.Add(-portFlapWindow)
	j := 0
	for _, t := range hist {
		if t.After(cutoff) || t.Equal(cutoff) {
			hist[j] = t
			j++
		}
	}
	pa.flapHist[routerID][port.ID] = hist[:j]
	if len(hist) >= portFlapThreshold {
		if last, ok := pa.flapAlerted[key]; !ok || now.Sub(last) > portFlapCooldown {
			pa.flapAlerted[key] = now
			pa.engine.EmitOrUpdate(AlertEvent{
				ID:       fmt.Sprintf("port-flap-%s-%s", routerID, port.ID),
				Category: alerts.CatSystem, Urgent: false,
				Severity: "warn",
				Title:    fmt.Sprintf("Puerto %s flappeando en %s", port.Label, routerID),
				Description: fmt.Sprintf("%d transiciones up/down en los últimos %s — revisa cable o conector.",
					len(hist), portFlapWindow),
				Time:     "ahora mismo",
				RouterID: routerID,
			})
		}
	}
}

func (pa *PortAlerts) detectGhost(routerID string, port *EthPort, now time.Time) {
	key := pa.key(routerID, port.ID)
	if pa.ghostCounters[routerID] == nil {
		pa.ghostCounters[routerID] = map[string]*ghostCounters{}
	}
	if pa.ghostBaseline[routerID] == nil {
		pa.ghostBaseline[routerID] = map[string]bool{}
	}
	c := pa.ghostCounters[routerID][port.ID]
	if c == nil {
		c = &ghostCounters{}
		pa.ghostCounters[routerID][port.ID] = c
	}
	traffic := port.RxBps + port.TxBps
	if !port.Up || traffic == 0 {
		c.silent++
		c.nonZero = 0
	} else {
		c.nonZero++
		c.silent = 0
	}
	if !pa.ghostBaseline[routerID][port.ID] && c.nonZero >= ghostBaselineTicks {
		pa.ghostBaseline[routerID][port.ID] = true
	}
	if pa.ghostBaseline[routerID][port.ID] && c.silent >= ghostSilentTicks && !pa.ghostAlerted[key] {
		pa.ghostAlerted[key] = true
		pa.engine.EmitOrUpdate(AlertEvent{
			ID:       fmt.Sprintf("port-ghost-%s-%s", routerID, port.ID),
			Category: alerts.CatSystem, Urgent: false,
			Severity: "warn",
			Title:    fmt.Sprintf("Puerto %s sin tráfico en %s", port.Label, routerID),
			Description: fmt.Sprintf("%s tenía tráfico estable y ha estado %d ticks a cero — el dispositivo conectado podría estar caído.",
				port.Label, c.silent),
			Time:     "ahora mismo",
			RouterID: routerID,
		})
	}
	if traffic > 0 {
		pa.ghostAlerted[key] = false
	}
}

func (pa *PortAlerts) detectDegraded(routerID string, port *EthPort, now time.Time) {
	if !port.Up || port.Speed == "" {
		return
	}
	cur := speedMbps(port.Speed)
	if cur <= 0 {
		return
	}
	key := pa.key(routerID, port.ID)
	if pa.speedHist[routerID] == nil {
		pa.speedHist[routerID] = map[string]float64{}
	}
	if pa.speedDownTicks[routerID] == nil {
		pa.speedDownTicks[routerID] = map[string]int{}
	}
	maxSpeed := pa.speedHist[routerID][port.ID]
	if cur > maxSpeed {
		pa.speedHist[routerID][port.ID] = cur
		pa.speedDownTicks[routerID][port.ID] = 0
		pa.speedAlerted[key] = false
		return
	}
	if cur < maxSpeed {
		pa.speedDownTicks[routerID][port.ID]++
		if pa.speedDownTicks[routerID][port.ID] >= degradedThreshold && !pa.speedAlerted[key] {
			pa.speedAlerted[key] = true
			pa.engine.EmitOrUpdate(AlertEvent{
				ID:       fmt.Sprintf("port-degraded-%s-%s", routerID, port.ID),
				Category: alerts.CatSystem, Urgent: false,
				Severity: "warn",
				Title:    fmt.Sprintf("Puerto %s negoció a %s en %s", port.Label, port.Speed, routerID),
				Description: fmt.Sprintf("Velocidad histórica %.0f Mbps; ahora %.0f Mbps. Posible cable o NIC dañado.",
					maxSpeed, cur),
				Time:     "ahora mismo",
				RouterID: routerID,
			})
		}
	} else {
		pa.speedDownTicks[routerID][port.ID] = 0
	}
}

func (pa *PortAlerts) detectUnknownMacOnTrunk(routerID string, fdb map[string]string, now time.Time) {
	// Contar MACs por puerto.
	counts := map[string]int{}
	for _, port := range fdb {
		counts[port]++
	}
	if pa.trunkMacs[routerID] == nil {
		pa.trunkMacs[routerID] = map[string]map[string]bool{}
	}
	for mac, port := range fdb {
		if counts[port] < trunkMacThreshold {
			continue // solo trunks/uplinks con muchas MACs
		}
		if pa.trunkMacs[routerID][port] == nil {
			pa.trunkMacs[routerID][port] = map[string]bool{}
		}
		if !pa.trunkMacs[routerID][port][mac] {
			pa.trunkMacs[routerID][port][mac] = true
			key := fmt.Sprintf("port-trunkmac-%s-%s-%s", routerID, port, mac)
			if !pa.trunkMacAlert[key] {
				pa.trunkMacAlert[key] = true
				pa.engine.Emit(AlertEvent{
					ID:       key,
					Category: alerts.CatSystem, Urgent: false,
					Severity: "warn",
					Title:    fmt.Sprintf("MAC nueva en trunk %s de %s", port, routerID),
					Description: fmt.Sprintf("%s apareció en %s, puerto con %d MACs aprendidas.",
						mac, port, counts[port]),
					Time:     "ahora mismo",
					RouterID: routerID,
				})
			}
		}
	}
}

func (pa *PortAlerts) prune(now time.Time) {
	cutoff := now.Add(-portFlapWindow)
	for r := range pa.flapHist {
		for p, hist := range pa.flapHist[r] {
			j := 0
			for _, t := range hist {
				if t.After(cutoff) {
					hist[j] = t
					j++
				}
			}
			pa.flapHist[r][p] = hist[:j]
		}
	}
	for k, t := range pa.flapAlerted {
		if now.Sub(t) > portFlapCooldown {
			delete(pa.flapAlerted, k)
		}
	}
}

// speedMbps convierte "1 Gbps", "100 Mbps", "10 Mbps" en Mbps.
// Valores no reconocidos devuelven 0.
func speedMbps(s string) float64 {
	s = strings.ToLower(strings.TrimSpace(s))
	fields := strings.Fields(s)
	if len(fields) < 2 {
		return 0
	}
	n, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	switch fields[1] {
	case "gbps":
		return n * 1000
	case "mbps":
		return n
	case "kbps":
		return n / 1000
	}
	return 0
}
