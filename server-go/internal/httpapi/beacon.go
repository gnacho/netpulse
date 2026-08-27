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
	"time"

	"github.com/gnacho/netpulse/agent/probe"
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
type beaconPacket struct {
	V     int          `json:"v"` // versión de esquema; solo se acepta 1
	Seq   uint32       `json:"seq"`
	Slug  string       `json:"slug"`
	Token string       `json:"token"`
	Ports []beaconPort `json:"ports"`
}

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
	if s.cfg != nil && s.cfg.DemoMode {
		return
	}
	if s.agents == nil {
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
	pl := &probe.Payload{
		Router:   p.Slug,
		Ts:       time.Now().Unix(),
		Version:  beaconVersion,
		Kind:     "external",
		Interval: beaconIntervalSec,
		Data:     probe.PayloadData{FDB: &probe.FDBData{Ports: ports}},
	}
	s.agents.Ingest(pl)
	s.persistAgentState(p.Slug, s.agents.Snapshot(p.Slug))
	s.beaconSeqNote(p.Slug, p.Seq)
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
	defer s.beaconSeqMu.Unlock()
	if s.beaconSeq == nil {
		s.beaconSeq = map[string]uint32{}
	}
	if old, ok := s.beaconSeq[slug]; ok && seq != old+1 {
		log.Printf("[netpulse:beacon] %s: seq %d tras %d (pérdida, reorden o replay)", slug, seq, old)
	}
	s.beaconSeq[slug] = seq
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
