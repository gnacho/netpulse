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

// updateFdbMemo refresca la memo con el FDB real de este tick y poda las
// entradas caducadas. Devuelve el snapshot de la memo (para el overlay).
//
// Higiene (#678): solo se memorizan observaciones en bocas LOCALES (las que no
// aprenden la MAC de bridge de otro router = no son uplinks). Así la memo
// significa "dónde está enchufado el cable" y no se contamina con el tránsito
// que ve el gateway por su uplink (que si no pisaba la observación local del AP
// y hacía que un cliente del AP pareciera del router principal).
func (l *Live) updateFdbMemo(polled map[string]*routerPolled, nowMs int64) map[string]fdbPortMemo {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.fdbMemo == nil {
		l.fdbMemo = map[string]fdbPortMemo{}
	}
	brMacs := map[string]bool{}
	for _, p := range polled {
		if p.brMac != "" {
			brMacs[p.brMac] = true
		}
	}
	for routerID, p := range polled {
		infraPorts := map[string]bool{}
		for mac, port := range p.fdb {
			if brMacs[mac] {
				infraPorts[port] = true
			}
		}
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
