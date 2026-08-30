// config_backup_test.go — tests del endpoint /api/config-backup (#340).
package httpapi_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"testing"
)

func setAgentToken(t *testing.T, ts *testServer, slug, token string) {
	t.Helper()
	sum := sha256.Sum256([]byte(token))
	_, err := ts.db.Exec(
		"INSERT INTO kv (key, value) VALUES (?, ?)",
		"agent.token."+slug, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatalf("insert agent token: %v", err)
	}
}

func TestConfigBackupUploadRequiresAgentToken(t *testing.T) {
	ts := makeTestServer(t)

	req, _ := http.NewRequest("POST", ts.URL+"/api/config-backup", bytes.NewReader([]byte("fake gzip")))
	req.Header.Set("Content-Type", "application/gzip")
	req.Header.Set("X-Router-ID", "flint2")
	req.Header.Set("X-Snapshot-ID", "snap-1")
	req.Header.Set("X-Configs", "network,firewall")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("sin token: esperaba 401, got %d", res.StatusCode)
	}
}

func TestConfigBackupUploadAcceptsValidAgentToken(t *testing.T) {
	ts := makeTestServer(t)
	setAgentToken(t, ts, "flint2", "secret-token-123")

	body := []byte("fake gzip payload")
	req, _ := http.NewRequest("POST", ts.URL+"/api/config-backup", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/gzip")
	req.Header.Set("X-Router-ID", "flint2")
	req.Header.Set("X-Snapshot-ID", "snap-1")
	req.Header.Set("X-Configs", "network,firewall")
	req.Header.Set("Authorization", "Bearer secret-token-123")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("con token válido: esperaba 200, got %d: %s", res.StatusCode, string(b))
	}

	// El admin puede listarlo.
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	req2, _ := http.NewRequest("GET", ts.URL+"/api/config-backup?router=flint2", nil)
	req2.Header.Set("Cookie", "session="+cookie)
	res2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer res2.Body.Close()
	if res2.StatusCode != http.StatusOK {
		t.Fatalf("list admin: esperaba 200, got %d", res2.StatusCode)
	}
}

func TestConfigBackupListRequiresAdmin(t *testing.T) {
	ts := makeTestServer(t)

	res, err := http.Get(ts.URL + "/api/config-backup")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("list sin sesión: esperaba 401, got %d", res.StatusCode)
	}
}
