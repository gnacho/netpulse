// update_stream_test.go — GET /api/update/stream (issue #280): SSE con el
// estado del update en cada cambio de paso/progreso.
package httpapi_test

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUpdateStreamSSE(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git no disponible")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Script con retardo: permite observar el evento del paso intermedio.
	script := "#!/bin/bash\necho STEP:fetch\nsleep 2\necho STEP:done\ntrue\n"
	if err := os.WriteFile(filepath.Join(root, "deploy", "update.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	writeGitHead(t, root)
	ts := makeTestServerWithUpdater(t, root)
	cookie := adminCookie(t, ts)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/update/stream", nil)
	req.Header.Set("Cookie", "session="+cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status: %d", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type: %s", ct)
	}

	// Lectura con timeout: cada evento termina en línea vacía.
	br := bufio.NewReader(res.Body)
	readEvent := func() string {
		evt := ""
		ch := make(chan string, 1)
		go func() {
			for {
				line, err := br.ReadString('\n')
				evt += line
				if line == "\n" || line == "" || err != nil {
					ch <- evt
					return
				}
			}
		}()
		select {
		case <-ch:
			return evt
		case <-time.After(5 * time.Second):
			t.Fatal("timeout leyendo evento SSE")
			return ""
		}
	}

	// Evento inicial: estado completo (idle, sin updating).
	ev1 := readEvent()
	if !strings.Contains(ev1, "event: update") {
		t.Fatalf("evento inicial: %q", ev1)
	}

	// Disparar el apply y observar el evento del paso por el stream.
	req2, _ := http.NewRequestWithContext(ctx, "POST", ts.URL+"/api/update/apply", nil)
	req2.Header.Set("Cookie", "session="+cookie)
	res2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	io.Copy(io.Discard, res2.Body)
	res2.Body.Close()
	if res2.StatusCode != 202 {
		t.Fatalf("apply status: %d", res2.StatusCode)
	}

	// El apply puede emitir varios eventos seguidos (start → fetch): leer
	// hasta el del paso fetch (con su peso #280).
	deadline := time.Now().Add(5 * time.Second)
	for {
		ev := readEvent()
		if strings.Contains(ev, `"step":"fetch"`) {
			if !strings.Contains(ev, `"progress":12`) {
				t.Fatalf("progreso de fetch: %q", ev)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sin evento fetch: %q", ev)
		}
	}
	cancel()
}
