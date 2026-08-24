package orchestr

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/executor"
)

// Fixture: túnel wg0 activo con 2 peers (admin + uno gestionado).
// Formato real de `wg show` + `uci show network`.
const wireGuardOut = `===WG_BIN===
yes
===WG===
interface: wg0
  public key: SERVERPUB
  listening port: 51820
  peer: ADMINPUB
    allowed ips: 10.0.0.2/32
  peer: PEER2PUB
    allowed ips: 10.0.0.3/32
===NETWORK===
network.lan=interface
network.lan.device='br-lan'
network.wg0=interface
network.wg0.proto='wireguard'
network.wg0.private_key='secret'
network.wg0.addresses='10.0.0.1/24'
network.wgpeer1=wireguard_wg0
network.wgpeer1.public_key='ADMINPUB'
network.wgpeer1.allowed_ips='10.0.0.2/32'
network.wgpeer2=wireguard_wg0
network.wgpeer2.public_key='PEER2PUB'
network.wgpeer2.allowed_ips='10.0.0.3/32'
network.wgpeer2.allowed_ips='10.0.0.9/32'
===END===`

// fakeWGCommandRunner implementa CommandRunner para tests del probe/healthcheck.
type fakeWGCommandRunner struct {
	out  string
	err  error
	cmd  string
	host string
}

func (f *fakeWGCommandRunner) Run(host, cmd string, _ time.Duration) (string, error) {
	f.host, f.cmd = host, cmd
	return f.out, f.err
}

// TestParseWireGuardDetectaTunelYPeers: wg instalado, túnel activo y peers.
func TestParseWireGuardDetectaTunelYPeers(t *testing.T) {
	sc := parseWireGuard(wireGuardOut)
	if !sc.WGInstalled {
		t.Error("WGInstalled: esperaba true")
	}
	if !sc.WGActive {
		t.Error("WGActive: esperaba true (wg show reporta wg0)")
	}
	if len(sc.ActiveIfaces) != 1 || sc.ActiveIfaces[0] != "wg0" {
		t.Errorf("ActiveIfaces: got %v want [wg0]", sc.ActiveIfaces)
	}
	if len(sc.WGIfaces) != 1 || sc.WGIfaces[0] != "wg0" {
		t.Errorf("WGIfaces: got %v want [wg0]", sc.WGIfaces)
	}
	if len(sc.Peers) != 2 {
		t.Fatalf("Peers: got %d want 2", len(sc.Peers))
	}
	if sc.Peers[0].Name != "wgpeer1" || sc.Peers[0].PublicKey != "ADMINPUB" {
		t.Errorf("peer[0]: got %+v", sc.Peers[0])
	}
	if sc.Peers[1].AllowedIPs != "10.0.0.3/32,10.0.0.9/32" {
		t.Errorf("peer[1] allowed_ips (lista): got %q", sc.Peers[1].AllowedIPs)
	}
}

// TestParseWireGuardSinTunel: wg instalado pero sin interfaces ni peers.
func TestParseWireGuardSinTunel(t *testing.T) {
	out := `===WG_BIN===
yes
===WG===
===NETWORK===
network.lan=interface
===END===`
	sc := parseWireGuard(out)
	if !sc.WGInstalled {
		t.Error("WGInstalled: esperaba true")
	}
	if sc.WGActive {
		t.Error("WGActive: esperaba false sin interfaces")
	}
	if len(sc.WGIfaces) != 0 || len(sc.Peers) != 0 {
		t.Errorf("no debería haber ifaces/peers: %v %v", sc.WGIfaces, sc.Peers)
	}
}

// TestParseWireGuardWgNoInstalado: wg ausente → escenario tolerante (no error).
func TestParseWireGuardWgNoInstalado(t *testing.T) {
	out := `===WG_BIN===
no
===WG===
===NETWORK===
network.lan=interface
===END===`
	sc := parseWireGuard(out)
	if sc.WGInstalled {
		t.Error("WGInstalled: esperaba false")
	}
	if sc.WGActive {
		t.Error("WGActive: esperaba false sin wg")
	}
}

