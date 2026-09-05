// clientbw.go — ingesta del tráfico por cliente (#551). El server recibe de
// cada router contadores ACUMULADOS por MAC (fuente nlbwmon o hostapd bytes
// por estación del agente) y los convierte a samples delta por (router, mac)
// en la store client_bw_raw, con el rate medio del intervalo (bps).
//
// Diseño (decisión del usuario, 5-Sep-2026): por router, la fuente PREFERENTE
// es nlbwmon cuando está disponible y responde (cubre cableados y todo lo que
// enruta el router); si no, se usan los contadores hostapd por estación
// (clientes wifi asociados a ese AP). La demo no alimenta la store: sus datos
// sintéticos de tráfico por device siguen intactos.
package adapters

import (
	"time"

	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/clientbw"
)

// clientBwCounter es el último contador absoluto observado de un cliente en
// un router (para el delta entre polls).
type clientBwCounter struct {
	at     time.Time
	rx     uint64
	tx     uint64
	source string // "nlbwmon" | "hostapd" (logging)
}

// clientBwSample es el par (MAC, contadores absolutos) que reporta una fuente
// en un poll. MAC normalizada a mayúsculas.
type clientBwSample struct {
	mac string
	rx  uint64
	tx  uint64
}

// recordClientBwSamples persiste el delta de cada cliente desde el último
// contador absoluto conocido. Debe llamarse UNA vez por poll con datos
// frescos (no en rebuilds del snapshot con el mismo payload cacheado; patrón
// issue #365 de portseries). now es el instante del sondeo.
func (l *Live) recordClientBwSamples(routerID string, now time.Time, samples []clientBwSample, source string) {
	if l.db == nil || l.db.ClientBW == nil || len(samples) == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lastClientBw == nil {
		l.lastClientBw = map[string]map[string]*clientBwCounter{}
	}
	last := l.lastClientBw[routerID]
	if last == nil {
		last = map[string]*clientBwCounter{}
		l.lastClientBw[routerID] = last
	}
	for _, s := range samples {
		prev := last[s.mac]
		if prev == nil {
			// Primera observación: solo se siembra el contador; no hay
			// delta que persistir todavía.
			last[s.mac] = &clientBwCounter{at: now, rx: s.rx, tx: s.tx, source: source}
			continue
		}
		dt := now.Sub(prev.at).Seconds()
		// Delta: si el contador reinició (nlbwmon nuevo periodo, cliente
		// que se reasoció y hostapd reinició bytes), no fabricar un delta
		// negativo gigante: se re-siembra y se espera al siguiente poll.
		if dt <= 0 || s.rx < prev.rx || s.tx < prev.tx {
			prev.at = now
			prev.rx = s.rx
			prev.tx = s.tx
			prev.source = source
			continue
		}
		rxDelta := s.rx - prev.rx
		txDelta := s.tx - prev.tx
		_ = l.db.ClientBW.Insert(clientbw.Sample{
			MAC: s.mac, RouterID: routerID, TS: now,
			RxBytes: rxDelta, TxBytes: txDelta,
			RxBps: float64(rxDelta) * 8 / dt,
			TxBps: float64(txDelta) * 8 / dt,
		})
		prev.at = now
		prev.rx = s.rx
		prev.tx = s.tx
		prev.source = source
	}
}

// resolveClientBwSources resuelve los contadores por MAC de un sondeo con la
// fuente preferente por router: nlbwmon si el poll trae ClientBw con
// Available=true y hosts (cubre cableados + todo lo que enruta el router); si
// no, hostapd bytes por estación (clientes wifi asociados). Devuelve las
// muestras y la fuente usada; ok=false si no hay datos de tráfico por cliente.
func resolveClientBwSources(wireless map[string]WirelessClient, clientBw *probe.ClientBwData) (samples []clientBwSample, source string, ok bool) {
	// Fuente 1: nlbwmon (solo routers que enrutan; ver cabecera).
	if clientBw != nil && clientBw.Available && len(clientBw.Hosts) > 0 {
		out := make([]clientBwSample, 0, len(clientBw.Hosts))
		for mac, h := range clientBw.Hosts {
			out = append(out, clientBwSample{mac: mac, rx: h.RxBytes, tx: h.TxBytes})
		}
		return out, "nlbwmon", true
	}
	// Fuente 2: hostapd bytes por estación (clientes asociados a este AP).
	var out []clientBwSample
	for mac, c := range wireless {
		if c.RxBytes == 0 && c.TxBytes == 0 {
			continue
		}
		out = append(out, clientBwSample{mac: mac, rx: c.RxBytes, tx: c.TxBytes})
	}
	if len(out) > 0 {
		return out, "hostapd", true
	}
	return nil, "", false
}
