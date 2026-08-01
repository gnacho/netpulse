package adapters

import "testing"

// FDB/port helpers para los tests de inferencia.
func polledWithFDB(routerID, brMac string, fdb map[string]string) map[string]*routerPolled {
	return map[string]*routerPolled{
		routerID: {cfg: RouterConfig{ID: routerID}, brMac: brMac, fdb: fdb},
	}
}

func dev(mac, routerID, band string) Device {
	return Device{ID: "d-" + mac, MAC: mac, RouterID: routerID, Band: band, Online: true}
}

func TestInferTopologyCableadoDirecto(t *testing.T) {
	polled := polledWithFDB("flint2", "94:83:C4:00:00:01", map[string]string{
		"00:11:32:9C:51:B7": "lan1",
	})
	devices := []Device{dev("00:11:32:9C:51:B7", "flint2", "cable")}
	devices, dists := inferTopology(polled, devices)
	if len(dists) != 0 {
		t.Fatalf("no debería haber distnodes: %+v", dists)
	}
	if devices[0].Port != "lan1" {
		t.Fatalf("port: %q", devices[0].Port)
	}
	if devices[0].AttachTo != "" {
		t.Fatalf("attachTo debería estar vacío: %q", devices[0].AttachTo)
	}
}

func TestInferTopologySwitchInferido(t *testing.T) {
	polled := polledWithFDB("flint2", "94:83:C4:00:00:01", map[string]string{
		"78:2B:CB:AA:01:01": "lan3",
		"8C:EA:48:AA:02:02": "lan3",
		"3C:D9:2B:AA:03:03": "lan3",
	})
	devices := []Device{
		dev("78:2B:CB:AA:01:01", "flint2", "cable"),
		dev("8C:EA:48:AA:02:02", "flint2", "cable"),
		// la 3ª MAC es desconocida (sin device): solo cuenta en macCount
	}
	devices, dists := inferTopology(polled, devices)
	if len(dists) != 1 {
		t.Fatalf("distnodes: %+v", dists)
	}
	dn := dists[0]
	if dn.ID != "dist-flint2-lan3" || dn.Kind != "inferred" || dn.RouterID != "flint2" || dn.Port != "lan3" || dn.MacCount != 3 {
		t.Fatalf("distnode: %+v", dn)
	}
	for _, d := range devices {
		if d.AttachTo != "dist-flint2-lan3" {
			t.Fatalf("attachTo de %s: %q", d.MAC, d.AttachTo)
		}
		if d.Port != "lan3" {
			t.Fatalf("port de %s: %q", d.MAC, d.Port)
		}
	}
}

func TestInferTopologyHipervisor(t *testing.T) {
	polled := polledWithFDB("flint2", "94:83:C4:00:00:01", map[string]string{
		"3C:52:82:10:20:30": "lan2", // host (OUI normal)
		"BC:24:11:00:20:10": "lan2", // CT1 (OUI Proxmox)
		"BC:24:11:00:20:11": "lan2", // CT2
	})
	devices := []Device{
		dev("3C:52:82:10:20:30", "flint2", "cable"),
		dev("BC:24:11:00:20:10", "flint2", "cable"),
		dev("BC:24:11:00:20:11", "flint2", "cable"),
	}
	devices, dists := inferTopology(polled, devices)
	if len(dists) != 1 {
		t.Fatalf("distnodes: %+v", dists)
	}
	dn := dists[0]
	hostID := devices[0].ID
	if dn.Kind != "hypervisor" || dn.HostDeviceID != hostID || dn.MacCount != 3 {
		t.Fatalf("distnode hipervisor: %+v", dn)
	}
	if devices[0].AttachTo != "" {
		t.Fatalf("el host cuelga del router, no del distnode: %q", devices[0].AttachTo)
	}
	for _, d := range devices[1:] {
		if d.AttachTo != hostID {
			t.Fatalf("CT %s attachTo: %q (esperaba host %s)", d.MAC, d.AttachTo, hostID)
		}
	}
}

func TestInferTopologyExclusiones(t *testing.T) {
	polled := polledWithFDB("flint2", "94:83:C4:00:00:01", map[string]string{
		"94:83:C4:00:00:02": "lan1", // bridge MAC de un AP (no es cliente)
		"F2:6D:19:A8:44:C2": "lan1", // cliente wifi del AP (otro routerId)
	})
	// El AP también aparece en polled con su propia brMac
	polled["living"] = &routerPolled{cfg: RouterConfig{ID: "living"}, brMac: "94:83:C4:00:00:02", fdb: map[string]string{}}
	devices := []Device{
		dev("F2:6D:19:A8:44:C2", "living", "5 GHz"),
	}
	devices, dists := inferTopology(polled, devices)
	if len(dists) != 0 {
		t.Fatalf("no debería inferir nada del uplink: %+v", dists)
	}
	if devices[0].Port != "" || devices[0].AttachTo != "" {
		t.Fatalf("cliente wifi del AP tocado: port=%q attachTo=%q", devices[0].Port, devices[0].AttachTo)
	}
}

