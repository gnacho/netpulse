package httpapi

// FORK: tests for the live server fingerprint (Deps.ServerFPFunc).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Pairing hands agents the key the server serves now, not the one it served
// at start-up: a user-supplied certificate can change key on renewal.
func TestPairingHandsOutTheLiveFingerprint(t *testing.T) {
	s := panelTestServer(t)
	s.serverFP = "stale-static-value"
	current := "aa11"
	s.serverFPFunc = func() string { return current }

	read := func() string {
		rec := httptest.NewRecorder()
		s.handlePairingToken(rec, httptest.NewRequest(http.MethodGet, "/api/pairing/token", nil))
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v (%s)", err, rec.Body.String())
		}
		return body["server_fp"]
	}
	if got := read(); got != "aa11" {
		t.Fatalf("server_fp = %q, want the live value", got)
	}
	current = "bb22"
	if got := read(); got != "bb22" {
		t.Fatalf("server_fp = %q after a renewal, want the new key", got)
	}
}

// Without a live source the static value is used, as before.
func TestPairingFallsBackToTheStaticFingerprint(t *testing.T) {
	s := panelTestServer(t)
	s.serverFP = "cc33"
	if got := s.fingerprint(); got != "cc33" {
		t.Fatalf("fingerprint = %q, want the static value", got)
	}
}
