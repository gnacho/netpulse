// beacon_test.go — #291: listener UDP de beacons de pushers embebidos.
// Tests internos (package httpapi) para construir un *server mínimo con
// db + registry y ejercitar ingestBeacon/startBeaconListener directamente.
package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/alerts"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

// newBeaconTestServer: server mínimo (db + registry + rate limit) con el
// token del agente "switch16" ya registrado en kv.
func newBeaconTestServer(t *testing.T) (*server, string) {
	t.Helper()
	dataDir := t.TempDir()
	cfg, err := config.Load(map[string]string{
		"AUTH_USER": "admin", "AUTH_PASS": "test123456",
		"DEMO_MODE": "0", "DATA_DIR": dataDir, "NODE_ENV": "test",
	}, dataDir)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	d, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	token := "beacontoken1234567890abcdef"
	sum := sha256.Sum256([]byte(token))
	if _, err := d.Exec("INSERT INTO kv (key, value) VALUES (?, ?)",
		agentTokenKey("switch16"), hex.EncodeToString(sum[:])); err != nil {
		t.Fatalf("token kv: %v", err)
	}
	eng := alerts.New(d, nil)
	return &server{
		cfg:         cfg,
		db:          d,
		agents:      adapters.NewAgentRegistry(90 * time.Second),
		ingestLimit: newIPRateLimit(ingestRateLimit, ingestRateWindow),
		adapter:     adapters.NewDemo(eng),
	}, token
}

