package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestRunSerializesPrivilegedCommands(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() {
		releaseOnce.Do(func() { close(release) })
	}
	defer releaseAll()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ingest/agent":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		case "/api/agents/patio/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			flusher, ok := w.(http.Flusher)
			if !ok {
				return
			}
			_, _ = fmt.Fprint(w, "event: upgrade\ndata: first\n\n")
			flusher.Flush()
			_, _ = fmt.Fprint(w, "event: upgrade\ndata: second\n\n")
			flusher.Flush()
			<-r.Context().Done()
		default:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{
			Server:        srv.URL,
			Token:         "tok",
			Slug:          "patio",
			Interval:      20 * time.Millisecond,
			HeartbeatFile: filepath.Join(t.TempDir(), "hb"),
			Version:       "test",
			OnUpgrade: func(data string) {
				started <- data
				<-release
			},
		})
	}()

	waitStarted := func() string {
		t.Helper()
		select {
		case got := <-started:
			return got
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for command to start")
			return ""
		}
	}

	if got := waitStarted(); got != "first" {
		t.Fatalf("first command = %q, want first", got)
	}
	select {
	case got := <-started:
		t.Fatalf("second command started before first finished: %q", got)
	case <-time.After(200 * time.Millisecond):
	}

	releaseAll()
	if got := waitStarted(); got != "second" {
		t.Fatalf("second command = %q, want second", got)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop after cancellation")
	}
}
