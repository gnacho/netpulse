package sse

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestHandleStreamHonorsMaxClientsConcurrently(t *testing.T) {
	h := NewHub(nil, 1, nil)
	const n = 8

	start := make(chan struct{})
	results := make(chan int, n)
	cancels := make([]context.CancelFunc, n)
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		req := httptest.NewRequest("GET", "/api/stream", nil)
		ctx, cancel := context.WithCancel(req.Context())
		cancels[i] = cancel
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			h.HandleStream(rec, req)
			results <- rec.Code
		}()
	}
	close(start)

	for i := 0; i < n-1; i++ {
		select {
		case code := <-results:
			if code != 503 {
				t.Fatalf("concurrent stream: got %d want 503", code)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timeout waiting for rejected streams")
		}
	}

	for _, cancel := range cancels {
		cancel()
	}

	select {
	case code := <-results:
		if code != 200 {
			t.Fatalf("accepted stream: got %d want 200", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for accepted stream")
	}
	wg.Wait()

	h.mu.Lock()
	left := len(h.clients)
	h.mu.Unlock()
	if left != 0 {
		t.Fatalf("clients left: %d", left)
	}
}
