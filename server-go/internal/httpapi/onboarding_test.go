// onboarding_test.go — contrato del endpoint de onboarding (#772).
package httpapi_test

import (
	"io"
	"net/http"
	"testing"
)

// TestOnboardingDismiss silencia la alerta first-seen de una MAC: escribe el
// kv de memoria (#248) y responde ok; MAC inválida → 400.
func TestOnboardingDismiss(t *testing.T) {
	ts := makeDeviceActionsTestServer(t, nil)
	defer ts.Server.Close()

	mac := "AA:BB:CC:DD:EE:FF"
	resp := deviceReq(t, "POST", ts.Server.URL, "/api/onboarding/dismiss", ts.cookie,
		`{"mac":"`+mac+`"}`)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("POST dismiss: %d, want 200", resp.StatusCode)
	}
	io.Copy(io.Discard, resp.Body)

	// La MAC quedó persistida en kv como ya avisada.
	var v string
	if err := ts.db.QueryRow("SELECT value FROM kv WHERE key = ?", "unknown_alerted:"+mac).Scan(&v); err != nil {
		t.Fatalf("kv unknown_alerted no persistido: %v", err)
	}
	if v != "1" {
		t.Fatalf("kv value = %q, want 1", v)
	}

	// MAC inválida → 400.
	resp2 := deviceReq(t, "POST", ts.Server.URL, "/api/onboarding/dismiss", ts.cookie,
		`{"mac":"no-es-una-mac"}`)
	defer resp2.Body.Close()
	io.Copy(io.Discard, resp2.Body)
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("POST dismiss MAC inválida: %d, want 400", resp2.StatusCode)
	}
}
