package sse

// FORK: a stream served over HTTP/2 survives idle time past the write
// deadline a single write needs.

import (
	"bufio"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAStreamOverHTTP2OutlivesTheWriteDeadline(t *testing.T) {
	if testing.Short() {
		t.Skip("waits past the 10 s write deadline")
	}
	h := NewAgentHub(func(string, string) bool { return true })
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/agents/{slug}/stream", h.HandleStream)
	ts := httptest.NewUnstartedServer(mux)
	ts.EnableHTTP2 = true
	ts.StartTLS()
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/agents/test-router/stream", nil)
	req.Header.Set("Authorization", "Bearer x")
	res, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.ProtoMajor != 2 {
		t.Fatalf("served over HTTP/%d, the test needs HTTP/2", res.ProtoMajor)
	}
	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			lines <- sc.Text()
		}
		close(lines)
	}()
	waitFor := func(want string, within time.Duration) bool {
		deadline := time.After(within)
		for {
			select {
			case l, ok := <-lines:
				if !ok {
					return false
				}
				if strings.Contains(l, want) {
					return true
				}
			case <-deadline:
				return false
			}
		}
	}
	if !waitFor("event: connected", 3*time.Second) {
		t.Fatal("no connected event")
	}
	time.Sleep(12 * time.Second) // past the 10 s a write's deadline allows
	if !h.Send("test-router", "ping", map[string]int{"n": 1}) {
		t.Fatal("the stream was gone after 12 s idle")
	}
	if !waitFor("event: ping", 3*time.Second) {
		t.Fatal("a command sent after 12 s idle never arrived")
	}
}
