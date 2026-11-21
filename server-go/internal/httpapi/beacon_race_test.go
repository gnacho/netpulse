package httpapi

import (
	"fmt"
	"sync"
	"testing"
)

// TestBeaconFDBIngestNoRaceOnSystem cubre la carrera que introdujo #2134: el
// datagrama FDB reutiliza el System del payload anterior, ya publicado, e
// Ingest normalizaba las MAC escribiendo en él mientras otras goroutines lo
// leían. Solo detecta la carrera con el detector:
//
//	go test -race ./internal/httpapi/ -run TestBeaconFDBIngestNoRaceOnSystem
func TestBeaconFDBIngestNoRaceOnSystem(t *testing.T) {
	s, token := newBeaconTestServer(t)

	// 1) Beacon periódico inicial con fw/up/mac: crea prev.Data.System en el registry.
	initPkt := fmt.Sprintf(
		`{"v":1,"slug":"switch16","token":%q,"seq":1,"up":120,"fw":"1.0.0","mac":"78D800323149","ports":[{"n":1,"l":2}]}`,
		token,
	)
	s.ingestBeacon("192.168.1.20", []byte(initPkt))
	if p, ok := s.agents.Fresh("switch16"); !ok || p == nil || p.Data.System == nil {
		t.Fatalf("esperaba payload inicial con Data.System tras el primer beacon")
	}

	fdbPkt := []byte(fmt.Sprintf(
		`{"v":1,"slug":"switch16","token":%q,"seq":2,"fdb":{"AABBCCDDEE01":"lan1"}}`,
		token,
	))

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Lector concurrente que simula Live.polledFromAgent / GET /api/agents.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				if p, ok := s.agents.Fresh("switch16"); ok && p != nil && p.Data.System != nil {
					if p.Data.System.BridgeMAC == "" {
						t.Errorf("esperaba BridgeMAC no vacía")
					}
				}
			}
		}
	}()

	// Datagramas FDB repetidos: cada uno reinyecta prev.Data.System en
	// fdbPayload y llama a s.agents.Ingest(fdbPayload).
	for i := 0; i < 50; i++ {
		s.ingestBeacon("192.168.1.20", fdbPkt)
	}

	close(stop)
	wg.Wait()
}