func sendUDP(t *testing.T, addr net.Addr, payload string) {
	t.Helper()
	c, err := net.Dial("udp", addr.String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close()
	if _, err := c.Write([]byte(payload)); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func waitFresh(t *testing.T, s *server, slug string) *probe.Payload {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if p, ok := s.agents.Fresh(slug); ok && p.Version == beaconVersion {
			return p
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("el beacon no dejó el agente fresh")
	return nil
}

// Un beacon válido actualiza el registry: fresh, kind external, interval 30,
// versión beacon-1.0 y puertos mapeados (up + velocidad, labels enriquecidos
// desde el último push del scraper).
func TestBeaconValidIngestOverUDP(t *testing.T) {
	s, token := newBeaconTestServer(t)

	// Estado previo "scraper": MACs + puertos con labels (v2.1).
	s.agents.Ingest(&probe.Payload{
		Router: "switch16", Ts: time.Now().Unix() - 60, Version: "scraper-2.1",
		Kind: "external", Interval: 300,
		Data: probe.PayloadData{FDB: &probe.FDBData{
			MACs: map[string]string{"AA:BB:CC:DD:EE:FF": "1"},
			Ports: []probe.EthPort{
				{ID: "lan1", Label: "uplink", Up: true, Speed: "1G"},
				{ID: "lan2", Label: "hikvision", Up: true, Speed: "100M"},
			},
		}},
	})

	addr, err := s.startBeaconListener("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	defer s.beaconConn.Close()

	sendUDP(t, addr, fmt.Sprintf(
		`{"v":1,"seq":7,"slug":"switch16","token":"%s","ports":[{"n":1,"l":3,"tx":10,"rx":20},{"n":2,"l":0,"tx":0,"rx":0}]}`,
		token))

	p := waitFresh(t, s, "switch16")
	if p.Version != "beacon-1.0" || p.Kind != "external" || p.Interval != 30 {
		t.Fatalf("payload: version=%q kind=%q interval=%d", p.Version, p.Kind, p.Interval)
	}
	if p.Data.FDB == nil || len(p.Data.FDB.Ports) != 2 {
		t.Fatalf("fdb.ports: %+v", p.Data.FDB)
	}
	p1, p2 := p.Data.FDB.Ports[0], p.Data.FDB.Ports[1]
	if !p1.Up || p1.Speed != "1G" || p1.Label != "uplink" {
		t.Fatalf("puerto 1: %+v (label del scraper perdido)", p1)
	}
	if p2.Up || p2.Label != "hikvision" {
		t.Fatalf("puerto 2: %+v", p2)
	}
	// El estado persistido en kv también lleva el payload del beacon. El
	// write ocurre en el goroutine del listener DESPUÉS de Ingest: poll con
	// deadline en vez de una consulta única (race en CI lenta).
	deadline := time.Now().Add(3 * time.Second)
	var raw string
	for {
		err := s.db.QueryRow("SELECT value FROM kv WHERE key = ?", agentStateKey("switch16")).Scan(&raw)
		if err == nil && strings.Contains(raw, "beacon-1.0") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("persist: estado del beacon no llegó a kv (último err: %v, raw: %.60s)", err, raw)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Token inválido, esquema distinto de 1 y JSON roto: sin efecto en el
// registry (el agente NO queda fresh).
func TestBeaconRejectsBadPackets(t *testing.T) {
	s, token := newBeaconTestServer(t)
	addr, err := s.startBeaconListener("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	defer s.beaconConn.Close()

	sendUDP(t, addr, `{"v":1,"seq":1,"slug":"switch16","token":"wrong","ports":[]}`)
	sendUDP(t, addr, fmt.Sprintf(`{"v":2,"seq":1,"slug":"switch16","token":"%s","ports":[]}`, token))
	sendUDP(t, addr, `{no-json`)
	sendUDP(t, addr, fmt.Sprintf(`{"v":1,"seq":2,"slug":"BAD slug","token":"%s","ports":[]}`, token))

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := s.agents.Fresh("switch16"); ok {
			t.Fatal("un paquete rechazado dejó el agente fresh")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// El anuncio de descubrimiento (broadcast sin token, con dev/fw) queda como
// candidato; un beacon VALIDADO desde esa IP lo limpia (#291).
func TestBeaconAnnounceBecomesCandidate(t *testing.T) {
	s, token := newBeaconTestServer(t)
	addr, err := s.startBeaconListener("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	defer s.beaconConn.Close()

	sendUDP(t, addr, `{"v":1,"seq":1,"slug":"","token":"","dev":"KP-9000-9XHML-X","fw":"rtlplayground-v0.1.0","ports":[{"n":1,"l":3,"tx":0,"rx":0}]}`)
	deadline := time.Now().Add(2 * time.Second)
	got := []beaconCandidate{}
	for time.Now().Before(deadline) {
		got = s.beaconCandidates()
		if len(got) == 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(got) != 1 {
		t.Fatalf("candidato: %+v", got)
	}
	if got[0].Dev != "KP-9000-9XHML-X" || got[0].Fw != "rtlplayground-v0.1.0" || got[0].Ports != 1 {
		t.Fatalf("candidato mal: %+v", got[0])
	}

	// Beacon validado desde la misma IP → el candidato desaparece.
	sendUDP(t, addr, fmt.Sprintf(
		`{"v":1,"seq":2,"slug":"switch16","token":"%s","ports":[{"n":1,"l":3,"tx":1,"rx":1}]}`, token))
	waitFresh(t, s, "switch16")
	if got := s.beaconCandidates(); len(got) != 0 {
		t.Fatalf("candidato debería limpiarse tras beacon validado: %+v", got)
	}
}

// Un announce con token INVALIDO no es candidato ni agente: ruido desechado.
func TestBeaconAnnounceWithBadTokenDropped(t *testing.T) {
	s, _ := newBeaconTestServer(t)
	// announce con token inventado: cae en la rama de token inválido (log)
	s.ingestBeacon("10.0.0.9", []byte(`{"v":1,"seq":1,"slug":"switch16","token":"bad","dev":"X","fw":"y","ports":[]}`))
	if got := s.beaconCandidates(); len(got) != 0 {
		t.Fatalf("token inválido no debe crear candidato: %+v", got)
	}
}

func waitForAlert(t *testing.T, s *server, substr string) alerts.AlertEvent {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, a := range s.alertsEngine().List() {
			if strings.Contains(a.Title, substr) {
				return a
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no llegó la alerta %q; feed: %+v", substr, s.alertsEngine().List())
	return alerts.AlertEvent{}
}

// Un evento loop llega por UDP y produce una alerta urgente en el feed.
func TestBeaconLoopEventEmitsUrgentAlert(t *testing.T) {
	s, token := newBeaconTestServer(t)
	addr, err := s.startBeaconListener("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	defer s.beaconConn.Close()

	sendUDP(t, addr, fmt.Sprintf(
		`{"v":1,"ev":"loop","port":2,"mac":"AABBCCDDEEFF","seq":50,"slug":"switch16","token":"%s"}`, token))

	a := waitForAlert(t, s, "Bucle detectado")
	if !a.Urgent || a.RouterID != "switch16" || a.Severity != "warn" {
		t.Fatalf("alerta mal: %+v", a)
	}
	if !strings.Contains(a.Title, "boca 2") {
		t.Fatalf("título sin la boca: %q", a.Title)
	}
}

// El fallback por delta: dos beacons seguidos con un link cambiado generan
// la alerta de link caído sin necesidad de eventos del firmware.
func TestBeaconDeltaFallbackEmitsLinkChange(t *testing.T) {
	s, token := newBeaconTestServer(t)
	addr, err := s.startBeaconListener("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	defer s.beaconConn.Close()

	up := fmt.Sprintf(`{"v":1,"seq":1,"slug":"switch16","token":"%s","ports":[{"n":2,"l":2,"tx":1,"rx":1}]}`, token)
	sendUDP(t, addr, up)
	waitFresh(t, s, "switch16")
	down := fmt.Sprintf(`{"v":1,"seq":2,"slug":"switch16","token":"%s","ports":[{"n":2,"l":0,"tx":1,"rx":1}]}`, token)
	sendUDP(t, addr, down)
	waitForAlert(t, s, "Link caído")
}

// Datagrama FDB (v1.2): sustituye la tabla MAC conservando las bocas del
// último beacon (nada se borra al llegar solo-MACs).
func TestBeaconFDBDatagramPreservesPorts(t *testing.T) {
	s, token := newBeaconTestServer(t)
	addr, err := s.startBeaconListener("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	defer s.beaconConn.Close()

	sendUDP(t, addr, fmt.Sprintf(
		`{"v":1,"seq":1,"slug":"switch16","token":"%s","ports":[{"n":1,"l":3,"tx":1,"rx":1},{"n":2,"l":0,"tx":0,"rx":0}]}`, token))
	waitFresh(t, s, "switch16")

	sendUDP(t, addr, fmt.Sprintf(
		`{"v":1,"seq":2,"slug":"switch16","token":"%s","fdb":{"AABBCCDDEE01":"1","AABBCCDDEE02":"2"}}`, token))

	deadline := time.Now().Add(3 * time.Second)
	var got *probe.Payload
	for time.Now().Before(deadline) {
		if p, ok := s.agents.StalePayload("switch16"); ok && p != nil && p.Data.FDB != nil && len(p.Data.FDB.MACs) == 2 {
			got = p
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if got == nil {
		t.Fatal("el datagrama FDB no actualizó la tabla MAC")
	}
	if len(got.Data.FDB.Ports) != 2 || !got.Data.FDB.Ports[0].Up || got.Data.FDB.Ports[1].Up {
		t.Fatalf("las bocas del beacon anterior no se conservaron: %+v", got.Data.FDB.Ports)
	}
	if got.Data.FDB.MACs["AA:BB:CC:DD:EE:01"] != "1" {
		t.Fatalf("macs mal: %+v", got.Data.FDB.MACs)
	}
}

// Las MACs del firmware llegan SIN separadores: se normalizan al formato
// canónico con dos puntos para que leases/wifi/enriquecimiento casen.
func TestBeaconFDBNormalizesBareMACs(t *testing.T) {
	s, token := newBeaconTestServer(t)
	addr, err := s.startBeaconListener("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	defer s.beaconConn.Close()

	sendUDP(t, addr, fmt.Sprintf(
		`{"v":1,"seq":1,"slug":"switch16","token":"%s","fdb":{"AABBCCDDEE01":"3","aa:bb:cc:dd:ee:02":"8","corta":"1"}}`, token))
	deadline := time.Now().Add(3 * time.Second)
	var macs map[string]string
	for time.Now().Before(deadline) {
		if p, ok := s.agents.StalePayload("switch16"); ok && p != nil && p.Data.FDB != nil && len(p.Data.FDB.MACs) == 2 {
			macs = p.Data.FDB.MACs
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if macs == nil {
		t.Fatal("el datagrama FDB no llegó al registry")
	}
	if macs["AA:BB:CC:DD:EE:01"] != "3" || macs["AA:BB:CC:DD:EE:02"] != "8" {
		t.Fatalf("normalización de MACs mal: %+v", macs)
	}
}

// Seq que vuelve a 1 = reboot del switch → alerta info en el feed.
func TestBeaconSeqResetEmitsRebootAlert(t *testing.T) {
	s, token := newBeaconTestServer(t)
	addr, err := s.startBeaconListener("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listener: %v", err)
	}
	defer s.beaconConn.Close()

	sendUDP(t, addr, fmt.Sprintf(`{"v":1,"seq":40,"slug":"switch16","token":"%s","ports":[{"n":1,"l":3,"tx":0,"rx":0}]}`, token))
	waitFresh(t, s, "switch16")
	sendUDP(t, addr, fmt.Sprintf(`{"v":1,"seq":1,"slug":"switch16","token":"%s","ports":[{"n":1,"l":3,"tx":0,"rx":0}]}`, token))
	a := waitForAlert(t, s, "Switch reiniciado")
	if a.Severity != "info" || a.RouterID != "switch16" {
		t.Fatalf("alerta de reboot mal: %+v", a)
	}
}
