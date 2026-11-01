package adapters

import (
	"fmt"
	"log"
	"strings"
	"time"

	npsnmp "github.com/gnacho/netpulse/server-go/internal/snmp"
)

// snmpSurfaceEntry (#1726): lo que SNMP sabe de una unidad y el payload del
// pusher no. Se cachea por router para no sondear SNMP en cada push.
type snmpSurfaceEntry struct {
	at      time.Time
	byIface map[string]EthPort
	lldp    []LldpNeighbor
	ok      bool
}

// mergeSNMPSurface completa con SNMP un sondeo que ya llegó por pusher
// (#1726). Una unidad puede tener snmp_enabled y a la vez estar alimentada
// por un agente: hasta ahora el poller devolvía en cuanto había payload
// fresco y la rama SNMP no se alcanzaba nunca, así que el ajuste era un
// no-op silencioso. Aquí SNMP es segunda fuente: solo rellena lo que el
// payload no trae (velocidad de boca, contadores de error y vecinos LLDP).
// No toca vitals (CPU/RAM/temperatura), que manda el payload desde #1724, ni
// el FDB, que sí trae el agente.
func (l *Live) mergeSNMPSurface(cfg RouterConfig, p *routerPolled) {
	if !cfg.SnmpEnabled || p == nil {
		return
	}
	interval := cfg.SnmpPollInterval
	if interval <= 0 {
		interval = 60
	}
	now := l.now()
	l.mu.Lock()
	entry, has := l.snmpSurface[cfg.ID]
	l.mu.Unlock()
	if !has || now.Sub(entry.at) >= time.Duration(interval)*time.Second {
		entry = l.pollSNMPSurface(cfg, now)
		l.mu.Lock()
		if l.snmpSurface == nil {
			l.snmpSurface = map[string]snmpSurfaceEntry{}
		}
		l.snmpSurface[cfg.ID] = entry
		l.mu.Unlock()
	}
	if !entry.ok {
		return
	}
	applySNMPSurface(p, entry)
}

// pollSNMPSurface sondea solo la superficie que el pusher no puede dar: la
// tabla de interfaces (velocidad de enlace y contadores de error) y los
// vecinos LLDP. Deliberadamente no llama a recordPortSamples ni a
// portMon.Observe: las muestras del pusher ya entran por otro camino con sus
// propios IDs, y duplicarlas aquí intercalaría filas con ids "snmp-N".
func (l *Live) pollSNMPSurface(cfg RouterConfig, now time.Time) snmpSurfaceEntry {
	entry := snmpSurfaceEntry{at: now}
	session, err := npsnmp.NewSession(npsnmp.Config{
		Host:      cfg.Host,
		Port:      cfg.SnmpPort,
		Community: cfg.SnmpCommunity,
	})
	if err != nil {
		l.recordSnmpFailure(cfg, err)
		return entry
	}
	defer npsnmp.CloseSession(session)

	ports, ifErr := npsnmp.PollIfTable(session)
	if ifErr != nil || len(ports) == 0 {
		pollErr := ifErr
		if pollErr == nil {
			pollErr = fmt.Errorf("snmp ifTable %s: sin puertos", cfg.Host)
		}
		l.recordSnmpFailure(cfg, pollErr)
		return entry
	}
	// Misma salud SNMP que el poll completo: si esta unidad deja de responder
	// por SNMP, la alerta de #930 sigue viéndose aunque el pusher vaya bien.
	l.recordSnmpSuccess(cfg)

	byIface := make(map[string]EthPort, len(ports))
	portIdxToName := make(map[int]string, len(ports))
	for _, ps := range ports {
		name := ps.DisplayName()
		portIdxToName[ps.Index] = name
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "" {
			continue
		}
		byIface[key] = EthPort{
			ID:     fmt.Sprintf("snmp-%d", ps.Index),
			Label:  name,
			Up:     ps.OperUp,
			Speed:  ps.SpeedString(),
			Iface:  name,
			RxErrs: ps.RxErrors,
			TxErrs: ps.TxErrors,
			Snmp:   true,
		}
	}

	var lldp []LldpNeighbor
	if rem, lldpErr := npsnmp.PollLldpRemTable(session); lldpErr != nil {
		log.Printf("[netpulse] SNMP LLDP %s: %v", cfg.LogLabel(), lldpErr)
	} else {
		lldp = snmpLldpNeighbors(rem, portIdxToName)
	}

	entry.byIface = byIface
	entry.lldp = lldp
	entry.ok = true
	return entry
}

// applySNMPSurface rellena en el sondeo del pusher lo que SNMP sí trae. Las
// bocas se casan por ifName/ifAlias (el payload trae el mismo nombre que la
// tabla de interfaces del equipo). Un dato del payload nunca se pisa: solo se
// rellena cuando viene vacío o a cero.
func applySNMPSurface(p *routerPolled, entry snmpSurfaceEntry) {
	if p == nil || len(entry.byIface) == 0 {
		return
	}
	for i := range p.ports {
		port := &p.ports[i]
		key := strings.ToLower(strings.TrimSpace(port.Iface))
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(port.Label))
		}
		ref, ok := entry.byIface[key]
		if !ok {
			continue
		}
		// ifHighSpeed=0 significa "sin enlace" o "no reportado", no una
		// velocidad: solo se propaga una cifra positiva y solo si el payload
		// no dio ninguna.
		if parseSpeedMbps(port.Speed) == 0 && parseSpeedMbps(ref.Speed) > 0 {
			port.Speed = ref.Speed
		}
		// El contrato del payload no distingue "no reporto errores" de "cero
		// errores", así que un 0 se trata como desconocido y SNMP lo completa.
		if port.RxErrs == 0 && ref.RxErrs > 0 {
			port.RxErrs = ref.RxErrs
		}
		if port.TxErrs == 0 && ref.TxErrs > 0 {
			port.TxErrs = ref.TxErrs
		}
	}
	// RouterOS por API no expone LLDP (#1726), así que en estas unidades la
	// topología cae a las MACs del FDB. Los vecinos de LLDP-MIB son mejores
	// que esa conjetura.
	if len(p.lldp) == 0 && len(entry.lldp) > 0 {
		p.lldp = entry.lldp
		p.lldpUnavailable = false
		p.lldpHint = ""
	}
}
