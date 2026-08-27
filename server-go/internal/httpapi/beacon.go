// beacon.go — listener UDP de beacons de dispositivos embebidos (#291).
//
// Los pushers embebidos sin Linux (RTLPlayground en el KP-9000) emiten un
// datagrama UDP cada 30 s con sus stats: payload NEUTRO (spec v1 documentada
// en el repo del firmware), sin depender de NetPulse. Este listener valida
// el token del agente (mismo kv sha256 que el ingest HTTP), estampa el ts de
// llegada (el emisor no tiene RTC) y alimenta el MISMO registry: el agente
// queda kind=external, interval=30 y fresh con cada beacon.
//
// La tabla MAC/FDB NO viaja en el beacon (crece con la red): se omite en el
// payload y el merge anti-parpadeo de polledFromAgent conserva la última
// buena del scraper. Reparto: beacon = estado/counters a 30 s; scraper =
// FDB a 5 min.
//
// Seguridad (decisión consciente v1): token en claro y sin firma (modelo LAN
// de confianza; ver #291). Mitigaciones: rate limit por IP origen (mismo
// modelo que el ingest HTTP) y log de seq que retrocede (replay/reorden).
package httpapi

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

// beaconPort: una boca del beacon. n = número 1..9; l = código de link
// (0 down · 1 10M · 2 100M · 3 1G · 4 500M · 5 10G · 6 2.5G · 7 5G);
// tx/rx = contadores acumulados de tramas good (uint32, decimal).
type beaconPort struct {
	N  int    `json:"n"`
	L  int    `json:"l"`
	Tx uint32 `json:"tx"`
	Rx uint32 `json:"rx"`
}

// beaconPacket: spec v1 del datagrama (una línea JSON ASCII, ~400 B).
// Dev/Fw son opcionales: los lleva el ANUNCIO de descubrimiento (broadcast,
// sin token) para que el switch pueda encontrarse antes del pareado.
// Ev (con Port/Mac) convierte el datagrama en EVENTO inmediato (spec v1.1:
// loop, port_up, port_down, port_disabled, port_recovered).
type beaconPacket struct {
	V     int          `json:"v"` // versión de esquema; solo se acepta 1
	Seq   uint32       `json:"seq"`
	Slug  string       `json:"slug"`
	Token string       `json:"token"`
	Dev   string       `json:"dev,omitempty"`
	Fw    string       `json:"fw,omitempty"`
	Ev    string       `json:"ev,omitempty"`
	Port  int          `json:"port,omitempty"`
	Mac   string       `json:"mac,omitempty"`
	Ports []beaconPort `json:"ports"`
	// FDB (v1.2): datagrama dedicado de tabla MAC cada 5 min. nil = no va
	// en este datagrama (el beacon periódico no la lleva); {} = sin entradas
	// (estado real). Valores = puerto ("3", sin prefijo lan: la
	// normalización de polledFromAgent los alinea con las bocas).
	FDB map[string]string `json:"fdb,omitempty"`
}

// beaconCandidate: un switch embebido anunciándose por broadcast SIN parar
// (token vacío). Se expone al admin para completar el pareado (#291).
type beaconCandidate struct {
	IP       string `json:"ip"`
	Dev      string `json:"dev"`
	Fw       string `json:"fw,omitempty"`
	Ports    int    `json:"ports,omitempty"`
	LastSeen int64  `json:"lastSeen"` // unix segundos
}

// beaconCandidateTTL: un anuncio sin refresco caduca a los 10 min.
const beaconCandidateTTL = 10 * time.Minute

// beaconLinkSpeed traduce el código de link a la etiqueta de velocidad del
// resto de la app (0 = down).
var beaconLinkSpeed = map[int]string{
	0: "",
	1: "10M",
	2: "100M",
	3: "1G",
	4: "500M",
	5: "10G",
	6: "2.5G",
	7: "5G",
}

const (
	beaconVersion     = "beacon-1.0"
	beaconIntervalSec = 30
	// UIP_BUFSIZE del emisor es 2000: el datagrama no puede pasar de ahí
	// (y de todos modos el MTU lo limita a ~1500).
	beaconMaxDatagram = 2048
)

