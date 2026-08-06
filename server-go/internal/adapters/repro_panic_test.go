package adapters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
)

// TestReproNilDerefRealPayloads reproduce el panic del poller (issue #57)
// con los payloads reales de los 4 agentes extraídos de prod (kv agent.state.*).
// Ruta: /tmp/opencode/payload-<slug>.json y gateway-payload.json.
func TestReproNilDerefRealPayloads(t *testing.T) {
	files := []string{
		"/tmp/opencode/gateway-payload.json",
		"/tmp/opencode/payload-redmi-ax6.json",
		"/tmp/opencode/payload-redmi-ax6-2.json",
		"/tmp/opencode/payload-redmi-ax6-3.json",
	}
	payloads := []*probe.Payload{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Logf("skip %s: %v", filepath.Base(f), err)
			continue
		}
		var p probe.Payload
		if err := json.Unmarshal(raw, &p); err != nil {
			t.Fatalf("unmarshal %s: %v", f, err)
		}
		payloads = append(payloads, &p)
	}
	if len(payloads) == 0 {
		t.Skip("sin payloads reales")
	}

	reg := NewAgentRegistry(90 * time.Second)
	l := newLiveAgentTest(t, reg)
	cfg := RouterConfig{ID: "patio", Name: "Patio", Host: "127.0.0.1"}

	// Ingiere todos los payloads (con el slug del test) y sondea en bucle.
	for _, p := range payloads {
		cp := *p
		cp.Router = "patio"
		reg.Ingest(&cp)
	}
	for i := 0; i < 2000; i++ {
		rp, err := l.pollRouter(t.Context(), cfg)
		if err != nil {
			t.Fatalf("pollRouter iter %d: %v", i, err)
		}
		_ = l.buildRouter(rp, nil)
		_ = l.GetMetricsRows(t.Context())
	}
	t.Logf("2000 iteraciones con %d payloads reales sin panic", len(payloads))
}
