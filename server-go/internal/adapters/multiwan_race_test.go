package adapters

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
)

// TestPolledFromAgentMultiWanNoWriteToPayload: un payload ya publicado en
// AgentRegistry con una sección multiWan sin "uplinks" (Uplinks == nil) lo
// leen otras goroutines (persistAgentState lo serializa). polledFromAgent no
// debe escribir en él: con -race la escritura en el struct compartido sale
// como DATA RACE, y sin -race el payload queda modificado.
func TestPolledFromAgentMultiWanNoWriteToPayload(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	cfg := RouterConfig{ID: "rt1", Name: "Router", Host: "192.0.2.1", AgentOnly: true}

	for i := 0; i < 50; i++ {
		p := &probe.Payload{
			Router: "rt1",
			Kind:   "native",
			Data: probe.PayloadData{
				MultiWan: &probe.MultiWanInfo{Mode: "failover", Active: "wan"},
			},
		}

		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			// Como persistAgentState con el payload publicado.
			_, _ = json.Marshal(p)
		}()
		var out *routerPolled
		go func() {
			defer wg.Done()
			<-start
			out = l.polledFromAgent(cfg, p)
		}()
		close(start)
		wg.Wait()

		if p.Data.MultiWan.Uplinks != nil {
			t.Fatalf("el payload publicado cambió: Uplinks=%#v, se esperaba nil", p.Data.MultiWan.Uplinks)
		}
		if out == nil || out.multiWan == nil {
			t.Fatalf("polledFromAgent no devolvió la sección multiWan")
		}
		if out.multiWan.Uplinks == nil {
			t.Fatalf("esperaba Uplinks vacío (no nil) en el resultado, obtuve nil")
		}
		if out.multiWan.Active != "wan" || out.multiWan.Mode != "failover" {
			t.Fatalf("esperaba la sección copiada (failover, wan), obtuve %q, %q", out.multiWan.Mode, out.multiWan.Active)
		}
	}
}
