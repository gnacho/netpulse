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
	return &server{
		cfg:         cfg,
		db:          d,
		agents:      adapters.NewAgentRegistry(90 * time.Second),
		ingestLimit: newIPRateLimit(ingestRateLimit, ingestRateWindow),
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
	// El estado persistido en kv también lleva el payload del beacon.
	var raw string
	if err := s.db.QueryRow("SELECT value FROM kv WHERE key = ?", agentStateKey("switch16")).Scan(&raw); err != nil {
		t.Fatalf("persist: %v", err)
	}
	if !strings.Contains(raw, "beacon-1.0") {
		t.Fatalf("kv sin el estado del beacon: %.80s", raw)
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
