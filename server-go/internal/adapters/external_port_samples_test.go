package adapters

import (
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

// #639/#641: los pushers external/beacon persisten sus propias series de
// puerto con contadores de tramas (recordBeaconPortSamples en httpapi); sus
// EthPort no llevan contadores, así que polledFromAgent NO debe pasar por
// recordPortSamples (escribiría filas a 0 intercaladas con las buenas). Un
// agente nativo SÍ debe persistir sus samples.
func TestPolledFromAgentExternalNoPortSamples(t *testing.T) {
	d := openLiveTestDB(t)
	l := NewLive(nil, d, nil, nil)

	ports := []probe.EthPort{{ID: "lan1", Label: "Port 1", Up: true, Speed: "1G"}}

	// Payload external (beacon): con FDB.Ports pero sin contadores.
	ext := &probe.Payload{
		Router: "switch16", Ts: time.Now().Unix(), Kind: "external", Interval: 30,
		Data: probe.PayloadData{FDB: &probe.FDBData{Ports: ports}},
	}
	_ = l.polledFromAgent(RouterConfig{ID: "switch16", Host: "192.168.1.6"}, ext)
	nExt := countPortSamples(t, d, "switch16")
	if nExt != 0 {
		t.Fatalf("external persistió %d port samples, esperaba 0 (los persiste el beacon)", nExt)
	}

	// Payload nativo: debe persistir.
	nat := &probe.Payload{
		Router: "rt1", Ts: time.Now().Unix(), Kind: "native",
		Data: probe.PayloadData{FDB: &probe.FDBData{Ports: ports}},
	}
	_ = l.polledFromAgent(RouterConfig{ID: "rt1", Host: "192.168.1.2"}, nat)
	nNat := countPortSamples(t, d, "rt1")
	if nNat == 0 {
		t.Fatalf("agente nativo no persistió port samples, esperaba >0")
	}
}

func countPortSamples(t *testing.T, d *db.DB, routerID string) int {
	t.Helper()
	var n int
	if err := d.QueryRow(
		"SELECT COUNT(*) FROM port_series_raw WHERE router_id = ?", routerID,
	).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", routerID, err)
	}
	return n
}
