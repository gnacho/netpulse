package adapters

import (
	"fmt"
	"log"
	"math"
	"net"
	"os"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
	npsnmp "github.com/gnacho/netpulse/server-go/internal/snmp"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
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
		// #930: error de sesión = fallo del poll SNMP (lastErr + consecFail).
		l.recordSnmpFailure(cfg, err)
		return nil, fmt.Errorf("snmp %s: %w", cfg.Host, err)
	}
	defer npsnmp.CloseSession(session)

	sysInfo, sysErr := npsnmp.PollSystem(session)
	if sysErr != nil {
		log.Printf("[netpulse] SNMP system %s: %v", cfg.LogLabel(), sysErr)
	}
	// #1256: temperatura del chasis en MikroTik SwOS (grados enteros).
	// Best-effort: los switches que no exponen el OID responden
	// NoSuchInstance y hasTemp queda false (vitals a null como antes).
	mikroTemp, hasTemp := npsnmp.PollMikrotikTemp(session)
	// #1036: MAC propia del switch (dot1dBaseBridgeAddress). Sin ella el
	// equipo no tenía MAC en flota ni casaba por MAC en topología. Cache:
	// si el OID no contesta en este poll se conserva la última conocida.
	brMac, _ := npsnmp.PollBridgeAddress(session)
	l.mu.Lock()
	if brMac == "" {
		brMac = l.snmpBrMac[cfg.ID]
	} else {
		l.snmpBrMac[cfg.ID] = brMac
	}
	l.mu.Unlock()
	ports, ifErr := npsnmp.PollIfTable(session)
	if ifErr != nil {
		log.Printf("[netpulse] SNMP ifTable %s: %v", cfg.LogLabel(), ifErr)
	}
	fdbPoll, fdbErr := npsnmp.PollFdbTable(session)
	fdb := fdbPoll.Entries
	if fdbErr != nil {
		log.Printf("[netpulse] SNMP FDB %s: %v", cfg.LogLabel(), fdbErr)
	} else {
		// #928: visibilidad mínima del resultado del FDB. Se loguea cuando el
		// conteo cambia respecto al ciclo anterior (o en el primer poll), no
		// cada 60 s: suficiente para diagnosticar "walk vacío/fallido" sin
		// inundar el journal. #948: la línea lleva además los PDUs crudos de
		// cada walk para distinguir "tabla vacía" de "datos no utilizables".
		l.mu.Lock()
		last, seen := l.snmpFdbCount[cfg.ID]
		l.snmpFdbCount[cfg.ID] = len(fdb)
		l.mu.Unlock()
		if !seen || last != len(fdb) {
			log.Printf("[netpulse] SNMP FDB %s: %d entradas (fuente %s; pdus dot1d=%d dot1q=%d)", cfg.LogLabel(), len(fdb), fdbPoll.Source, fdbPoll.RawDot1d, fdbPoll.RawDot1q)
		}
	}

	// #930: salud del poll SNMP. Un poll se considera fallido cuando la
	// sesión falla (arriba), el Get de sistema no responde o el walk ifTable
	// no devuelve puertos. En fallo NO se devuelve error al caller (el router
	// puede estar vivo: es la comunidad/el puerto lo que falla); el error se
	// refleja en los contadores y, al 3.er fallo, en el ping de respaldo.
	if len(ports) == 0 {
		pollErr := ifErr
		if pollErr == nil {
			if sysErr != nil {
				pollErr = sysErr
			} else {
				pollErr = fmt.Errorf("snmp ifTable %s: sin puertos", cfg.Host)
			}
		}
		l.recordSnmpFailure(cfg, pollErr)
	} else {
		l.recordSnmpSuccess(cfg)
	}

	// #931: vecinos LLDP del switch vía LLDP-MIB (mismo contrato que la
	// sonda lldpd de los routers, #300). Alimenta routerPolled.lldp y con
	// ello la fase LLDP-first de la topología (inferLldpLinks). Un switch
	// sin LLDP devuelve (nil, nil) sin ruido; el error solo se loguea.
	lldpRem, lldpErr := npsnmp.PollLldpRemTable(session)
	if lldpErr != nil {
		log.Printf("[netpulse] SNMP LLDP %s: %v", cfg.LogLabel(), lldpErr)
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
			Family:  p.Family,
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
		brMac:     brMac,
		lldp:      snmpLldpNeighbors(lldpRem, portIdxToName),
		polledAt:  now.UnixMilli(),
	}
	if sysInfo != nil {
		// #1256: sysDescr declara el modelo completo del equipo (p. ej.
		// "CSS610-8G-2S+ SwOS v2.21") y sysName el hostname que define el
		// usuario; buildRouter los reparte entre Model y Name.
		p.sysDescr = sysInfo.Descr
		p.sysName = sysInfo.Name
	}
	p.temp, p.hasTemp = mikroTemp, hasTemp
	l.mu.Lock()
	l.snmpLastPoll[cfg.ID] = now
	l.mu.Unlock()
	return p, nil
}

