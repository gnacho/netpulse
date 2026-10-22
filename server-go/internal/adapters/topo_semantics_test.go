package adapters

import (
	"fmt"
	"reflect"
	"testing"
)

// Golden test de la topología semántica (SPEC-65 D65-3): para el canon demo,
// los links/rings/hiddenPeers del builder Go deben coincidir con lo que
// produce hoy buildTopologyModel de app/src/components/topology/model.ts
// (revisado a mano contra model.ts @ audit/v2-main):
//   - wan + 3 uplinks (living/estudio con puerto LLDP del gateway; patio wifi)
//   - 2 enlaces dist (switch inferido del gateway y GS308E managed del Salón)
//   - cableados directos: nas/mac-mini/hue-hub anclados al GATEWAY (mac-mini y
//     hue-hub son de Estudio pero no tienen evidencia: ni attachTo ni puerto
//     FDB → regla de anclaje al gateway), ps5 al Salón, pve (host hipervisor)
//   - 8 clientes tras el switch inferido + 3 tras el GS308E + 10 CTs del pve
//   - 2 túneles WG (los 2 peers activos)
//   - El GS308E (switch-netgear) NO genera chips/enlaces: su MAC es la
//     chassis-MAC del distnode managed (D1)
//   - Anillos: cableados primero, luego 5GHz, luego 2.4GHz (orden dataset)
//   - hiddenPeers ausente: todos los anillos bajo el límite de chips visibles
//     de la app (gateway 13, AP 20)
func TestTopoSemanticsGoldenCanon(t *testing.T) {
	sem := BuildTopoSemantics(canonRouters(), canonAllDevices(), canonWireguard(), canonDistributionNodes(), "", nil, nil, nil)

	wantLinks := []TopoLink{
		{From: "internet", To: "flint2", Kind: "wan"},
		{From: "flint2", To: "living", Kind: "uplink", Port: "lan1"},
		{From: "flint2", To: "estudio", Kind: "uplink", Port: "lan2"},
		{From: "flint2", To: "patio", Kind: "uplink"},
		{From: "flint2", To: "dist-flint2-lan3", Kind: "dist", Port: "lan3"},
		{From: "living", To: "dist-living-lan3", Kind: "dist", Port: "lan3"},
		{From: "flint2", To: "nas-synology", Kind: "wired", Port: "lan4"},
		{From: "flint2", To: "mac-mini", Kind: "wired"},
		{From: "flint2", To: "hue-hub", Kind: "wired"},
		{From: "living", To: "ps5", Kind: "wired", Port: "lan1"},
		{From: "flint2", To: "pve", Kind: "wired", Port: "lan5"},
		{From: "dist-flint2-lan3", To: "pc-sobremesa", Kind: "wired"},
		{From: "dist-flint2-lan3", To: "raspberry-pi", Kind: "wired"},
		{From: "dist-flint2-lan3", To: "tv-salon-cable", Kind: "wired"},
		{From: "dist-flint2-lan3", To: "impresora-hp", Kind: "wired"},
		{From: "dist-flint2-lan3", To: "xbox-one", Kind: "wired"},
		{From: "dist-flint2-lan3", To: "receptor-av", Kind: "wired"},
		{From: "dist-flint2-lan3", To: "deco-orange", Kind: "wired"},
		{From: "dist-flint2-lan3", To: "pc-invitado", Kind: "wired"},
		{From: "dist-living-lan3", To: "xbox-series-s", Kind: "wired"},
		{From: "dist-living-lan3", To: "apple-tv-4k", Kind: "wired"},
		{From: "dist-living-lan3", To: "receptor-denon", Kind: "wired"},
		{From: "pve", To: "ct-pihole", Kind: "wired"},
		{From: "pve", To: "ct-home-assistant", Kind: "wired"},
		{From: "pve", To: "ct-nextcloud", Kind: "wired"},
		{From: "pve", To: "ct-jellyfin", Kind: "wired"},
		{From: "pve", To: "ct-immich", Kind: "wired"},
		{From: "pve", To: "ct-gitea", Kind: "wired"},
		{From: "pve", To: "ct-uptime-kuma", Kind: "wired"},
		{From: "pve", To: "ct-adguard-sync", Kind: "wired"},
		{From: "pve", To: "ct-postgres", Kind: "wired"},
		{From: "pve", To: "ct-redis", Kind: "wired"},
		{From: "peer-pixel-8-pro", To: "internet", Kind: "wg"},
		{From: "peer-macbook-air", To: "internet", Kind: "wg"},
	}
	if !reflect.DeepEqual(sem.Links, wantLinks) {
		t.Fatalf("links:\n got: %+v\nwant: %+v", sem.Links, wantLinks)
	}

	wantRings := map[string][]string{
		"flint2": {"nas-synology", "mac-mini", "hue-hub", "pve",
			"pixel-8-pro", "iphone-ana", "macbook-pro", "pixel-7",
			"timbre-nest", "enchufe-lavadora"},
		"living": {"ps5",
			"imac-salon", "tv-samsung", "galaxy-tab-s9", "chromecast", "homepod-mini",
			"galaxy-s23", "nintendo-switch", "portatil-invitado",
			"bombilla-1", "bombilla-2", "bombilla-3", "bombilla-4", "bombilla-5",
			"bombilla-6", "echo-dot"},
		"estudio": {"macbook-air", "ipad-pro", "iphone-trabajo",
			"nest-mini", "enchufe-ventilador", "sonos-one"},
		"patio": {"robot-aspirador", "camara-porche", "camara-jardin",
			"sensor-riego", "enchufe-calefactor"},
	}
	if !reflect.DeepEqual(sem.Rings, wantRings) {
		t.Fatalf("rings:\n got: %+v\nwant: %+v", sem.Rings, wantRings)
	}

	// Todos los anillos bajo el límite de chips visibles → sin "+N".
	if len(sem.HiddenPeers) != 0 {
		t.Fatalf("hiddenPeers debería estar vacío en el canon: %+v", sem.HiddenPeers)
	}
}

