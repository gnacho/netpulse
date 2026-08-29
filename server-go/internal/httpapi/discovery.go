// discovery.go — responder UDP de descubrimiento zero-touch (#367).
//
// NetGrip (panel on-router con el agente embebido) no tiene config inicial:
// para encontrarnos emite un probe UDP en broadcast al puerto del listener
// de beacons. Este responder contesta UNICAST con la URL HTTP real del
// server (derivada de un socket conectado hacia el prober: en hosts
// multi-homed es la IP de la interfaz correcta) y, si AGENT_AUTOENROLL=1,
// con un token de alta de red que /api/agents/pair acepta SOLO para slugs
// nuevos.
//
// Contrato v1:
//
//	probe    → {"v":1,"type":"netgrip-probe"}
//	respuesta→ {"v":1,"type":"netpulse-server","url":"http://IP:PORT",
//	            "autoenroll":true|false,"pairing_token":"<uuid>"}
//
// Seguridad (modelo LAN de confianza, misma decisión consciente que el
// beacon #291): el token de alta viaja en claro, solo hacia la IP del
// prober, y únicamente autoriza crear agentes NUEVOS (nunca rotar ni
// suplantar uno existente: slug ocupado → 409). Rotación automática a las
// 24 h para acotar la ventana de validez. Rate limit por IP origen igual
// que la ingesta. Servidores viejos ignoran el probe en silencio (sin
// campo "type" en su parser de beacons), y este server ignora datagramas
// que no sean probes.
package httpapi

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"
	"time"
)

// discoveryProbeV es la única versión de probe aceptada.
const discoveryProbeV = 1

// discoveryProbe es el datagrama que envía NetGrip en broadcast.
type discoveryProbe struct {
	V    int    `json:"v"`
	Type string `json:"type"`
}

// isDiscoveryProbe dice si un datagrama crudo es un probe (parse barato;
// los beacons reales no llevan "type" y devuelven false).
func isDiscoveryProbe(raw []byte) bool {
	var p discoveryProbe
	if err := json.Unmarshal(raw, &p); err != nil {
		return false
	}
	return p.Type == "netgrip-probe" && p.V == discoveryProbeV
}

// discoveryResponse es la respuesta unicast al prober.
type discoveryResponse struct {
	V            int    `json:"v"`
	Type         string `json:"type"`
	URL          string `json:"url"`
	Autoenroll   bool   `json:"autoenroll"`
	PairingToken string `json:"pairing_token,omitempty"`
}

// autoenrollTokenTTL: el token de red rota cuando su ts de generación
// supera esta edad. Ventana acotada: un token filtrado deja de valer solo.
const autoenrollTokenTTL = 24 * time.Hour

const (
	autoenrollTokenKey = "autoenroll.token"
	autoenrollTSKey    = "autoenroll.token.ts"
)

var autoenrollMu sync.Mutex

// getAutoenrollToken devuelve el token de alta de red vigente, creándolo o
// rotándolo si no existe o si superó el TTL. La rotación es perezosa (en
// lectura): no hay timer, y el token previo muere en el mismo instante.
func (s *server) getAutoenrollToken() (string, error) {
	autoenrollMu.Lock()
	defer autoenrollMu.Unlock()
	var stored string
	_ = s.db.QueryRow("SELECT COALESCE(value,'') FROM kv WHERE key = ?", autoenrollTokenKey).Scan(&stored)
	var tsRaw string
	_ = s.db.QueryRow("SELECT COALESCE(value,'') FROM kv WHERE key = ?", autoenrollTSKey).Scan(&tsRaw)
	ts := int64(0)
	if tsRaw != "" {
		if v, perr := strconv.ParseInt(tsRaw, 10, 64); perr == nil {
			ts = v
		}
	}
	if stored != "" && ts > 0 &&
		time.Since(time.Unix(ts, 0)) < autoenrollTokenTTL {
		return stored, nil
	}
	tok, err := newPairingToken()
	if err != nil {
		return "", err
	}
	now := strconv.FormatInt(time.Now().Unix(), 10)
	if _, err := s.db.Exec(
		"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value",
		autoenrollTokenKey, tok); err != nil {
		return "", err
	}
	if _, err := s.db.Exec(
		"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value",
		autoenrollTSKey, now); err != nil {
		return "", err
	}
	return tok, nil
}

// checkAutoenrollToken valida un token contra el vigente (tiempo constante).
func (s *server) checkAutoenrollToken(token string) bool {
	if token == "" || !s.autoenrollEnabled() {
		return false
	}
	stored, err := s.getAutoenrollToken()
	if err != nil || stored == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(stored)) == 1
}

// autoenrollEnabled: flag de config (nil-safe para tests).
func (s *server) autoenrollEnabled() bool {
	return s.cfg != nil && s.cfg.AgentAutoenroll
}

// answerDiscovery responde unicast al prober con la URL del server y, si
// procede, el token de alta. La IP de la URL sale de un socket UDP
// conectado hacia el origen (la ruta elegida dice qué interfaz usar) y el
// puerto es el HTTP de la config (default 3000 si no hay cfg).
func (s *server) answerDiscovery(pc net.PacketConn, src net.Addr) {
	host, _, err := net.SplitHostPort(src.String())
	if err != nil {
		host = src.String()
	}
	if ok, _ := s.ingestLimit.allow(host); !ok {
		return
	}
	port := 3000
	if s.cfg != nil && s.cfg.Port > 0 {
		port = s.cfg.Port
	}
	// Socket conectado hacia el prober: LocalAddr lleva la IP de la
	// interfaz por la que el kernel enrutaría la respuesta.
	conn, err := net.Dial("udp", src.String())
	if err != nil {
		return
	}
	defer conn.Close()
	local, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || local.IP == nil || local.IP.IsUnspecified() {
		return
	}
	resp := discoveryResponse{
		V:          discoveryProbeV,
		Type:       "netpulse-server",
		URL:        fmt.Sprintf("http://%s:%d", local.IP.String(), port),
		Autoenroll: s.autoenrollEnabled(),
	}
	if resp.Autoenroll {
		tok, err := s.getAutoenrollToken()
		if err != nil {
			log.Printf("[netpulse:discovery] token de alta no disponible: %v", err)
			return
		}
		resp.PairingToken = tok
	}
	payload, err := json.Marshal(resp)
	if err != nil {
		return
	}
	if _, err := conn.Write(payload); err != nil {
		log.Printf("[netpulse:discovery] respuesta a %s: %v", host, err)
	}
}
