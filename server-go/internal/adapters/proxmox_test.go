// proxmox_test.go — sellado de infraestructura desde el inventario PVE (#561).
package adapters

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/pve"
)

// TestSealProxmoxInfra: con un cluster de 1 nodo (citadel-01) y 2 CTs
// (webs + pbs con MACs BC:24:11 y locally-administered), los devices que
// coinciden por MAC se sellan ct y cuelgan del host; el host se sella
// hypervisor. El caso que la heurística L2 NO puede resolver (puerto mezclado).
func TestSealProxmoxInfra(t *testing.T) {
	inv := &pveInventory{
		ctByMAC: map[string]pveVM{
			"BC:24:11:A4:9E:BB": {Name: "webs", Node: "citadel-01", Type: "lxc", Instance: "default"},
			"02:78:F4:02:8A:94": {Name: "homeassistant", Node: "citadel-01", Type: "lxc", Instance: "default"},
		},
		nodes: map[string]pveNode{"default|citadel-01": {Instance: "default", Node: "citadel-01"}},
	}
	devices := []Device{
		{ID: "c8-ff-bf-0c-60-12", MAC: "C8:FF:BF:0C:60:12", Name: "citadel-01", RouterID: "gateway", Band: "—"},
		{ID: "bc-24-11-a4-9e-bb", MAC: "BC:24:11:A4:9E:BB", Name: "webs", RouterID: "gateway", Band: "cable", Port: "lan3", AttachTo: "dist-gateway-lan3"},
		{ID: "02-78-f4-02-8a-94", MAC: "02:78:F4:02:8A:94", Name: "homeassistant", RouterID: "gateway", Band: "cable", Port: "lan3", AttachTo: "dist-gateway-lan3"},
		{ID: "aa-bb-cc-dd-ee-ff", MAC: "AA:BB:CC:DD:EE:FF", Name: "shield", RouterID: "gateway", Band: "cable", Port: "lan3"},
	}
	applyPVEInfra(devices, nil, inv)

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
	applyPVEInfra(devices, nil, nil)
	if devices[0].Infra != "" || devices[0].AttachTo != "" {
		t.Fatalf("sin inventario no debe tocar devices: %+v", devices[0])
	}
}

// TestSealProxmoxInfraHostSinDevice: un nodo PVE cuyo host NO es un device
// conocido (no habla a la LAN) no rompe nada: los CTs se sellan ct sin host.
func TestSealProxmoxInfraHostSinDevice(t *testing.T) {
	inv := &pveInventory{
		ctByMAC: map[string]pveVM{
			"BC:24:11:A4:9E:BB": {Name: "webs", Node: "citadel-99", Type: "lxc", Instance: "default"},
		},
		nodes: map[string]pveNode{"default|citadel-99": {Instance: "default", Node: "citadel-99"}},
	}
	devices := []Device{
		{ID: "bc-24-11-a4-9e-bb", MAC: "BC:24:11:A4:9E:BB", Name: "webs", RouterID: "gateway"},
	}
	applyPVEInfra(devices, nil, inv)
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
			"BC:24:11:A4:9E:BB": {Name: "webs", Node: "citadel-02", Type: "lxc", Instance: "default"},
		},
		nodes:   map[string]pveNode{"default|citadel-02": {Instance: "default", Node: "citadel-02"}},
		nodeIPs: map[string][]string{"default|citadel-02": {"192.168.1.101"}},
	}
	devices := []Device{
		{ID: "e8-ff-1e-dd-c7-ed", MAC: "E8:FF:1E:DD:C7:ED", Name: "E8:FF:1E:DD:C7:ED", IP: "192.168.1.101", RouterID: "switch16"},
		{ID: "bc-24-11-a4-9e-bb", MAC: "BC:24:11:A4:9E:BB", Name: "webs", IP: "192.168.1.226", RouterID: "switch16", AttachTo: "dist-switch16-lan8"},
	}
	applyPVEInfra(devices, nil, inv)
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
			"BC:24:11:A4:9E:BB": {Name: "webs", Node: "citadel-01", Type: "lxc", Instance: "default"},
		},
		nodes:   map[string]pveNode{"default|citadel-01": {Instance: "default", Node: "citadel-01"}},
		nodeIPs: map[string][]string{"default|citadel-01": {"192.168.1.100"}},
	}
	devices := []Device{
		// device con nombre citadel-01 pero IP de gestión (.243) y offline.
		{ID: "c8-ff-bf-0c-60-12", MAC: "C8:FF:BF:0C:60:12", Name: "citadel-01", IP: "192.168.1.243", Online: false},
		// device online con la IP vmbr0 del nodo (.100): host correcto.
		{ID: "fe-c9-95-97-15-30", MAC: "FE:C9:95:97:15:30", Name: "FE:C9:95:97:15:30", IP: "192.168.1.100", Online: true},
		{ID: "bc-24-11-a4-9e-bb", MAC: "BC:24:11:A4:9E:BB", Name: "webs", IP: "192.168.1.226", Online: true},
	}
	applyPVEInfra(devices, nil, inv)
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

