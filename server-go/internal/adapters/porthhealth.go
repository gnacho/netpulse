// Package adapters - per-port health score (issue #299).
//
// ComputePortHealth aggregates signals from a single port into a 0-100 score
// with a breakdown of weighted signals. Signals: link_up (30%), utilization
// (25%), errors (25%), flapping (20%).
package adapters

import (
	"time"

	"github.com/gnacho/netpulse/server-go/internal/portseries"
)

// PortHealthItem is a single signal in the port health breakdown.
type PortHealthItem struct {
	Signal string `json:"signal"`
	Weight int    `json:"weight"`
	Status string `json:"status"` // "ok"|"warn"|"crit"
}

// PortHealth is the per-port health score (0-100) with breakdown.
type PortHealth struct {
	Score     int              `json:"score"`
	Breakdown []PortHealthItem `json:"breakdown"`
}

// computePortHealthFromSignals builds a PortHealth from pre-computed signal values.
// linkUp: whether the port is operationally up.
// utilPct: utilization percentage (0-100+).
// errorRate: errors per second (delta errors / delta time).
// flapCount: number of state transitions in the observation window.
func computePortHealthFromSignals(linkUp bool, utilPct float64, errorRate float64, flapCount int) PortHealth {
	if !linkUp {
		return PortHealth{
			Score: 0,
			Breakdown: []PortHealthItem{
				{Signal: "link_down", Weight: 100, Status: "crit"},
			},
		}
	}

	score := 100
	breakdown := []PortHealthItem{}

	linkItem := PortHealthItem{Signal: "link_up", Weight: 30, Status: "ok"}
	breakdown = append(breakdown, linkItem)

	utilStatus := "ok"
	utilPenalty := 0
	if utilPct > 95 {
		utilStatus = "crit"
		utilPenalty = 25
	} else if utilPct > 90 {
		utilStatus = "warn"
		utilPenalty = 15
	} else if utilPct > 80 {
		utilStatus = "warn"
		utilPenalty = 8
	}
	score -= utilPenalty
	breakdown = append(breakdown, PortHealthItem{Signal: "utilization", Weight: 25, Status: utilStatus})

	errStatus := "ok"
	errPenalty := 0
	if errorRate > 10 {
		errStatus = "crit"
		errPenalty = 25
	} else if errorRate > 1 {
		errStatus = "warn"
		errPenalty = 15
	} else if errorRate > 0.1 {
		errStatus = "warn"
		errPenalty = 5
	}
	score -= errPenalty
	breakdown = append(breakdown, PortHealthItem{Signal: "errors", Weight: 25, Status: errStatus})

	flapStatus := "ok"
	flapPenalty := 0
	if flapCount > 10 {
		flapStatus = "crit"
		flapPenalty = 20
	} else if flapCount > 5 {
		flapStatus = "warn"
		flapPenalty = 12
	} else if flapCount > 2 {
		flapStatus = "warn"
		flapPenalty = 5
	}
	score -= flapPenalty
	breakdown = append(breakdown, PortHealthItem{Signal: "flapping", Weight: 20, Status: flapStatus})

	if score < 0 {
		score = 0
	}
	return PortHealth{Score: score, Breakdown: breakdown}
}

// ComputePortHealth computes the health score for a single port from its
// current state, historical samples, and flapping count.
func ComputePortHealth(port EthPort, series []portseries.PortPoint, flappingCount int) PortHealth {
	if !port.Up {
		return computePortHealthFromSignals(false, 0, 0, 0)
	}

	utilPct := computeUtilization(port, series)
	errorRate := computeErrorRate(series)

	return computePortHealthFromSignals(true, utilPct, errorRate, flappingCount)
}

// computeUtilization estimates utilization % from the port's current speed
// and recent traffic rates. Falls back to 0 if speed cannot be parsed.
func computeUtilization(port EthPort, series []portseries.PortPoint) float64 {
	speedMbps := parseSpeedMbps(port.Speed)
	if speedMbps <= 0 {
		if len(series) > 0 {
			speedMbps = series[len(series)-1].SpeedMbps
		}
	}
	if speedMbps <= 0 {
		return 0
	}

	var rxBps, txBps float64
	if port.RxBps > 0 || port.TxBps > 0 {
		rxBps = port.RxBps
		txBps = port.TxBps
	} else if len(series) > 0 {
		last := series[len(series)-1]
		rxBps = last.RxBps
		txBps = last.TxBps
	}

	capacityBps := float64(speedMbps) * 1e6
	if capacityBps <= 0 {
		return 0
	}
	maxBps := rxBps
	if txBps > maxBps {
		maxBps = txBps
	}
	return (maxBps / capacityBps) * 100
}

// computeErrorRate computes errors/second from the series delta. Returns 0
// if there are fewer than 2 points or the time span is zero.
func computeErrorRate(series []portseries.PortPoint) float64 {
	if len(series) < 2 {
		return 0
	}
	first := series[0]
	last := series[len(series)-1]
	span := last.TS.Sub(first.TS).Seconds()
	if span <= 0 {
		return 0
	}
	errDelta := float64(last.RxErrors+last.TxErrors) - float64(first.RxErrors+first.TxErrors)
	if errDelta < 0 {
		errDelta = 0
	}
	return errDelta / span
}

// parseSpeedMbps is defined in live.go

// enrichPortHealth computes and attaches health scores to each port in the
// slice. Requires access to the port series store (db.PortSeries). Flapping
// is estimated from speed-to-zero transitions in the last hour.
func (l *Live) enrichPortHealth(routerID string, ports []EthPort) {
	if l.db == nil || l.db.PortSeries == nil || len(ports) == 0 {
		return
	}
	now := time.Now()
	from := now.Add(-1 * time.Hour)
	for i := range ports {
		series, err := l.db.PortSeries.GetSeries(routerID, ports[i].ID, from, now, "raw")
		if err != nil || len(series) == 0 {
			h := ComputePortHealth(ports[i], nil, 0)
			ports[i].Health = &h
			continue
		}
		flapCount := estimateFlapping(series)
		h := ComputePortHealth(ports[i], series, flapCount)
		ports[i].Health = &h
	}
}

// estimateFlapping counts link-down events (speed dropping to 0) in the series
// as a proxy for flapping. A transition from >0 to 0 counts as one flap.
func estimateFlapping(series []portseries.PortPoint) int {
	if len(series) < 2 {
		return 0
	}
	count := 0
	prevUp := series[0].SpeedMbps > 0
	for _, p := range series[1:] {
		up := p.SpeedMbps > 0
		if prevUp && !up {
			count++
		}
		prevUp = up
	}
	return count
}
