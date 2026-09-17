package pve

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Fixtures del formato REAL de las configs de Proxmox (verificado en
// pve-docs chapter-pct/chapter-qm).
func TestMACsOfConfigLXC(t *testing.T) {
	cfg := map[string]any{
		"hostname": "webs",
		"net0":     "bridge=vmbr0,hwaddr=BC:24:11:A4:9E:BB,ip=dhcp,name=eth0,type=veth",
		"net1":     "bridge=vmbr0,hwaddr=bc:24:11:00:00:01,ip=dhcp,name=eth1,type=veth",
		"memory":   float64(2048),
	}
	macs := MACsOfConfig(cfg)
	sort.Strings(macs)
	want := []string{"BC:24:11:00:00:01", "BC:24:11:A4:9E:BB"}
	sort.Strings(want)
	if !reflect.DeepEqual(macs, want) {
		t.Fatalf("macs lxc: %v (want %v)", macs, want)
	}
}

func TestMACsOfConfigQEMU(t *testing.T) {
	cfg := map[string]any{
		"name": "pbs-vm",
		"net0": "e1000=EE:D2:28:5F:B6:3E,bridge=vmbr0",
		"net1": "virtio=52:54:00:12:34:56,bridge=vmbr0",
	}
	macs := MACsOfConfig(cfg)
	sort.Strings(macs)
	want := []string{"52:54:00:12:34:56", "EE:D2:28:5F:B6:3E"}
	sort.Strings(want)
	if !reflect.DeepEqual(macs, want) {
		t.Fatalf("macs qemu: %v (want %v)", macs, want)
	}
}

func TestMACsOfConfigIgnoraCampos(t *testing.T) {
	// net0 sin MAC (ip estática sin hwaddr explícita), otros campos, basura.
	cfg := map[string]any{
		"net0":   "name=eth0,bridge=vmbr0,ip=192.168.1.10/24",
		"net1":   "bridge=vmbr0,hwaddr=00:11:22:33:44:55,ip=dhcp,name=eth1",
		"memory": "2048",               // no es net*, se ignora
		"netx":   "no es una NIC real", // prefijo net pero sin MAC
	}
	macs := MACsOfConfig(cfg)
	want := []string{"00:11:22:33:44:55"}
	if !reflect.DeepEqual(macs, want) {
		t.Fatalf("macs: %v (want %v)", macs, want)
	}
}

// TestClientClusterResources: el cliente parsea {"data":[...]} de la API PVE
// y manda el header de token.
func TestClientClusterResources(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/api2/json/cluster/resources" {
			t.Errorf("path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[
			{"id":"node/citadel-01","node":"citadel-01","type":"node","status":"online","name":"citadel-01"},
			{"id":"lxc/100","vmid":100,"node":"citadel-01","type":"lxc","status":"running","name":"webs"},
			{"id":"qemu/200","vmid":200,"node":"citadel-02","type":"qemu","status":"running","name":"pbs"},
			{"id":"lxc/101","vmid":101,"node":"citadel-01","type":"lxc","status":"stopped","name":"apagado"}
		]}`))
	}))
	defer srv.Close()

	c := NewClient(Config{URL: srv.URL, TokenID: "root@pam!netpulse", Secret: "uuid-secret"})
	res, err := c.ClusterResources(context.Background())
	if err != nil {
		t.Fatalf("cluster: %v", err)
	}
	if len(res) != 4 {
		t.Fatalf("resources: %d", len(res))
	}
	if res[1].VMID != 100 || res[1].Type != "lxc" || res[1].Node != "citadel-01" || res[1].Name != "webs" || res[1].Status != "running" {
		t.Fatalf("lxc webs: %+v", res[1])
	}
	if res[2].VMID != 200 || res[2].Type != "qemu" || res[2].Node != "citadel-02" {
		t.Fatalf("qemu pbs: %+v", res[2])
	}
	if res[0].VMID != 0 || res[0].Type != "node" {
		t.Fatalf("node: %+v", res[0])
	}
	if want := "PVEAPIToken=root@pam!netpulse=uuid-secret"; gotAuth != want {
		t.Fatalf("auth: %q (want %q)", gotAuth, want)
	}
}

func TestClientErrors(t *testing.T) {
	// 401 sin token válido
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"errors":{"root@pam!netpulse":"no such token"}}`))
	}))
	defer srv.Close()
	c := NewClient(Config{URL: srv.URL, TokenID: "root@pam!netpulse", Secret: "mal"})
	if _, err := c.ClusterResources(context.Background()); err == nil {
		t.Fatal("401 debería dar error")
	}
}