// TestWireGuardOpsCreaInterfazYPeers: sin interfaz ni túnel, el plan crea
// wg0 + un peer y termina con commit + reload (sin wg_check: no había túnel).
func TestWireGuardOpsCreaInterfazYPeers(t *testing.T) {
	sc := parseWireGuard(`===WG_BIN===
no
===WG===
===NETWORK===
network.lan=interface
===END===`)
	ops := WireGuardOps(WireGuardDesired{
		Interface: "wg0",
		Peers:     []WireGuardPeer{{Name: "laptop", PublicKey: "PEER1PUB", AllowedIPs: "10.0.0.2/32"}},
	}, sc)
	if len(ops) == 0 {
		t.Fatal("plan generó 0 ops")
	}
	if err := validateWireGuardOps(ops); err != nil {
		t.Fatalf("ops no validan en executor: %v", err)
	}
	// Crea la interfaz (uci_set_named network.wg0=interface + proto).
	if !hasOp(ops, "uci_set_named", "section", "wg0") {
		t.Error("plan sin uci_set_named de la interfaz wg0")
	}
	if !hasOp(ops, "uci_set", "option", "proto") {
		t.Error("plan sin uci_set proto=wireguard")
	}
	// Crea el peer wgpeer1 con su public_key y allowed_ips.
	if !hasOp(ops, "uci_set_named", "section", "wgpeer1") {
		t.Error("plan sin uci_set_named del peer wgpeer1")
	}
	if !hasOp(ops, "uci_set", "option", "public_key") {
		t.Error("plan sin uci_set public_key del peer")
	}
	if !hasOp(ops, "uci_add_list", "option", "allowed_ips") {
		t.Error("plan sin uci_add_list allowed_ips del peer")
	}
	if !hasOp(ops, "uci_commit", "config", "network") {
		t.Error("plan sin uci_commit network")
	}
	if !hasOp(ops, "service", "action", "reload") {
		t.Error("plan sin service network reload")
	}
	// Sin túnel activo previo → sin wg_check (nada que proteger).
	for _, o := range ops {
		if o.Kind == "wg_check" {
			t.Error("no debería incluir wg_check sin túnel activo previo")
		}
	}
}

// TestWireGuardOpsNoBorraPeerAdmin: anti-lockout. El peer del admin está en el
// router pero no en el desired; con AdminPeerPublicKey se conserva. El otro
// peer (en desired) se actualiza, no se borra.
func TestWireGuardOpsNoBorraPeerAdmin(t *testing.T) {
	sc := parseWireGuard(wireGuardOut)
	ops := WireGuardOps(WireGuardDesired{
		Interface:          "wg0",
		AdminPeerPublicKey: "ADMINPUB",
		Peers: []WireGuardPeer{
			{Name: "peer2", PublicKey: "PEER2PUB", AllowedIPs: "10.0.0.3/32"},
		},
	}, sc)
	if err := validateWireGuardOps(ops); err != nil {
		t.Fatalf("ops no validan: %v", err)
	}
	// Sin protección, wgpeer1 (admin) sería candidato a borrado. Con
	// AdminPeerPublicKey no debe aparecer ningún uci_delete_section.
	for _, o := range ops {
		if o.Kind == "uci_delete_section" {
			t.Errorf("anti-lockout roto: plan borra la sección %q", o.Args["section"])
		}
	}
}

// TestWireGuardOpsBorraPeersSinDesired: sin anti-lockout configurado, los
// peers que no están en el desired se borran.
func TestWireGuardOpsBorraPeersSinDesired(t *testing.T) {
	sc := parseWireGuard(wireGuardOut)
	ops := WireGuardOps(WireGuardDesired{Interface: "wg0"}, sc)
	delSections := map[string]bool{}
	for _, o := range ops {
		if o.Kind == "uci_delete_section" {
			delSections[o.Args["section"]] = true
		}
	}
	if !delSections["wgpeer1"] || !delSections["wgpeer2"] {
		t.Errorf("desired sin peers debe borrar ambos: %v", delSections)
	}
}

