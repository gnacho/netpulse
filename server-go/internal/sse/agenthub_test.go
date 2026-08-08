package sse

import (
	"net/http/httptest"
	"sync"
	"testing"
)

// TestAgentConnWriteConcurrentNoRace verifica que llamadas concurrentes a
// agentConn.write() (desde Send, heartbeat y el "bye" de reemplazo) no
// intercalan escrituras en el ResponseWriter. El detector -race debe estar
// limpio. Sin el wmu en agentConn, este test detecta la data race.
func TestAgentConnWriteConcurrentNoRace(t *testing.T) {
	rec := httptest.NewRecorder()
	c := &agentConn{
		slug:    "test",
		w:       rec,
		flusher: rec,
		done:    make(chan struct{}),
	}

	const goroutines = 8
	const writesEach = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < writesEach; j++ {
				if err := c.write("event: ping\ndata: {}\n\n"); err != nil {
					t.Errorf("write failed: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	totalWrites := goroutines * writesEach
	got := rec.Body.Len()
	// Cada payload "event: ping\ndata: {}\n\n" = 22 bytes.
	want := totalWrites * len("event: ping\ndata: {}\n\n")
	if got != want {
		t.Fatalf("body length = %d, want %d (interleaved/corrupted writes)", got, want)
	}
}
