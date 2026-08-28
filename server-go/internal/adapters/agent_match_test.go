// agent_match_test.go — Emparejamiento agente↔router por slug/hostname/MAC
// (#282) y alerta cuando SSH falla pero el agente sigue vivo (#281).
package adapters

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

func TestAgentRegistryMatchRouterByHostname(t *testing.T) {
	reg := NewAgentRegistry(90 * time.Second)
	now := time.Now()
	reg.SetClock(func() time.Time { return now })

	pl := testPayload()
	pl.Router = "flint2"
	pl.Data.System.Board.Hostname = "flint2"
	reg.Ingest(pl)

	cfg := RouterConfig{ID: "gl-inet-gl-mt6000", Name: "flint2", Host: "192.168.10.1"}
	slug, p, fresh := reg.MatchRouter(cfg, nil)
	if !fresh || slug != "flint2" || p == nil {
		t.Fatalf("esperaba match fresco por hostname; got slug=%q fresh=%v p=%v", slug, fresh, p)
	}
}

func TestAgentRegistryMatchRouterByMac(t *testing.T) {
	reg := NewAgentRegistry(90 * time.Second)
	now := time.Now()
	reg.SetClock(func() time.Time { return now })

	pl := testPayload()
	pl.Router = "ap-east"
	pl.Data.System.Board.Hostname = "otro"
	pl.Data.System.BridgeMAC = "94:83:C4:00:00:09"
	reg.Ingest(pl)

	cfg := RouterConfig{ID: "tp-link-eap225-09", Name: "Este", Host: "192.168.1.9"}
	macs := map[string]string{cfg.ID: "94:83:C4:00:00:09"}
	slug, p, fresh := reg.MatchRouter(cfg, macs)
	if !fresh || slug != "ap-east" || p == nil {
		t.Fatalf("esperaba match fresco por MAC; got slug=%q fresh=%v p=%v", slug, fresh, p)
	}
}

func TestAgentRegistryMatchRouterStale(t *testing.T) {
	reg := NewAgentRegistry(50 * time.Millisecond)
	now := time.Now()
	reg.SetClock(func() time.Time { return now })

	pl := testPayload()
	pl.Router = "flint2"
	pl.Data.System.Board.Hostname = "flint2"
	reg.Ingest(pl)

	now = now.Add(100 * time.Millisecond)
	cfg := RouterConfig{ID: "gl-inet-gl-mt6000", Name: "flint2", Host: "192.168.10.1"}
	_, p, fresh := reg.MatchRouter(cfg, nil)
	if fresh || p == nil {
		t.Fatalf("esperaba match pero no fresco; got fresh=%v", fresh)
	}
}

func TestLiveAgentFuzzyMatchSkipsSSH(t *testing.T) {
	reg := NewAgentRegistry(90 * time.Second)
	l := newLiveAgentTest(t, reg)

	pl := testPayload()
	pl.Router = "flint2"
	pl.Data.System.Board.Hostname = "flint2"
	reg.Ingest(pl)

	cfg := RouterConfig{ID: "gl-inet-gl-mt6000", Name: "flint2", Host: "127.0.0.1"}
	p, err := l.pollRouter(t.Context(), cfg)
	if err != nil {
		t.Fatalf("con agente emparejado por hostname no debería tocar SSH: %v", err)
	}
	if p.cpu != 12 {
		t.Fatalf("esperaba datos del payload; cpu=%d", p.cpu)
	}
}

func TestLiveAgentSSHAuthFailAlert(t *testing.T) {
	reg := NewAgentRegistry(90 * time.Second)
	l := newLiveAgentTest(t, reg)

	// Simula que el último sondeo SSH fue de acceso denegado (#257) y que el
	// agente sigue empujando (por hostname) -> alerta no urgente (#281).
	l.mu.Lock()
	l.lastErr["gl-inet-gl-mt6000"] = errors.New("permission denied")
	l.mu.Unlock()

	pl := testPayload()
	pl.Router = "flint2"
	pl.Data.System.Board.Hostname = "flint2"
	reg.Ingest(pl)

	cfg := RouterConfig{ID: "gl-inet-gl-mt6000", Name: "flint2", Host: "127.0.0.1"}
	if _, err := l.pollRouter(t.Context(), cfg); err != nil {
		t.Fatalf("agente fresco: %v", err)
	}

	a := findAlert(l.engine.List(), "Acceso SSH perdido")
	if a == nil {
		t.Fatalf("esperaba alerta #281; alerts=%+v", l.engine.List())
	}
	if a.Category != alerts.CatSystem || a.Urgent || a.Severity != "warn" {
		t.Fatalf("alerta #281 con categoría errónea: %+v", a)
	}
	if !strings.Contains(a.Description, "authorized_keys") {
		t.Fatalf("descripción sin hint de authorized_keys: %s", a.Description)
	}

	// Si el acceso SSH se recupera, la alerta no debe repetirse (se borra el flag).
	l.mu.Lock()
	delete(l.lastErr, "gl-inet-gl-mt6000")
	l.mu.Unlock()
	if _, err := l.pollRouter(t.Context(), cfg); err != nil {
		t.Fatalf("segundo poll: %v", err)
	}
	if n := countAlerts(l.engine.List(), "Acceso SSH perdido"); n != 1 {
		t.Fatalf("alerta duplicada tras recuperación: %d", n)
	}
}

func countAlerts(list []AlertEvent, titlePart string) int {
	n := 0
	for _, a := range list {
		if strings.Contains(a.Title, titlePart) {
			n++
		}
	}
	return n
}
