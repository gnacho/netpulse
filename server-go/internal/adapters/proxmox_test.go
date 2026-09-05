// proxmox_test.go — sellado de infraestructura desde el inventario PVE (#561).
package adapters

import (
	"testing"

)

// TestSealProxmoxInfra: con un cluster de 1 nodo (citadel-01) y 2 CTs
// (webs + pbs con MACs BC:24:11 y locally-administered), los devices que
// coinciden por MAC se sellan ct y cuelgan del host; el host se sella
// hypervisor. El caso que la heurística L2 NO puede resolver (puerto mezclado).
func TestSealProxmoxInfra(t *testing.T) {
	inv := &pveInventory{
		ctByMAC: map[string]pveVM{
			"BC:24:11:A4:9E:BB": {Name: "webs", Node: "citadel-01", Type: "lxc"},
			"02:78:F4:02:8A:94": {Name: "homeassistant", Node: "citadel-01", Type: "lxc"},
		},
		nodeNames: map[string]bool{"citadel-01": true},
	}
	devices := []Device{
		{ID: "c8-ff-bf-0c-60-12", MAC: "C8:FF:BF:0C:60:12", Name: "citadel-01", RouterID: "gateway", Band: "—"},
		{ID: "bc-24-11-a4-9e-bb", MAC: "BC:24:11:A4:9E:BB", Name: "webs", RouterID: "gateway", Band: "cable", Port: "lan3"},
		{ID: "02-78-f4-02-8a-94", MAC: "02:78:F4:02:8A:94", Name: "homeassistant", RouterID: "gateway", Band: "cable", Port: "lan3"},
		{ID: "aa-bb-cc-dd-ee-ff", MAC: "AA:BB:CC:DD:EE:FF", Name: "shield", RouterID: "gateway", Band: "cable", Port: "lan3"},
	}
	applyPVEInfra(devices, inv)

	if devices[0].Infra != "hypervisor" {
		t.Fatalf("host citadel-01: infra=%q (want hypervisor)", devices[0].Infra)
	}
	hostID := devices[0].ID
	for i, want := range map[int]string{
		1: "ct", 2: "ct", 3: "",
	} {
		if devices[i].Infra != want {
			t.Errorf("device[%d] %s: infra=%q (want %q)", i, devices[i].Name, devices[i].Infra, want)
		}
	}
	if devices[1].AttachTo != hostID {
		t.Errorf("webs attachTo=%q (want host %q)", devices[1].AttachTo, hostID)
	}
	if devices[2].AttachTo != hostID {
		t.Errorf("homeassistant attachTo=%q (want host %q)", devices[2].AttachTo, hostID)
	}
	// El device no relacionado (shield) queda intacto.
	if devices[3].Infra != "" || devices[3].AttachTo != "" {
		t.Errorf("shield tocado: %+v", devices[3])
	}
}

// TestSealProxmoxInfraSinInventario: sin cluster configurado o sin CTs
// conocidos → no-op (los devices no cambian).
func TestSealProxmoxInfraSinInventario(t *testing.T) {
	devices := []Device{{ID: "c8-ff-bf-0c-60-12", MAC: "C8:FF:BF:0C:60:12", Name: "citadel-01"}}
	applyPVEInfra(devices, nil)
	if devices[0].Infra != "" || devices[0].AttachTo != "" {
		t.Fatalf("sin inventario no debe tocar devices: %+v", devices[0])
	}
}

// TestSealProxmoxInfraHostSinDevice: un nodo PVE cuyo host NO es un device
// conocido (no habla a la LAN) no rompe nada: los CTs se sellan ct sin host.
func TestSealProxmoxInfraHostSinDevice(t *testing.T) {
	inv := &pveInventory{
		ctByMAC: map[string]pveVM{
			"BC:24:11:A4:9E:BB": {Name: "webs", Node: "citadel-99", Type: "lxc"},
		},
		nodeNames: map[string]bool{"citadel-99": true},
	}
	devices := []Device{
		{ID: "bc-24-11-a4-9e-bb", MAC: "BC:24:11:A4:9E:BB", Name: "webs", RouterID: "gateway"},
	}
	applyPVEInfra(devices, inv)
	if devices[0].Infra != "ct" {
		t.Fatalf("webs infra=%q (want ct aunque no haya host device)", devices[0].Infra)
	}
	if devices[0].AttachTo != "" {
		t.Fatalf("sin host device no debe haber attachTo: %q", devices[0].AttachTo)
	}
}

// TestPVEHostMACDeviceID: el id del device (MAC minúsculas con guiones) casa
// con la clave del inventario (MAC mayúsculas con ':') para CTs.
func TestPVEMacToDeviceID(t *testing.T) {
	if got := macToDeviceID("BC:24:11:A4:9E:BB"); got != "bc-24-11-a4-9e-bb" {
		t.Fatalf("macToDeviceID: %q", got)
	}
}
