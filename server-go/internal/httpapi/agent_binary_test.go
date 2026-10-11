// agent_binary_test.go — #1367: el endpoint de binario del agente no debe
// defaultear a arm64 en silencio cuando falta ?arch=, y el one-liner de
// instalación no debe pre-descargar el binario (la detección de arquitectura
// la hace install-agent.sh contra el router, que es quien la sabe).
package httpapi_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/agentbin"
)

// getBinary hace GET /api/agents/<slug>/binary autenticado con el token del
// agente; query se añade tal cual ("" o "?arch=mipsle").
func getBinary(t *testing.T, ts *agentTestServer, slug, token, query string) *http.Response {
	t.Helper()
	url := ts.URL + "/api/agents/" + slug + "/binary" + query
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return res
}

func createTokenFor(t *testing.T, ts *agentTestServer, slug string) string {
	t.Helper()
	status, token, _ := createAgentToken(t, ts, slug)
	if status != 201 {
		t.Fatalf("create token %s: %d", slug, status)
	}
	return token
}

// #1367: sin ?arch= el server servía arm64 en silencio; un router MIPS que
// instalara con el one-liner recibía un binario aarch64 (exec format error).
// El default silencioso es la causa del reporte: ahora se exige la arch.
func TestAgentBinaryRequiresArch(t *testing.T) {
	ts := makeAgentTestServer(t)
	token := createTokenFor(t, ts, "mipsbox")
	res := getBinary(t, ts, "mipsbox", token, "")
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("sin ?arch= quiero 400 pidiendo la arch, tengo %d: %s", res.StatusCode, body)
	}
}

// Con ?arch= explícita el binario servido debe ser el de ESA arquitectura
// (Content-Disposition y sha256 presentes), no el default.
func TestAgentBinaryServesRequestedArch(t *testing.T) {
	if !agentbin.HasBinaries() {
		t.Skip("sin binarios embebidos (árbol dev sin agentbin/agents)")
	}
	ts := makeAgentTestServer(t)
	token := createTokenFor(t, ts, "mipsbox")
	res := getBinary(t, ts, "mipsbox", token, "?arch=mipsle")
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("?arch=mipsle quiero 200, tengo %d: %s", res.StatusCode, body)
	}
	if cd := res.Header.Get("Content-Disposition"); !strings.Contains(cd, "mipsle") {
		t.Fatalf("Content-Disposition = %q, quiero mipsle", cd)
	}
	if res.Header.Get("X-Checksum-Sha256") == "" {
		t.Fatal("falta la cabecera X-Checksum-Sha256")
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(res.Body, head); err != nil {
		t.Fatalf("leer body: %v", err)
	}
	if string(head) != "\x7fELF" {
		t.Fatalf("el body no es un ELF: %x", head)
	}
}

// Arch desconocida: 404 (comportamiento preexistente, se fija como contrato).
func TestAgentBinaryUnknownArch(t *testing.T) {
	ts := makeAgentTestServer(t)
	token := createTokenFor(t, ts, "mipsbox")
	res := getBinary(t, ts, "mipsbox", token, "?arch=sparc")
	defer res.Body.Close()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("?arch=sparc quiero 404, tengo %d", res.StatusCode)
	}
}

// #1367: el one-liner NO debe pre-descargar el binario (sin arch no puede
// acertar en MIPS); debe delegar en install-agent.sh, que detecta la arch
// del router por SSH y descarga del propio server con ?arch=.
func TestInstallLineDelegatesBinaryToInstaller(t *testing.T) {
	ts := makeAgentTestServer(t)
	_, _, install := createAgentToken(t, ts, "rt1")
	if strings.Contains(install, "/api/agents/") {
		t.Fatalf("el one-liner no debe descargar el binario: %s", install)
	}
	if !strings.Contains(install, "install-agent.sh") {
		t.Fatalf("el one-liner debe invocar install-agent.sh: %s", install)
	}
	for _, want := range []string{"--slug=rt1", "--token=", "--server="} {
		if !strings.Contains(install, want) {
			t.Fatalf("falta %s en el one-liner: %s", want, install)
		}
	}
}
