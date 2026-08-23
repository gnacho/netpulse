// pairing_test.go — Fase 9 R3: tests del protocolo de pairing.
package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// pairResult es la respuesta de POST /api/agents/pair.
type pairResult struct {
	Slug     string `json:"slug"`
	Token    string `json:"token"`
	ServerFP string `json:"server_fp"`
}

func getPairingToken(t *testing.T, ts *agentTestServer) string {
	t.Helper()
	req, _ := http.NewRequest("GET", ts.URL+"/api/pairing/token", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: ts.cookie})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET pairing/token: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("pairing token status: %d", res.StatusCode)
	}
	var body struct {
		Token string `json:"token"`
	}
	json.NewDecoder(res.Body).Decode(&body)
	if body.Token == "" {
		t.Fatal("pairing token vacío")
	}
	return body.Token
}

func pair(t *testing.T, ts *agentTestServer, pairingToken, slug string) (int, *pairResult) {
	t.Helper()
	body := fmt.Sprintf(`{"pairing_token":%q,"slug":%q}`, pairingToken, slug)
	res, err := http.Post(ts.URL+"/api/agents/pair", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("pair: %v", err)
	}
	defer res.Body.Close()
	var pr pairResult
	json.NewDecoder(res.Body).Decode(&pr)
	return res.StatusCode, &pr
}

func TestPairCreatesAgentAndReturnsToken(t *testing.T) {
	ts := makeAgentTestServer(t)
	pairTok := getPairingToken(t, ts)

	status, pr := pair(t, ts, pairTok, "patio")
	if status != 201 {
		t.Fatalf("pair status: %d", status)
	}
	if pr.Slug != "patio" || pr.Token == "" {
		t.Fatalf("pair response: %+v", pr)
	}

	// El token devuelto debe funcionar para ingesta.
	res := ingest(t, ts, pr.Token, "10.0.0.1", validPayload())
	defer res.Body.Close()
	if res.StatusCode != 202 && res.StatusCode != 204 {
		t.Fatalf("ingest tras pair falló: %d", res.StatusCode)
	}
}

func TestPairRejectsInvalidToken(t *testing.T) {
	ts := makeAgentTestServer(t)
	status, _ := pair(t, ts, "token-falso-no-existe", "bad-ap")
	if status != 401 {
		t.Fatalf("expected 401, got %d", status)
	}
}

func TestPairRejectsInvalidSlug(t *testing.T) {
	ts := makeAgentTestServer(t)
	pairTok := getPairingToken(t, ts)
	// Slug con mayúsculas (inválido: regex ^[a-z0-9][a-z0-9-]{0,63}$).
	status, _ := pair(t, ts, pairTok, "BAD")
	if status != 400 {
		t.Fatalf("expected 400 for bad slug, got %d", status)
	}
}

func TestPairRequiresTokenField(t *testing.T) {
	ts := makeAgentTestServer(t)
	body := `{"slug":"test-ap"}`
	res, err := http.Post(ts.URL+"/api/agents/pair", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("pair: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatalf("expected 400 without pairing_token, got %d", res.StatusCode)
	}
}

func TestPairingRotateChangesToken(t *testing.T) {
	ts := makeAgentTestServer(t)
	old := getPairingToken(t, ts)

	// Rotar.
	req, _ := http.NewRequest("POST", ts.URL+"/api/pairing/rotate", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: ts.cookie})
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("rotate status: %d", res.StatusCode)
	}
	var body struct {
		Token string `json:"token"`
	}
	json.NewDecoder(res.Body).Decode(&body)
	if body.Token == "" || body.Token == old {
		t.Fatalf("rotate should generate a new token: old=%s new=%s", old, body.Token)
	}

	// El token viejo ya no debe funcionar para pairing.
	status, _ := pair(t, ts, old, "post-rotate")
	if status != 401 {
		t.Fatalf("old pairing token should be invalid after rotate, got %d", status)
	}

	// El token nuevo sí funciona.
	status2, _ := pair(t, ts, body.Token, "post-rotate")
	if status2 != 201 {
		t.Fatalf("new pairing token should work, got %d", status2)
	}
}

func TestPairingTokenRequiresAdmin(t *testing.T) {
	ts := makeAgentTestServer(t)
	// Sin cookie de sesión.
	res, err := http.Get(ts.URL + "/api/pairing/token")
	if err != nil {
		t.Fatalf("GET sin sesión: %v", err)
	}
	res.Body.Close()
	if res.StatusCode == 200 {
		t.Fatal("GET /api/pairing/token sin sesión debería fallar")
	}
}
