// device_actions_internal_test.go — unitarias de los helpers de sanitización
// uci de device_actions.go (#693): escapado de comillas simples y validación
// de hostname DNS.
package httpapi

import (
	"strings"
	"testing"
)

func TestUciQuoteEscapesSingleQuote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"tv-salon", "tv-salon"},
		{"", ""},
		{"a'b", `a'\''b`},
		{`x'; reboot; echo '`, `x'\''; reboot; echo '\''`},
		{"it''s", `it'\'''\''s`},
	}
	for _, c := range cases {
		if got := uciQuote(c.in); got != c.want {
			t.Errorf("uciQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestUciSetCmdQuotesValue(t *testing.T) {
	got := uciSetCmd("dhcp", "np_host_x", "name", "a'b")
	want := `uci set dhcp.np_host_x.name='a'\''b'`
	if got != want {
		t.Errorf("uciSetCmd = %q, want %q", got, want)
	}
	// El payload del issue queda inerte: cada comilla simple del valor viaja
	// como secuencia de escape, nunca como cierre del quoting.
	raw := uciSetCmd("firewall", "np_block_x", "name", `x'; id; echo '`)
	wantRaw := `uci set firewall.np_block_x.name='x'\''; id; echo '\'''`
	if raw != wantRaw {
		t.Errorf("uciSetCmd con payload hostil = %q, want %q", raw, wantRaw)
	}
}

func TestValidDHCPHostname(t *testing.T) {
	valid := []string{
		"",            // vacío = no tocar el name
		"tv-salon",    // clásico
		"a",           // mínimo
		"nas.lan",     // multi-etiqueta
		"n-a-s.l-a-n", // etiquetas con guiones internos
		strings.Repeat("a", 63) + "." + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 61), // 253 justo
	}
	for _, h := range valid {
		if !validDHCPHostname(h) {
			t.Errorf("validDHCPHostname(%q) = false, want true", h)
		}
	}
	invalid := []string{
		"x'; touch /tmp/poc; echo '", // PoC del issue #693
		"tv salon",                   // espacio
		"x$(id)",                     // sustitución
		"x`id`",                      // backticks
		"pc_1",                       // underscore (no es DNS)
		"-lead",                      // guion inicial
		"trail-",                     // guion final
		"a..b",                       // etiqueta vacía
		".a",                         // empieza en punto
		"télé",                       // acento
		strings.Repeat("a", 254),     // demasiado largo
	}
	for _, h := range invalid {
		if validDHCPHostname(h) {
			t.Errorf("validDHCPHostname(%q) = true, want false", h)
		}
	}
}
