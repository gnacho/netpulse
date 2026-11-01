package adapters

import (
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/portseries"
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

// #1741: un pusher que sí manda contadores o tasas por boca es una fuente
// válida de port_series. Vetarlo por kind dejaba su línea de 24 h sin datos.
func TestPolledFromAgentExternalWithCountersRecordsPortSamples(t *testing.T) {
	d := openLiveTestDB(t)
	l := NewLive(nil, d, nil, nil)

	ports := []probe.EthPort{{
		ID: "sfp-sfpplus1", Label: "sfp-sfpplus1", Up: true, Speed: "10G",
		RxBytes: 1000, TxBytes: 2000,
	}}
	p := &probe.Payload{
		Router: "crs305", Ts: time.Now().Unix(), Kind: "external", Interval: 30,
		Data: probe.PayloadData{FDB: &probe.FDBData{Ports: ports}},
	}
	_ = l.polledFromAgent(RouterConfig{ID: "crs305", Host: "192.168.2.15"}, p)
	if n := countPortSamples(t, d, "crs305"); n == 0 {
		t.Fatalf("el pusher con contadores no persistió port samples")
	}
}

// #1741: el respaldo del sparkline colgaba de VitalsAvailable, y #1724 la deja
// en true en un pusher que confirma CPU/temperatura, así que la línea de 24 h
// se quedaba a 0. Tampoco puede exigir contadores de tramas: RouterOS y
// AsusWRT mandan tasas por boca.
func TestFillSparklineFromPortsUsesBpsAndIgnoresVitalsFlag(t *testing.T) {
	d := openLiveTestDB(t)
	l := NewLive(nil, d, nil, nil)
	if err := d.PortSeries.RecordSample(portseries.PortSample{
		RouterID: "crs305", PortID: "sfp-sfpplus1", TS: time.Now(), RxBps: 800e6, TxBps: 200e6,
	}); err != nil {
		t.Fatal(err)
	}

	list := []Router{
		{ID: "crs305", Sparkline: make([]float64, 24), sparklineFromPorts: true, VitalsAvailable: bptr(true)},
		{ID: "rt1", Sparkline: make([]float64, 24)},
	}
	l.fillSparklineFromPorts(list)

	last := list[0].Sparkline[len(list[0].Sparkline)-1]
	if last < 999e6 || last > 1001e6 {
		t.Fatalf("última hora: %.0f bps, esperaba 1e9 (800M+200M)", last)
	}
	if !allZeroF(list[1].Sparkline) {
		t.Fatalf("un router con serie propia no debe rellenarse desde los puertos")
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