// TestWireGuardOpsHealthcheckSoloSiTunelActivo: wg_check solo si el túnel
// estaba arriba antes del apply (proteger el túnel existente).
func TestWireGuardOpsHealthcheckSoloSiTunelActivo(t *testing.T) {
	sc := parseWireGuard(wireGuardOut)
	ops := WireGuardOps(WireGuardDesired{Interface: "wg0", Peers: []WireGuardPeer{{Name: "p", PublicKey: "PEER2PUB", AllowedIPs: "10.0.0.3/32"}}}, sc)
	found := false
	for _, o := range ops {
		if o.Kind == "wg_check" {
			found = true
			if o.Args["interface"] != "wg0" {
				t.Errorf("wg_check interface: got %q want wg0", o.Args["interface"])
			}
		}
	}
	if !found {
		t.Error("con túnel activo previo el plan debe incluir wg_check")
	}
}

// TestWireGuardOpsActualizaAllowedIPs: un peer existente con allowed_ips
// distinto se resetea (delete + add_list) sin recrear la sección.
func TestWireGuardOpsActualizaAllowedIPs(t *testing.T) {
	sc := parseWireGuard(wireGuardOut)
	ops := WireGuardOps(WireGuardDesired{
		Interface: "wg0",
		Peers:     []WireGuardPeer{{Name: "peer2", PublicKey: "PEER2PUB", AllowedIPs: "10.0.0.3/32,10.0.0.42/32"}},
	}, sc)
	if err := validateWireGuardOps(ops); err != nil {
		t.Fatalf("ops no validan: %v", err)
	}
	if !hasOp(ops, "uci_delete", "section", "wgpeer2") {
		t.Error("debe resetear allowed_ips del peer existente (uci_delete)")
	}
	count := 0
	for _, o := range ops {
		if o.Kind == "uci_add_list" && o.Args["section"] == "wgpeer2" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("esperaba 2 add_list en wgpeer2, got %d", count)
	}
}

// TestWireGuardOpsSinCambios: desired == scenario → sin ops (idempotente).
func TestWireGuardOpsSinCambios(t *testing.T) {
	sc := parseWireGuard(wireGuardOut)
	ops := WireGuardOps(WireGuardDesired{
		Interface: "wg0",
		Peers: []WireGuardPeer{
			{Name: "admin", PublicKey: "ADMINPUB", AllowedIPs: "10.0.0.2/32"},
			{Name: "peer2", PublicKey: "PEER2PUB", AllowedIPs: "10.0.0.3/32,10.0.0.9/32"},
		},
	}, sc)
	if len(ops) != 0 {
		t.Errorf("sin diferencias debería generar 0 ops, got %d:\n%+v", len(ops), ops)
	}
}

// TestWireGuardHealthcheck: `wg show wg0` arriba → nil; fallo → error.
func TestWireGuardHealthcheck(t *testing.T) {
	run := &fakeWGCommandRunner{out: "interface: wg0\n  public key: X\n"}
	if err := WireGuardHealthcheck(run, "192.168.1.1", "wg0"); err != nil {
		t.Fatalf("túnel arriba: esperaba nil, got %v", err)
	}
	if !strings.Contains(run.cmd, "wg show wg0") {
		t.Errorf("comando: got %q, esperaba contener 'wg show wg0'", run.cmd)
	}

	run.err = errors.New("wg show exit 1")
	if err := WireGuardHealthcheck(run, "192.168.1.1", "wg0"); err == nil {
		t.Error("túnel caído: esperaba error")
	}
}

// hasOp busca una op por kind + (arg, value).
func hasOp(ops []executor.Op, kind, arg, value string) bool {
	for _, o := range ops {
		if o.Kind != kind {
			continue
		}
		if v, ok := o.Args[arg]; ok && v == value {
			return true
		}
	}
	return false
}
