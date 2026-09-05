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
		{ID: "bc-24-11-a4-9e-bb", MAC: "BC:24:11:A4:9E:BB", Name: "webs", RouterID: "gateway", Band: "cable", Port: "lan3", AttachTo: "dist-gateway-lan3"},
		{ID: "02-78-f4-02-8a-94", MAC: "02:78:F4:02:8A:94", Name: "homeassistant", RouterID: "gateway", Band: "cable", Port: "lan3", AttachTo: "dist-gateway-lan3"},
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
	// El sello PVE sobreescribe el attachTo inferido por L2 (dist-gateway-lan3)
	// y cuelga los CTs del host.
	if devices[1].AttachTo != hostID {
		t.Errorf("webs attachTo=%q (want host %q, sobreescribe el inferred)", devices[1].AttachTo, hostID)
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

// TestSealProxmoxInfraHostPorIP: cuando NetPulse no conoce el NOMBRE del host
// (aparece solo por MAC/IP, p. ej. citadel-02 = E8:FF:1E... con IP .101), el
// host se casa por IP del nodo (vmbr0).
func TestSealProxmoxInfraHostPorIP(t *testing.T) {
	inv := &pveInventory{
		ctByMAC: map[string]pveVM{
			"BC:24:11:A4:9E:BB": {Name: "webs", Node: "citadel-02", Type: "lxc"},
		},
		nodeNames: map[string]bool{"citadel-02": true},
		nodeIPs:   map[string]string{"citadel-02": "192.168.1.101"},
	}
	devices := []Device{
		{ID: "e8-ff-1e-dd-c7-ed", MAC: "E8:FF:1E:DD:C7:ED", Name: "E8:FF:1E:DD:C7:ED", IP: "192.168.1.101", RouterID: "switch16"},
		{ID: "bc-24-11-a4-9e-bb", MAC: "BC:24:11:A4:9E:BB", Name: "webs", IP: "192.168.1.226", RouterID: "switch16", AttachTo: "dist-switch16-lan8"},
	}
	applyPVEInfra(devices, inv)
	if devices[0].Infra != "hypervisor" {
		t.Fatalf("host por IP: infra=%q (want hypervisor)", devices[0].Infra)
	}
	if devices[0].Name != "citadel-02" {
		t.Fatalf("host renombrado: name=%q (want citadel-02)", devices[0].Name)
	}
	if devices[1].AttachTo != devices[0].ID {
		t.Fatalf("webs attachTo=%q (want host por IP %q)", devices[1].AttachTo, devices[0].ID)
	}
}

// TestSealProxmoxInfraHostConflictoNICs: un host físico con dos NICs aparece
// como dos devices (p. ej. citadel-01 online con IP vmbr0 .100, y el mismo
// host offline con IP de gestión .243). El host correcto es el de la IP del
// nodo (vmbr0), aunque otro device tenga el nombre "citadel-01".
func TestSealProxmoxInfraHostConflictoNICs(t *testing.T) {
	inv := &pveInventory{
		ctByMAC: map[string]pveVM{
			"BC:24:11:A4:9E:BB": {Name: "webs", Node: "citadel-01", Type: "lxc"},
		},
		nodeNames: map[string]bool{"citadel-01": true},
		nodeIPs:   map[string]string{"citadel-01": "192.168.1.100"},
	}
	devices := []Device{
		// device con nombre citadel-01 pero IP de gestión (.243) y offline.
		{ID: "c8-ff-bf-0c-60-12", MAC: "C8:FF:BF:0C:60:12", Name: "citadel-01", IP: "192.168.1.243", Online: false},
		// device online con la IP vmbr0 del nodo (.100): host correcto.
		{ID: "fe-c9-95-97-15-30", MAC: "FE:C9:95:97:15:30", Name: "FE:C9:95:97:15:30", IP: "192.168.1.100", Online: true},
		{ID: "bc-24-11-a4-9e-bb", MAC: "BC:24:11:A4:9E:BB", Name: "webs", IP: "192.168.1.226", Online: true},
	}
	applyPVEInfra(devices, inv)
	// El hypervisor debe ser el device con IP .100 (online), no el offline.
	if devices[1].Infra != "hypervisor" || devices[1].Name != "citadel-01" {
		t.Fatalf("host por IP debería ganar: %+v (infra=%q name=%q)", devices[1], devices[1].Infra, devices[1].Name)
	}
	if devices[0].Infra == "hypervisor" {
		t.Fatalf("el device offline con nombre citadel-01 NO debe ser hypervisor: %+v", devices[0])
	}
	if devices[2].AttachTo != devices[1].ID {
		t.Fatalf("webs attachTo=%q (want host online %q)", devices[2].AttachTo, devices[1].ID)
	}
}

// TestPVEHostMACDeviceID: el id del device (MAC minúsculas con guiones) casa
// con la clave del inventario (MAC mayúsculas con ':') para CTs.
func TestPVEMacToDeviceID(t *testing.T) {
	if got := macToDeviceID("BC:24:11:A4:9E:BB"); got != "bc-24-11-a4-9e-bb" {
		t.Fatalf("macToDeviceID: %q", got)
	}
}