// snmpPollStat (issue #930): salud del sondeo SNMP por router. En memoria,
// protegido por l.mu. failingOpen marca el incidente ABIERTO (alerta emitida
// y aún no recuperada) para no repetir la alerta hasta la recuperación.
type snmpPollStat struct {
	Ok          int64
	Fail        int64
	ConsecFail  int64
	LastOkMs    int64
	LastFailMs  int64
	LastErr     string
	failingOpen bool
}

// snmpStat devuelve (creando si falta) el contador de salud SNMP del router.
// Debe llamarse con l.mu tomado.
func (l *Live) snmpStat(id string) *snmpPollStat {
	if l.snmpPollStats == nil {
		l.snmpPollStats = map[string]*snmpPollStat{}
	}
	s := l.snmpPollStats[id]
	if s == nil {
		s = &snmpPollStat{}
		l.snmpPollStats[id] = s
	}
	return s
}

// recordSnmpSuccess registra un poll SNMP con datos (#930): ok++,
// consecFail=0. Si había un incidente abierto, lo resuelve y emite la alerta
// ok de recuperación (patrón agente outdated/updated).
func (l *Live) recordSnmpSuccess(cfg RouterConfig) {
	name := cfg.Name
	if name == "" {
		name = cfg.Host
	}
	now := time.Now().UnixMilli()
	l.mu.Lock()
	s := l.snmpStat(cfg.ID)
	s.Ok++
	s.ConsecFail = 0
	s.LastOkMs = now
	wasOpen := s.failingOpen
	s.failingOpen = false
	l.mu.Unlock()

	if wasOpen {
		log.Printf("[netpulse] SNMP recuperado %s", cfg.LogLabel())
		l.engine.Resolve(fmt.Sprintf("alert-snmp-failing-%s", cfg.ID))
		l.engine.Emit(AlertEvent{
			ID:       fmt.Sprintf("alert-snmp-recovered-%s-%d", cfg.ID, now),
			Category: alerts.CatSystem, Urgent: false,
			Severity:    "ok",
			Title:       "SNMP polling recovered on " + name,
			Description: fmt.Sprintf("SNMP polling for %s is responding again", name),
			Type:        alerts.TypeSnmpRecovered,
			Vars:        map[string]string{"router": name},
			Time:        "just now", RouterID: cfg.ID,
		})
	}
}

