// agent_reject_reason_2211_test.go — #2211: el 401 llevaba solo un
// timestamp, así que un reloj desfasado (stale_payload) o un agente viejo
// (invalid_signature) se veían igual que un token muerto. El motivo se guarda
// al lado del timestamp, se expone en la lista de agentes y el primer push
// válido limpia los dos.
package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func rejectReasonFor(t *testing.T, ts *agentTestServer, slug string) string {
	t.Helper()
	var v string
	err := ts.db.QueryRow("SELECT value FROM kv WHERE key = 'agent.reject_reason.'||? ", slug).Scan(&v)
	if err != nil {
		return ""
	}
	return v
}

func agentsRejectReason(t *testing.T, ts *agentTestServer) (int64, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", ts.URL+"/api/agents", nil)
	req.Header.Set("Cookie", "session="+ts.cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/agents: %v", err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var list struct {
		Agents []struct {
			Slug           string `json:"slug"`
			RejectedAt     int64  `json:"rejectedAt"`
			RejectedReason string `json:"rejectedReason"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("decode agents: %v (%.120s)", err, raw)
	}
	for _, it := range list.Agents {
		if it.Slug == "patio" {
			return it.RejectedAt, it.RejectedReason
		}
	}
	return 0, ""
}

func TestIngestRejectReasonPerCause(t *testing.T) {
	ts := makeAgentTestServer(t)
	status, token, _ := createAgentToken(t, ts, "patio")
	if status != 201 || token == "" {
		t.Fatalf("create: %d", status)
	}

	cases := []struct {
		name string
		do   func()
		want string
	}{
		{
			name: "token que no cuadra",
			do: func() {
				res := ingest(t, ts, "deadbeef", "10.9.1.1", validPayload())
				res.Body.Close()
			},
			want: "unauthorized",
		},
		{
			name: "firma que no cuadra",
			do: func() {
				req, _ := http.NewRequest("POST", ts.URL+"/api/ingest/agent", strings.NewReader(validPayload()))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Forwarded-For", "10.9.1.2")
				req.Header.Set("Authorization", "Bearer "+token)
				req.Header.Set("X-Agent-Signature", "caffee")
				res, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatalf("firma mala: %v", err)
				}
				res.Body.Close()
			},
			want: "invalid_signature",
		},
		{
			name: "payload fuera de la ventana de frescura",
			do: func() {
				old := time.Now().Add(-10 * time.Minute).Unix()
				res := ingest(t, ts, token, "10.9.1.3", validPayload(old))
				res.Body.Close()
			},
			want: "stale_payload",
		},
	}

	for _, tc := range cases {
		tc.do()
		if got := rejectReasonFor(t, ts, "patio"); got != tc.want {
			t.Fatalf("%s: motivo guardado %q, quería %q", tc.name, got, tc.want)
		}
		at, reason := agentsRejectReason(t, ts)
		if at == 0 || reason != tc.want {
			t.Fatalf("%s: la lista expone rejectedAt=%d rejectedReason=%q", tc.name, at, reason)
		}
	}

	// El primer push válido limpia timestamp y motivo.
	res := ingest(t, ts, token, "10.9.1.4", validPayload())
	res.Body.Close()
	if res.StatusCode != 202 {
		t.Fatalf("push válido: %d", res.StatusCode)
	}
	if got := rejectReasonFor(t, ts, "patio"); got != "" {
		t.Fatalf("el motivo debe borrarse con un push válido, queda %q", got)
	}
	if at, reason := agentsRejectReason(t, ts); at != 0 || reason != "" {
		t.Fatalf("rejectedAt=%d rejectedReason=%q tras push válido", at, reason)
	}
}
