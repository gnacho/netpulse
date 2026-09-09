package adapters

import (
	"fmt"
	"log"
	"math"
	"time"

	npsnmp "github.com/gnacho/netpulse/server-go/internal/snmp"
)

func (l *Live) pollRouterSNMP(cfg RouterConfig) (*routerPolled, error) {
	// issue #414: si no ha pasado el intervalo configurado, reutiliza el último
	// sondeo real para no martillear al switch con SNMP cada 5 s.
	interval := cfg.SnmpPollInterval
	if interval <= 0 {
		interval = 60
	}
	now := l.now()
	l.mu.Lock()
	last, hasLast := l.lastPolled[cfg.ID]
	lastPoll := l.snmpLastPoll[cfg.ID]
	l.mu.Unlock()
	if hasLast && last != nil && now.Sub(lastPoll) < time.Duration(interval)*time.Second {
		return last, nil
	}

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

	for i := range ethPorts {
		ethPorts[i].Snmp = true
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

	// #661: el path SNMP nunca persistía las muestras de puerto, así que el
	// historial de tráfico del switch quedaba vacío aunque el sondeo leyera los
	// contadores correctamente. Ahora se registran (misma ruta que SSH/agente).
	l.recordPortSamples(cfg.ID, ethPorts)

	// FDB → mapa MAC→puerto. Antes se descartaban las entradas cuyo ifIndex no
	// resolvía a un puerto conocido, lo que dejaba el switch con 0 dispositivos
	// conectados aunque el FDB viniera lleno (#661). Se cae a bridgePort y a un
	// nombre generado para no perder MACs (el conteo de clientes solo necesita
	// la clave; el nombre es secundario).
	fdbMap := snmpFdbMap(fdb, portIdxToName)

	l.portMon.Observe(cfg.ID, ethPorts, l.engine)

	// #661: agregado de red del switch (suma de la tasa de todos los puertos)
	// para que la tarjeta de la flota pinte un sparkline con bps reales en
	// lugar de la línea plana a 0 (los switches SNMP sí tienen contadores de
	// bytes, a diferencia de los beacons, que solo reportan tramas).
	netPtr := snmpAggregateNet(ethPorts)

	var uptimeSec float64
	if sysInfo != nil {
		uptimeSec = float64(sysInfo.UpTimeSec)
	}

	p := &routerPolled{
		cfg:       cfg,
		uptimeSec: uptimeSec,
		net:       netPtr,
		ports:     ethPorts,
		fdb:       fdbMap,
		polledAt:  now.UnixMilli(),
	}
	l.mu.Lock()
	l.snmpLastPoll[cfg.ID] = now
	l.mu.Unlock()
	return p, nil
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

// snmpFdbMap convierte el FDB (MAC→puerto) en el mapa que consume la
// topología/dispositivos. #661: NO descarta entradas cuyo ifIndex no resuelve
// a un puerto conocido (antes eso dejaba el switch con 0 dispositivos);
// cae a bridgePort y, en último caso, a un nombre generado.
func snmpFdbMap(fdb []npsnmp.FdbEntry, portIdxToName map[int]string) map[string]string {
	out := map[string]string{}
	for _, e := range fdb {
		name := ""
		if n, ok := portIdxToName[e.IfIndex]; ok {
			name = n
		} else if n, ok := portIdxToName[e.BridgePortIndex]; ok {
			name = n
		}
		if name == "" {
			name = fmt.Sprintf("port-%d", e.IfIndex)
		}
		out[e.MAC] = name
	}
	return out
}

// snmpAggregateNet suma la tasa (bps) de todos los puertos para dar una
// métrica agregada al switch SNMP (#661). Devuelve nil si todo está a 0
// (primera muestra sin delta o sin tráfico).
func snmpAggregateNet(ports []EthPort) *NetDevBps {
	var aggRx, aggTx float64
	for i := range ports {
		aggRx += ports[i].RxBps
		aggTx += ports[i].TxBps
	}
	if aggRx == 0 && aggTx == 0 {
		return nil
	}
	rx, tx := aggRx, aggTx
	return &NetDevBps{RxBps: &rx, TxBps: &tx}
}
