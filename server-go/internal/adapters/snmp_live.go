package adapters

import (
	"fmt"
	"log"
	"math"
	"runtime/debug"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
	npsnmp "github.com/gnacho/netpulse/server-go/internal/snmp"
)

func (l *Live) pollRouterSNMP(cfg RouterConfig) (*routerPolled, error) {
	log.Printf("[netpulse] pollRouterSNMP %s@%s:%d", cfg.ID, cfg.Host, cfg.SnmpPort)
	session, err := npsnmp.NewSession(npsnmp.Config{
		Host:      cfg.Host,
		Port:      cfg.SnmpPort,
		Community: cfg.SnmpCommunity,
	})
	if err != nil {
		return nil, fmt.Errorf("snmp %s: %w", cfg.Host, err)
	}
	defer npsnmp.CloseSession(session)

	sysInfo, err := npsnmp.PollSystem(session)
	if err != nil {
		log.Printf("[netpulse] SNMP system %s: %v", cfg.ID, err)
	}
	ports, err := npsnmp.PollIfTable(session)
	if err != nil {
		log.Printf("[netpulse] SNMP ifTable %s: %v", cfg.ID, err)
	}
	fdb, err := npsnmp.PollFdbTable(session)
	if err != nil {
		log.Printf("[netpulse] SNMP FDB %s: %v", cfg.ID, err)
	}

	portIdxToName := map[int]string{}
	for _, p := range ports {
		portIdxToName[p.Index] = p.DisplayName()
	}

	ethPorts := make([]EthPort, 0, len(ports))
	for _, p := range ports {
		name := p.DisplayName()
		label := name
		ethPorts = append(ethPorts, EthPort{
			ID:      fmt.Sprintf("snmp-%d", p.Index),
			Label:   label,
			Up:      p.OperUp,
			Speed:   p.SpeedString(),
			Iface:   name,
			RxBytes: p.RxBytes,
			TxBytes: p.TxBytes,
			RxErrs:  p.RxErrors,
			TxErrs:  p.TxErrors,
		})
	}

	prevPorts := l.snmpPrevPorts(cfg.ID)
	l.mu.Lock()
	l.snmpPortCache(cfg.ID, ethPorts)
	l.mu.Unlock()
	for i := range ethPorts {
		prev, ok := prevPorts[ethPorts[i].ID]
		if !ok {
			continue
		}
		dt := time.Since(prev.at).Seconds()
		if dt <= 0 {
			continue
		}
		if ethPorts[i].RxBytes >= prev.rxBytes {
			ethPorts[i].RxBps = math.Round(float64(ethPorts[i].RxBytes-prev.rxBytes)*8/dt*10) / 10
		}
		if ethPorts[i].TxBytes >= prev.txBytes {
			ethPorts[i].TxBps = math.Round(float64(ethPorts[i].TxBytes-prev.txBytes)*8/dt*10) / 10
		}
	}

	fdbMap := map[string]string{}
	for _, e := range fdb {
		name, ok := portIdxToName[e.IfIndex]
		if !ok || name == "" {
			continue
		}
		fdbMap[e.MAC] = name
	}

	l.portMon.Observe(cfg.ID, ethPorts, l.engine)
	l.recordPortSamples(cfg.ID, ethPorts)

	var uptimeSec float64
	if sysInfo != nil {
		uptimeSec = float64(sysInfo.UpTimeSec)
	}

	return &routerPolled{
		cfg:       cfg,
		uptimeSec: uptimeSec,
		ports:     ethPorts,
		fdb:       fdbMap,
	}, nil
}

type snmpPortSample struct {
	at      time.Time
	rxBytes uint64
	txBytes uint64
}

func (l *Live) snmpPrevPorts(routerID string) map[string]snmpPortSample {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.snmpPorts == nil {
		return nil
	}
	return l.snmpPorts[routerID]
}

