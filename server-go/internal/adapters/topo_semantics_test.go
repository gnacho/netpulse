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
	sem := BuildTopoSemantics(canonRouters(), canonAllDevices(), canonWireguard(), canonDistributionNodes(), "", nil)

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
	sem := BuildTopoSemantics(routers, devices, WireGuardStats{}, nil, "", nil)
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
	want := BuildTopoSemantics(canonRouters(), canonAllDevices(), canonWireguard(), canonDistributionNodes(), "", nil)
	if !reflect.DeepEqual(ov.Topology, want) {
		t.Fatalf("overview.Topology != builder canon:\n got: %+v\nwant: %+v", ov.Topology, want)
	}
}

// Sin routers: semántica vacía pero no nil (la app cae a su cálculo propio).
func TestTopoSemanticsSinRouters(t *testing.T) {
	sem := BuildTopoSemantics(nil, nil, WireGuardStats{}, nil, "", nil)
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
	sem := BuildTopoSemantics(routers, devices, WireGuardStats{}, dists, "", nil)

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
	sem := BuildTopoSemantics(routers, devices, WireGuardStats{}, nil, "100.64.0.1", nil)
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
	sem2 := BuildTopoSemantics(routers, devices, WireGuardStats{}, nil, "", nil)
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
	sem := BuildTopoSemantics(routers, devices, WireGuardStats{}, nil, "", nil)
	links := map[string]TopoLink{}
	for _, l := range sem.Links {
		if l.Kind == "uplink" {
			links[l.To] = l
		}
	}
	if got := links["sw1"]; got.From != "gw" || got.Port != "1" {
		t.Fatalf("uplink sw1: %+v, want from gw puerto 1", got)
	}
	if got := links["ap1"]; got.From != "sw1" || got.Port != "5" {
		t.Fatalf("uplink ap1: %+v, want from sw1 puerto 5", got)
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
	sem := BuildTopoSemantics(routers, devices, WireGuardStats{}, nil, "", ev)
	links := map[string]TopoLink{}
	for _, l := range sem.Links {
		if l.Kind == "uplink" {
			links[l.To] = l
		}
	}
	if got := links["ap1"]; got.From != "sw1" || got.Port != "5" {
		t.Fatalf("uplink ap1: %+v, want from sw1 puerto 5", got)
	}
	if got := links["sw1"]; got.From != "gw" || got.Port != "1" {
		t.Fatalf("uplink sw1: %+v, want from gw puerto 1", got)
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
	sem := BuildTopoSemantics(routers, devices, WireGuardStats{}, nil, "", ev)
	for _, l := range sem.Links {
		if l.Kind == "uplink" && l.To == "ap1" {
			if l.From != "gw" || l.Port != "4" {
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
	sem := BuildTopoSemantics(routers, devices, WireGuardStats{}, dists, "", ev)
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
