// fdbsticky_test.go — memoria pegajosa del puerto FDB (issue #656).
package adapters

import (
	"testing"
	"time"
)

// TestOverlayStickyFdb: las MACs recordadas (frescas) que faltan en el FDB
// actual se añaden al router correspondiente; las caducadas y las ya
// presentes no se tocan (el FDB real manda); polled no se muta.
func TestOverlayStickyFdb(t *testing.T) {
	now := int64(1_700_000_000_000)
	polled := map[string]*routerPolled{
		"gateway": {cfg: RouterConfig{ID: "gateway", IsGateway: true}, brMac: "GG:GG:GG:GG:GG:GG",
			fdb: map[string]string{"GG:GG:GG:GG:GG:GG": "lan1", "11:11:11:11:11:11": "lan1"}},
	}
	memo := map[string]fdbPortMemo{
		// fresca y ausente del FDB → se añade
		"AA:AA:AA:AA:AA:AA": {routerID: "gateway", port: "lan1", ts: now - 60_000},
		// caducada → fuera
		"BB:BB:BB:BB:BB:BB": {routerID: "gateway", port: "lan2", ts: now - fdbStickyTTL.Milliseconds() - 1},
		// ya presente con OTRA boca → el FDB real manda
		"11:11:11:11:11:11": {routerID: "gateway", port: "lan3", ts: now},
		// de un router que ya no existe → fuera
		"CC:CC:CC:CC:CC:CC": {routerID: "borrado", port: "lan1", ts: now},
	}
	out := overlayStickyFdb(polled, memo, now)

	gw := out["gateway"]
	if gw.fdb["AA:AA:AA:AA:AA:AA"] != "lan1" {
		t.Fatalf("la memo fresca debería añadirse: %+v", gw.fdb)
	}
	if _, ok := gw.fdb["BB:BB:BB:BB:BB:BB"]; ok {
		t.Fatal("la memo caducada no debería añadirse")
	}
	if gw.fdb["11:11:11:11:11:11"] != "lan1" {
		t.Fatalf("el FDB real manda sobre la memo: %q", gw.fdb["11:11:11:11:11:11"])
	}
	if _, ok := gw.fdb["CC:CC:CC:CC:CC:CC"]; ok {
		t.Fatal("la memo de un router inexistente no debería añadirse")
	}
	// polled original intacto (no mutación)
	if _, ok := polled["gateway"].fdb["AA:AA:AA:AA:AA:AA"]; ok {
		t.Fatal("overlayStickyFdb no debe mutar polled")
	}
}

// TestInferTopologyStickyKeepsSwitchAttach: un dispositivo callado (online por
// ARP, RouterID=gateway) cuya entrada FDB caducó conserva su puesto bajo el
// switch inferido de su boca gracias al overlay.
func TestInferTopologyStickyKeepsSwitchAttach(t *testing.T) {
	now := int64(1_700_000_000_000)
	// FDB actual: el gateway solo ve la MAC del citadel en lan1 (la del
	// marantz caducó por silencio).
	polled := map[string]*routerPolled{
		"gateway": {cfg: RouterConfig{ID: "gateway", Name: "GW", IsGateway: true}, brMac: "GG:GG:GG:GG:GG:GG",
			fdb: map[string]string{
				"GG:GG:GG:GG:GG:GG": "lan1",
				"04:D4:C4:8B:30:A7": "lan1",
			}},
	}
	memo := map[string]fdbPortMemo{
		"AA:BB:CC:DD:EE:FF": {routerID: "gateway", port: "lan1", ts: now - 5*60_000},
	}
	sticky := overlayStickyFdb(polled, memo, now)

	devices := []Device{
		// marantz: online vía ARP (RouterID gateway), sin entrada FDB propia
		{ID: "aa-bb-cc-dd-ee-ff", MAC: "AA:BB:CC:DD:EE:FF", Name: "marantz", RouterID: "gateway", Band: "cable", Online: true},
		{ID: "04-d4-c4-8b-30-a7", MAC: "04:D4:C4:8B:30:A7", Name: "shield", RouterID: "gateway", Band: "cable", Online: true},
	}
	devices, dists := inferTopology(sticky, devices)

	if len(dists) == 0 || dists[0].ID != "dist-gateway-lan1" || dists[0].Kind != "inferred" {
		t.Fatalf("esperado dist-gateway-lan1 inferred, got %+v", dists)
	}
	var marantz *Device
	for i := range devices {
		if devices[i].MAC == "AA:BB:CC:DD:EE:FF" {
			marantz = &devices[i]
		}
	}
	if marantz == nil {
		t.Fatal("marantz no encontrado")
	}
	if marantz.Port != "lan1" || marantz.AttachTo != "dist-gateway-lan1" {
		t.Fatalf("marantz debería conservar lan1/dist-gateway-lan1: port=%q attachTo=%q", marantz.Port, marantz.AttachTo)
	}
}