func TestInferTopologyWanIgnorado(t *testing.T) {
	polled := polledWithFDB("flint2", "94:83:C4:00:00:01", map[string]string{
		"A0:B1:C2:D3:E4:F5": "wan",
		"A0:B1:C2:D3:E4:F6": "wan",
	})
	devices, dists := inferTopology(polled, nil)
	if len(dists) != 0 || len(devices) != 0 {
		t.Fatalf("el puerto WAN no genera distribución: %+v %+v", dists, devices)
	}
}

// Regla de anclaje al gateway (2-Ago-2026): un puerto multi-MAC en un AP sin
// evidencia de hipervisor es casi seguro su uplink → no se crea nodo ni se
// anota puerto; esos clientes quedan "sin evidencia" (el front los ancla al
// gateway). El mismo patrón en el gateway SÍ crea nodo inferido.
func TestInferTopologyApMultiMacSinEvidencia(t *testing.T) {
	polled := map[string]*routerPolled{
		"flint2": {cfg: RouterConfig{ID: "flint2", IsGateway: true}, brMac: "94:83:C4:00:00:01", fdb: map[string]string{
			"78:2B:CB:AA:01:01": "lan3",
			"8C:EA:48:AA:02:02": "lan3",
		}},
		"rt2": {cfg: RouterConfig{ID: "rt2"}, brMac: "94:83:C4:00:00:02", fdb: map[string]string{
			"3C:D9:2B:AA:03:03": "lan2",
			"50:EC:50:AA:04:04": "lan2",
		}},
	}
	devices := []Device{
		dev("78:2B:CB:AA:01:01", "flint2", "cable"),
		dev("8C:EA:48:AA:02:02", "flint2", "cable"),
		dev("3C:D9:2B:AA:03:03", "rt2", "cable"),
		dev("50:EC:50:AA:04:04", "rt2", "cable"),
	}
	devices, dists := inferTopology(polled, devices)
	if len(dists) != 1 || dists[0].ID != "dist-flint2-lan3" || dists[0].Kind != "inferred" {
		t.Fatalf("solo el gateway genera nodo inferido: %+v", dists)
	}
	for _, d := range devices[2:] {
		if d.Port != "" || d.AttachTo != "" {
			t.Fatalf("cliente tras AP sin evidencia tocado: %s port=%q attachTo=%q", d.MAC, d.Port, d.AttachTo)
		}
	}
}

// Excepción a la regla: hipervisor claro tras un AP (MACs OUI hipervisor +
// exactamente un host) = servidor con VMs colgando de ese AP.
func TestInferTopologyApHipervisor(t *testing.T) {
	polled := map[string]*routerPolled{
		"flint2": {cfg: RouterConfig{ID: "flint2", IsGateway: true}, brMac: "94:83:C4:00:00:01", fdb: map[string]string{}},
		"rt2": {cfg: RouterConfig{ID: "rt2"}, brMac: "94:83:C4:00:00:02", fdb: map[string]string{
			"3C:52:82:10:20:30": "lan1", // host
			"BC:24:11:00:20:10": "lan1", // CT1
			"BC:24:11:00:20:11": "lan1", // CT2
		}},
	}
	devices := []Device{
		dev("3C:52:82:10:20:30", "rt2", "cable"),
		dev("BC:24:11:00:20:10", "rt2", "cable"),
		dev("BC:24:11:00:20:11", "rt2", "cable"),
	}
	devices, dists := inferTopology(polled, devices)
	if len(dists) != 1 || dists[0].Kind != "hypervisor" || dists[0].RouterID != "rt2" {
		t.Fatalf("hipervisor tras AP: %+v", dists)
	}
	hostID := devices[0].ID
	for _, d := range devices[1:] {
		if d.AttachTo != hostID {
			t.Fatalf("CT %s attachTo: %q (esperaba host)", d.MAC, d.AttachTo)
		}
	}
}

// Puerto con 1 MAC en un AP: evidencia clara de cable directo → Port (igual
// que en el gateway).
func TestInferTopologyApCableadoDirecto(t *testing.T) {
	polled := map[string]*routerPolled{
		"flint2": {cfg: RouterConfig{ID: "flint2", IsGateway: true}, brMac: "94:83:C4:00:00:01", fdb: map[string]string{}},
		"rt2": {cfg: RouterConfig{ID: "rt2"}, brMac: "94:83:C4:00:00:02", fdb: map[string]string{
			"00:11:32:9C:51:B7": "lan3",
		}},
	}
	devices := []Device{dev("00:11:32:9C:51:B7", "rt2", "cable")}
	devices, dists := inferTopology(polled, devices)
	if len(dists) != 0 {
		t.Fatalf("no debería haber distnodes: %+v", dists)
	}
	if devices[0].Port != "lan3" {
		t.Fatalf("port: %q", devices[0].Port)
	}
}
