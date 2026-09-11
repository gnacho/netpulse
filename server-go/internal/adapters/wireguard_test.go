// wireguard_test.go — tests de parseo del adapter WireGuard (issue #659):
// resolución del nombre del peer por la option `description` de UCI.
package adapters

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestParseWGUciDescs(t *testing.T) {
	uci := `network.wg0=interface
network.wg0.proto='wireguard'
network.wg0.private_key='x'
network.peer1=wireguard_wg0
network.peer1.public_key='AAA='
network.peer1.allowed_ips='10.0.0.2/32'
network.peer1.description='iPhone'
network.peer2=wireguard_wg0
network.peer2.public_key='BBB='
network.peer2.allowed_ips='10.1.0.0/24'
network.peer2.description='Site A'
network.peer3=wireguard_wg0
network.peer3.public_key='CCC='
network.peer3.allowed_ips='10.0.0.4/32'`

	descs := parseWGUciDescs(uci)

	cases := map[string]string{
		"AAA=":        "iPhone",
		"10.0.0.2":    "iPhone", // allowed_ip /32 normalizada
		"BBB=":        "Site A",
		"10.1.0.0/24": "Site A", // allowed_ip /24 → clave tal cual (solo se recorta /32)
		"CCC=":        "",
		"10.0.0.4":    "", // peer sin description → descartado
		"peerX=":      "",
		"10.0.0.99":   "",
	}
	for key, want := range cases {
		if got := descs[key]; got != want {
			t.Errorf("descs[%q] = %q, want %q", key, got, want)
		}
	}
}

func TestSplitWGUci(t *testing.T) {
	out := "peer-a\tAAA=\t10.0.0.2/32\n===NETPULSE_UCI===\nnetwork.wg0=interface\n"
	dump, uci := splitWGUci(out)
	if dump != "peer-a\tAAA=\t10.0.0.2/32\n" {
		t.Errorf("dump = %q, want dump", dump)
	}
	if uci != "network.wg0=interface\n" {
		t.Errorf("uci = %q, want uci", uci)
	}

	// Sin marker: todo es dump, uci vacío.
	dump, uci = splitWGUci("solo-dump\n")
	if dump != "solo-dump\n" || uci != "" {
		t.Errorf("sin marker: dump=%q uci=%q, want dump con uci vacío", dump, uci)
	}
}