// TestUpdateFdbMemoRefreshAndPrune: la memo se refresca con el FDB real y las
// entradas viejas se podan.
func TestUpdateFdbMemoRefreshAndPrune(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	now := int64(1_700_000_000_000)
	l.fdbMemo["AA:AA:AA:AA:AA:AA"] = fdbPortMemo{routerID: "gateway", port: "lan1", ts: now - fdbStickyTTL.Milliseconds() - 5_000}
	polled := map[string]*routerPolled{
		"gateway": {cfg: RouterConfig{ID: "gateway"}, fdb: map[string]string{"BB:BB:BB:BB:BB:BB": "lan2"}},
	}
	memo := l.updateFdbMemo(polled, now)
	if _, ok := memo["AA:AA:AA:AA:AA:AA"]; ok {
		t.Fatal("la entrada caducada debería podarse")
	}
	if m, ok := memo["BB:BB:BB:BB:BB:BB"]; !ok || m.port != "lan2" || m.routerID != "gateway" {
		t.Fatalf("la entrada fresca debería estar: %+v", memo)
	}
}

// TestUpdateFdbMemoIgnoresUplinkSightings (#678): la memo solo guarda bocas
// LOCALES; una observación en la boca que aprende la MAC de bridge de otro
// router (uplink = tránsito) no se memoriza ni pisa una observación local.
func TestUpdateFdbMemoIgnoresUplinkSightings(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	now := int64(1_700_000_000_000)
	polled := map[string]*routerPolled{
		"gateway": {cfg: RouterConfig{ID: "gateway", IsGateway: true}, brMac: "GG:GG:GG:GG:GG:GG",
			fdb: map[string]string{
				"GG:GG:GG:GG:GG:GG": "lan1", // bridge propio
				"AA:AA:AA:AA:AA:AA": "lan1", // bridge del AP → lan1 = uplink (infra)
				"44:44:44:44:44:44": "lan1", // cliente del AP, visto en tránsito
			}},
		"ap": {cfg: RouterConfig{ID: "ap"}, brMac: "AA:AA:AA:AA:AA:AA",
			fdb: map[string]string{
				"AA:AA:AA:AA:AA:AA": "lan1",
				"44:44:44:44:44:44": "lan3", // boca LOCAL del AP
			}},
	}
	memo := l.updateFdbMemo(polled, now)
	if m, ok := memo["44:44:44:44:44:44"]; !ok || m.routerID != "ap" || m.port != "lan3" {
		t.Fatalf("la boca local del AP debe ganar en la memo: %+v", memo["44:44:44:44:44:44"])
	}
	// La MAC de bridge del AP en lan1 del gateway no debe memorizarse como cliente.
	if _, ok := memo["AA:AA:AA:AA:AA:AA"]; ok {
		t.Fatalf("la MAC de bridge no debería estar en la memo: %+v", memo)
	}
}

// TestBuildDevicesStickySatelliteAttribution (#678): un dispositivo callado que
// este tick solo aparece por ARP (atribuido al gateway por orden alfabético)
// recupera el satélite donde la memo lo vio por última vez en una boca local.
func TestBuildDevicesStickySatelliteAttribution(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{{ID: "gateway", Name: "GW", IsGateway: true}}, nil)
	now := time.Now().UnixMilli()
	l.fdbMemo["44:44:44:44:44:44"] = fdbPortMemo{routerID: "ap", port: "lan3", ts: now}
	polled := map[string]*routerPolled{
		"gateway": {cfg: RouterConfig{ID: "gateway", IsGateway: true}, brMac: "GG:GG:GG:GG:GG:GG",
			fdb: map[string]string{"GG:GG:GG:GG:GG:GG": "lan1", "AA:AA:AA:AA:AA:AA": "lan1"},
			arp: map[string]string{"44:44:44:44:44:44": "192.168.13.123"}},
		"ap": {cfg: RouterConfig{ID: "ap", Name: "Luizjana2"}, brMac: "AA:AA:AA:AA:AA:AA",
			fdb: map[string]string{"AA:AA:AA:AA:AA:AA": "lan1"}},
	}
	devs := l.buildDevices(polled)
	if d := mustDevice(t, devs, "44:44:44:44:44:44"); d.RouterID != "ap" {
		t.Fatalf("RouterID=%q (want ap por la memo sticky)", d.RouterID)
	}
}

// TestBuildDevicesGatewayLocalWinsOverSticky (#678): si el gateway ve el
// dispositivo en una boca NO-uplink, esa evidencia fresca manda sobre la memo.
func TestBuildDevicesGatewayLocalWinsOverSticky(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{{ID: "gateway", Name: "GW", IsGateway: true}}, nil)
	now := time.Now().UnixMilli()
	l.fdbMemo["44:44:44:44:44:44"] = fdbPortMemo{routerID: "ap", port: "lan3", ts: now}
	polled := map[string]*routerPolled{
		"gateway": {cfg: RouterConfig{ID: "gateway", IsGateway: true}, brMac: "GG:GG:GG:GG:GG:GG",
			fdb: map[string]string{
				"GG:GG:GG:GG:GG:GG": "lan1",
				"44:44:44:44:44:44": "lan4", // boca local del gateway (no infra)
			}},
		"ap": {cfg: RouterConfig{ID: "ap"}, brMac: "AA:AA:AA:AA:AA:AA",
			fdb: map[string]string{"AA:AA:AA:AA:AA:AA": "lan1"}},
	}
	devs := l.buildDevices(polled)
	if d := mustDevice(t, devs, "44:44:44:44:44:44"); d.RouterID != "gateway" {
		t.Fatalf("RouterID=%q (want gateway: boca local fresca manda)", d.RouterID)
	}
}
