// backup_api_test.go — #1462: snapshot standalone de la config por SSE.
// Contrato de POST /api/agents/{slug}/backup (admin): igual de auth que el
// upgrade, pero a diferencia de este NO se encola cuando el agente no está
// conectado (409): un backup no es imperativo.
package httpapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func postBackup(t *testing.T, ts *upgradeTestServer, slug, cookie string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", ts.URL+"/api/agents/"+slug+"/backup", nil)
	if cookie != "" {
		req.Header.Set("Cookie", "session="+cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /backup: %v", err)
	}
	defer res.Body.Close()
	var body map[string]any
	_ = json.NewDecoder(res.Body).Decode(&body)
	return res.StatusCode, body
}

// openStreamRec igual que openStream pero devuelve el recorder para leer el
// evento enviado al stream.
func openStreamRec(t *testing.T, ts *upgradeTestServer, slug, token string) (*httptest.ResponseRecorder, context.CancelFunc, chan struct{}) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, ts.URL+"/api/agents/"+slug+"/stream", nil).WithContext(ctx)
	req.SetPathValue("slug", slug)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		ts.hub.HandleStream(rec, req)
		close(done)
	}()
	time.Sleep(200 * time.Millisecond)
	return rec, cancel, done
}

func TestBackupSinSesion401(t *testing.T) {
	ts := makeUpgradeTestServer(t)
	status, body := postBackup(t, ts, "patio", "")
	if status != 401 || body["error"] != "unauthorized" {
		t.Fatalf("sin sesión: got %d %v", status, body)
	}
}

func TestBackupSlugDesconocido404(t *testing.T) {
	ts := makeUpgradeTestServer(t)
	status, body := postBackup(t, ts, "noexiste", ts.cookie)
	if status != 404 || body["error"] != "not_found" {
		t.Fatalf("slug desconocido: got %d %v", status, body)
	}
}

// TestBackupNoConectado409: a diferencia del upgrade (#284) el backup NO se
// encola: si el agente no está conectado por SSE se rechaza con 409.
func TestBackupNoConectado409(t *testing.T) {
	ts := makeUpgradeTestServer(t)
	st, tok := createUpgradeToken(t, ts, "patio")
	if st != 201 || tok == "" {
		t.Fatalf("create: %d", st)
	}
	status, body := postBackup(t, ts, "patio", ts.cookie)
	if status != 409 || body["error"] != "not_connected" {
		t.Fatalf("no conectado: got %d %v", status, body)
	}
}

// TestBackupEnviado202: con el stream conectado el comando sale (202) y el
// evento SSE "config_backup" llega al agente.
func TestBackupEnviado202(t *testing.T) {
	ts := makeUpgradeTestServer(t)
	st, tok := createUpgradeToken(t, ts, "patio")
	if st != 201 || tok == "" {
		t.Fatalf("create: %d", st)
	}
	rec, cancel, done := openStreamRec(t, ts, "patio", tok)
	defer func() { cancel(); <-done }()

	status, body := postBackup(t, ts, "patio", ts.cookie)
	if status != 202 || body["status"] != "sent" {
		t.Fatalf("enviado: got %d %v", status, body)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(rec.Body.String(), "config_backup") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	data, _ := io.ReadAll(rec.Result().Body)
	t.Fatalf("evento config_backup no llegó al stream; body: %q", string(data))
}
