package httpapi

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

// Un estado persistido antes de #2134 con MAC en minúsculas y con guiones
// vuelve canónico al restaurarlo: Ingest ya no normaliza, y sin esto los
// overrides manuales no casaban hasta el siguiente push.
func TestStateRestorerNormalizesMACs(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer d.Close()
	if _, err := d.Exec("INSERT INTO kv (key, value) VALUES (?, ?)", agentStateKey("sw1"),
		`{"payload":{"router":"sw1","data":{"dhcp":{"leases":[{"mac":"aa-bb-cc-dd-ee-01","ip":"192.0.2.50"}]},"fdb":{"macs":{"aa:bb:cc:dd:ee:02":"lan1"}}}},"lastSeen":1,"version":"1.0.0","bridgeMAC":"aa-bb-cc-dd-ee-00"}`); err != nil {
		t.Fatalf("sembrar estado: %v", err)
	}

	reg := adapters.NewAgentRegistry(0)
	NewStateRestorer(d)(reg)
	st := reg.Snapshot("sw1")
	if st == nil || st.Payload == nil || st.Payload.Data.DHCP == nil || st.Payload.Data.FDB == nil {
		t.Fatalf("esperaba el estado restaurado, obtuve %+v", st)
	}
	if got := st.Payload.Data.DHCP.Leases[0].MAC; got != "AA:BB:CC:DD:EE:01" {
		t.Errorf("esperaba MAC de lease canónica, obtuve %q", got)
	}
	if _, ok := st.Payload.Data.FDB.MACs["AA:BB:CC:DD:EE:02"]; !ok || len(st.Payload.Data.FDB.MACs) != 1 {
		t.Errorf("esperaba la clave FDB canónica, obtuve %v", st.Payload.Data.FDB.MACs)
	}
	if st.BridgeMAC != "AA:BB:CC:DD:EE:00" {
		t.Errorf("esperaba BridgeMAC canónica, obtuve %q", st.BridgeMAC)
	}
}