func TestConfigEnabled(t *testing.T) {
	if (Config{}).Enabled() {
		t.Fatal("config vacía no enabled")
	}
	if !(Config{URL: "https://x:8006", TokenID: "a!b", Secret: "c"}).Enabled() {
		t.Fatal("config completa debería ser enabled")
	}
}

func TestNodeIP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"data":[
			{"iface":"eno1","type":"eth","address":""},
			{"iface":"vmbr0","type":"bridge","address":"192.168.1.101"},
			{"iface":"vmbr0","type":"bridge","address":"fe80::1"}
		]}`))
	}))
	defer srv.Close()
	c := NewClient(Config{URL: srv.URL, TokenID: "a!b", Secret: "c"})
	ip, err := c.NodeIP(context.Background(), "citadel-02")
	if err != nil {
		t.Fatalf("nodeip: %v", err)
	}
	if ip != "192.168.1.101" {
		t.Fatalf("ip: %q", ip)
	}
}

// pveTestServer: an endpoint that answers /version and /cluster/resources
// with what it is given, and /nodes/{n}/status with the audit verdict.
func pveTestServer(t *testing.T, resources string, nodeStatus int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api2/json/version":
			w.Write([]byte(`{"data":{"release":"8.4","version":"8.4.21"}}`))
		case r.URL.Path == "/api2/json/cluster/resources":
			w.Write([]byte(`{"data":[` + resources + `]}`))
		case strings.HasSuffix(r.URL.Path, "/status"):
			w.WriteHeader(nodeStatus)
			if nodeStatus == 403 {
				w.Write([]byte(`{"message":"Permission check failed (/nodes/pve1, Sys.Audit)\n","data":null}`))
				return
			}
			w.Write([]byte(`{"data":{"uptime":1}}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
}

func TestTestCountsWhatItSees(t *testing.T) {
	srv := pveTestServer(t, `
		{"id":"node/pve1","node":"pve1","type":"node","status":"online"},
		{"id":"lxc/100","vmid":100,"node":"pve1","type":"lxc","status":"running"},
		{"id":"qemu/200","vmid":200,"node":"pve1","type":"qemu","status":"running"},
		{"id":"qemu/201","vmid":201,"node":"pve1","type":"qemu","status":"stopped"},
		{"id":"storage/local","node":"pve1","type":"storage"}`, 200)
	defer srv.Close()

	res, err := NewClient(Config{URL: srv.URL, TokenID: "netpulse@pam!t", Secret: "s"}).Test(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Version != "8.4.21" || res.Nodes != 1 || res.VMs != 2 || res.CTs != 1 {
		t.Fatalf("result: %+v", res)
	}
	// With guests in sight there is nothing to warn about.
	if res.Limited {
		t.Fatalf("limited should be false: %+v", res)
	}
}

// The failure the endpoint exists for: the token authenticates, PVE answers
// 200 with empty lists because it filters what the token may not see, and
// the integration goes quiet with nothing to show for it.
func TestTestDetectsATokenWithoutAuditRights(t *testing.T) {
	srv := pveTestServer(t, `{"id":"node/pve1","node":"pve1","type":"node","status":"online"}`, 403)
	defer srv.Close()

	res, err := NewClient(Config{URL: srv.URL, TokenID: "netpulse@pam!t", Secret: "s"}).Test(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Limited {
		t.Fatalf("a 403 on the node status means the token is blind: %+v", res)
	}
	if res.Nodes != 1 || res.VMs != 0 || res.CTs != 0 {
		t.Fatalf("result: %+v", res)
	}
}

// A cluster that really is empty is not reported as a permissions problem.
func TestTestDoesNotCryWolfOnAnEmptyCluster(t *testing.T) {
	srv := pveTestServer(t, `{"id":"node/pve1","node":"pve1","type":"node","status":"online"}`, 200)
	defer srv.Close()

	res, err := NewClient(Config{URL: srv.URL, TokenID: "netpulse@pam!t", Secret: "s"}).Test(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Limited {
		t.Fatalf("no guests but full rights: %+v", res)
	}
}

func TestTestReportsUnreachableAndUnconfigured(t *testing.T) {
	if _, err := NewClient(Config{URL: "http://127.0.0.1:1", TokenID: "a!b", Secret: "s"}).
		Test(context.Background()); err == nil {
		t.Fatal("expected a connection error")
	}
	if _, err := NewClient(Config{URL: "http://x"}).Test(context.Background()); err == nil {
		t.Fatal("expected an error for an incomplete config")
	}
}
