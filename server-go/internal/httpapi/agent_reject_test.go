package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestIngestRechazoBadgeYLimpiame (#1717): un push con token invalido deja
// huella (badge en GET /api/agents) y el primer push valido la limpia.
func TestIngestRechazoBadgeYLimpieza(t *testing.T) {
	ts := makeAgentTestServer(t)
	status, token, _ := createAgentToken(t, ts, "patio")
	if status != 201 || token == "" {
		t.Fatalf("create: %d", status)
	}

	// Push con token incorrecto -> 401 y kv agent.reject_ts.patio.
	res := ingest(t, ts, "deadbeef", "10.9.0.1", validPayload())
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("token malo: %d", res.StatusCode)
	}
	var rej string
	if err := ts.db.QueryRow("SELECT value FROM kv WHERE key = 'agent.reject_ts.patio'").Scan(&rej); err != nil {
		t.Fatalf("kv de rechazo no creado: %v", err)
	}
	if rej == "" || rej == "0" {
		t.Fatalf("kv de rechazo vacío: %q", rej)
	}

	// La lista de agentes lo expone como rejectedAt.
	var list1 struct {
		Agents []struct {
			Slug       string `json:"slug"`
			RejectedAt int64  `json:"rejectedAt"`
		} `json:"agents"`
	}
	getAgents := func() (int, []byte) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/agents", nil)
		req.Header.Set("Cookie", "session="+ts.cookie)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET /api/agents: %v", err)
		}
		defer res.Body.Close()
		body := new(strings.Builder)
		buf := make([]byte, 32*1024)
		for {
			n, rerr := res.Body.Read(buf)
			body.Write(buf[:n])
			if rerr != nil {
				break
			}
		}
		return res.StatusCode, []byte(body.String())
	}
	st, raw := getAgents()
	if st != 200 {
		t.Fatalf("GET /api/agents: %d", st)
	}
	if err := json.Unmarshal(raw, &list1); err != nil {
		t.Fatalf("decode agents: %v (%.120s)", err, raw)
	}
	found := int64(0)
	for _, it := range list1.Agents {
		if it.Slug == "patio" {
			found = it.RejectedAt
		}
	}
	if found == 0 {
		t.Fatalf("GET /api/agents no expone rejectedAt para patio")
	}

	// Firma invalida (token valido, HMAC mal) tambien deja huella.
	req, _ := http.NewRequest("POST", ts.URL+"/api/ingest/agent", strings.NewReader(validPayload()))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "10.9.0.2")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Agent-Signature", "caffee")
	res2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("ingest firma mala: %v", err)
	}
	res2.Body.Close()
	if res2.StatusCode != 401 {
		t.Fatalf("firma mala: %d", res2.StatusCode)
	}

	// Push valido -> 202, kv limpio y rejectedAt fuera de la lista.
	res3 := ingest(t, ts, token, "10.9.0.3", validPayload())
	res3.Body.Close()
	if res3.StatusCode != 202 {
		t.Fatalf("push válido: %d", res3.StatusCode)
	}
	if err := ts.db.QueryRow("SELECT value FROM kv WHERE key = 'agent.reject_ts.patio'").Scan(&rej); err == nil {
		t.Fatalf("kv de rechazo debería haberse limpiado, queda %q", rej)
	}
	st2, raw2 := getAgents()
	if st2 != 200 {
		t.Fatalf("GET /api/agents (2): %d", st2)
	}
	var list2 struct {
		Agents []struct {
			Slug       string `json:"slug"`
			RejectedAt int64  `json:"rejectedAt"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(raw2, &list2); err != nil {
		t.Fatalf("decode agents 2: %v (%.120s)", err, raw2)
	}
	for _, it := range list2.Agents {
		if it.Slug == "patio" && it.RejectedAt != 0 {
			t.Fatalf("rejectedAt debería ser 0 tras un push válido: %d", it.RejectedAt)
		}
	}
}
