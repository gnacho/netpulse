package adapters

import (
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
	sem := BuildTopoSemantics(canonRouters(), canonAllDevices(), canonWireguard(), canonDistributionNodes())

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

// HiddenPeers: superado el límite de chips visibles del anillo (gateway 13,
// AP 20), el exceso se reporta como "+N" por router.
func TestTopoSemanticsHiddenPeers(t *testing.T) {
	routers := []Router{
		{ID: "gw", RoleBadge: "Principal"},
		{ID: "ap", RoleBadge: "AP"},
	}
	devices := make([]Device, 0, 15+22)
	for i := 0; i < 15; i++ {
		devices = append(devices, Device{
			ID: "gw-" + string(rune('a'+i)), MAC: "00:00:00:00:01:" + string(rune('A'+i)),
			RouterID: "gw", Band: "5 GHz", Online: true,
		})
	}
	for i := 0; i < 22; i++ {
		devices = append(devices, Device{
			ID: "ap-" + string(rune('a'+i)), MAC: "00:00:00:00:02:" + string(rune('A'+i)),
			RouterID: "ap", Band: "2.4 GHz", Online: true,
		})
	}
	sem := BuildTopoSemantics(routers, devices, WireGuardStats{}, nil)
	want := map[string]int{"gw": 2, "ap": 2} // 15-13 y 22-20
	if !reflect.DeepEqual(sem.HiddenPeers, want) {
		t.Fatalf("hiddenPeers: got %+v want %+v", sem.HiddenPeers, want)
	}
	if got := len(sem.Rings["gw"]); got != 15 {
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
	want := BuildTopoSemantics(canonRouters(), canonAllDevices(), canonWireguard(), canonDistributionNodes())
	if !reflect.DeepEqual(ov.Topology, want) {
		t.Fatalf("overview.Topology != builder canon:\n got: %+v\nwant: %+v", ov.Topology, want)
	}
}

// Sin routers: semántica vacía pero no nil (la app cae a su cálculo propio).
func TestTopoSemanticsSinRouters(t *testing.T) {
	sem := BuildTopoSemantics(nil, nil, WireGuardStats{}, nil)
	if sem == nil || sem.Links == nil || sem.Rings == nil {
		t.Fatalf("semántica vacía debe tener links/rings no-nil: %+v", sem)
	}
	if len(sem.Links) != 0 || len(sem.Rings) != 0 {
		t.Fatalf("sin routers no hay links ni anillos: %+v", sem)
	}
}