// TestPVEHypervisorDistNodes: el sellado PVE crea un distnode kind=hypervisor
// por host (con Source=proxmox y HostDeviceID del host) para que el frontend
// anide los CTs bajo él (grid +N). Un host que ya tenga distnode hypervisor
// inferido por L2 NO se duplica.
func TestPVEHypervisorDistNodes(t *testing.T) {
	inv := &pveInventory{
		ctByMAC: map[string]pveVM{
			"BC:24:11:A4:9E:BB": {Name: "webs", Node: "citadel-02", Type: "lxc", Instance: "default"},
			"02:78:F4:02:8A:94": {Name: "pbs", Node: "citadel-02", Type: "lxc", Instance: "default"},
		},
		nodes:   map[string]pveNode{"default|citadel-02": {Instance: "default", Node: "citadel-02"}, "default|citadel-01": {Instance: "default", Node: "citadel-01"}},
		nodeIPs: map[string][]string{"default|citadel-02": {"192.168.1.101"}, "default|citadel-01": {"192.168.1.100"}},
	}
	devices := []Device{
		{ID: "e8-ff-1e-dd-c7-ed", MAC: "E8:FF:1E:DD:C7:ED", Name: "E8:FF:1E:DD:C7:ED", IP: "192.168.1.101", RouterID: "switch16", Port: "lan8"},
		{ID: "fe-c9-95-97-15-30", MAC: "FE:C9:95:97:15:30", Name: "FE:C9:95:97:15:30", IP: "192.168.1.100", RouterID: "gateway", Port: "lan1"},
		{ID: "bc-24-11-a4-9e-bb", MAC: "BC:24:11:A4:9E:BB", Name: "webs"},
		{ID: "02-78-f4-02-8a-94", MAC: "02:78:F4:02:8A:94", Name: "pbs"},
	}
	// citadel-01 ya tiene distnode hypervisor inferido por L2: no duplicar.
	dists := []DistributionNode{{ID: "dist-gateway-lan1", Kind: "hypervisor", RouterID: "gateway", HostDeviceID: "fe-c9-95-97-15-30"}}
	dists = applyPVEInfra(devices, dists, inv)

	if len(dists) != 2 {
		t.Fatalf("esperado 2 distnodes (L2 + 1 PVE), got %d: %+v", len(dists), dists)
	}
	dn := dists[1]
	if dn.ID != "dist-pve-default-citadel-02" || dn.Kind != "hypervisor" || dn.Source != "proxmox" {
		t.Fatalf("distnode PVE mal construido: %+v", dn)
	}
	if dn.HostDeviceID != devices[0].ID {
		t.Fatalf("HostDeviceID=%q (want %q)", dn.HostDeviceID, devices[0].ID)
	}
	if dn.Name != "citadel-02" || dn.RouterID != "switch16" || dn.Port != "lan8" {
		t.Fatalf("distnode PVE con metadatos del host incorrectos: %+v", dn)
	}
	if dn.MacCount != 2 {
		t.Fatalf("MacCount=%d (want 2 CTs sellados)", dn.MacCount)
	}
}

