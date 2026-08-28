package adapters

import (
	"fmt"
	"log"
	"math"
	"time"

	npsnmp "github.com/gnacho/netpulse/server-go/internal/snmp"
)

func (l *Live) pollRouterSNMP(cfg RouterConfig) (*routerPolled, error) {
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
