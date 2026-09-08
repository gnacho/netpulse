package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// postAgentCreateHot publica POST /api/agents con body dado y devuelve la
// respuesta decodificada.
func postAgentCreateHot(t *testing.T, base, cookie, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", base+"/api/agents", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "session="+cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/agents: %v", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return res.StatusCode, m
}

// TestRotateTokenHot: con hot:true y un router OpenWrt accesible por SSH,
// el token se aplica en caliente (método hot + 1 comando SSH de push).
func TestRotateTokenHot(t *testing.T) {
	ssh := &fakeSSH{}
	ts := makeRearmTestServer(t, ssh, 1500*time.Millisecond)

	status, body := postAgentCreateHot(t, ts.URL, ts.cookie, `{"slug":"patio","hot":true}`)
	if status != 201 {
		t.Fatalf("create: %d", status)
	}
	if body["method"] != "hot" {
		t.Fatalf("quiero method=hot, tuve %v", body["method"])
	}
	if body["token"] == "" {
		t.Fatal("sin token")
	}
	if ssh.count() != 1 {
		t.Fatalf("quiero 1 comando SSH de push, tuve %d", ssh.count())
	}
	if !strings.Contains(ssh.cmds[0], "NETPULSE_TOKEN=") || !strings.Contains(ssh.cmds[0], `"$INIT" restart`) {
		t.Fatalf("el comando SSH no reescribe el token/restart: %q", ssh.cmds[0])
	}
}

// TestRotateTokenManual: sin router en la tabla (o hot=false) no hay push
// SSH: método manual y el token se conserva en kv con el one-liner.
func TestRotateTokenManual(t *testing.T) {
	ssh := &fakeSSH{}
	ts := makeRearmTestServer(t, ssh, 1500*time.Millisecond)

	// Slug sin router asociado → manual, sin SSH.
	status, body := postAgentCreateHot(t, ts.URL, ts.cookie, `{"slug":"solitario","hot":true}`)
	if status != 201 {
		t.Fatalf("create: %d", status)
	}
	if body["method"] != "manual" {
		t.Fatalf("quiero method=manual, tuve %v", body["method"])
	}
	if ssh.count() != 0 {
		t.Fatalf("no debe ejecutarse SSH con router desconocido, tuve %d", ssh.count())
	}
	if body["install"] == "" {
		t.Fatal("manual debe incluir el one-liner de instalación")
	}

	// hot=false → manual aunque el router exista (comportamiento previo).
	status, body = postAgentCreateHot(t, ts.URL, ts.cookie, `{"slug":"patio"}`)
	if status != 201 {
		t.Fatalf("create: %d", status)
	}
	if body["method"] != "manual" {
		t.Fatalf("sin hot=true quiero method=manual, tuve %v", body["method"])
	}
	if ssh.count() != 0 {
		t.Fatalf("sin hot=true no debe ejecutarse SSH, tuve %d", ssh.count())
	}
}

// TestRotateTokenHotSSHFail: si el push SSH falla, no se rompe el flujo y el
// método cae a manual (el token ya rotado se entrega vía Install).
func TestRotateTokenHotSSHFail(t *testing.T) {
	ssh := &fakeSSH{fail: true}
	ts := makeRearmTestServer(t, ssh, 1500*time.Millisecond)

	status, body := postAgentCreateHot(t, ts.URL, ts.cookie, `{"slug":"patio","hot":true}`)
	if status != 201 {
		t.Fatalf("create: %d", status)
	}
	if body["method"] != "manual" {
		t.Fatalf("SSH caído quiero method=manual, tuve %v", body["method"])
	}
	if body["install"] == "" {
		t.Fatal("debe incluir el one-liner para recuperar manualmente")
	}
}