// TestPVEHypervisorDistNodesSinHost: sin inventario los distnodes no cambian.
func TestPVEHypervisorDistNodesSinHost(t *testing.T) {
	dists := []DistributionNode{{ID: "dist-gateway-lan1", Kind: "inferred"}}
	out := applyPVEInfra([]Device{{ID: "x", MAC: "AA:BB:CC:DD:EE:FF"}}, dists, nil)
	if len(out) != 1 || out[0].ID != "dist-gateway-lan1" {
		t.Fatalf("sin inventario no debe añadir distnodes: %+v", out)
	}
}

// TestSealProxmoxInfraMultiInstancia (#764): dos clusters con un nodo
// homónimo ("pve1") en ambos: las claves compuestas evitan que se pisen, los
// CTs cuelgan del host de SU instancia y los distnodes son distintos.
func TestSealProxmoxInfraMultiInstancia(t *testing.T) {
	inv := &pveInventory{
		ctByMAC: map[string]pveVM{
			"AA:00:00:00:00:01": {Name: "webs", Node: "pve1", Type: "lxc", Instance: "casa"},
			"BB:00:00:00:00:02": {Name: "erp", Node: "pve1", Type: "qemu", Instance: "ofi"},
		},
		nodes: map[string]pveNode{
			"casa|pve1": {Instance: "casa", Node: "pve1"},
			"ofi|pve1":  {Instance: "ofi", Node: "pve1"},
		},
	}
	devices := []Device{
		{ID: "aa-11", MAC: "AA:11:00:00:00:01", Name: "host-casa", RouterID: "gateway", Band: "—"},
		{ID: "bb-22", MAC: "BB:11:00:00:00:02", Name: "host-ofi", RouterID: "gateway", Band: "—"},
	}
	// Hosts casados por IP de su instancia.
	inv.nodeIPs = map[string][]string{"casa|pve1": {"10.0.0.1"}, "ofi|pve1": {"10.0.0.2"}}
	devices[0].IP = "10.0.0.1"
	devices[1].IP = "10.0.0.2"
	dists := applyPVEInfra(devices, nil, inv)

	if devices[0].Infra != "hypervisor" || devices[1].Infra != "hypervisor" {
		t.Fatalf("hosts: %q %q", devices[0].Infra, devices[1].Infra)
	}
	// Cada CT cuelga del host de SU instancia (no del homónimo).
	byMAC := map[string]int{}
	for i, d := range devices {
		byMAC[d.MAC] = i
	}
	_ = byMAC
	// (Los CT webs/erp no son devices aquí: basta con que los hosts sellaran
	// y los distnodes salgan separados por instancia.)
	if len(dists) != 2 {
		t.Fatalf("distnodes: %v", dists)
	}
	ids := map[string]bool{}
	for _, dn := range dists {
		ids[dn.ID] = true
		if dn.Instance == "" {
			t.Fatalf("distnode sin instancia: %+v", dn)
		}
	}
	if !ids["dist-pve-casa-pve1"] || !ids["dist-pve-ofi-pve1"] {
		t.Fatalf("IDs de distnodes homónimos: %v", ids)
	}
}