// HiddenPeers: superado el límite de chips visibles del anillo (gateway 60,
// AP 40), el exceso se reporta como "+N" por router.
func TestTopoSemanticsHiddenPeers(t *testing.T) {
	routers := []Router{
		{ID: "gw", RoleBadge: "Principal"},
		{ID: "ap", RoleBadge: "AP"},
	}
	devices := make([]Device, 0, 62+42)
	for i := 0; i < 62; i++ {
		devices = append(devices, Device{
			ID: fmt.Sprintf("gw-%02d", i), MAC: fmt.Sprintf("00:00:00:00:01:%02d", i),
			RouterID: "gw", Band: "5 GHz", Online: true,
		})
	}
	for i := 0; i < 42; i++ {
		devices = append(devices, Device{
			ID: fmt.Sprintf("ap-%02d", i), MAC: fmt.Sprintf("00:00:00:00:02:%02d", i),
			RouterID: "ap", Band: "2.4 GHz", Online: true,
		})
	}
	sem := BuildTopoSemantics(routers, devices, WireGuardStats{}, nil, "", nil, nil, nil)
	want := map[string]int{"gw": 2, "ap": 2} // 62-60 y 42-40
	if !reflect.DeepEqual(sem.HiddenPeers, want) {
		t.Fatalf("hiddenPeers: got %+v want %+v", sem.HiddenPeers, want)
	}
	if got := len(sem.Rings["gw"]); got != 62 {
		t.Fatalf("el anillo conserva TODOS los clientes (visibles+ocultos): %d", got)
	}
}