func TestWGStatsCommand(t *testing.T) {
	cmd := wgStatsCommand("wg0")
	if !contains(cmd, "wg show wg0 dump") {
		t.Errorf("comando no incluye el dump de la interfaz: %q", cmd)
	}
	if !contains(cmd, "===NETPULSE_UCI===") {
		t.Errorf("comando no incluye el marker UCI: %q", cmd)
	}
	if !contains(cmd, "exit $rc") {
		t.Errorf("comando no preserva el exit de wg: %q", cmd)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// --- Descubrimiento multi-interfaz (#713) ---

// fakeWGRunner implementa sshRunner: responde por comando vía un handler y
// registra los comandos ejecutados para poder afirmar qué se sondeó.
type fakeWGRunner struct {
	run   func(cmd string) (string, error)
	calls []string
}

func (f *fakeWGRunner) Run(host, cmd string, timeout time.Duration) (string, error) {
	f.calls = append(f.calls, cmd)
	if f.run == nil {
		return "", nil
	}
	return f.run(cmd)
}

// wgPeerDump construye la salida de `wg show <iface> dump`: una línea de
// interfaz (ignorada por ParseWGDump) y una línea por peer (8 campos TSV).
func wgPeerDump(iface string, peers ...string) string {
	out := iface + "\tPRIVKEY\t\t51820\n"
	for _, p := range peers {
		out += p + "\n"
	}
	return out
}

// wgPeerLine construye la línea de un peer (8 campos TSV).
func wgPeerLine(pubkey, allowedIPs, handshake string) string {
	return fmt.Sprintf("%s\t\t(none)\t%s\t%s\t1000\t2000\t25", pubkey, allowedIPs, handshake)
}

// wgStatsOutput compone la salida combinada dump + marker + UCI que
// GetWireGuardStats espera del comando de stats.
func wgStatsOutput(dump, uci string) string {
	return dump + wgUciMarker + "\n" + uci
}

func TestParseWGInterfaces(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"wg0\n", []string{"wg0"}},
		{"wg0 wg1 wg2\n", []string{"wg0", "wg1", "wg2"}},
		{"  wg0\t wg1 \n", []string{"wg0", "wg1"}},
		{"", []string{}},
		{"   \n", []string{}},
	}
	for _, c := range cases {
		got := parseWGInterfaces(c.in)
		if len(got) != len(c.want) {
			t.Errorf("parseWGInterfaces(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("parseWGInterfaces(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestWGInterfacesFilter(t *testing.T) {
	// Filtro explícito: no se ejecuta ningún comando en el router.
	f := &fakeWGRunner{}
	for _, tc := range []struct {
		iface string
		want  []string
	}{
		{"wg0", []string{"wg0"}},
		{"wg0,wg1", []string{"wg0", "wg1"}},
		{" wg0 , wg1 , ", []string{"wg0", "wg1"}},
	} {
		got, err := wgInterfaces(f, "host", tc.iface)
		if err != nil {
			t.Fatalf("wgInterfaces(%q): %v", tc.iface, err)
		}
		if len(got) != len(tc.want) {
			t.Errorf("wgInterfaces(%q) = %v, want %v", tc.iface, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("wgInterfaces(%q)[%d] = %q, want %q", tc.iface, i, got[i], tc.want[i])
			}
		}
	}
	if len(f.calls) != 0 {
		t.Errorf("filtro explícito no debe sondear el router, llamadas: %v", f.calls)
	}

	// auto / vacío: descubre con `wg show interfaces`.
	for _, iface := range []string{"auto", ""} {
		f := &fakeWGRunner{run: func(cmd string) (string, error) {
			return "wg0 wg1\n", nil
		}}
		got, err := wgInterfaces(f, "host", iface)
		if err != nil {
			t.Fatalf("wgInterfaces(%q): %v", iface, err)
		}
		if len(got) != 2 || got[0] != "wg0" || got[1] != "wg1" {
			t.Errorf("wgInterfaces(%q) = %v, want [wg0 wg1]", iface, got)
		}
		if len(f.calls) != 1 || f.calls[0] != wgListCommand {
			t.Errorf("wgInterfaces(%q) llamadas = %v, want [%s]", iface, f.calls, wgListCommand)
		}
	}
}

func TestGetWireGuardStatsSingleInterface(t *testing.T) {
	dump := wgPeerDump("wg0", wgPeerLine("AAA=", "10.0.0.2/32", fmt.Sprintf("%d", time.Now().Unix()-10)))
	uci := "network.peer1=wireguard_wg0\nnetwork.peer1.public_key='AAA='\nnetwork.peer1.allowed_ips='10.0.0.2/32'\nnetwork.peer1.description='iPhone'\n"
	f := &fakeWGRunner{run: func(cmd string) (string, error) {
		switch {
		case cmd == wgListCommand:
			return "wg0\n", nil
		case strings.Contains(cmd, "wg show wg0 dump"):
			return wgStatsOutput(dump, uci), nil
		}
		return "", fmt.Errorf("comando inesperado: %s", cmd)
	}}
	stats, err := GetWireGuardStats(f, "host", "auto", "", nil)
	if err != nil {
		t.Fatalf("GetWireGuardStats: %v", err)
	}
	if stats.Interface != "wg0" {
		t.Errorf("Interface = %q, want wg0", stats.Interface)
	}
	if stats.Status != "active" {
		t.Errorf("Status = %q, want active", stats.Status)
	}
	if len(stats.Peers) != 1 {
		t.Fatalf("len(Peers) = %d, want 1", len(stats.Peers))
	}
	p := stats.Peers[0]
	if p.Name != "iPhone" {
		t.Errorf("Name = %q, want iPhone", p.Name)
	}
	if p.TunnelIP != "10.0.0.2" {
		t.Errorf("TunnelIP = %q, want 10.0.0.2", p.TunnelIP)
	}
	if p.ID != "peer-1" {
		t.Errorf("ID = %q, want peer-1", p.ID)
	}
	if !p.Active {
		t.Errorf("Active = false, want true (handshake reciente)")
	}
}

func TestGetWireGuardStatsMultipleInterfaces(t *testing.T) {
	dump0 := wgPeerDump("wg0", wgPeerLine("AAA=", "10.0.0.2/32", "0"))
	dump1 := wgPeerDump("wg1", wgPeerLine("BBB=", "10.1.0.2/32", "0"))
	f := &fakeWGRunner{run: func(cmd string) (string, error) {
		switch {
		case cmd == wgListCommand:
			return "wg0 wg1\n", nil
		case strings.Contains(cmd, "wg show wg0 dump"):
			return wgStatsOutput(dump0, ""), nil
		case strings.Contains(cmd, "wg show wg1 dump"):
			return wgStatsOutput(dump1, ""), nil
		}
		return "", fmt.Errorf("comando inesperado: %s", cmd)
	}}
	stats, err := GetWireGuardStats(f, "host", "auto", "", nil)
	if err != nil {
		t.Fatalf("GetWireGuardStats: %v", err)
	}
	if stats.Interface != "wg0, wg1" {
		t.Errorf("Interface = %q, want \"wg0, wg1\"", stats.Interface)
	}
	if len(stats.Peers) != 2 {
		t.Fatalf("len(Peers) = %d, want 2", len(stats.Peers))
	}
	if stats.Peers[0].ID == stats.Peers[1].ID {
		t.Errorf("IDs de peer no únicos: %q", stats.Peers[0].ID)
	}
	if stats.Peers[0].ID != "peer-1" || stats.Peers[1].ID != "peer-2" {
		t.Errorf("IDs = [%q %q], want [peer-1 peer-2]", stats.Peers[0].ID, stats.Peers[1].ID)
	}
	// Nombres caen al tunnelIP (sin peerNames ni UCI).
	if stats.Peers[0].Name != "10.0.0.2" || stats.Peers[1].Name != "10.1.0.2" {
		t.Errorf("Names = [%q %q], want [10.0.0.2 10.1.0.2]", stats.Peers[0].Name, stats.Peers[1].Name)
	}
}

func TestGetWireGuardStatsNoInterfaces(t *testing.T) {
	f := &fakeWGRunner{run: func(cmd string) (string, error) {
		if cmd == wgListCommand {
			return "", nil
		}
		return "", fmt.Errorf("comando inesperado: %s", cmd)
	}}
	stats, err := GetWireGuardStats(f, "host", "auto", "", nil)
	if err != nil {
		t.Fatalf("GetWireGuardStats: %v", err)
	}
	if stats.Status != "inactive" {
		t.Errorf("Status = %q, want inactive", stats.Status)
	}
	if len(stats.Peers) != 0 {
		t.Errorf("len(Peers) = %d, want 0", len(stats.Peers))
	}
	if stats.Interface != "" {
		t.Errorf("Interface = %q, want \"\"", stats.Interface)
	}
}

func TestGetWireGuardStatsExplicitFilter(t *testing.T) {
	dump := wgPeerDump("wg1", wgPeerLine("AAA=", "10.1.0.2/32", "0"))
	f := &fakeWGRunner{run: func(cmd string) (string, error) {
		if strings.Contains(cmd, "wg show wg1 dump") {
			return wgStatsOutput(dump, ""), nil
		}
		if cmd == wgListCommand {
			return "wg0 wg1\n", nil // no debería llamarse
		}
		return "", fmt.Errorf("comando inesperado: %s", cmd)
	}}
	stats, err := GetWireGuardStats(f, "host", "wg1", "", nil)
	if err != nil {
		t.Fatalf("GetWireGuardStats: %v", err)
	}
	if stats.Interface != "wg1" {
		t.Errorf("Interface = %q, want wg1", stats.Interface)
	}
	if len(stats.Peers) != 1 {
		t.Fatalf("len(Peers) = %d, want 1", len(stats.Peers))
	}
	if len(f.calls) != 1 {
		t.Errorf("llamadas = %v, want 1 (solo el dump de wg1)", f.calls)
	}
	if len(f.calls) == 1 && !strings.Contains(f.calls[0], "wg show wg1 dump") {
		t.Errorf("llamada inesperada: %v", f.calls[0])
	}
}
