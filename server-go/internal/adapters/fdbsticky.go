// fdbsticky.go — memoria pegajosa del puerto FDB (issue #656).
//
// Problema: las entradas del bridge FDB caducan (~5 min) y los dispositivos
// callados (AV: TV, receptor, shield…) pierden su evidencia de puerto entre
// ticks aunque sigan online (ARP). El mapa los re-anclaba al gateway y
// "saltaban" del switch inferido donde están cableados físicamente.
//
// Solución: recordar la última boca REAL donde se vio cada MAC (fdbMemo, con
// TTL) y usar ese recuerdo como OVERLAY de inferTopology, SOLO para MACs que
// el FDB actual ya no tiene. La memo nunca crea presencia: si el dispositivo
// no está online por sí mismo (wireless/FDB/ARP), no se dibuja. El FDB real
// siempre manda sobre el recuerdo.
package adapters

import "time"

// fdbStickyTTL: frescura de la memo. Los dispositivos AV despiertan y hablan
// (refrescando la memo) mucho antes; 30 min es un margen holgado que además
// acota el efecto de un dispositivo cambiado de boca.
const fdbStickyTTL = 30 * time.Minute

// fdbPortMemo: última boca real donde el FDB vio una MAC.
type fdbPortMemo struct {
	routerID string
	port     string
	ts       int64 // epoch ms
}

// uplinkPortsLocked devuelve los puertos de un router que son uplinks: la unión
// de los persistidos (que ALGUNA VEZ aprendieron una brMac de otro router) y los
// detectados en el FDB de este tick. Los detectados se graban en el set
// persistente para sobrevivir a ticks sin la brMac y a sondeos fallidos (#694).
// Asume que l.mu está tomado.
func (l *Live) uplinkPortsLocked(routerID string, fdb map[string]string, brMacs map[string]bool) map[string]bool {
	if l.uplinkPorts == nil {
		l.uplinkPorts = map[string]map[string]bool{}
	}
	memo := l.uplinkPorts[routerID]
	if memo == nil {
		memo = map[string]bool{}
		l.uplinkPorts[routerID] = memo
	}
	ports := make(map[string]bool, len(memo)+2)
	for p := range memo {
		ports[p] = true
	}
	for mac, port := range fdb {
		if brMacs[mac] {
			ports[port] = true
			memo[port] = true
		}
	}
	return ports
}

// uplinkPortsFor es la variante de uplinkPortsLocked para callers sin l.mu.
func (l *Live) uplinkPortsFor(routerID string, fdb map[string]string, brMacs map[string]bool) map[string]bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.uplinkPortsLocked(routerID, fdb, brMacs)
}

// updateFdbMemo refresca la memo con el FDB real de este tick y poda las
// entradas caducadas. Devuelve el snapshot de la memo (para el overlay).
//
// Higiene (#678): solo se memorizan observaciones en bocas LOCALES (las que no
// aprenden la MAC de bridge de otro router = no son uplinks). Así la memo
// significa "dónde está enchufado el cable" y no se contamina con el tránsito
// que ve el gateway por su uplink (que si no pisaba la observación local del AP
// y hacía que un cliente del AP pareciera del router principal).
//
// #694: la clasificación de uplink usa el set PERSISTENTE (uplinkPortsLocked),
// no solo las brMacs presentes en este tick. Así el puerto gateway→AP sigue
// siendo uplink aunque la brMac del AP no esté en el FDB del gateway ese tick,
// y el gateway deja de pisar la observación local del satélite (el ganador deja
// de depender del orden aleatorio de iteración del mapa).
func (l *Live) updateFdbMemo(polled map[string]*routerPolled, nowMs int64) map[string]fdbPortMemo {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fdbMemo == nil {
		l.fdbMemo = map[string]fdbPortMemo{}
	}
	brMacs := map[string]bool{}
	online := map[string]bool{}
	for _, p := range polled {
		if p.brMac != "" {
			brMacs[p.brMac] = true
		}
		for mac := range p.arp {
			online[mac] = true
		}
		for _, le := range p.leases {
			if le.MAC != "" {
				online[le.MAC] = true
			}
		}
	}
	for routerID, p := range polled {
		infraPorts := l.uplinkPortsLocked(routerID, p.fdb, brMacs)
		for mac, port := range p.fdb {
			if infraPorts[port] {
				continue // tránsito por el uplink: no es dónde va el cable
			}
			l.fdbMemo[mac] = fdbPortMemo{routerID: routerID, port: port, ts: nowMs}
		}
	}
	ttlMs := fdbStickyTTL.Milliseconds()
	for mac, m := range l.fdbMemo {
		if nowMs-m.ts > ttlMs {
			if online[mac] {
				// #694: el device sigue online (ARP/lease) aunque no hable por
				// FDB: refresca la memo para que un cliente callado conserve su
				// boca en vez de caducar y caer al fallback del gateway.
				m.ts = nowMs
				l.fdbMemo[mac] = m
				continue
			}
			delete(l.fdbMemo, mac)
		}
	}
	return l.fdbMemo
}

// overlayStickyFdb devuelve una copia de `polled` donde cada router lleva, ADEMÁS
// de su FDB real, las MACs recordadas (memo fresca) que ya no estén presentes.
// Función pura: no toca polled ni memo. Solo la usa inferTopology.
func overlayStickyFdb(polled map[string]*routerPolled, memo map[string]fdbPortMemo, nowMs int64) map[string]*routerPolled {
	out := make(map[string]*routerPolled, len(polled))
	for id, p := range polled {
		np := *p // shallow copy: solo se sustituye fdb (map propio)
		nf := make(map[string]string, len(p.fdb)+4)
		for k, v := range p.fdb {
			nf[k] = v
		}
		np.fdb = nf
		out[id] = &np
	}
	ttlMs := fdbStickyTTL.Milliseconds()
	for mac, m := range memo {
		if nowMs-m.ts > ttlMs {
			continue
		}
		p, ok := out[m.routerID]
		if !ok {
			continue // el router ya no está en la flota
		}
		if _, exists := p.fdb[mac]; exists {
			continue // el FDB real manda
		}
		p.fdb[mac] = m.port
	}
	return out
}
