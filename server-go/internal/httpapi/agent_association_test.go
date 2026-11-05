// agent_association_test.go — #1874: un push event-driven (wireless-only, sin
// sección system) no debe perder la asociación agente↔router. El registry
// retiene la identidad (#852) y GET /api/agents tiene que usarla; sin eso el
// router se quedaba sin routerId y la UI pintaba "agente no instalado" con el
// agente vivo.
package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

type agentListEntry struct {
	Slug     string `json:"slug"`
	RouterID string `json:"routerId"`
	Hostname string `json:"hostname"`
	Fresh    bool   `json:"fresh"`
}

func getAgents(t *testing.T, ts *agentTestServer) []agentListEntry {
	t.Helper()
	req, _ := http.NewRequest("GET", ts.URL+"/api/agents", nil)
	req.Header.Set("Cookie", "session="+ts.cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/agents: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/agents: %d", res.StatusCode)
	}
	var body struct {
		Agents []agentListEntry `json:"agents"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode /api/agents: %v", err)
	}
	return body.Agents
}

func findAgentEntry(list []agentListEntry, slug string) *agentListEntry {
	for i := range list {
		if list[i].Slug == slug {
			return &list[i]
		}
	}
	return nil
}

func TestAgentAssociationSurvivesWirelessOnlyPush(t *testing.T) {
	ts := makeAgentTestServer(t)
	now := time.Now().Unix()
	// Router cuyo id NO coincide con el slug del agente, como en la flota
	// real (id gl-inet-gl-mt6000, agente flint2): la asociación solo puede
	// salir de la MAC del bridge o del hostname del board.
	if _, err := ts.db.Exec(
		`INSERT INTO routers (id, name, host, type, is_gateway, created_at) VALUES (?,?,?,?,?,?)`,
		"gl-inet-gl-mt6000", "GL.iNet GL-MT6000", "192.168.10.1", "glinet", 1, now*1000,
	); err != nil {
		t.Fatalf("insert router: %v", err)
	}
	if _, err := ts.db.Exec(
		`UPDATE routers SET agent_only = 1, mac = ? WHERE id = ?`,
		"94:83:C4:A4:FB:71", "gl-inet-gl-mt6000",
	); err != nil {
		t.Fatalf("update router: %v", err)
	}
	_, token, _ := createAgentToken(t, ts, "flint2")

	full := fmt.Sprintf(`{"router":"flint2","ts":%d,"version":"0.1.0","data":{"system":{"bridgeMac":"94:83:C4:A4:FB:71","board":{"hostname":"Flint2","model":"GL.iNet GL-MT6000"},"cpu":7},"wireless":{"clients":{}}}}`, now)
	res := ingest(t, ts, token, "10.0.9.1", full)
	res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("ingest full: %d", res.StatusCode)
	}
	entry := findAgentEntry(getAgents(t, ts), "flint2")
	if entry == nil || entry.RouterID != "gl-inet-gl-mt6000" || entry.Hostname != "flint2" {
		t.Fatalf("asociación tras push completo: %+v", entry)
	}

	// Push de evento nl80211: mismo contrato que en producción, sin system.
	wireless := fmt.Sprintf(`{"router":"flint2","ts":%d,"version":"0.1.0","data":{"arp":{},"dhcp":{"leases":[]},"wireless":{"clients":{}}}}`, now+1)
	res = ingest(t, ts, token, "10.0.9.1", wireless)
	res.Body.Close()
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("ingest wireless: %d", res.StatusCode)
	}
	entry = findAgentEntry(getAgents(t, ts), "flint2")
	if entry == nil {
		t.Fatal("el agente desapareció del listado")
	}
	if entry.RouterID != "gl-inet-gl-mt6000" {
		t.Fatalf("el push wireless-only perdió la asociación: routerId=%q", entry.RouterID)
	}
	if entry.Hostname != "flint2" {
		t.Fatalf("hostname retenido = %q, esperaba flint2", entry.Hostname)
	}
	if !entry.Fresh {
		t.Fatalf("el agente debería seguir fresco: %+v", entry)
	}
}
