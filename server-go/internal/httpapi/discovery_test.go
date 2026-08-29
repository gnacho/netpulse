// discovery_test.go — #367: responder UDP de descubrimiento y token de alta
// de red (autoenroll). Tests internos para ejercitar el listener UDP y el
// handler de pair directamente.
package httpapi

import (
	"encoding/json"
	"net"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

// newDiscoveryTestServer: server mínimo con cfg configurable (autoenroll y
// PORT) para el responder UDP.
func newDiscoveryTestServer(t *testing.T, autoenroll bool) *server {
	t.Helper()
	dataDir := t.TempDir()
	env := map[string]string{
		"AUTH_USER": "admin", "AUTH_PASS": "test123456",
		"DEMO_MODE": "0", "DATA_DIR": dataDir, "NODE_ENV": "test",
		"PORT": "3457",
	}
	if autoenroll {
		env["AGENT_AUTOENROLL"] = "1"
	}
	cfg, err := config.Load(env, dataDir)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	d, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return &server{
		cfg:         cfg,
		db:          d,
		agents:      adapters.NewAgentRegistry(90 * time.Second),
		ingestLimit: newIPRateLimit(ingestRateLimit, ingestRateWindow),
	}
}

// probeAndWait: envía un probe desde un socket propio y devuelve la
// respuesta del responder (o falla por timeout).
func probeAndWait(t *testing.T, addr net.Addr) discoveryResponse {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer pc.Close()
	if _, err := pc.WriteTo([]byte(`{"v":1,"type":"netgrip-probe"}`), addr); err != nil {
		t.Fatalf("write probe: %v", err)
	}
	buf := make([]byte, 1024)
	_ = pc.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, _, err := pc.ReadFrom(buf)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	var resp discoveryResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, buf[:n])
	}
	return resp
}

// pairViaHandler llama a handleAgentPair con un recorder.
func pairViaHandler(t *testing.T, s *server, pairingToken, slug string) (int, string) {
	t.Helper()
	body := `{"pairing_token":` + strconv.Quote(pairingToken) + `,"slug":` + strconv.Quote(slug) + `}`
	req := httptest.NewRequest("POST", "/api/agents/pair", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.handleAgentPair(rec, req)
	return rec.Code, rec.Body.String()
}

func TestDiscoveryProbeDetection(t *testing.T) {
	if !isDiscoveryProbe([]byte(`{"v":1,"type":"netgrip-probe"}`)) {
		t.Fatal("probe válido no detectado")
	}
	for _, raw := range []string{
		`{"v":1,"seq":7,"slug":"switch16","token":"x","ports":[]}`,
		`{"v":1,"type":"otra-cosa"}`,
		`{"v":2,"type":"netgrip-probe"}`,
		`no json`,
	} {
		if isDiscoveryProbe([]byte(raw)) {
			t.Fatalf("datagrama no-probe detectado como probe: %s", raw)
		}
	}
}

func TestDiscoveryAnswerWithoutAutoenroll(t *testing.T) {
	s := newDiscoveryTestServer(t, false)
	addr, err := s.startBeaconListener("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	defer s.beaconConn.Close()

	resp := probeAndWait(t, addr)
	if resp.Type != "netpulse-server" || resp.V != 1 {
		t.Fatalf("respuesta: %+v", resp)
	}
	if resp.Autoenroll || resp.PairingToken != "" {
		t.Fatalf("sin AGENT_AUTOENROLL no debe ir token: %+v", resp)
	}
	if resp.URL != "http://127.0.0.1:3457" {
		t.Fatalf("url: %q (esperaba el PORT de la config)", resp.URL)
	}
}

func TestDiscoveryAnswerWithAutoenroll(t *testing.T) {
	s := newDiscoveryTestServer(t, true)
	addr, err := s.startBeaconListener("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	defer s.beaconConn.Close()

	resp := probeAndWait(t, addr)
	if !resp.Autoenroll || resp.PairingToken == "" {
		t.Fatalf("con AGENT_AUTOENROLL debe ir token: %+v", resp)
	}

	// El token de red crea un slug nuevo...
	status, body := pairViaHandler(t, s, resp.PairingToken, "router-nuevo")
	if status != 201 || !strings.Contains(body, `"token"`) {
		t.Fatalf("pair con token de red: %d %s", status, body)
	}
	// ...pero NO puede tocar un slug existente.
	status, body = pairViaHandler(t, s, resp.PairingToken, "router-nuevo")
	if status != 409 || !strings.Contains(body, "slug_taken") {
		t.Fatalf("pair repetido con token de red: %d %s", status, body)
	}
	// Token desconocido: 401.
	status, _ = pairViaHandler(t, s, "token-falso", "otro")
	if status != 401 {
		t.Fatalf("token falso: %d", status)
	}
	// El pairing token de admin sigue rotando slugs existentes.
	adminTok, err := s.getPairingToken()
	if err != nil {
		t.Fatalf("admin token: %v", err)
	}
	status, _ = pairViaHandler(t, s, adminTok, "router-nuevo")
	if status != 201 {
		t.Fatalf("admin pair sobre slug existente: %d", status)
	}
}

func TestAutoenrollTokenStableAndRotatable(t *testing.T) {
	s := newDiscoveryTestServer(t, true)
	t1, err := s.getAutoenrollToken()
	if err != nil || t1 == "" {
		t.Fatalf("token 1: %v %q", err, t1)
	}
	t2, err := s.getAutoenrollToken()
	if err != nil || t2 != t1 {
		t.Fatalf("el token debe ser estable dentro del TTL: %q vs %q", t1, t2)
	}
	// Simular expiración: borrar el ts → rota en la siguiente lectura.
	if _, err := s.db.Exec("DELETE FROM kv WHERE key = ?", autoenrollTSKey); err != nil {
		t.Fatalf("delete ts: %v", err)
	}
	t3, err := s.getAutoenrollToken()
	if err != nil || t3 == "" || t3 == t1 {
		t.Fatalf("tras expirar debe rotar: %q", t3)
	}
	// Con el flag apagado, checkAutoenrollToken nunca acepta.
	s.cfg.AgentAutoenroll = false
	if s.checkAutoenrollToken(t3) {
		t.Fatal("checkAutoenrollToken debe rechazar con el flag apagado")
	}
}
