package httpapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func TestValidFirmwareID(t *testing.T) {
	valid := []string{"rt1", "router-1", "a.b", "file_1"}
	for _, id := range valid {
		if !validFirmwareID(id) {
			t.Errorf("validFirmwareID(%q) = false", id)
		}
	}
	invalid := []string{"", ".", "..", "../x", `..\x`, "a/b", `a\b`, "v1..2", "/abs"}
	for _, id := range invalid {
		if validFirmwareID(id) {
			t.Errorf("validFirmwareID(%q) = true", id)
		}
	}
}

func TestFirmwareUploadPathStaysInsideDir(t *testing.T) {
	dir := t.TempDir()
	path, ok := firmwareUploadPath(dir, "rt1")
	if !ok || path != filepath.Join(dir, "rt1.bin") {
		t.Fatalf("firmwareUploadPath valid = (%q, %v)", path, ok)
	}
	for _, id := range []string{"../escape", `..\escape`, "..", "/etc/passwd"} {
		if path, ok := firmwareUploadPath(dir, id); ok {
			t.Errorf("firmwareUploadPath(%q) = (%q, true)", id, path)
		}
	}
}

func newExecutorTokenServer(t *testing.T) (*server, string) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	token := "agent-secret"
	if _, err := d.Exec(
		`INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		agentTokenKey("rt1"), hashAgentToken(token),
	); err != nil {
		t.Fatalf("seed agent token: %v", err)
	}
	return &server{db: d}, token
}

func executorTokenRequest(token, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/agents/executor-token", strings.NewReader(body))
	r.Header.Set("X-Router-ID", "rt1")
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

func TestHandleAgentExecutorTokenLimits(t *testing.T) {
	s, agentToken := newExecutorTokenServer(t)

	for _, tc := range []struct {
		name string
		body string
		want int
	}{
		{"valid", `{"token":"exec-token"}`, http.StatusOK},
		{"empty", `{"token":""}`, http.StatusBadRequest},
		{"too long", `{"token":"` + strings.Repeat("a", 513) + `"}`, http.StatusBadRequest},
		{"body too large", `{"token":"` + strings.Repeat("a", maxBodyBytes+1) + `"}`, http.StatusRequestEntityTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.handleAgentExecutorToken(rec, executorTokenRequest(agentToken, tc.body))
			if rec.Code != tc.want {
				t.Fatalf("got %d want %d (body %s)", rec.Code, tc.want, rec.Body.String())
			}
		})
	}

	var stored string
	if err := s.db.QueryRow(`SELECT value FROM kv WHERE key = ?`, "netgrip.executor_token.rt1").Scan(&stored); err != nil {
		t.Fatalf("read stored token: %v", err)
	}
	if stored != "exec-token" {
		t.Fatalf("stored token = %q", stored)
	}
}

func TestHandleAgentExecutorTokenRejectsBadAuth(t *testing.T) {
	s, _ := newExecutorTokenServer(t)
	rec := httptest.NewRecorder()
	s.handleAgentExecutorToken(rec, executorTokenRequest("wrong", `{"token":"exec-token"}`))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d want 401", rec.Code)
	}
}