func (l *Live) snmpPortCache(routerID string, ports []EthPort) {
	if l.snmpPorts == nil {
		l.snmpPorts = map[string]map[string]snmpPortSample{}
	}
	samples := map[string]snmpPortSample{}
	now := time.Now()
	for _, p := range ports {
		samples[p.ID] = snmpPortSample{at: now, rxBytes: p.RxBytes, txBytes: p.TxBytes}
	}
	l.snmpPorts[routerID] = samples
}

// snmpPollLoop sondea switches SNMP cada 60 s en un goroutine independiente.
// El bucle propio evita martillear al switch y garantiza deltas de contadores.
func (l *Live) snmpPollLoop() {
	log.Printf("[netpulse] SNMP poll loop iniciado")
	defer close(l.snmpDone)
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	// Primer tick inmediato para no esperar 60 s al arrancar.
	l.pollSwitchesOnce()
	for {
		select {
		case <-ticker.C:
			l.pollSwitchesOnce()
		case <-l.snmpStop:
			return
		}
	}
}

func (l *Live) pollSwitchesOnce() {
	l.mu.Lock()
	routers := make([]RouterConfig, 0, len(l.routers))
	for _, r := range l.routers {
		if r.SnmpEnabled {
			routers = append(routers, r)
		}
	}
	l.mu.Unlock()
	log.Printf("[netpulse] pollSwitchesOnce: %d routers SNMP", len(routers))
	if len(routers) == 0 {
		return
	}

	for _, cfg := range routers {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[netpulse] panic SNMP %s: %v\n%s", cfg.ID, r, debug.Stack())
				}
			}()
			p, err := l.pollRouterSNMP(cfg)

			l.mu.Lock()
			wasOffline := l.lastStatus[cfg.ID] == "offline"
			var emit AlertEvent
			var doEmit bool
			if err != nil {
				fails := l.failCount[cfg.ID] + 1
				l.failCount[cfg.ID] = fails
				l.lastErr[cfg.ID] = err
				log.Printf("[netpulse] switch SNMP %s inalcanzable (%d): %v", cfg.ID, fails, err)
				if fails >= 2 && !wasOffline {
					l.lastStatus[cfg.ID] = "offline"
					name := cfg.Name
					if name == "" {
						name = cfg.Host
					}
					emit = AlertEvent{
						ID:       fmt.Sprintf("alert-offline-%s-%d", cfg.ID, time.Now().UnixMilli()),
						Category: alerts.CatRouter, Urgent: true,
						Severity:    "critical",
						Title:       name + " offline",
						Description: fmt.Sprintf("Sin respuesta SNMP de %s: %v", cfg.Host, err),
						Hint:        alerts.HintFor(alerts.HintDeviceOffline),
						Time:        "ahora mismo", RouterID: cfg.ID,
					}
					doEmit = true
				}
				l.mu.Unlock()
				if doEmit {
					l.engine.Emit(emit)
				}
				return
			}
			l.snmpSnapshots[cfg.ID] = p
			l.failCount[cfg.ID] = 0
			delete(l.lastErr, cfg.ID)
			l.lastStatus[cfg.ID] = "online"
			mac := p.brMac
			l.mu.Unlock()

			if wasOffline {
				name := cfg.Name
				if name == "" {
					name = cfg.Host
				}
				l.engine.Emit(AlertEvent{
					ID:       fmt.Sprintf("alert-recovered-%s-%d", cfg.ID, time.Now().UnixMilli()),
					Category: alerts.CatRouter, Urgent: false,
					Severity:    "ok",
					Title:       name + " recuperado",
					Description: fmt.Sprintf("%s vuelve a responder SNMP", name),
					Time:        "ahora mismo", RouterID: cfg.ID,
				})
			}
			if l.db != nil && mac != "" {
				_, _ = l.db.Exec("UPDATE routers SET mac = ? WHERE id = ?", mac, cfg.ID)
			}
		}()
	}
}

