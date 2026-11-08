// forget_agent_1979_internal_test.go - la baja del agente debe borrar el
// estado persistido del registry, o el siguiente arranque lo restaura y una
// unidad sin agente alerta "Agent down - falling back to SSH" (#1979).
package httpapi

import (
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

func TestForgetAgentPurgesPersistedState(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()

	reg := adapters.NewAgentRegistry(0)
	reg.Ingest(&probe.Payload{Router: "test-gw", Ts: time.Now().Unix(), Version: "3.0.38"})
	s := &server{db: d, agents: reg}

	// Estado persistido y token, como los deja una instalación real.
	for _, kv := range [][2]string{
		{agentTokenKey("test-gw"), "tok"},
		{agentRejectKey("test-gw"), "3"},
		{agentStateKey("test-gw"), `{"lastSeen":1}`},
		{agentTokenKey("other"), "keep"},
		{agentStateKey("other"), `{"lastSeen":1}`},
	} {
		if _, err := d.Exec("INSERT INTO kv (key, value) VALUES (?, ?)", kv[0], kv[1]); err != nil {
			t.Fatalf("seed %s: %v", kv[0], err)
		}
	}

	if revoked := s.forgetAgent("test-gw"); !revoked {
		t.Fatal("forgetAgent deberia informar de token revocado")
	}

	for _, key := range []string{agentTokenKey("test-gw"), agentRejectKey("test-gw"), agentStateKey("test-gw")} {
		var v string
		if err := d.QueryRow("SELECT value FROM kv WHERE key = ?", key).Scan(&v); err == nil {
			t.Fatalf("%s sobrevivio a la baja del agente", key)
		}
	}
	// La unidad ajena no se toca.
	for _, key := range []string{agentTokenKey("other"), agentStateKey("other")} {
		var v string
		if err := d.QueryRow("SELECT value FROM kv WHERE key = ?", key).Scan(&v); err != nil {
			t.Fatalf("%s se borro por error: %v", key, err)
		}
	}
	if _, _, _, _, ok := reg.Info("test-gw"); ok {
		t.Fatal("el registry deberia olvidar el slug")
	}
	if _, _, _, _, ok := reg.Info("other"); ok {
		t.Fatal("el registry no tenia 'other'; Info no debe inventarlo")
	}
}

// Tras la baja y un reinicio simulado (registry nuevo + restaurador), la
// unidad no vuelve a aparecer en el registry.
func TestForgetAgentSurvivesRestart(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()

	reg := adapters.NewAgentRegistry(0)
	reg.Ingest(&probe.Payload{Router: "test-gw", Ts: time.Now().Unix(), Version: "3.0.38"})
	s := &server{db: d, agents: reg}

	// Estado persistido como el que escribe la ingesta real.
	if _, err := d.Exec("INSERT INTO kv (key, value) VALUES (?, ?)",
		agentStateKey("test-gw"), `{"payload":{"router":"test-gw"},"lastSeen":1,"version":"3.0.38"}`); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	s.forgetAgent("test-gw")

	restored := adapters.NewAgentRegistry(0)
	NewStateRestorer(d)(restored)
	if _, _, _, _, ok := restored.Info("test-gw"); ok {
		t.Fatal("el restaurador revivio un agente dado de baja")
	}
}
