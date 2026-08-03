package adapters

import "testing"

// SPEC-65 D65-2: Device.infra sellado server-side ("hypervisor" | "ct" |
// "managed-switch"); ausente = dispositivo normal.

// Demo: el canon sella pve=hypervisor, sus 10 CTs/VMs=ct y
// switch-netgear=managed-switch; ningún otro device lleva infra.
func TestInfraSelladoDemoCanon(t *testing.T) {
	devices := canonAllDevices()
	cts := 0
	for _, d := range devices {
		switch {
		case d.ID == "pve":
			if d.Infra != "hypervisor" {
				t.Fatalf("pve.infra=%q, esperaba hypervisor", d.Infra)
			}
		case d.ID == "switch-netgear":
			if d.Infra != "managed-switch" {
				t.Fatalf("switch-netgear.infra=%q, esperaba managed-switch", d.Infra)
			}
		case len(d.ID) > 3 && d.ID[:3] == "ct-":
			if d.Infra != "ct" {
				t.Fatalf("%s.infra=%q, esperaba ct", d.ID, d.Infra)
			}
			cts++
		default:
			if d.Infra != "" {
				t.Fatalf("%s no debería llevar infra: %q", d.ID, d.Infra)
			}
		}
	}
	if cts != 10 {
		t.Fatalf("CTs del pve: %d, esperaba 10", cts)
	}
	// Y llega al snapshot del overview.
	d := NewDemo()
	ov, err := d.GetOverview(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]string{}
	for _, dev := range ov.TopDevices {
		found[dev.ID] = dev.Infra
	}
	for _, dev := range d.GetDevices(t.Context()) {
		if dev.ID == "pve" && dev.Infra != "hypervisor" {
			t.Fatalf("pve.infra en /api/devices: %q", dev.Infra)
		}
	}
}

// Live: el pipeline de topología sella el host del hipervisor y sus VMs.
func TestInfraSelladoLiveHipervisor(t *testing.T) {
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
	devices, _ = inferTopology(polled, devices)
	if devices[0].Infra != "hypervisor" {
		t.Fatalf("host.infra=%q, esperaba hypervisor", devices[0].Infra)
	}
	for _, d := range devices[1:] {
		if d.Infra != "ct" {
			t.Fatalf("CT %s.infra=%q, esperaba ct", d.MAC, d.Infra)
		}
	}
}

// Live: un vecino LLDP managed cuya chassis-MAC existe como Device queda
// sellado managed-switch.
func TestInfraSelladoLiveManagedSwitch(t *testing.T) {
	polled := polledWithFDB("living", "C1:83:C4:00:00:02", map[string]string{
		"28:C6:8E:1D:90:44": "lan3", // chassis-MAC del GS308E
		"7C:ED:8D:4A:11:22": "lan3", // cliente tras el switch
	})
	polled["living"].lldp = []LldpNeighbor{{
		Port: "lan3", ChassisMac: "28:C6:8E:1D:90:44", Chassis: "GS308E",
		Mgmt: "192.168.8.13", Caps: []string{"Bridge"},
	}}
	devices := []Device{
		dev("28:C6:8E:1D:90:44", "living", "cable"),
		dev("7C:ED:8D:4A:11:22", "living", "cable"),
	}
	devices, dists := inferTopology(polled, devices)
	if len(dists) != 1 || dists[0].Kind != "managed" {
		t.Fatalf("distnode managed esperado: %+v", dists)
	}
	if devices[0].Infra != "managed-switch" {
		t.Fatalf("switch.infra=%q, esperaba managed-switch", devices[0].Infra)
	}
	if devices[1].Infra != "" {
		t.Fatalf("el cliente tras el switch no lleva infra: %q", devices[1].Infra)
	}
}
