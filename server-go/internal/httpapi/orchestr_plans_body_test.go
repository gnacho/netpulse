package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/executor"
	"github.com/gnacho/netpulse/server-go/internal/orchestr"
)

// TestPlansCreateRespondsWithBody (#500): el POST /api/plans con diff
// explícito debe devolver el plan serializado (se vio un 201 con body vacío
// en producción que rompía a los clientes JSON).
func TestPlansCreateRespondsWithBody(t *testing.T) {
	ts := makeTestServer(t)
	defer ts.Close()
	cookie := adminCookie(t, ts)

	body := `{"routerId":"rt-test","resource":"wireless","diff":[{"kind":"uci_set","args":{"config":"wireless","section":"radio1","option":"channel","value":"1"},"desc":"channel"}]}`
	req, _ := http.NewRequest("POST", ts.URL+"/api/plans", strings.NewReader(body))
	req.Header.Set("Cookie", "session="+cookie)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post plans: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", res.StatusCode)
	}
	var plan map[string]any
	if err := json.NewDecoder(res.Body).Decode(&plan); err != nil {
		t.Fatalf("response body is not JSON (regression: empty body): %v", err)
	}
	if plan["id"] == nil || plan["status"] != "pending" {
		t.Fatalf("unexpected plan body: %+v", plan)
	}
}

// TestPlanMarshalDirect aísla el problema de serialización: un Plan con el
// diff que manda la UI del channel plan debe serializar sin error.
func TestPlanMarshalDirect(t *testing.T) {
	p := orchestr.Plan{
		ID:       "p1",
		RouterID: "rt2",
		Resource: "wireless",
		Diff: []executor.Op{
			{Kind: "uci_set", Args: map[string]string{"config": "wireless", "section": "radio1", "option": "channel", "value": "1"}, Desc: "channel radio1 -> 1"},
			{Kind: "uci_commit", Args: map[string]string{"config": "wireless"}, Desc: "commit"},
			{Kind: "service", Args: map[string]string{"name": "network", "action": "restart"}, Desc: "reload"},
		},
		Status:    "pending",
		CreatedBy: "admin",
		CreatedAt: time.Now().UnixMilli(),
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	t.Logf("plan json: %.200s", b)
}
