package httpapi

// #1899: la subida de un backup por agente deja una línea en el journal con
// el nombre del archivo, igual que la vía SSH. Sin ella, un backup manual o
// programado de una unidad con agente no dejaba rastro en el servidor.

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/firmware"
)

func TestAgentBackupUploadLogsTheArchiveName(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	token := "agent-secret"
	if _, err := d.Exec(
		`INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		agentTokenKey("rt1"), hashAgentToken(token)); err != nil {
		t.Fatalf("token: %v", err)
	}
	s := &server{db: d, firmware: firmware.NewStore(d.DB)}
	mux := http.NewServeMux()
	s.registerFirmwareBackupRoutes(mux)

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	req := httptest.NewRequest(http.MethodPost, "/api/agents/rt1/firmware-backup", strings.NewReader("\x1f\x8btar-gz-de-prueba"))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Name == "" {
		t.Fatalf("respuesta: %s (%v)", rec.Body.String(), err)
	}
	line := buf.String()
	if !strings.Contains(line, resp.Name) || !strings.Contains(line, "rt1") {
		t.Fatalf("el journal no nombra el archivo %q: %q", resp.Name, line)
	}
}