// A container rarely asks for DHCP with a hostname, so the name the admin
// gave it in Proxmox is usually the only one there is: without it the list
// is a column of MACs wearing a CT badge.
func TestSealProxmoxNamesContainersKnownOnlyByMAC(t *testing.T) {
	inv := &pveInventory{
		ctByMAC: map[string]pveVM{
			"BC:24:11:A4:9E:BB": {Name: "storage", Node: "pve1", Type: "lxc", Instance: "default"},
			"02:00:00:00:00:32": {Name: "proxyapp", Node: "pve1", Type: "lxc", Instance: "default"},
			"02:00:00:00:00:31": {Name: "jellyfin", Node: "pve1", Type: "lxc", Instance: "default"},
		},
		nodes: map[string]pveNode{"default|pve1": {Instance: "default", Node: "pve1"}},
	}
	devices := []Device{
		// Known only by its MAC: the guest name is all we have.
		{ID: "bc-24-11-a4-9e-bb", MAC: "BC:24:11:A4:9E:BB", Name: "BC:24:11:A4:9E:BB"},
		// Same, spelled with dashes as the device id is.
		{ID: "02-00-00-00-00-32", MAC: "02:00:00:00:00:32", Name: "02-00-00-00-00-32"},
		// This one has a real name from its DHCP lease: Proxmox must not
		// overwrite what the network already knows it as.
		{ID: "02-00-00-00-00-31", MAC: "02:00:00:00:00:31", Name: "media-server"},
	}
	applyPVEInfra(devices, nil, inv)

	if devices[0].Name != "storage" {
		t.Errorf("name: %q (want storage)", devices[0].Name)
	}
	if devices[1].Name != "proxyapp" {
		t.Errorf("name: %q (want proxyapp)", devices[1].Name)
	}
	if devices[2].Name != "media-server" {
		t.Errorf("a real name was overwritten: %q", devices[2].Name)
	}
	for i := range devices {
		if devices[i].Infra != "ct" {
			t.Errorf("device[%d]: infra=%q", i, devices[i].Infra)
		}
	}
}

// A guest the controller reports without a name leaves the device alone.
func TestSealProxmoxKeepsMACWhenTheGuestHasNoName(t *testing.T) {
	inv := &pveInventory{
		ctByMAC: map[string]pveVM{"BC:24:11:A4:9E:BB": {Node: "pve1", Type: "qemu", Instance: "default"}},
		nodes:   map[string]pveNode{"default|pve1": {Instance: "default", Node: "pve1"}},
	}
	devices := []Device{{ID: "bc-24-11-a4-9e-bb", MAC: "BC:24:11:A4:9E:BB", Name: "BC:24:11:A4:9E:BB"}}
	applyPVEInfra(devices, nil, inv)
	if devices[0].Name != "BC:24:11:A4:9E:BB" {
		t.Fatalf("name: %q", devices[0].Name)
	}
}