// recordSnmpFailure registra un poll SNMP fallido (#930): fail++,
// consecFail++, lastErr. En la transición al 3.er fallo seguido hace un ping
// de respaldo al host: si responde, emite una alerta warn "snmp-failing"
// (una por incidente); si no responde, solo loguea (la caída del router ya
// la cubre el alerteo de offline).
func (l *Live) recordSnmpFailure(cfg RouterConfig, err error) {
	name := cfg.Name
	if name == "" {
		name = cfg.Host
	}
	now := time.Now().UnixMilli()
	l.mu.Lock()
	s := l.snmpStat(cfg.ID)
	s.Fail++
	s.ConsecFail++
	s.LastFailMs = now
	s.LastErr = err.Error()
	consec := s.ConsecFail
	open := s.failingOpen
	lastErr := s.LastErr
	l.mu.Unlock()

	switch {
	case consec == 1:
		log.Printf("[netpulse] SNMP fallo %s: %v", cfg.LogLabel(), err)
	case consec == 3 && !open:
		pingFn := l.ping
		if pingFn == nil {
			pingFn = pingHost
		}
		if !pingFn(cfg.Host) {
			log.Printf("[netpulse] SNMP %s: 3 fallos seguidos y sin respuesta a ping (equipo caído); sin alerta SNMP", cfg.LogLabel())
			return
		}
		// Marcar incidente abierto (re-check bajo lock para no duplicar en
		// carreras entre goroutines de distintos routers).
		l.mu.Lock()
		s2 := l.snmpStat(cfg.ID)
		already := s2.failingOpen
		s2.failingOpen = true
		l.mu.Unlock()
		if already {
			return
		}
		l.engine.Emit(AlertEvent{
			ID:       fmt.Sprintf("alert-snmp-failing-%s", cfg.ID),
			Category: alerts.CatSystem, Urgent: false,
			Severity:    "warn",
			Title:       "SNMP polling failing on " + name,
			Description: fmt.Sprintf("%s responds to ping but SNMP polling has failed 3 times (%s) - check community/port", name, lastErr),
			Hint:        alerts.HintFor(alerts.HintSnmpFailing),
			Type:        alerts.HintSnmpFailing,
			Vars:        map[string]string{"router": name, "error": lastErr},
			Time:        "just now", RouterID: cfg.ID,
		})
	}
}

// pingHost hace un ping ICMP (echo request) sin privilegios (#930): usa un
// socket UDP no privilegiado (ListenPacket("udp4"), soportado por el
// ping_group_range del kernel) en vez de un socket raw que requeriría root.
// Devuelve true si el host responde al echo dentro de ~2 s.
func pingHost(host string) bool {
	ip, err := net.ResolveIPAddr("ip4", host)
	if err != nil {
		return false
	}
	c, err := icmp.ListenPacket("udp4", "0.0.0.0")
	if err != nil {
		return false
	}
	defer c.Close()
	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{
			ID:   os.Getpid() & 0xffff,
			Seq:  1,
			Data: []byte("netpulse"),
		},
	}
	b, err := msg.Marshal(nil)
	if err != nil {
		return false
	}
	if _, err := c.WriteTo(b, &net.UDPAddr{IP: ip.IP}); err != nil {
		return false
	}
	if err := c.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		return false
	}
	rb := make([]byte, 1500)
	for {
		n, _, err := c.ReadFrom(rb)
		if err != nil {
			return false
		}
		rm, err := icmp.ParseMessage(1, rb[:n])
		if err != nil {
			continue
		}
		if rm.Type == ipv4.ICMPTypeEchoReply {
			if echo, ok := rm.Body.(*icmp.Echo); ok && echo.ID == os.Getpid()&0xffff {
				return true
			}
		}
	}
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

// snmpLldpNeighbors convierte los vecinos LLDP-MIB del switch al contrato
// LldpNeighbor (#931). El puerto se nombra como en el FDB (DisplayName del
// ifTable vía ifIndex; fallback al número de puerto de bridge, paridad con
// snmpFdbMap #661) para que inferLldpLinks case el vecino con las MACs
// aprendidas en esa boca.
func snmpLldpNeighbors(rem []npsnmp.LldpRemEntry, portIdxToName map[int]string) []LldpNeighbor {
	if len(rem) == 0 {
		return nil
	}
	out := make([]LldpNeighbor, 0, len(rem))
	for _, e := range rem {
		port := portIdxToName[e.IfIndex]
		if port == "" {
			port = portIdxToName[e.LocalPortNum]
		}
		if port == "" {
			port = fmt.Sprintf("port-%d", e.LocalPortNum)
		}
		out = append(out, LldpNeighbor{
			Port:               port,
			ChassisMac:         e.ChassisMac,
			Chassis:            e.Chassis,
			Mgmt:               e.Mgmt,
			Caps:               e.Caps,
			PortDesc:           e.PortDesc,
			ChassisFromLocDesc: e.ChassisFromLocDesc,
		})
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
