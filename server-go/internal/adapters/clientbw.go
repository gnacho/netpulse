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
//
// Solapamiento futuro (nota, no bloquea): cuando el pusher del gateway (NetGrip)
// emita clientBw con nlbwmon, el gateway reportará TODAS las MACs (cable + wifi
// que enruta) y los APs seguirán reportando hostapd de sus clientes wifi → la
// misma MAC tendría filas de dos fuentes y GetMACSeries sumaría el doble. La
// flota actual NO solapa (APs sin nlbwmon, gateway sin clientBw aún): rt2/rt4
// reportan hostapd y el gateway nada. Antes de activar nlbwmon en el gateway
// hay que decidir la reconciliación (p. ej. marcar source en la fila y que la
// agregación por device filtre por la fuente del router que enruta).
package adapters

import (
	"strings"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/clientbw"
)

// clientBwCounter es el último contador absoluto observado de un cliente en
// un router (para el delta entre polls) + el rate medio del último intervalo
// (bps) para el TrafficMbps de la lista sin consultar la store.
type clientBwCounter struct {
	at     time.Time
	rx     uint64
	tx     uint64
	source string // "nlbwmon" | "hostapd" (logging)
	// rxBps/txBps: rate medio del ÚLTIMO intervalo persistido (0 si aún no
	// hay delta: primera observación o reinicio del contador).
	rxBps float64
	txBps float64
}

// clientBwSample es el par (MAC, contadores absolutos) que reporta una fuente
// en un poll. MAC sin normalizar (viene de los parsers en mayúsculas).
type clientBwSample struct {
	mac string
	rx  uint64
	tx  uint64
}

// canonBWMac normaliza la MAC al formato canónico del server para las rutas
// /api/devices/{mac}/* (minúsculas con ':'). Las fuentes (hostapd/nlbwmon)
// entregan mayúsculas; la store client_bw_raw debe casar con normalizeMAC.
func canonBWMac(mac string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(mac) {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			b.WriteRune(r)
		}
	}
	s := b.String()
	var out strings.Builder
	for i := 0; i+1 < len(s); i += 2 {
		if i > 0 {
			out.WriteByte(':')
		}
		out.WriteString(s[i : i+2])
	}
	return out.String()
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
		mac := canonBWMac(s.mac)
		if mac == "" {
			continue
		}
		prev := last[mac]
		if prev == nil {
			// Primera observación: solo se siembra el contador; no hay
			// delta que persistir todavía.
			last[mac] = &clientBwCounter{at: now, rx: s.rx, tx: s.tx, source: source}
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
			prev.rxBps = 0
			prev.txBps = 0
			continue
		}
		rxDelta := s.rx - prev.rx
		txDelta := s.tx - prev.tx
		prev.rxBps = float64(rxDelta) * 8 / dt
		prev.txBps = float64(txDelta) * 8 / dt
		_ = l.db.ClientBW.Insert(clientbw.Sample{
			MAC: mac, RouterID: routerID, TS: now,
			RxBytes: rxDelta, TxBytes: txDelta,
			RxBps: prev.rxBps, TxBps: prev.txBps,
		})
		prev.at = now
		prev.rx = s.rx
		prev.tx = s.tx
		prev.source = source
	}
}

// clientBwRateFor devuelve (rxBps, txBps) del último intervalo conocido del
// cliente en cualquier router (para el TrafficMbps del device en la lista).
// Protegido por mu. Sin datos → 0,0. Prioriza nlbwmon sobre hostapd cuando
// una MAC es reportada por varias fuentes (el nlbwmon del router que enruta
// ya contabiliza todo el tráfico de la MAC; sumar hostapd doblaría).
func (l *Live) clientBwRateFor(mac string) (rxBps, txBps float64) {
	mac = canonBWMac(mac)
	l.mu.Lock()
	defer l.mu.Unlock()
	if c := l.findClientBwRate(mac, "nlbwmon"); c != nil {
		return c.rxBps, c.txBps
	}
	if c := l.findClientBwRate(mac, "hostapd"); c != nil {
		return c.rxBps, c.txBps
	}
	return 0, 0
}

func (l *Live) findClientBwRate(mac, source string) *clientBwCounter {
	for _, byMac := range l.lastClientBw {
		if c := byMac[mac]; c != nil && c.source == source {
			return c
		}
	}
	return nil
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