// ingestBeacon valida y aplica un datagrama recibido. src es la IP origen
// (sin puerto) para el rate limit y los logs. Fallos se ignoran con log: el
// beacon es best-effort, el emisor reintenta en la próxima cadencia.
func (s *server) ingestBeacon(src string, raw []byte) {
	if len(raw) == 0 || len(raw) > beaconMaxDatagram {
		return
	}
	var p beaconPacket
	if err := json.Unmarshal(raw, &p); err != nil {
		log.Printf("[netpulse:beacon] datagrama malformado desde %s (%d B)", src, len(raw))
		return
	}
	if p.V != 1 {
		log.Printf("[netpulse:beacon] esquema v%d no soportado desde %s", p.V, src)
		return
	}
	// ANUNCIO de descubrimiento (#291): broadcast sin token pero con
	// identidad de firmware (dev/fw). No es un agente: queda como candidato
	// hasta que el admin lo parea (POST /api/agents + beacon <ip> <token>).
	if p.Token == "" && p.Dev != "" {
		if ok, _ := s.ingestLimit.allow(src); ok {
			s.recordBeaconCandidate(src, p)
		}
		return
	}
	if !agentSlugRe.MatchString(p.Slug) {
		return
	}
	// Rate limit por IP origen, mismo modelo que el ingest HTTP.
	if ok, _ := s.ingestLimit.allow(src); !ok {
		return
	}
	if !s.checkAgentToken(p.Slug, p.Token) {
		log.Printf("[netpulse:beacon] token inválido para %s (origen %s)", p.Slug, src)
		return
	}
	// Beacon validado: si esa IP tenía un candidato pendiente, ya está parado.
	s.dropBeaconCandidate(src)
	if s.cfg != nil && s.cfg.DemoMode {
		return
	}
	if s.agents == nil {
		return
	}

	// EVENTO inmediato (spec v1.1): no lleva estado, solo la alerta. No pasa
	// por el payload (un evento sin ports borraría las bocas del registry).
	if p.Ev != "" {
		s.handleBeaconEvent(p)
		s.beaconSeqNote(p.Slug, p.Seq)
		return
	}

	// Datagrama FDB (v1.2, cada 5 min): solo tabla MAC. Las bocas viajan por
	// el beacon periódico; aquí se conservan las últimas conocidas para no
	// borrarlas, y la tabla MAC se sustituye entera ({} = sin entradas).
	if p.FDB != nil && len(p.Ports) == 0 {
		ports := []probe.EthPort{}
		if prev, okPrev := s.agents.StalePayload(p.Slug); okPrev && prev != nil && prev.Data.FDB != nil {
			ports = prev.Data.FDB.Ports
		}
		// MACs sin separadores ("AABBCCDDEEFF") → formato canónico con dos
		// puntos; las irrecuperables se descartan (firmware razonable).
		macs := make(map[string]string, len(p.FDB))
		for mac, port := range p.FDB {
			if norm := normalizeBeaconMAC(mac); norm != "" {
				macs[norm] = port
			}
		}
		fdbPayload := &probe.Payload{
			Router: p.Slug, Ts: time.Now().Unix(), Version: beaconVersion,
			Kind: "external", Interval: beaconIntervalSec,
			Data: probe.PayloadData{FDB: &probe.FDBData{MACs: macs, Ports: ports}},
		}
		s.agents.Ingest(fdbPayload)
		s.persistAgentState(p.Slug, s.agents.Snapshot(p.Slug))
		s.beaconSeqNote(p.Slug, p.Seq)
		return
	}

	// Puertos → fdb.ports. El beacon viaja sin labels (ahorro de bytes en
	// el 8051): se reinyectan los ÚLTIMOS labels conocidos del scraper para
	// que los nombres (v2.1) no se pierdan entre push y push (#291).
	labels := s.lastPortLabels(p.Slug)
	ports := make([]probe.EthPort, 0, len(p.Ports))
	for _, bp := range p.Ports {
		if bp.N < 1 || bp.N > 32 {
			continue
		}
		id := fmt.Sprintf("lan%d", bp.N)
		ep := probe.EthPort{ID: id, Label: fmt.Sprintf("Port %d", bp.N)}
		if l, ok := labels[id]; ok && l != "" {
			ep.Label = l
		}
		if sp, ok := beaconLinkSpeed[bp.L]; ok && bp.L != 0 {
			ep.Up = true
			ep.Speed = sp
		}
		ports = append(ports, ep)
	}
	// El beacon no sondea MACs: viaja con la ÚLTIMA tabla conocida (del
	// datagrama FDB) en vez de nil. Así el estado persistido sobrevive a los
	// reinicios del server (restore con MACs) y no hay ventana de 5 min sin
	// atribución tras cada arranque. Un FDB real vacío sigue llegando como {}
	// (estado) y nunca como nil (ausencia).
	var macs map[string]string
	if prev, okPrev := s.agents.StalePayload(p.Slug); okPrev && prev != nil && prev.Data.FDB != nil {
		macs = prev.Data.FDB.MACs
	}
	pl := &probe.Payload{
		Router:   p.Slug,
		Ts:       time.Now().Unix(),
		Version:  beaconVersion,
		Kind:     "external",
		Interval: beaconIntervalSec,
		Data:     probe.PayloadData{FDB: &probe.FDBData{MACs: macs, Ports: ports}},
	}
	// Fallback de cambios de link por delta entre beacons (#291): si el
	// firmware no manda eventos, el cambio se detecta comparando el payload
	// anterior. Con eventos, el título idéntico hace que el dedup del engine
	// colapse el duplicado. Solo con datagramas que traen bocas (el FDB
	// dedicado repite las anteriores: no hay cambio que evaluar).
	if len(p.Ports) > 0 {
		if prev, okPrev := s.agents.StalePayload(p.Slug); okPrev && prev != nil && prev.Data.FDB != nil {
			was := map[string]bool{}
			for _, ep := range prev.Data.FDB.Ports {
				was[ep.ID] = ep.Up
			}
			for _, np := range ports {
				if before, ok := was[np.ID]; ok && before != np.Up {
					s.emitPortLinkChange(p.Slug, np.Label, np.Up)
				}
			}
		}
	}
	s.agents.Ingest(pl)
	s.persistAgentState(p.Slug, s.agents.Snapshot(p.Slug))
	s.beaconSeqNote(p.Slug, p.Seq)
}

