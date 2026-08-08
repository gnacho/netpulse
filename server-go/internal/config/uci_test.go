// uci_test.go — Fase 9 R4/R5: parser de `uci show`, precedencia de entorno
// del loader UCI y generador de contraseña del bootstrap. Todo puro: sin
// binario uci, sin tocar el entorno real del proceso.
package config

import (
	"strings"
	"testing"
)

func TestParseUCIShow(t *testing.T) {
	out := strings.Join([]string{
		"netpulse.server=server",                      // declaración de sección: ignorar
		"netpulse.server.port='3100'",                 // normal
		"netpulse.server.auth_user='nacho'",           // normal
		"netpulse.server.data_dir='/mnt/netpulse da'", // espacio dentro de comillas
		"netpulse.server.auth_pass='it'\\''s'",        // comilla escapada al estilo uci
		"netpulse.server.session_secret=''",           // vacío: descartar
		"netpulse.@server[0].wg_interface='wg1'",      // sección anónima: aceptar
		"netpulse.server.port='9999'",                 // duplicado: gana el primero
		"netpulse.extra.option='x'",                   // otra sección: ignorar
		"otropaquete.server.port='1'",                 // otro paquete: ignorar
		"netpulse.server.demo_mode.subniv='1'",        // 4 segmentos: ignorar
		"linea sin igual",                             // malformada: ignorar
		"",                                            // vacía
	}, "\n")

	vals := parseUCIShow(out)

	want := map[string]string{
		"port":         "3100",
		"auth_user":    "nacho",
		"data_dir":     "/mnt/netpulse da",
		"auth_pass":    "it's",
		"wg_interface": "wg1",
	}
	if len(vals) != len(want) {
		t.Fatalf("parseUCIShow: %d claves, esperaba %d (%v)", len(vals), len(want), vals)
	}
	for k, v := range want {
		if vals[k] != v {
			t.Errorf("parseUCIShow[%q] = %q, esperaba %q", k, vals[k], v)
		}
	}
}

func TestUCIUnquote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"'3000'", "3000"},
		{"''", ""},
		{"sin-comillas", "sin-comillas"},
		{"'it'\\''s'", "it's"},
		{"'a b c'", "a b c"},
		{"'", "'"}, // una sola comilla: se deja tal cual
	}
	for _, c := range cases {
		if got := uciUnquote(c.in); got != c.want {
			t.Errorf("uciUnquote(%q) = %q, esperaba %q", c.in, got, c.want)
		}
	}
}

func TestApplyUCIEnvPrecedencia(t *testing.T) {
	vals := map[string]string{
		"port":        "3100",
		"auth_user":   "uci-user",
		"auto_rearm":  "1",
		"desconocida": "x", // fuera de la allowlist: nunca se inyecta
	}
	env := map[string]string{
		"AUTH_USER": "env-user", // ya presente: UCI no pisa
	}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	set := func(k, v string) error { env[k] = v; return nil }

	applyUCIEnv(vals, lookup, set)

	if env["PORT"] != "3100" {
		t.Errorf("PORT = %q, esperaba 3100 (inyección desde UCI)", env["PORT"])
	}
	if env["AUTH_USER"] != "env-user" {
		t.Errorf("AUTH_USER = %q: el entorno existente debe ganar a UCI", env["AUTH_USER"])
	}
	if env["NETPULSE_AUTO_REARM"] != "1" {
		t.Errorf("NETPULSE_AUTO_REARM = %q, esperaba 1 (mapeo auto_rearm)", env["NETPULSE_AUTO_REARM"])
	}
	if _, ok := env["DESCONOCIDA"]; ok {
		t.Error("una opción fuera de la allowlist no debe llegar al entorno")
	}
	if len(env) != 3 {
		t.Errorf("entorno final con %d claves, esperaba 3: %v", len(env), env)
	}
}

func TestGenerateInitialPassword(t *testing.T) {
	a, err := GenerateInitialPassword()
	if err != nil {
		t.Fatalf("GenerateInitialPassword: %v", err)
	}
	b, err := GenerateInitialPassword()
	if err != nil {
		t.Fatalf("GenerateInitialPassword (2ª): %v", err)
	}
	if len(a) != InitialPasswordLen || len(b) != InitialPasswordLen {
		t.Fatalf("longitudes %d/%d, esperaba %d", len(a), len(b), InitialPasswordLen)
	}
	if a == b {
		t.Fatal("dos contraseñas generadas idénticas: rand roto")
	}
	for _, r := range a + b {
		if !strings.ContainsRune(passwordCharset, r) {
			t.Fatalf("carácter %q fuera del charset", r)
		}
	}
}
