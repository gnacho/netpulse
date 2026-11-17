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

// #1334: con varios agentes de hostname duplicado (APs puente idénticos), la
// MAC de bridge decide: es la identidad física autoritativa.
func TestAgentRegistryMatchRouterPrefersBridgeMacOverAmbiguousHostname(t *testing.T) {
	reg := NewAgentRegistry(90 * time.Second)
	now := time.Now()
	reg.SetClock(func() time.Time { return now })

	first := testPayload()
	first.Router = "ap-first"
	first.Data.System.Board.Hostname = "Jidu6401"
	first.Data.System.BridgeMAC = "AA:AA:AA:AA:AA:01"
	reg.Ingest(first)
	second := testPayload()
	second.Router = "ap-second"
	second.Data.System.Board.Hostname = "Jidu6401"
	second.Data.System.BridgeMAC = "AA:AA:AA:AA:AA:02"
	reg.Ingest(second)

	cfg := RouterConfig{ID: "jidu6401-2", Name: "Jidu6401", Host: "192.168.1.2"}
	slug, p, fresh := reg.MatchRouter(cfg, map[string]string{cfg.ID: "AA:AA:AA:AA:AA:02"})
	if !fresh || slug != "ap-second" || p == nil || p.Router != "ap-second" {
		t.Fatalf("bridge MAC should select the physical AP: slug=%q fresh=%v payload=%v", slug, fresh, p)
	}
}

