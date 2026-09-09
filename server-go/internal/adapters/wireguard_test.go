// wireguard_test.go — tests de parseo del adapter WireGuard (issue #659):
// resolución del nombre del peer por la option `description` de UCI.
package adapters

import "testing"

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