// handleBeaconEvent traduce un evento del firmware a alertas del feed (#291).
func (s *server) handleBeaconEvent(p beaconPacket) {
	eng := s.alertsEngine()
	if eng == nil {
		return
	}
	labels := s.lastPortLabels(p.Slug)
	portName := fmt.Sprintf("boca %d", p.Port)
	if l := labels[fmt.Sprintf("lan%d", p.Port)]; l != "" {
		portName = fmt.Sprintf("%s (boca %d)", l, p.Port)
	}
	id := fmt.Sprintf("beacon-%s-%s-%d-%d", p.Ev, p.Slug, p.Port, time.Now().UnixMilli())
	ev := alerts.AlertEvent{
		ID: id, Category: alerts.CatSystem, Urgent: false,
		Severity: "warn", Time: "ahora mismo", RouterID: p.Slug,
	}
	switch p.Ev {
	case "loop":
		ev.Urgent = true
		ev.Title = "Bucle detectado en " + portName
		ev.Description = fmt.Sprintf(
			"La guardia del switch ha detectado la MAC %s en dos bocas y ha deshabilitado %s",
			p.Mac, portName)
	case "port_disabled":
		ev.Title = "Boca deshabilitada: " + portName
		ev.Description = "Desactivada por la guardia de bucles; se reintentará en 5 min (máx. 3 veces)"
	case "port_recovered":
		ev.Severity = "ok"
		ev.Title = "Boca re-habilitada: " + portName
		ev.Description = "La guardia de bucles ha vuelto a habilitar la boca"
	case "port_down":
		ev.Title = "Link caído en " + portName
		ev.Description = "El beacon del switch reporta pérdida de link"
	case "port_up":
		ev.Severity = "ok"
		ev.Title = "Link restablecido en " + portName
		ev.Description = "El beacon del switch reporta link activo"
	default:
		log.Printf("[netpulse:beacon] evento desconocido %q de %s", p.Ev, p.Slug)
		return
	}
	eng.Emit(ev)
}

// emitPortLinkChange: alerta de cambio de link detectada por delta (#291).
// Mismos títulos que los eventos para que el dedup colapse duplicados.
func (s *server) emitPortLinkChange(slug, label string, up bool) {
	eng := s.alertsEngine()
	if eng == nil {
		return
	}
	name := label
	if name == "" {
		name = "una boca"
	}
	var ev alerts.AlertEvent
	if up {
		ev = alerts.AlertEvent{
			ID: fmt.Sprintf("beacon-port-up-%s-%d", slug, time.Now().UnixMilli()),
			Category: alerts.CatSystem, Severity: "ok", Time: "ahora mismo",
			RouterID: slug, Title: "Link restablecido en " + name,
			Description: "Detectado por el cambio entre beacons",
		}
	} else {
		ev = alerts.AlertEvent{
			ID: fmt.Sprintf("beacon-port-down-%s-%d", slug, time.Now().UnixMilli()),
			Category: alerts.CatSystem, Severity: "warn", Time: "ahora mismo",
			RouterID: slug, Title: "Link caído en " + name,
			Description: "Detectado por el cambio entre beacons",
		}
	}
	eng.Emit(ev)
}

// alertsEngine devuelve el motor de alertas del adapter (nil si no hay).
func (s *server) alertsEngine() *alerts.Engine {
	if s.adapter == nil {
		return nil
	}
	return s.adapter.AlertsEngine()
}

// lastPortLabels extrae {id: label} del último payload conocido del slug
// (el del scraper, que viaja con nombres desde v2.1). Usado para enriquecer
// los puertos del beacon, que viajan sin labels.
func (s *server) lastPortLabels(slug string) map[string]string {
	out := map[string]string{}
	if s.agents == nil {
		return out
	}
	st, ok := s.agents.StalePayload(slug)
	if !ok || st == nil || st.Data.FDB == nil {
		return out
	}
	for _, p := range st.Data.FDB.Ports {
		if p.Label != "" {
			out[p.ID] = p.Label
		}
	}
	return out
}

