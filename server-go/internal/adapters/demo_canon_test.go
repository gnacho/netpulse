package adapters

import (
	"context"
	"testing"
)

// Tests del canon reconciliado (SPEC-CANON fase 6, D1/D3/D4/D5). Fijan la
// enumeración del dataset: sin IDs duplicadas, totales derivados, el switch
// gestionado como Device + distnode managed y los puertos del gateway.

func deviceByID(t *testing.T, devices []Device, id string) Device {
	t.Helper()
	for _, d := range devices {
		if d.ID == id {
			return d
		}
	}
	t.Fatalf("device %q no existe en el dataset", id)
	return Device{}
}

// D3: una sola identidad por ID en el dataset reconciliado.
func TestCanonDatasetSinIDsDuplicadas(t *testing.T) {
	devices := canonAllDevices()
	seen := map[string]int{}
	for _, d := range devices {
		seen[d.ID]++
	}
	for id, n := range seen {
		if n != 1 {
			t.Fatalf("ID duplicada %q: %d definiciones", id, n)
		}
	}
	// Las dos identidades reconciliadas, en su versión final:
	xbox := deviceByID(t, devices, "xbox-series-s")
	if !xbox.Online || xbox.Band != "cable" || xbox.AttachTo != "dist-living-lan3" {
		t.Fatalf("xbox-series-s debería ser la cableada al GS308E: %+v", xbox)
	}
	hp := deviceByID(t, devices, "impresora-hp")
	if !hp.Online || hp.Band != "cable" || hp.AttachTo != "dist-flint2-lan3" {
		t.Fatalf("impresora-hp debería ser la cableada al switch inferido: %+v", hp)
	}
}

// D5: los totales se derivan del dataset — la enumeración manda.
func TestCanonTotalesDerivadosDelDataset(t *testing.T) {
	devices := canonAllDevices()
	totals := deviceTotalsOf(devices)
	// Enumeración del canon reconciliado: 65 IDs únicos, 59 online, 6
	// offline conocidos, 3 nuevos hoy.
	if totals != (DeviceTotals{Total: 65, Online: 59, KnownOffline: 6, NewToday: 3}) {
		t.Fatalf("enumeración del dataset: %+v", totals)
	}

	d := NewDemo()
	ov, err := d.GetOverview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ov.DeviceTotals != totals {
		t.Fatalf("overview.deviceTotals no derivado del dataset: %+v vs %+v", ov.DeviceTotals, totals)
	}
	if ov.Adguard.ClientsTotal != totals.Total {
		t.Fatalf("adguard.clientsTotal=%d, dataset=%d", ov.Adguard.ClientsTotal, totals.Total)
	}
	// router.clients = recuento real online por router; la suma = online.
	sum := 0
	porRouter := map[string]int{}
	for _, r := range ov.Routers {
		porRouter[r.ID] = r.Clients
		sum += r.Clients
		if want := onlineClientsOf(devices, r.ID); r.Clients != want {
			t.Fatalf("router %s clients=%d, enumeración=%d", r.ID, r.Clients, want)
		}
	}
	if sum != totals.Online {
		t.Fatalf("suma de clients por router=%d, online=%d", sum, totals.Online)
	}
	// Canon fijado: flint2/living/estudio/patio = 26/20/8/5.
	want := map[string]int{"flint2": 26, "living": 20, "estudio": 8, "patio": 5}
	for id, w := range want {
		if porRouter[id] != w {
			t.Fatalf("router %s clients=%d, canon=%d", id, porRouter[id], w)
		}
	}
}

