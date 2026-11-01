package httpapi_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/agentinstall"
)

// #1751: el instalador se sirve desde el propio servidor, sin sesión ni token
// (el script no lleva secretos: el host y el token van en los argumentos del
// one-liner) y byte a byte igual al que se embebe desde el repo.
func TestAgentInstallScriptServedFromServer(t *testing.T) {
	ts := makeAgentTestServer(t)
	res, err := http.Get(ts.URL + "/api/agents/install-agent.sh")
	if err != nil {
		t.Fatalf("GET install-agent.sh: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, quiero 200", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.Contains(ct, "shellscript") {
		t.Fatalf("content-type = %q, quiero text/x-shellscript", ct)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("leer body: %v", err)
	}
	if !strings.HasPrefix(string(body), "#!/bin/sh") {
		head := string(body)
		if len(head) > 40 {
			head = head[:40]
		}
		t.Fatalf("el cuerpo no parece el instalador: %q", head)
	}
	if string(body) != string(agentinstall.Script()) {
		t.Fatalf("el servidor debe servir el script embebido (%d bytes), recibidos %d",
			len(agentinstall.Script()), len(body))
	}
}