// beaconSeqNote registra el seq por slug: un salto distinto de +1 indica
// pérdida, reorden o replay. Acepta siempre (el beacon es idempotente); el
// salto queda logueado para diagnóstico.
func (s *server) beaconSeqNote(slug string, seq uint32) {
	s.beaconSeqMu.Lock()
	old, had := s.beaconSeq[slug]
	if s.beaconSeq == nil {
		s.beaconSeq = map[string]uint32{}
	}
	if had && seq != old+1 {
		log.Printf("[netpulse:beacon] %s: seq %d tras %d (pérdida, reorden o replay)", slug, seq, old)
	}
	s.beaconSeq[slug] = seq
	s.beaconSeqMu.Unlock()

	// Firma de reboot (regalo del firmware): el seq arranca en 1 tras el
	// boot, así que un salto a 1 (o 0) desde uno mayor = reinicio del
	// switch. El wrap de uint32 daría el mismo salto una vez cada 136 años.
	if had && seq <= 1 && old > 1 {
		if eng := s.alertsEngine(); eng != nil {
			eng.Emit(alerts.AlertEvent{
				ID:         fmt.Sprintf("beacon-reboot-%s-%d", slug, time.Now().UnixMilli()),
				Category:   alerts.CatSystem, Severity: "info", Time: "ahora mismo",
				RouterID:   slug, Title: "Switch reiniciado",
				Description: fmt.Sprintf("El contador de beacons de %s volvió a empezar (seq %d tras %d): el switch ha arrancado de nuevo", slug, seq, old),
			})
		}
	}
}

// startBeaconListener abre el socket UDP y sirve beacons hasta que el
// proceso termine (o se cierre el conn). Devuelve la dirección bindeada
// (útil con :0 en tests).
func (s *server) startBeaconListener(addr string) (net.Addr, error) {
	if s.agents == nil {
		return nil, fmt.Errorf("sin registry de agentes")
	}
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		return nil, err
	}
	s.beaconConn = pc
	go func() {
		buf := make([]byte, beaconMaxDatagram)
		for {
			n, src, err := pc.ReadFrom(buf)
			if err != nil {
				return // socket cerrado
			}
			if src == nil {
				continue
			}
			host, _, splitErr := net.SplitHostPort(src.String())
			if splitErr != nil {
				host = src.String()
			}
			s.ingestBeacon(host, buf[:n])
		}
	}()
	return pc.LocalAddr(), nil
}

// normalizeBeaconMAC: el firmware envía las MAC en MAYÚSCULAS sin
// separadores ("AABBCCDDEEFF"); el resto de la app (leases, wifi, FDB,
// enriquecimiento) usa "AA:BB:CC:DD:EE:FF". Devuelve "" si no es una MAC.
func normalizeBeaconMAC(mac string) string {
	clean := strings.ToUpper(strings.TrimSpace(mac))
	if len(clean) == 12 && !strings.Contains(clean, ":") {
		var b strings.Builder
		for i := 0; i < 12; i += 2 {
			if i > 0 {
				b.WriteByte(':')
			}
			b.WriteString(clean[i : i+2])
		}
		return b.String()
	}
	if len(clean) == 17 && strings.Count(clean, ":") == 5 {
		return clean
	}
	return ""
}

// recordBeaconCandidate guarda/refresca un anuncio de descubrimiento (#291).
func (s *server) recordBeaconCandidate(src string, p beaconPacket) {
	s.beaconCandMu.Lock()
	defer s.beaconCandMu.Unlock()
	if s.beaconCand == nil {
		s.beaconCand = map[string]beaconCandidate{}
	}
	s.beaconCand[src] = beaconCandidate{
		IP: src, Dev: p.Dev, Fw: p.Fw, Ports: len(p.Ports),
		LastSeen: time.Now().Unix(),
	}
}

// dropBeaconCandidate elimina el candidato de una IP (beacon validado).
func (s *server) dropBeaconCandidate(src string) {
	s.beaconCandMu.Lock()
	defer s.beaconCandMu.Unlock()
	delete(s.beaconCand, src)
}

// beaconCandidates lista los candidatos vivos (TTL), ordenados por IP.
func (s *server) beaconCandidates() []beaconCandidate {
	s.beaconCandMu.Lock()
	defer s.beaconCandMu.Unlock()
	out := []beaconCandidate{}
	now := time.Now()
	for ip, c := range s.beaconCand {
		if now.Sub(time.Unix(c.LastSeen, 0)) > beaconCandidateTTL {
			delete(s.beaconCand, ip)
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IP < out[j].IP })
	return out
}
