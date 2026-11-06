package httpapi

// #1894: la salida del script de reinstalación vuelve en la respuesta (éxito y
// fallo) en vez de perderse. Antes el handler la descartaba con `_` y el
// operador solo veía "exit status 1" en rojo durante unos segundos.

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
)

type reinstallFakePool struct {
	out string
	err error
}

func (p *reinstallFakePool) Run(host, cmd string, timeout time.Duration) (string, error) {
	return p.out, p.err
}

func makeReinstallOutputServer(t *testing.T, out string, runErr error) (*server, string) {
	t.Helper()
	dataDir := t.TempDir()
	cfg, err := config.Load(map[string]string{
		"AUTH_USER": "admin", "AUTH_PASS": "test123456",
		"DEMO_MODE": "0", "DATA_DIR": dataDir, "NODE_ENV": "test",
	}, dataDir)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	d, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	r, err := routerstore.AddRouter(d.DB, routerstore.AddInput{
		Name: "RecRouter", Host: "192.168.1.60", Type: "openwrt",
	})
	if err != nil {
		t.Fatalf("add router: %v", err)
	}
	return &server{cfg: cfg, db: d, pool: &reinstallFakePool{out: out, err: runErr}}, r.ID
}

func TestReinstallFailureReturnsScriptOutput(t *testing.T) {
	out := "descargando binario\ncurl: (7) Failed to connect\nno space left on device"
	s, id := makeReinstallOutputServer(t, out, errors.New("exit status 1"))
	req := httptest.NewRequest("POST", "/api/agents/"+id+"/reinstall", nil)
	req.SetPathValue("slug", id)
	rec := httptest.NewRecorder()
	s.handleAgentReinstall(rec, req)
	if rec.Code != 502 {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error   string `json:"error"`
		Message string `json:"message"`
		Output  string `json:"output"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	if body.Error != "ssh_failed" || !strings.Contains(body.Message, "exit status 1") {
		t.Fatalf("error/message inesperados: %+v", body)
	}
	if !strings.Contains(body.Output, "no space left on device") {
		t.Fatalf("la salida del script no llegó al operador: %+v", body)
	}
}

func TestReinstallSuccessReturnsScriptOutput(t *testing.T) {
	s, id := makeReinstallOutputServer(t, "instalando netpulse-agent\nagente iniciado", nil)
	req := httptest.NewRequest("POST", "/api/agents/"+id+"/reinstall", nil)
	req.SetPathValue("slug", id)
	rec := httptest.NewRecorder()
	s.handleAgentReinstall(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Installed bool   `json:"installed"`
		Output    string `json:"output"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("json: %v", err)
	}
	if !body.Installed || !strings.Contains(body.Output, "agente iniciado") {
		t.Fatalf("respuesta sin la salida del script: %+v", body)
	}
}

func TestOutputTailKeepsTheEnd(t *testing.T) {
	if got := outputTail("  hola\n", 10); got != "hola" {
		t.Fatalf("recorte corto = %q", got)
	}
	long := strings.Repeat("a", 100) + "FIN"
	got := outputTail(long, 3)
	if got != "…FIN" {
		t.Fatalf("recorte largo = %q", got)
	}
}