// D1: el switch gestionado GS308E es Device (type switch, lldp, attachTo al
// router, port de uplink) Y DistributionNode managed con Mac — y su Device
// NO cuelga de su propio nodo (D2, paridad con inferTopology).
func TestCanonSwitchGestionadoDeviceYNodo(t *testing.T) {
	devices := canonAllDevices()
	sw := deviceByID(t, devices, "switch-netgear")
	if sw.Type != "switch" || sw.IP != "192.168.8.13" || !sw.Online {
		t.Fatalf("GS308E device: %+v", sw)
	}
	if sw.RouterID != "living" || sw.AttachTo != "living" || sw.Port != "lan3" {
		t.Fatalf("GS308E attachTo/port de uplink: %+v", sw)
	}
	if sw.Lldp == nil || sw.Lldp.Chassis != "GS308E" || sw.Lldp.Mgmt != "192.168.8.13" {
		t.Fatalf("GS308E device sin lldp: %+v", sw.Lldp)
	}

	var managed *DistributionNode
	for i, dn := range canonDistributionNodes() {
		if dn.Kind == "managed" {
			if managed != nil {
				t.Fatalf("más de un distnode managed")
			}
			managed = &canonDistributionNodes()[i]
		}
	}
	if managed == nil {
		t.Fatal("no hay distnode managed")
	}
	if managed.ID != "dist-living-lan3" || managed.Mac == "" {
		t.Fatalf("distnode managed sin mac: %+v", managed)
	}
	if managed.Mac != sw.MAC {
		t.Fatalf("distnode.Mac=%q ≠ device.MAC=%q (la app cruza ambas para ocultar el chip)", managed.Mac, sw.MAC)
	}
	if sw.AttachTo == managed.ID {
		t.Fatalf("el switch NO debe colgar de su propio nodo: %+v", sw)
	}
	// Y llega por el adapter demo (overview + devices).
	d := NewDemo()
	ov, _ := d.GetOverview(context.Background())
	found := false
	for _, dn := range ov.DistributionNodes {
		if dn.ID == "dist-living-lan3" {
			found = true
			if dn.Mac != "28:C6:8E:1D:90:44" {
				t.Fatalf("overview distnode managed mac: %q", dn.Mac)
			}
		}
	}
	if !found {
		t.Fatal("overview sin dist-living-lan3")
	}
	deviceByID(t, d.GetDevices(context.Background()), "switch-netgear")
}

// D4: puertos del gateway y del Salón en devices y distnodes.
func TestCanonPuertosD4(t *testing.T) {
	devices := canonAllDevices()
	if d := deviceByID(t, devices, "nas-synology"); d.Port != "lan4" {
		t.Fatalf("NAS port: %q (D4: lan4)", d.Port)
	}
	if d := deviceByID(t, devices, "pve"); d.Port != "lan5" {
		t.Fatalf("pve port: %q (D4: lan5)", d.Port)
	}
	if d := deviceByID(t, devices, "ps5"); d.Port != "lan1" {
		t.Fatalf("PS5 port: %q (D4: lan1)", d.Port)
	}
	for _, dn := range canonDistributionNodes() {
		if dn.ID == "dist-pve" && dn.Port != "lan5" {
			t.Fatalf("dist-pve port: %q (D4: lan5)", dn.Port)
		}
		if dn.ID == "dist-flint2-lan3" && dn.Port != "lan3" {
			t.Fatalf("dist-flint2-lan3 port: %q", dn.Port)
		}
	}
	// Panel ethPorts del gateway cuadra con la tabla D4.
	extras := canonRouterExtras()["flint2"]
	ports := map[string]EthPort{}
	for _, p := range extras.EthPorts {
		ports[p.ID] = p
	}
	if ports["lan3"].ConnectedTo != "Switch sin gestión" || !ports["lan3"].Up {
		t.Fatalf("flint2 lan3 (switch inferido): %+v", ports["lan3"])
	}
	if ports["lan4"].ConnectedTo != "NAS Synology" || !ports["lan4"].Up {
		t.Fatalf("flint2 lan4 (NAS): %+v", ports["lan4"])
	}
	if ports["lan5"].ConnectedTo != "Proxmox pve" || ports["lan5"].Speed != "2.5 Gbps" || !ports["lan5"].Up {
		t.Fatalf("flint2 lan5 (pve 2.5G): %+v", ports["lan5"])
	}
	// Salón: lan3 = GS308E UP (antes "libre/down").
	living := map[string]EthPort{}
	for _, p := range canonRouterExtras()["living"].EthPorts {
		living[p.ID] = p
	}
	if !living["lan3"].Up || living["lan3"].ConnectedTo != "GS308E" {
		t.Fatalf("living lan3 (GS308E up): %+v", living["lan3"])
	}
}
