package httpapi_test

import (
	"fmt"
	"testing"
	"time"
)

// Un pusher externo manda las MAC en minúsculas y con guiones: el push de
// agente las normaliza antes de Ingest, que ya no lo hace.
func TestAgentPushNormalizesMACs(t *testing.T) {
	ts := makeAgentTestServer(t)
	status, token, _ := createAgentToken(t, ts, "patio")
	if status != 201 || token == "" {
		t.Fatalf("create: %d token=%q", status, token)
	}
	payload := fmt.Sprintf(`{"router":"patio","ts":%d,"version":"0.1.0","data":{"system":{"bridgeMac":"aa-bb-cc-dd-ee-00"},"dhcp":{"leases":[{"mac":"aa-bb-cc-dd-ee-01","ip":"192.0.2.50"}]},"fdb":{"macs":{"aa:bb:cc:dd:ee:02":"lan1"}}}}`, time.Now().Unix())
	res := ingest(t, ts, token, "10.0.0.1", payload)
	res.Body.Close()
	if res.StatusCode != 202 {
		t.Fatalf("esperaba 202, obtuve %d", res.StatusCode)
	}
	p, ok := ts.agents.Fresh("patio")
	if !ok || p == nil || p.Data.System == nil || p.Data.DHCP == nil || p.Data.FDB == nil {
		t.Fatalf("esperaba el payload en el registry, obtuve %+v", p)
	}
	if got := p.Data.System.BridgeMAC; got != "AA:BB:CC:DD:EE:00" {
		t.Errorf("esperaba BridgeMAC canónica, obtuve %q", got)
	}
	if got := p.Data.DHCP.Leases[0].MAC; got != "AA:BB:CC:DD:EE:01" {
		t.Errorf("esperaba MAC de lease canónica, obtuve %q", got)
	}
	if _, ok := p.Data.FDB.MACs["AA:BB:CC:DD:EE:02"]; !ok || len(p.Data.FDB.MACs) != 1 {
		t.Errorf("esperaba la clave FDB canónica, obtuve %v", p.Data.FDB.MACs)
	}
}