// #1334: un hostname duplicado SIN MAC que lo desambigue no inventa una
// asociación: mejor no emparejar que emparejar mal.
func TestAgentRegistryMatchRouterRejectsAmbiguousHostname(t *testing.T) {
	reg := NewAgentRegistry(90 * time.Second)
	now := time.Now()
	reg.SetClock(func() time.Time { return now })
	for _, slug := range []string{"ap-first", "ap-second"} {
		pl := testPayload()
		pl.Router = slug
		pl.Data.System.Board.Hostname = "Jidu6401"
		pl.Data.System.BridgeMAC = ""
		reg.Ingest(pl)
	}

	cfg := RouterConfig{ID: "jidu6401-2", Name: "Jidu6401", Host: "192.168.1.2"}
	if slug, payload, fresh := reg.MatchRouter(cfg, nil); slug != "" || payload != nil || fresh {
		t.Fatalf("ambiguous hostname must not invent an AP: slug=%q payload=%v fresh=%v", slug, payload, fresh)
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

	a := findAlert(l.engine.List(), "SSH access lost")
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
	if n := countAlerts(l.engine.List(), "SSH access lost"); n != 1 {
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

// TestAgentRegistryMatchRouterRetainedIdentity (#852): un push wireless-only
// (sin sección system, p. ej. un evento nl80211 de roam) NO debe romper el
// match por hostname/MAC: es la causa de los "router sin cliente" transitorios
// en routers agent-only emparejados sin slug exacto.
func TestAgentRegistryMatchRouterRetainedIdentity(t *testing.T) {
	reg := NewAgentRegistry(90 * time.Second)
	now := time.Now()
	reg.SetClock(func() time.Time { return now })

	pl := testPayload()
	pl.Router = "flint2"
	pl.Data.System.Board.Hostname = "flint2"
	pl.Data.System.BridgeMAC = "94:83:C4:00:00:09"
	reg.Ingest(pl)

	// Evento nl80211: push rápido wireless-only, sin sección system.
	ev := testPayload()
	ev.Router = "flint2"
	ev.Data.System = nil
	reg.Ingest(ev)

	cfg := RouterConfig{ID: "gl-inet-gl-mt6000", Name: "flint2", Host: "192.168.10.1"}
	slug, p, fresh := reg.MatchRouter(cfg, nil)
	if slug != "flint2" || p == nil || !fresh {
		t.Fatalf("match por hostname retenido tras push wireless-only: slug=%q fresh=%v p=%v", slug, fresh, p)
	}

	cfgMac := RouterConfig{ID: "tp-link-eap225-09", Name: "Otro", Host: "192.168.1.9"}
	macs := map[string]string{cfgMac.ID: "94:83:C4:00:00:09"}
	slug, p, fresh = reg.MatchRouter(cfgMac, macs)
	if slug != "flint2" || p == nil || !fresh {
		t.Fatalf("match por MAC retenida tras push wireless-only: slug=%q fresh=%v p=%v", slug, fresh, p)
	}
}

// TestAgentRegistryRestoreBackfillsIdentity (#852): los estados persistidos
// antes del fix no traen Host/BridgeMAC; Restore los recupera del payload.
func TestAgentRegistryRestoreBackfillsIdentity(t *testing.T) {
	reg := NewAgentRegistry(90 * time.Second)
	pl := testPayload()
	pl.Router = "flint2"
	pl.Data.System.Board.Hostname = "flint2"
	reg.Restore("flint2", &AgentState{Payload: pl, LastSeen: time.Now()})

	cfg := RouterConfig{ID: "gl-inet-gl-mt6000", Name: "flint2", Host: "192.168.10.1"}
	slug, p, fresh := reg.MatchRouter(cfg, nil)
	if slug != "flint2" || p == nil || !fresh {
		t.Fatalf("match tras Restore sin Host explícito: slug=%q fresh=%v p=%v", slug, fresh, p)
	}
}

// TestLiveAgentDownAgentOnlyIsUrgentRouter (#850): en routers agent-only la
// caída del agente es el único aviso de una fuente de datos muerta (el poll
// sirve el payload cacheado y el router nunca llega a marcar offline): la
// alerta debe ser category router + urgent para pasar los filtros de
// notificación "solo urgentes del topic router".
func TestLiveAgentDownAgentOnlyIsUrgentRouter(t *testing.T) {
	reg := NewAgentRegistry(50 * time.Millisecond)
	l := newLiveAgentTest(t, reg)
	cfg := RouterConfig{ID: "patio", Name: "Patio", Host: "127.0.0.1", AgentOnly: true}
	reg.Ingest(testPayload())

	if _, err := l.pollRouter(t.Context(), cfg); err != nil {
		t.Fatalf("agente fresco: %v", err)
	}
	time.Sleep(60 * time.Millisecond)
	// Agente expirado pero con payload cacheado: el poll NO falla (sirve lo
	// cacheado) — la tarjeta sigue "viva" con datos congelados.
	p, err := l.pollRouter(t.Context(), cfg)
	if err != nil {
		t.Fatalf("agent-only con payload cacheado no debe fallar el poll: %v", err)
	}
	if p == nil {
		t.Fatal("esperaba datos cacheados del agente")
	}
	down := findAlert(l.engine.List(), "Agent down on Patio")
	if down == nil {
		t.Fatalf("alerta de caída esperada: %+v", l.engine.List())
	}
	if down.Category != alerts.CatRouter || !down.Urgent || down.Severity != "critical" {
		t.Fatalf("agent-down agent-only debe ser router/urgent/critical: %+v", down)
	}
	if strings.Contains(down.Title, "SSH") {
		t.Fatalf("agent-only no debe mencionar SSH: %+v", down)
	}
	// Recuperación: alerta ok visible en el feed (system:all), sin urgencia.
	reg.Ingest(testPayload())
	if _, err := l.pollRouter(t.Context(), cfg); err != nil {
		t.Fatalf("agente recuperado: %v", err)
	}
	ok := findAlert(l.engine.List(), "Agent recovered on Patio")
	if ok == nil || ok.Category != alerts.CatSystem || ok.Urgent || ok.Severity != "ok" {
		t.Fatalf("alerta recuperación agent-only: %+v", ok)
	}
}

// The cached payload keeps the card populated (#850), but a pusher dead past
// the down-confirm window must read offline, agreeing with the probe table
// instead of claiming "online" with frozen vitals.
func TestLiveAgentOnlyStaleCardReadsOffline(t *testing.T) {
	reg := NewAgentRegistry(50 * time.Millisecond)
	l := newLiveAgentTest(t, reg)
	cfg := RouterConfig{ID: "patio", Name: "Patio", Host: "127.0.0.1", AgentOnly: true}
	reg.Ingest(testPayload())
	p, err := l.pollRouter(t.Context(), cfg)
	if err != nil || p == nil {
		t.Fatalf("fresh poll: p=%v err=%v", p, err)
	}
	if r := l.buildRouter(p, nil); r.Status == "offline" {
		t.Fatalf("fresh agent-only must not read offline: %q", r.Status)
	}
	time.Sleep(60 * time.Millisecond)
	p, err = l.pollRouter(t.Context(), cfg)
	if err != nil || p == nil {
		t.Fatalf("stale agent-only still serves cached payload: p=%v err=%v", p, err)
	}
	r := l.buildRouter(p, nil)
	if r.Status != "offline" || r.Health != 0 {
		t.Fatalf("stale agent-only card must read offline/health 0, got %q/%d", r.Status, r.Health)
	}
	reg.Ingest(testPayload())
	p, _ = l.pollRouter(t.Context(), cfg)
	if r := l.buildRouter(p, nil); r.Status == "offline" {
		t.Fatalf("recovered agent-only must not read offline")
	}
}