// El overview demo lleva topology y vm SIEMPRE (SPEC-65 D65-3/D65-4).
func TestDemoOverviewIncluyeTopologyYVM(t *testing.T) {
	d := NewDemo()
	ov, err := d.GetOverview(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if ov.VM != ViewModelVersion {
		t.Fatalf("overview.vm=%d, ViewModelVersion=%d", ov.VM, ViewModelVersion)
	}
	if ov.Topology == nil {
		t.Fatal("overview.Topology ausente en demo")
	}
	// Mismo resultado que el builder puro sobre el canon.
	want := BuildTopoSemantics(canonRouters(), canonAllDevices(), canonWireguard(), canonDistributionNodes(), "", nil, nil, nil)
	if !reflect.DeepEqual(ov.Topology, want) {
		t.Fatalf("overview.Topology != builder canon:\n got: %+v\nwant: %+v", ov.Topology, want)
	}
}

// Sin routers: semántica vacía pero no nil (la app cae a su cálculo propio).
func TestTopoSemanticsSinRouters(t *testing.T) {
	sem := BuildTopoSemantics(nil, nil, WireGuardStats{}, nil, "", nil, nil, nil)
	if sem == nil || sem.Links == nil || sem.Rings == nil {
		t.Fatalf("semántica vacía debe tener links/rings no-nil: %+v", sem)
	}
	if len(sem.Links) != 0 || len(sem.Rings) != 0 {
		t.Fatalf("sin routers no hay links ni anillos: %+v", sem)
	}
}

// TestTopoSemanticsDeviceHubBajoDistnode: un device-hub (host con CTs
// anidados por override, issue #142) que cuelga de un distnode inferred NO
// debe duplicar su cable: lo genera solo el bucle de hijos de distnodes.
func TestTopoSemanticsDeviceHubBajoDistnode(t *testing.T) {
	devices := []Device{
		{RouterID: "gateway", Band: "cable", Port: "lan1", ID: "host", MAC: "c8:ff:bf:08:6f:ba", AttachTo: "dist-gateway-lan1", Online: true, Infra: "hypervisor"},
		{RouterID: "gateway", Band: "cable", Port: "lan1", ID: "ct1", MAC: "bc:24:11:00:00:01", AttachTo: "host", Online: true, Infra: "ct"},
		{RouterID: "gateway", Band: "cable", Port: "lan1", ID: "ct2", MAC: "bc:24:11:00:00:02", AttachTo: "host", Online: true, Infra: "ct"},
	}
	routers := []Router{{ID: "gateway", Name: "gateway", RoleBadge: "Principal", Status: "online"}}
	dists := []DistributionNode{{ID: "dist-gateway-lan1", Kind: "inferred", RouterID: "gateway", Port: "lan1"}}
	sem := BuildTopoSemantics(routers, devices, WireGuardStats{}, dists, "", nil, nil, nil)

	var toHost []TopoLink
	for _, l := range sem.Links {
		if l.To == "host" {
			toHost = append(toHost, l)
		}
	}
	// Un solo cable host→su hub (dist-gateway-lan1), no duplicado.
	if len(toHost) != 1 {
		t.Fatalf("cable del device-hub duplicado (%d): %+v", len(toHost), toHost)
	}
	if toHost[0].From != "dist-gateway-lan1" {
		t.Errorf("host debe colgar de su distnode: %+v", toHost[0])
	}
	// Los CTs cuelgan del host exactamente una vez cada uno.
	ctLinks := map[string]int{}
	for _, l := range sem.Links {
		if l.From == "host" {
			ctLinks[l.To]++
		}
	}
	for _, ct := range []string{"ct1", "ct2"} {
		if ctLinks[ct] != 1 {
			t.Errorf("CT %s debe colgar del host una vez, got %d", ct, ctLinks[ct])
		}
	}
}

// #1042: el dispositivo cuya IP coincide con la puerta de enlace WAN es el
// módem/ONT aguas arriba: va bajo el nodo Internet (sem.WanPeer), queda
// fuera de los anillos y no genera enlace de cliente LAN.
func TestTopoSemanticsWanPeer(t *testing.T) {
	routers := []Router{
		{ID: "gw", Name: "gateway", RoleBadge: "Principal"},
	}
	devices := []Device{
		{ID: "modem", MAC: "AA:BB:CC:00:00:01", RouterID: "gw", Band: "cable", Online: true, IP: "100.64.0.1"},
		{ID: "nas", MAC: "AA:BB:CC:00:00:02", RouterID: "gw", Band: "cable", Online: true, IP: "192.168.1.10"},
	}
	sem := BuildTopoSemantics(routers, devices, WireGuardStats{}, nil, "100.64.0.1", nil, nil, nil)
	if sem.WanPeer != "modem" {
		t.Fatalf("WanPeer = %q, want modem", sem.WanPeer)
	}
	for _, id := range sem.Rings["gw"] {
		if id == "modem" {
			t.Fatal("el wan peer no debe estar en el anillo del gateway")
		}
	}
	found := false
	for _, l := range sem.Links {
		if l.Kind == "wan-peer" && l.From == "internet" && l.To == "modem" {
			found = true
		}
		if l.Kind == "wired" && l.To == "modem" {
			t.Fatal("el wan peer no debe tener enlace de cliente cableado")
		}
	}
	if !found {
		t.Fatal("falta el enlace internet→modem (wan-peer)")
	}
	// Sin gateway WAN no hay wan peer aunque haya dispositivos.
	sem2 := BuildTopoSemantics(routers, devices, WireGuardStats{}, nil, "", nil, nil, nil)
	if sem2.WanPeer != "" {
		t.Fatalf("WanPeer sin gateway WAN = %q, want vacío", sem2.WanPeer)
	}
}

// #1047: la MAC de una unidad de flota aprendida en el FDB de otro miembro
// ancla su uplink al padre real (ap1 cuelga de sw1, no del gateway), aunque
// no haya LLDP. La unidad deja de duplicarse como chip de cliente.
func TestTopoSemanticsFdbUplinkParent(t *testing.T) {
	routers := []Router{
		{ID: "gw", Name: "gateway", RoleBadge: "Principal", MAC: "AA:BB:CC:00:00:01"},
		{ID: "sw1", Name: "sw1", MAC: "AA:BB:CC:00:00:02"},
		{ID: "ap1", Name: "ap1", MAC: "AA:BB:CC:00:00:03"},
	}
	devices := []Device{
		// sw1 visto en el puerto 1 del gateway; ap1 en el puerto 5 de sw1.
		{ID: "dev-sw1", MAC: "AA:BB:CC:00:00:02", RouterID: "gw", Band: "cable", Online: true, Port: "1"},
		{ID: "dev-ap1", MAC: "AA:BB:CC:00:00:03", RouterID: "sw1", Band: "cable", Online: true, Port: "5"},
		{ID: "nas", MAC: "AA:BB:CC:00:00:09", RouterID: "gw", Band: "cable", Online: true, Port: "2"},
	}
	sem := BuildTopoSemantics(routers, devices, WireGuardStats{}, nil, "", nil, nil, nil)
	links := map[string]TopoLink{}
	for _, l := range sem.Links {
		if l.Kind == "uplink" {
			links[l.To] = l
		}
	}
	if got := links["sw1"]; got.From != "gw" || got.Port != "1" || got.Evidence != "device" {
		t.Fatalf("uplink sw1: %+v, want from gw puerto 1 evidencia device", got)
	}
	if got := links["ap1"]; got.From != "sw1" || got.Port != "5" || got.Evidence != "device" {
		t.Fatalf("uplink ap1: %+v, want from sw1 puerto 5 evidencia device", got)
	}
	// Las unidades de flota no duplican como chips de cliente.
	for _, ring := range sem.Rings {
		for _, id := range ring {
			if id == "dev-sw1" || id == "dev-ap1" {
				t.Fatalf("unidad de flota en anillo: %s", id)
			}
		}
	}
}

// #1051: el anclaje FDB no debe depender de que las unidades de flota
// aparezcan como devices: la evidencia directa del poller basta.
func TestTopoSemanticsFdbEvidenceSinDevices(t *testing.T) {
	routers := []Router{
		{ID: "gw", Name: "gateway", RoleBadge: "Principal", MAC: "AA:BB:CC:00:00:01"},
		{ID: "sw1", Name: "sw1", MAC: "AA:BB:CC:00:00:02"},
		{ID: "ap1", Name: "ap1", MAC: "AA:BB:CC:00:00:03"},
	}
	devices := []Device{
		{ID: "nas", MAC: "AA:BB:CC:00:00:09", RouterID: "gw", Band: "cable", Online: true, Port: "2"},
	}
	ev := map[string]topoParent{
		"sw1": {parent: "gw", port: "1"},
		"ap1": {parent: "sw1", port: "5"},
	}
	sem := BuildTopoSemantics(routers, devices, WireGuardStats{}, nil, "", ev, nil, nil)
	links := map[string]TopoLink{}
	for _, l := range sem.Links {
		if l.Kind == "uplink" {
			links[l.To] = l
		}
	}
	if got := links["ap1"]; got.From != "sw1" || got.Port != "5" || got.Evidence != "fdb" {
		t.Fatalf("uplink ap1: %+v, want from sw1 puerto 5 evidencia fdb", got)
	}
	if got := links["sw1"]; got.From != "gw" || got.Port != "1" || got.Evidence != "fdb" {
		t.Fatalf("uplink sw1: %+v, want from gw puerto 1 evidencia fdb", got)
	}
}

// #1051: la evidencia de device (attachTo/atribución) manda sobre la FDB
// directa cuando ambas existen.
func TestTopoSemanticsDeviceEvidenceManda(t *testing.T) {
	routers := []Router{
		{ID: "gw", Name: "gateway", RoleBadge: "Principal", MAC: "AA:BB:CC:00:00:01"},
		{ID: "ap1", Name: "ap1", MAC: "AA:BB:CC:00:00:03"},
	}
	devices := []Device{
		{ID: "dev-ap1", MAC: "AA:BB:CC:00:00:03", RouterID: "gw", Band: "cable", Online: true, Port: "4"},
	}
	ev := map[string]topoParent{"ap1": {parent: "swX", port: "9"}}
	sem := BuildTopoSemantics(routers, devices, WireGuardStats{}, nil, "", ev, nil, nil)
	for _, l := range sem.Links {
		if l.Kind == "uplink" && l.To == "ap1" {
			if l.From != "gw" || l.Port != "4" || l.Evidence != "device" {
				t.Fatalf("la evidencia de device debe mandar: %+v", l)
			}
			return
		}
	}
	t.Fatal("falta el uplink de ap1")
}

// #1051: fleetFdbEvidence deriva el padre del FDB crudo de cada poller.
func TestFleetFdbEvidence(t *testing.T) {
	polled := map[string]*routerPolled{
		"gw":  {brMac: "aa:bb:cc:00:00:01", fdb: map[string]string{"AA:BB:CC:00:00:02": "1"}},
		"sw1": {brMac: "AA:BB:CC:00:00:02", fdb: map[string]string{"AA:BB:CC:00:00:03": "5"}},
		"ap1": {brMac: "AA:BB:CC:00:00:03", fdb: map[string]string{}},
	}
	ev := fleetFdbEvidence(polled, "gw")
	if got := ev["sw1"]; got.parent != "gw" || got.port != "1" {
		t.Fatalf("sw1: %+v, want gw/1", got)
	}
	if got := ev["ap1"]; got.parent != "sw1" || got.port != "5" {
		t.Fatalf("ap1: %+v, want sw1/5", got)
	}
	if _, ok := ev["gw"]; ok {
		t.Fatal("gw no debe tener padre (cuelga de internet)")
	}
}

// #1186: el caso real del switch gestionado: el switch aprende la MAC del gw
// (su uplink, puerto 7) y la de los routers colgados de él; el router aprende
// la MAC del switch. El padre del router es el switch (NO el gateway, que es
// el artefacto de estrella plana) y el cable lleva ambos puertos.
func TestFleetFdbEvidenceBothPorts(t *testing.T) {
	polled := map[string]*routerPolled{
		"gw": {brMac: "AA:BB:CC:00:00:01", fdb: map[string]string{"AA:BB:CC:00:00:02": "1"}},
		"sw": {brMac: "AA:BB:CC:00:00:02", fdb: map[string]string{
			"AA:BB:CC:00:00:01": "7", // uplink hacia el gw
			"AA:BB:CC:00:00:03": "3", // rt colgado del puerto 3
		}},
		"rt": {brMac: "AA:BB:CC:00:00:03", fdb: map[string]string{"AA:BB:CC:00:00:02": "1"}},
	}
	ev := fleetFdbEvidence(polled, "gw")
	sw := ev["sw"]
	if sw.parent != "gw" || sw.port != "1" || sw.childPort != "7" {
		t.Fatalf("sw: %+v, want gw/1 con childPort 7", sw)
	}
	rt := ev["rt"]
	if rt.parent != "sw" || rt.port != "3" || rt.childPort != "1" {
		t.Fatalf("rt: %+v, want sw/3 con childPort 1", rt)
	}
}

// #1373: matriz FDB real de una flota con switch gestionado (12-Oct-2026):
// la relajación por profundidad dejaba el uplink del switch en un AP
// (rt3/rt4 se inflan la profundidad mutuamente en L2 plana y pisan la
// candidata provisional del gateway). La FDB propia del switch lo desmiente:
// los APs viven AGUAS ABAJO de su root port (lan7, donde aprende la MAC del
// gateway). Un candidato aprendido fuera del root port no puede ser padre.
func TestFleetFdbEvidenceRootPortDescartaDownstream(t *testing.T) {
	polled := map[string]*routerPolled{
		"gateway": {brMac: "94:83:C4:BA:BF:AA", fdb: map[string]string{
			"8C:DE:F9:33:71:58": "lan4", // rt2
			"9C:9D:7E:1B:EA:B2": "lan1", // rt3
			"9C:9D:7E:54:EF:B8": "lan1", // rt4
			"78:D8:00:32:31:49": "lan1", // switch16
		}},
		"redmi-ax6": {brMac: "8C:DE:F9:33:71:58", fdb: map[string]string{
			"94:83:C4:BA:BF:AA": "wan",
			"9C:9D:7E:1B:EA:B2": "wan",
			"9C:9D:7E:54:EF:B8": "wan",
		}},
		"redmi-ax6-2": {brMac: "9C:9D:7E:1B:EA:B2", fdb: map[string]string{
			"94:83:C4:BA:BF:AA": "lan1",
			"8C:DE:F9:33:71:58": "lan1",
			"9C:9D:7E:54:EF:B8": "lan1",
			"78:D8:00:32:31:49": "lan1",
		}},
		"redmi-ax6-3": {brMac: "9C:9D:7E:54:EF:B8", fdb: map[string]string{
			"94:83:C4:BA:BF:AA": "lan1",
			"8C:DE:F9:33:71:58": "lan1",
			"9C:9D:7E:1B:EA:B2": "lan1",
			"78:D8:00:32:31:49": "lan1",
		}},
		"switch16": {brMac: "78:D8:00:32:31:49", fdb: map[string]string{
			"94:83:C4:BA:BF:AA": "lan7", // gateway: root port real
			"8C:DE:F9:33:71:58": "lan7", // rt2 también cuelga del gateway
			"9C:9D:7E:1B:EA:B2": "lan3", // rt3: puerto de cobre 3
			"9C:9D:7E:54:EF:B8": "lan6", // rt4: puerto de cobre 6
		}},
	}
	ev := fleetFdbEvidence(polled, "gateway")
	sw := ev["switch16"]
	if sw.parent != "gateway" || sw.port != "lan1" || sw.childPort != "lan7" {
		t.Fatalf("switch16: %+v, want gateway/lan1 con childPort lan7", sw)
	}
	// Sin regresión en los APs: rt2 sigue colgando del gateway y, como
	// bonus, la FDB sola ya cuelga rt3/rt4 del switch (antes los dejaba en
	// cadena rt2->rt3->rt4): el switch los aprende fuera de su root port.
	if rt2 := ev["redmi-ax6"]; rt2.parent != "gateway" || rt2.port != "lan4" {
		t.Fatalf("rt2: %+v, want gateway/lan4", rt2)
	}
	if rt3 := ev["redmi-ax6-2"]; rt3.parent != "switch16" || rt3.port != "lan3" || rt3.childPort != "lan1" {
		t.Fatalf("rt3: %+v, want switch16/lan3 con childPort lan1", rt3)
	}
	if rt4 := ev["redmi-ax6-3"]; rt4.parent != "switch16" || rt4.port != "lan6" || rt4.childPort != "lan1" {
		t.Fatalf("rt4: %+v, want switch16/lan6 con childPort lan1", rt4)
	}
}

// #1373: no-regresión sobre el árbol documentado del contribuidor de #1279
// (gw -> sw1 -> {ap1, sw2 -> ap2}), con sus dos artefactos: estrella plana
// (el gateway aprende todas las MACs en lan1) y tránsito FDB (ap2 visible en
// la tabla de sw1 por el puerto hacia sw2). La guarda del root port NO debe
// cambiar la resolución: cada padre aprende a su hijo en puerto de bajada.
func TestFleetFdbEvidenceArbolContribuidor(t *testing.T) {
	polled := map[string]*routerPolled{
		"gw": {brMac: "AA:00:00:00:00:01", fdb: map[string]string{
			"AA:00:00:00:00:02": "lan1", // sw1
			"AA:00:00:00:00:03": "lan1", // ap1
			"AA:00:00:00:00:04": "lan1", // sw2
			"AA:00:00:00:00:05": "lan1", // ap2 (estrella plana)
		}},
		"sw1": {brMac: "AA:00:00:00:00:02", fdb: map[string]string{
			"AA:00:00:00:00:01": "1", // root port hacia el gw
			"AA:00:00:00:00:03": "3", // ap1
			"AA:00:00:00:00:04": "4", // sw2
			"AA:00:00:00:00:05": "4", // ap2 (tránsito FDB)
		}},
		"ap1": {brMac: "AA:00:00:00:00:03", fdb: map[string]string{
			"AA:00:00:00:00:01": "1",
			"AA:00:00:00:00:02": "1", // sw1 por su uplink
		}},
		"sw2": {brMac: "AA:00:00:00:00:04", fdb: map[string]string{
			"AA:00:00:00:00:01": "8", // root port hacia sw1
			"AA:00:00:00:00:02": "8", // sw1 por su uplink
			"AA:00:00:00:00:05": "7", // ap2
		}},
		"ap2": {brMac: "AA:00:00:00:00:05", fdb: map[string]string{
			"AA:00:00:00:00:01": "1",
			"AA:00:00:00:00:02": "1",
			"AA:00:00:00:00:04": "1", // sw2 por su uplink
		}},
	}
	ev := fleetFdbEvidence(polled, "gw")
	if sw1 := ev["sw1"]; sw1.parent != "gw" || sw1.port != "lan1" || sw1.childPort != "1" {
		t.Fatalf("sw1: %+v, want gw/lan1 childPort 1", sw1)
	}
	if ap1 := ev["ap1"]; ap1.parent != "sw1" || ap1.port != "3" || ap1.childPort != "1" {
		t.Fatalf("ap1: %+v, want sw1/3 childPort 1", ap1)
	}
	if sw2 := ev["sw2"]; sw2.parent != "sw1" || sw2.port != "4" || sw2.childPort != "8" {
		t.Fatalf("sw2: %+v, want sw1/4 childPort 8", sw2)
	}
	if ap2 := ev["ap2"]; ap2.parent != "sw2" || ap2.port != "7" || ap2.childPort != "1" {
		t.Fatalf("ap2: %+v, want sw2/7 childPort 1", ap2)
	}
}

// #1060: el uplink sale del distnode (círculo inferido/gestionado) cuando el
// puerto de la evidencia coincide con su (router, puerto): gateway -> círculo
// -> AP, sin saltarse el switch intermedio.
func TestTopoSemanticsUplinkViaDistNode(t *testing.T) {
	routers := []Router{
		{ID: "gw", Name: "gateway", RoleBadge: "Principal", MAC: "AA:BB:CC:00:00:01"},
		{ID: "ap1", Name: "ap1", MAC: "AA:BB:CC:00:00:03"},
	}
	devices := []Device{
		{ID: "nas", MAC: "AA:BB:CC:00:00:09", RouterID: "gw", Band: "cable", Online: true, Port: "2"},
	}
	dists := []DistributionNode{
		{ID: "dist-gw-lan1", RouterID: "gw", Kind: "inferred", Port: "lan1"},
	}
	ev := map[string]topoParent{"ap1": {parent: "gw", port: "lan1"}}
	sem := BuildTopoSemantics(routers, devices, WireGuardStats{}, dists, "", ev, nil, nil)
	found := false
	for _, l := range sem.Links {
		if l.Kind == "uplink" && l.To == "ap1" {
			found = true
			if l.From != "dist-gw-lan1" {
				t.Fatalf("uplink ap1: from = %q, want dist-gw-lan1 (%+v)", l.From, l)
			}
		}
	}
	if !found {
		t.Fatal("falta el uplink de ap1")
	}
}

// #1279: fleetLldpEvidence resuelve el uplink por LLDP con la regla del
// puerto raíz: el padre es el vecino que vive en el puerto local donde la
// unidad aprende la MAC bridge del gateway. Topología árbol real del
// contribuidor: gw -> sw1 -> {ap1, sw2 -> ap2}; sw2 ve a ap2 y a sw1 por
// LLDP, pero su root port es el 8 (hacia sw1), no el 7.
func TestFleetLldpEvidenceRootPort(t *testing.T) {
	polled := map[string]*routerPolled{
		"gw": {brMac: "AA:BB:CC:00:00:01", cfg: RouterConfig{ID: "gw", Name: "gateway"}},
		"sw1": {brMac: "AA:BB:CC:00:00:02", cfg: RouterConfig{ID: "sw1", Name: "sw1", Type: "managed-switch"},
			fdb: map[string]string{"AA:BB:CC:00:00:01": "two-gigabitEthernet 1/0/20"},
			lldp: []LldpNeighbor{
				{Port: "two-gigabitEthernet 1/0/20", Chassis: "gateway"},
				{Port: "two-gigabitEthernet 1/0/21", Chassis: "ap1"},
				{Port: "two-gigabitEthernet 1/0/22", Chassis: "sw2"},
			}},
		"sw2": {brMac: "AA:BB:CC:00:00:04", cfg: RouterConfig{ID: "sw2", Name: "sw2", Type: "managed-switch"},
			fdb: map[string]string{"AA:BB:CC:00:00:01": "ten-gigabitEthernet 1/0/8"},
			lldp: []LldpNeighbor{
				{Port: "two-gigabitEthernet 1/0/7", Chassis: "ap2"},
				{Port: "ten-gigabitEthernet 1/0/8", Chassis: "sw1"},
			}},
		"ap2": {brMac: "AA:BB:CC:00:00:05", cfg: RouterConfig{ID: "ap2", Name: "ap2"},
			lldp: []LldpNeighbor{{Port: "eth0", Chassis: "sw2", PortDesc: "two-gigabitEthernet 1/0/7"}}},
	}
	ev := fleetLldpEvidence(polled, "gw")
	if got := ev["sw1"]; got.parent != "gw" {
		t.Fatalf("sw1: %+v, want gw (root port 20)", got)
	}
	// #1471: la evidencia guarda la boca del hijo (su puerto local hacia el
	// padre), que es de donde se lee la velocidad negociada del enlace.
	if got := ev["sw1"]; got.childPort != "two-gigabitEthernet 1/0/20" {
		t.Fatalf("sw1: %+v, want childPort two-gigabitEthernet 1/0/20", got)
	}
	// sw2: root port 8 -> sw1; el puerto (lado padre) se rellena desde la
	// vista LLDP de sw1, que ve a sw2 en su 1/0/22.
	if got := ev["sw2"]; got.parent != "sw1" || got.port != "two-gigabitEthernet 1/0/22" {
		t.Fatalf("sw2: %+v, want sw1/two-gigabitEthernet 1/0/22", got)
	}
	if got := ev["sw2"]; got.childPort != "ten-gigabitEthernet 1/0/8" {
		t.Fatalf("sw2: %+v, want childPort ten-gigabitEthernet 1/0/8", got)
	}
	// ap2: candidato único, puerto del lado padre (el que sw2 anuncia).
	if got := ev["ap2"]; got.parent != "sw2" || got.port != "two-gigabitEthernet 1/0/7" {
		t.Fatalf("ap2: %+v, want sw2/two-gigabitEthernet 1/0/7", got)
	}
	if got := ev["ap2"]; got.childPort != "eth0" {
		t.Fatalf("ap2: %+v, want childPort eth0", got)
	}
	if _, ok := ev["gw"]; ok {
		t.Fatal("gw no debe tener padre (cuelga de internet)")
	}
}

// #1279: con varios candidatos y sin señal FDB, se prefiere el switch
// gestionado (hacia el núcleo) sobre un router regular.
func TestFleetLldpEvidenceAmbiguoPrefiereSwitch(t *testing.T) {
	polled := map[string]*routerPolled{
		"gw": {brMac: "AA:BB:CC:00:00:01", cfg: RouterConfig{ID: "gw", Name: "gateway"}},
		"sw": {brMac: "AA:BB:CC:00:00:02", cfg: RouterConfig{ID: "sw", Name: "sw", Type: "managed-switch"},
			lldp: []LldpNeighbor{
				{Port: "p1", Chassis: "rta"},
				{Port: "p2", Chassis: "swb"},
			}},
		"rta": {brMac: "AA:BB:CC:00:00:03", cfg: RouterConfig{ID: "rta", Name: "rta"}},
		"swb": {brMac: "AA:BB:CC:00:00:04", cfg: RouterConfig{ID: "swb", Name: "swb", Type: "managed-switch"}},
	}
	ev := fleetLldpEvidence(polled, "gw")
	if got := ev["sw"]; got.parent != "swb" {
		t.Fatalf("sw: %+v, want swb (switch manda en el empate)", got)
	}
}

// #1279 (reapertura): el Chassis sintetizado desde lldpLocPortDesc (la
// anotación del admin en el puerto LOCAL del Omada, p.ej. "ap2") NO es
// identidad remota: no debe resolver vecinos de flota para el uplink. En la
// flota del contribuidor estas entradas hacían que sw1 eligiera a sw2 (y
// viceversa) por el empate del switch gestionado, cerrando un bucle y
// dejando al gateway sin hijos. La identidad enriquecida sigue sirviendo
// para etiquetas (displayName), solo se excluye de la evidencia.
func TestFleetLldpEvidenceIgnoresLocDescIdentity(t *testing.T) {
	polled := map[string]*routerPolled{
		"gw": {brMac: "AA:BB:CC:00:00:01", cfg: RouterConfig{ID: "gw", Name: "gateway"}},
		// sw1 estilo Omada: los tres vecinos son anotaciones locPortDesc
		// (rem table sin identidad). Sin FDB de la MAC del gateway.
		"sw1": {brMac: "AA:BB:CC:00:00:02", cfg: RouterConfig{ID: "sw1", Name: "sw1", Type: "managed-switch"},
			lldp: []LldpNeighbor{
				{Port: "two-gigabitEthernet 1/0/20", Chassis: "gateway", ChassisFromLocDesc: true},
				{Port: "two-gigabitEthernet 1/0/21", Chassis: "ap1", ChassisFromLocDesc: true},
				{Port: "two-gigabitEthernet 1/0/22", Chassis: "sw2", ChassisFromLocDesc: true},
			}},
		"sw2": {brMac: "AA:BB:CC:00:00:04", cfg: RouterConfig{ID: "sw2", Name: "sw2", Type: "managed-switch"},
			lldp: []LldpNeighbor{
				{Port: "two-gigabitEthernet 1/0/7", Chassis: "ap2", ChassisFromLocDesc: true},
				{Port: "two-gigabitEthernet 1/0/8", Chassis: "sw1", ChassisFromLocDesc: true},
			}},
		"ap1": {brMac: "AA:BB:CC:00:00:03", cfg: RouterConfig{ID: "ap1", Name: "ap1"}},
		"ap2": {brMac: "AA:BB:CC:00:00:05", cfg: RouterConfig{ID: "ap2", Name: "ap2"}},
	}
	ev := fleetLldpEvidence(polled, "gw")
	if len(ev) != 0 {
		t.Fatalf("sin candidatos reales no debe haber evidencia LLDP: %+v", ev)
	}
	// Mezcla: un vecino con identidad remota real y otro sintetizado; el
	// real es el único candidato y manda.
	polled["sw1"] = &routerPolled{brMac: "AA:BB:CC:00:00:02", cfg: RouterConfig{ID: "sw1", Name: "sw1", Type: "managed-switch"},
		lldp: []LldpNeighbor{
			{Port: "two-gigabitEthernet 1/0/21", Chassis: "ap1", ChassisFromLocDesc: true},
			{Port: "ten-gigabitEthernet 1/0/25", Chassis: "rta", PortDesc: "lan2"},
		}}
	polled["rta"] = &routerPolled{brMac: "AA:BB:CC:00:00:06", cfg: RouterConfig{ID: "rta", Name: "rta"}}
	ev = fleetLldpEvidence(polled, "gw")
	if got := ev["sw1"]; got.parent != "rta" || got.port != "lan2" {
		t.Fatalf("sw1: %+v, want rta/lan2 (el candidato real; el de locDesc no cuenta)", got)
	}
}

// #1279 (reapertura): la evidencia LLDP nunca debe producir un ciclo entre
// unidades de flota. Topología real gw -> ap1 -> ap2 con evidencia cruzada
// falsa (ap1 ve a ap2 y ap2 ve a ap1, un candidato cada uno): sin guarda,
// "un candidato manda" fija padres mutuos. Con la guarda, esa evidencia se
// degrada y decide la FDB: el gateway conserva hijos y no hay bucle.
func TestFleetLldpEvidenceNoCycle(t *testing.T) {
	polled := map[string]*routerPolled{
		"gw": {brMac: "AA:BB:CC:00:00:01", cfg: RouterConfig{ID: "gw", Name: "gateway"}},
		"ap1": {brMac: "AA:BB:CC:00:00:02", cfg: RouterConfig{ID: "ap1", Name: "ap1"},
			lldp: []LldpNeighbor{{Port: "eth1", Chassis: "ap2", PortDesc: "eth0"}}},
		"ap2": {brMac: "AA:BB:CC:00:00:03", cfg: RouterConfig{ID: "ap2", Name: "ap2"},
			lldp: []LldpNeighbor{{Port: "eth0", Chassis: "ap1", PortDesc: "eth1"}}},
	}
	ev := fleetLldpEvidence(polled, "gw")
	for start := range ev {
		seen := map[string]bool{}
		for cur := start; ; {
			if seen[cur] {
				t.Fatalf("ciclo en la evidencia LLDP: %+v", ev)
			}
			seen[cur] = true
			e, ok := ev[cur]
			if !ok {
				break
			}
			cur = e.parent
		}
	}
	// Degradada la evidencia en bucle, la FDB decide y el gateway conserva
	// hijos: gw -> ap1 -> ap2.
	routers := []Router{
		{ID: "gw", Name: "gateway", RoleBadge: "Principal", MAC: "AA:BB:CC:00:00:01"},
		{ID: "ap1", Name: "ap1", MAC: "AA:BB:CC:00:00:02"},
		{ID: "ap2", Name: "ap2", MAC: "AA:BB:CC:00:00:03"},
	}
	fdb := map[string]topoParent{
		"ap1": {parent: "gw", port: "lan1"},
		"ap2": {parent: "ap1", port: "eth1"},
	}
	sem := BuildTopoSemantics(routers, nil, WireGuardStats{}, nil, "", fdb, ev, nil)
	uplink := map[string]string{}
	for _, l := range sem.Links {
		if l.Kind == "uplink" {
			uplink[l.To] = l.From
		}
	}
	if uplink["ap1"] != "gw" || uplink["ap2"] != "ap1" {
		t.Fatalf("uplinks: %+v, want ap1<-gw y ap2<-ap1 (FDB tras degradar el ciclo)", uplink)
	}
}

// #1279: la evidencia LLDP manda sobre la FDB/device en el uplink (ground
// truth de dos equipos que se anuncian por el cable).
func TestTopoSemanticsLldpEvidenceWins(t *testing.T) {
	routers := []Router{
		{ID: "gw", Name: "gateway", RoleBadge: "Principal", MAC: "AA:BB:CC:00:00:01"},
		{ID: "sw1", Name: "sw1", MAC: "AA:BB:CC:00:00:02"},
		{ID: "ap1", Name: "ap1", MAC: "AA:BB:CC:00:00:03"},
	}
	fdb := map[string]topoParent{"ap1": {parent: "gw", port: "9"}}
	lldp := map[string]topoParent{"ap1": {parent: "sw1", port: "two-gigabitEthernet 1/0/21"}}
	sem := BuildTopoSemantics(routers, nil, WireGuardStats{}, nil, "", fdb, lldp, nil)
	for _, l := range sem.Links {
		if l.Kind == "uplink" && l.To == "ap1" {
			if l.From != "sw1" || l.Port != "two-gigabitEthernet 1/0/21" || l.Evidence != "lldp" {
				t.Fatalf("LLDP debe mandar sobre la FDB: %+v", l)
			}
			return
		}
	}
	t.Fatal("falta el uplink de ap1")
}

// #1321: el attach manual de flota manda incluso sobre la evidencia LLDP,
// y si no fija puerto se conserva el que la inferencia hubiera dado.
func TestTopoSemanticsOverrideEvidenceWins(t *testing.T) {
	routers := []Router{
		{ID: "gw", Name: "gateway", RoleBadge: "Principal", MAC: "AA:BB:CC:00:00:01"},
		{ID: "sw1", Name: "sw1", MAC: "AA:BB:CC:00:00:02"},
		{ID: "ap1", Name: "ap1", MAC: "AA:BB:CC:00:00:03"},
	}
	lldp := map[string]topoParent{"ap1": {parent: "gw", port: "lan4"}}
	ovr := map[string]topoParent{"ap1": {parent: "sw1"}}
	sem := BuildTopoSemantics(routers, nil, WireGuardStats{}, nil, "", nil, lldp, ovr)
	for _, l := range sem.Links {
		if l.Kind == "uplink" && l.To == "ap1" {
			if l.From != "sw1" || l.Port != "lan4" || l.Evidence != "override" {
				t.Fatalf("el override debe mandar conservando el puerto inferido: %+v", l)
			}
			return
		}
	}
	t.Fatal("falta el uplink de ap1")
}

// #1325: sin evidencia de ninguna capa el uplink cae al gateway y no lleva
// tag de evidencia (Evidence vacía). El fallback al gateway no es evidencia.
func TestTopoSemanticsFallbackSinEvidencia(t *testing.T) {
	routers := []Router{
		{ID: "gw", Name: "gateway", RoleBadge: "Principal", MAC: "AA:BB:CC:00:00:01"},
		{ID: "ap1", Name: "ap1", MAC: "AA:BB:CC:00:00:03"},
	}
	sem := BuildTopoSemantics(routers, nil, WireGuardStats{}, nil, "", nil, nil, nil)
	for _, l := range sem.Links {
		if l.Kind == "uplink" && l.To == "ap1" {
			if l.From != "gw" || l.Evidence != "" {
				t.Fatalf("uplink ap1 sin evidencia: %+v, want from gw y Evidence vacía", l)
			}
			return
		}
	}
	t.Fatal("falta el uplink de ap1")
}

// #1374: el distnode inferred que es la sombra no gestionada de un switch
// gestionado de la flota se absorbe: el switch enlaza directo con el padre
// y los clientes del blob pasan a colgar del switch (su posición física).
func TestTopoSemanticsAbsorbeDistnodeSombra(t *testing.T) {
	routers := []Router{
		{ID: "gw", Name: "gateway", Role: "Gateway principal", RoleBadge: "Principal", MAC: "AA:BB:CC:00:00:01"},
		{ID: "sw", Name: "switch", Role: "Switch", RoleBadge: "SW", MAC: "AA:BB:CC:00:00:02"},
	}
	devices := []Device{
		{ID: "cam", MAC: "AA:BB:CC:00:00:09", RouterID: "sw", Band: "cable", Online: true, AttachTo: "dist-gw-lan1"},
		// Equipo de un switch NO gestionado colgado del gateway: el blob se
		// absorbe pero este cliente NO se re-ancla al switch (no es suyo).
		{ID: "pc", MAC: "AA:BB:CC:00:00:0A", RouterID: "gw", Band: "cable", Online: true, AttachTo: "dist-gw-lan1"},
	}
	dists := []DistributionNode{
		{ID: "dist-gw-lan1", RouterID: "gw", Kind: "inferred", Port: "lan1"},
	}
	ev := map[string]topoParent{"sw": {parent: "gw", port: "lan1"}}
	sem := BuildTopoSemantics(routers, devices, WireGuardStats{}, dists, "", ev, nil, nil)
	var uplink *TopoLink
	for i := range sem.Links {
		l := &sem.Links[i]
		if l.Kind == "uplink" && l.To == "sw" {
			uplink = l
		}
		if l.From == "dist-gw-lan1" || l.To == "dist-gw-lan1" {
			t.Fatalf("el distnode sombra no debe aparecer en enlaces: %+v", *l)
		}
	}
	if uplink == nil || uplink.From != "gw" || uplink.Port != "lan1" {
		t.Fatalf("uplink sw: %+v, want desde gw por lan1", uplink)
	}
	fromOf := map[string]string{}
	for _, l := range sem.Links {
		if l.Kind == "wired" {
			fromOf[l.To] = l.From
		}
	}
	if fromOf["cam"] != "sw" {
		t.Fatalf("cam cuelga de %q, want sw (cliente atribuido al switch)", fromOf["cam"])
	}
	if fromOf["pc"] != "gw" {
		t.Fatalf("pc cuelga de %q, want gw (cliente del switch tonto, no del gestionado)", fromOf["pc"])
	}
	// #1378: la absorción no debe corromper el slice del caller (aliasing):
	// dists conserva su len y contenido; los absorbidos se exponen aparte.
	if len(dists) != 1 || dists[0].ID != "dist-gw-lan1" {
		t.Fatalf("dists del caller corrupto: %+v", dists)
	}
	if len(sem.AbsorbedDists) != 1 || sem.AbsorbedDists[0] != "dist-gw-lan1" {
		t.Fatalf("AbsorbedDists: %+v", sem.AbsorbedDists)
	}
}

// #1485: con PPPoE la puerta de enlace WAN es el BRAS, así que el ONT/módem
// del primer salto no casa por IP. Su evidencia es el ARP del puerto WAN
// (Device.WanSide): debe dibujarse como peer WAN bajo Internet y no como
// cliente cableado del gateway.
func TestTopoSemanticsWanPeerByWanSide(t *testing.T) {
	routers := []Router{{ID: "gw", Name: "Gateway", RoleBadge: "Principal"}}
	devices := []Device{
		{ID: "ont", MAC: "c0-49-43-a3-d1-34", IP: "192.168.1.1", Online: true, WanSide: true, Band: "cable", RouterID: "gw"},
		{ID: "pc", MAC: "aa-bb-cc-00-00-01", Online: true, Band: "cable", RouterID: "gw"},
	}
	sem := BuildTopoSemantics(routers, devices, WireGuardStats{}, nil, "123.113.96.1", nil, nil, nil)
	if sem.WanPeer != "ont" {
		t.Fatalf("WanPeer %q, want ont", sem.WanPeer)
	}
	// Se dibuja como nodo WAN (enlace wan-peer desde Internet), no como
	// cliente cableado del gateway.
	for _, l := range sem.Links {
		if l.To == "ont" && l.Kind != "wan-peer" {
			t.Fatalf("el peer WAN no debe tener enlace LAN: %+v", l)
		}
	}
}

// El match por IP de siempre (#1042) sigue mandando cuando existe.
func TestTopoSemanticsWanPeerByIPWins(t *testing.T) {
	routers := []Router{{ID: "gw", Name: "Gateway", RoleBadge: "Principal"}}
	devices := []Device{
		{ID: "brasa", MAC: "00-00-00-00-00-aa", IP: "123.113.96.1", Online: true, Band: "cable", RouterID: "gw"},
		{ID: "ont", MAC: "c0-49-43-a3-d1-34", IP: "192.168.1.1", Online: true, WanSide: true, Band: "cable", RouterID: "gw"},
	}
	sem := BuildTopoSemantics(routers, devices, WireGuardStats{}, nil, "123.113.96.1", nil, nil, nil)
	if sem.WanPeer != "brasa" {
		t.Fatalf("WanPeer %q, want brasa (el match por IP manda)", sem.WanPeer)
	}
}
