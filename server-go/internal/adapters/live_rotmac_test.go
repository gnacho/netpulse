// live_rotmac_test.go — atribución del hint wireless-side del FDB (step 1b
// de buildDevices), pedido en la review de #1513: precedencia sobre los
// fallbacks cableados y preservación del signal_dbm ya persistido.
package adapters

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

// (1) manda sobre (1b): una MAC con asociación real de iw conserva su
// router, banda y señal aunque otro router la reporte en wirelessSide.
func TestBuildDevicesWirelessWinsOverWirelessSide(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	l.gatewayCfg = &RouterConfig{ID: "gw", Name: "Gateway", IsGateway: true}
	polled := map[string]*routerPolled{
		"gw": {cfg: RouterConfig{ID: "gw", Name: "Gateway", IsGateway: true}},
		"ap1": {cfg: RouterConfig{ID: "ap1", Name: "ap1"},
			wireless: map[string]WirelessClient{
				"AA:BB:CC:DD:EE:01": {SignalDbm: -50, Band: "5 GHz"},
			}},
		"ap2": {cfg: RouterConfig{ID: "ap2", Name: "ap2"},
			wirelessSide: map[string]string{"AA:BB:CC:DD:EE:01": "2.4 GHz"}},
	}
	devs := l.buildDevices(polled, "")
	var got *Device
	for i := range devs {
		if devs[i].MAC == "AA:BB:CC:DD:EE:01" {
			got = &devs[i]
		}
	}
	if got == nil {
		t.Fatalf("device no encontrado: %+v", devs)
	}
	if got.RouterID != "ap1" || got.Band != "5 GHz" || got.SignalDbm == nil || *got.SignalDbm != -50 {
		t.Fatalf("device = %+v, want ap1/5 GHz/-50 (la asociación real manda)", got)
	}
}

// (1b) manda sobre (2)/(3): una MAC rotada que el satélite y el gateway ven
// en tránsito por sus FDB no se redibuja como cableada: el hint la deja en
// la radio del AP que la aprende en un puerto wireless.
func TestBuildDevicesWirelessSideBeatsFdbFallbacks(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	gwCfg := RouterConfig{ID: "gw", Name: "Gateway", IsGateway: true}
	l.gatewayCfg = &gwCfg
	const (
		gwMac = "AA:BB:CC:00:00:01"
		rot   = "AA:BB:CC:DD:EE:02"
	)
	polled := map[string]*routerPolled{
		"gw": {cfg: gwCfg, brMac: gwMac, fdb: map[string]string{
			rot: "br-lan", // tránsito: misma LAN bridged
		}},
		"sw": {cfg: RouterConfig{ID: "sw", Name: "sw"}, brMac: "AA:BB:CC:00:00:02", fdb: map[string]string{
			gwMac: "1", // root port del switch
			rot:   "5", // boca local: sin hint, (2) se la quedaría
		}},
		"ap": {cfg: RouterConfig{ID: "ap", Name: "ap"}, brMac: "AA:BB:CC:00:00:03",
			wirelessSide: map[string]string{rot: "2.4 GHz"}},
	}
	devs := l.buildDevices(polled, "")
	var got *Device
	for i := range devs {
		if devs[i].MAC == rot {
			got = &devs[i]
		}
	}
	if got == nil {
		t.Fatalf("device no encontrado: %+v", devs)
	}
	if got.RouterID != "ap" || got.Band != "2.4 GHz" {
		t.Fatalf("device = %+v, want ap/2.4 GHz (el hint manda sobre FDB de tránsito)", got)
	}
}

// El upsert de (1b) no debe pisar con NULL un signal_dbm bueno persistido por
// una asociación real: en ticks sin (1) pero con (1b), el RSSI anterior tiene
// que sobrevivir en device_attrib.
func TestBuildDevicesWirelessSideKeepsPersistedSignal(t *testing.T) {
	d := openLiveTestDB(t)
	l := NewLive(nil, d, nil, nil)
	l.gatewayCfg = &RouterConfig{ID: "gw", Name: "Gateway", IsGateway: true}
	const rot = "AA:BB:CC:DD:EE:03"
	if _, err := d.Exec(
		`INSERT INTO device_attrib (mac, router_id, band, signal_dbm, last_seen) VALUES (?, ?, ?, ?, ?)`,
		rot, "ap", "5 GHz", -42, db.NowMS()); err != nil {
		t.Fatalf("seed device_attrib: %v", err)
	}
	polled := map[string]*routerPolled{
		"gw": {cfg: RouterConfig{ID: "gw", Name: "Gateway", IsGateway: true}},
		"ap": {cfg: RouterConfig{ID: "ap", Name: "ap"},
			wirelessSide: map[string]string{rot: "2.4 GHz"}},
	}
	l.buildDevices(polled, "")
	var sig *int
	var band string
	if err := d.QueryRow(`SELECT signal_dbm, band FROM device_attrib WHERE mac = ?`, rot).Scan(&sig, &band); err != nil {
		t.Fatalf("read device_attrib: %v", err)
	}
	if sig == nil || *sig != -42 {
		t.Fatalf("signal_dbm = %v, want -42 conservado (el hint no trae RSSI)", sig)
	}
	if band != "2.4 GHz" {
		t.Fatalf("band = %q, want la banda fresca del hint", band)
	}
}