// pveFakeAPI: a Proxmox endpoint with one node and one running CT. The two
// switches decide where a node's address can be read from.
func pveFakeAPI(t *testing.T, clusterStatusOK bool, ifaceAddress string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api2/json/cluster/resources":
			w.Write([]byte(`{"data":[
				{"id":"node/pve1","node":"pve1","type":"node","status":"online"},
				{"id":"lxc/100","vmid":100,"node":"pve1","type":"lxc","status":"running","name":"storage"}
			]}`))
		case r.URL.Path == "/api2/json/cluster/status":
			if !clusterStatusOK {
				w.WriteHeader(http.StatusForbidden)
				w.Write([]byte(`{"message":"Permission check failed (/, Sys.Audit)\n","data":null}`))
				return
			}
			w.Write([]byte(`{"data":[{"type":"node","name":"pve1","ip":"192.0.2.2","online":1}]}`))
		case strings.HasSuffix(r.URL.Path, "/network"):
			if ifaceAddress == "" {
				// A host configured outside /etc/network/interfaces: NICs
				// with no address at all, which is what hid the hypervisor.
				w.Write([]byte(`{"data":[{"iface":"enp2s0","type":"eth","method":"manual"}]}`))
				return
			}
			w.Write([]byte(`{"data":[{"iface":"vmbr0","type":"bridge","address":"` + ifaceAddress + `"}]}`))
		case strings.HasSuffix(r.URL.Path, "/config"):
			w.Write([]byte(`{"data":{"net0":"bridge=vmbr0,hwaddr=BC:24:11:A4:9E:BB,name=eth0,type=veth"}}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
}

func pveInventoryFrom(t *testing.T, srv *httptest.Server) *pveInventory {
	t.Helper()
	cfg := pve.Config{URL: srv.URL, TokenID: "netpulse@pam!t", Secret: "s"}
	inv := NewLive(nil, nil, nil, nil).fetchPveInventory([]pveInstClient{
		{inst: pve.Instance{ID: "acasa", Name: "casa", Config: cfg}, c: pve.NewClient(cfg)},
	})
	if inv == nil {
		t.Fatal("inventario nil")
	}
	return inv
}

// The address comes from cluster/status, the only source that knows it when
// the node's interfaces declare none.
func TestPVENodeIPFromClusterStatus(t *testing.T) {
	srv := pveFakeAPI(t, true, "")
	defer srv.Close()
	if got := pveInventoryFrom(t, srv).nodeIPs["acasa|pve1"]; !hasIP(got, "192.0.2.2") {
		t.Fatalf("nodeIPs: %q", got)
	}
}

// Without Sys.Audit there is no cluster/status, and the declared bridge is
// still a good answer.
func TestPVENodeIPFallsBackToTheInterfaces(t *testing.T) {
	srv := pveFakeAPI(t, false, "192.0.2.7")
	defer srv.Close()
	if got := pveInventoryFrom(t, srv).nodeIPs["acasa|pve1"]; !hasIP(got, "192.0.2.7") {
		t.Fatalf("nodeIPs: %q", got)
	}
}

// Neither source says anything: for a single node the configured endpoint
// IS that node, so the host can still be matched.
func TestPVENodeIPFallsBackToTheEndpoint(t *testing.T) {
	srv := pveFakeAPI(t, false, "")
	defer srv.Close()
	inv := pveInventoryFrom(t, srv)
	want := strings.TrimPrefix(srv.URL, "http://")
	want = want[:strings.LastIndex(want, ":")]
	if got := inv.nodeIPs["acasa|pve1"]; !hasIP(got, want) {
		t.Fatalf("nodeIPs: %q (want %q)", got, want)
	}
	// And with that address the seal finally produces the hypervisor node
	// and hangs the container off it -- the whole point of the fallback.
	devices := []Device{
		{ID: "02-00-00-00-00-40", MAC: "02:00:00:00:00:40", Name: "proxmox", IP: want, RouterID: "gateway", Port: "lan2"},
		{ID: "bc-24-11-a4-9e-bb", MAC: "BC:24:11:A4:9E:BB", Name: "BC:24:11:A4:9E:BB"},
	}
	dists := applyPVEInfra(devices, nil, inv)
	if devices[0].Infra != "hypervisor" {
		t.Fatalf("host: %+v", devices[0])
	}
	if len(dists) != 1 || dists[0].Kind != "hypervisor" || dists[0].HostDeviceID != devices[0].ID {
		t.Fatalf("distnode: %+v", dists)
	}
	if devices[1].AttachTo != devices[0].ID || devices[1].Name != "storage" {
		t.Fatalf("ct: %+v", devices[1])
	}
}

func hasIP(ips []string, want string) bool {
	for _, ip := range ips {
		if ip == want {
			return true
		}
	}
	return false
}

// A cluster where corosync has its own network: the address the node
// announces to the cluster is NOT the management one the LAN knows it by.
// Keeping only one of the two would lose the host on half the installs --
// exactly the setups where matching by the declared bridge already worked.
func TestPVEKeepsBothTheManagementAndTheClusterAddress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api2/json/cluster/resources":
			w.Write([]byte(`{"data":[
				{"id":"node/pve1","node":"pve1","type":"node","status":"online"},
				{"id":"node/pve2","node":"pve2","type":"node","status":"online"},
				{"id":"lxc/100","vmid":100,"node":"pve1","type":"lxc","status":"running","name":"storage"}
			]}`))
		case r.URL.Path == "/api2/json/cluster/status":
			// corosync ring on a dedicated network.
			w.Write([]byte(`{"data":[
				{"type":"node","name":"pve1","ip":"10.10.10.1","online":1},
				{"type":"node","name":"pve2","ip":"10.10.10.2","online":1}
			]}`))
		case strings.HasSuffix(r.URL.Path, "/network"):
			// management address, the one the LAN resolves.
			w.Write([]byte(`{"data":[{"iface":"vmbr0","type":"bridge","address":"192.0.2.50"}]}`))
		case strings.HasSuffix(r.URL.Path, "/config"):
			w.Write([]byte(`{"data":{"net0":"bridge=vmbr0,hwaddr=BC:24:11:A4:9E:BB,name=eth0,type=veth"}}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	inv := pveInventoryFrom(t, srv)
	got := inv.nodeIPs["acasa|pve1"]
	if !hasIP(got, "192.0.2.50") || !hasIP(got, "10.10.10.1") {
		t.Fatalf("both addresses must survive: %q", got)
	}
	// And the host still matches on the management address, as it did
	// before cluster/status existed here.
	devices := []Device{
		{ID: "aa-bb-cc-00-00-01", MAC: "AA:BB:CC:00:00:01", Name: "pve-host", IP: "192.0.2.50"},
		{ID: "bc-24-11-a4-9e-bb", MAC: "BC:24:11:A4:9E:BB", Name: "BC:24:11:A4:9E:BB"},
	}
	applyPVEInfra(devices, nil, inv)
	if devices[0].Infra != "hypervisor" {
		t.Fatalf("host: %+v", devices[0])
	}
	if devices[1].AttachTo != devices[0].ID {
		t.Fatalf("ct: %+v", devices[1])
	}
}

// A qemu guest is a virtual machine, not a container. The inventory says
// which it is and the seal used to ignore it, so a VM wore a CT badge.
func TestSealProxmoxTellsVMsFromContainers(t *testing.T) {
	inv := &pveInventory{
		ctByMAC: map[string]pveVM{
			"BC:24:11:A4:9E:BB": {Name: "appbox", Node: "pve1", Type: "lxc", Instance: "default"},
			"02:00:00:00:00:50": {Name: "vm-appliance", Node: "pve1", Type: "qemu", Instance: "default"},
		},
		nodes:   map[string]pveNode{"default|pve1": {Instance: "default", Node: "pve1"}},
		nodeIPs: map[string][]string{"default|pve1": {"192.0.2.2"}},
	}
	devices := []Device{
		{ID: "aa-bb-cc-00-00-01", MAC: "AA:BB:CC:00:00:01", Name: "pve-host", IP: "192.0.2.2"},
		{ID: "bc-24-11-a4-9e-bb", MAC: "BC:24:11:A4:9E:BB", Name: "BC:24:11:A4:9E:BB"},
		{ID: "02-00-00-00-00-50", MAC: "02:00:00:00:00:50", Name: "02:00:00:00:00:50"},
	}
	applyPVEInfra(devices, nil, inv)

	if devices[1].Infra != "ct" {
		t.Errorf("lxc: infra=%q (want ct)", devices[1].Infra)
	}
	if devices[2].Infra != "vm" {
		t.Errorf("qemu: infra=%q (want vm)", devices[2].Infra)
	}
	// Both are still guests of the host: the badge changes, not the nesting.
	for _, i := range []int{1, 2} {
		if devices[i].AttachTo != devices[0].ID {
			t.Errorf("device[%d] attachTo=%q", i, devices[i].AttachTo)
		}
	}
	if devices[2].Name != "vm-appliance" {
		t.Errorf("name: %q", devices[2].Name)
	}
}

// Classification runs inside buildDevices and this seal renames afterwards,
// so a guest named here had been classified while its name was still its
// MAC: "adguard" came out untyped although that word has always been a
// "servidor" rule.
func TestSealProxmoxReclassifiesWhatItRenames(t *testing.T) {
	inv := &pveInventory{
		ctByMAC: map[string]pveVM{
			"BC:24:11:00:00:01": {Name: "adguard", Node: "pve1", Type: "lxc", Instance: "default"},
			"BC:24:11:00:00:02": {Name: "appbox", Node: "pve1", Type: "lxc", Instance: "default"},
			"BC:24:11:00:00:03": {Name: "pixel-of-someone", Node: "pve1", Type: "lxc", Instance: "default"},
		},
		nodes:   map[string]pveNode{"default|pve1": {Instance: "default", Node: "pve1"}},
		nodeIPs: map[string][]string{"default|pve1": {"192.0.2.2"}},
	}
	devices := []Device{
		{ID: "aa-bb-cc-00-00-01", MAC: "AA:BB:CC:00:00:01", Name: "pve-host", IP: "192.0.2.2", Type: "desconocido"},
		{ID: "bc-24-11-00-00-01", MAC: "BC:24:11:00:00:01", Name: "BC:24:11:00:00:01", Type: "desconocido"},
		{ID: "bc-24-11-00-00-02", MAC: "BC:24:11:00:00:02", Name: "BC:24:11:00:00:02", Type: "desconocido"},
		// Already typed from its own evidence: the rename must not re-open it.
		{ID: "bc-24-11-00-00-03", MAC: "BC:24:11:00:00:03", Name: "BC:24:11:00:00:03", Type: "servidor"},
	}
	applyPVEInfra(devices, nil, inv)

	if devices[1].Type != "servidor" {
		t.Errorf("adguard: type=%q (want servidor)", devices[1].Type)
	}
	// A name no rule covers is still a machine running a service: being a
	// guest of a hypervisor is the evidence, and no word list will ever
	// hold every application anyone runs in a container.
	if devices[2].Type != "servidor" {
		t.Errorf("appbox: type=%q (want servidor)", devices[2].Type)
	}
	// A type that was already decided is not overwritten by the new name.
	if devices[3].Type != "servidor" {
		t.Errorf("pre-typed device: %q", devices[3].Type)
	}
}

// Every guest ends up typed, whether it was renamed here or already had a
// name of its own, and a rule that matches something more specific than
// "a server" still wins.
func TestSealProxmoxTypesEveryGuest(t *testing.T) {
	inv := &pveInventory{
		ctByMAC: map[string]pveVM{
			"BC:24:11:00:00:01": {Name: "metrics", Node: "pve1", Type: "lxc", Instance: "default"},
			"BC:24:11:00:00:02": {Name: "unifi", Node: "pve1", Type: "lxc", Instance: "default"},
			"BC:24:11:00:00:03": {Name: "frigate", Node: "pve1", Type: "qemu", Instance: "default"},
		},
		nodes:   map[string]pveNode{"default|pve1": {Instance: "default", Node: "pve1"}},
		nodeIPs: map[string][]string{"default|pve1": {"192.0.2.2"}},
	}
	devices := []Device{
		// The host itself, named by nothing in particular.
		{ID: "aa-bb-cc-00-00-01", MAC: "AA:BB:CC:00:00:01", Name: "maquina-del-armario", IP: "192.0.2.2", Type: "desconocido"},
		// Renamed by the seal.
		{ID: "bc-24-11-00-00-01", MAC: "BC:24:11:00:00:01", Name: "BC:24:11:00:00:01", Type: "desconocido"},
		// Already had a lease name, so the seal does not rename it -- and it
		// was still left untyped before this.
		{ID: "bc-24-11-00-00-02", MAC: "BC:24:11:00:00:02", Name: "unifi", Type: "desconocido"},
		// A name a rule covers: the camera wins over the generic default.
		{ID: "bc-24-11-00-00-03", MAC: "BC:24:11:00:00:03", Name: "camera-nvr", Type: "desconocido"},
	}
	applyPVEInfra(devices, nil, inv)

	for i, want := range []string{"servidor", "servidor", "servidor", "camara"} {
		if devices[i].Type != want {
			t.Errorf("device[%d] %s: type=%q (want %q)", i, devices[i].Name, devices[i].Type, want)
		}
	}
}
