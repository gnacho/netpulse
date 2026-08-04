// demo_internal_test.go — prueba unitaria de setDemoModeInEnv (package httpapi).
package httpapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetDemoModeInEnv(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, ".env")

	// Caso 1: DEMO_MODE=0 existente → se reescribe a 1 conservando el resto.
	orig := "PORT=3000\nDEMO_MODE=0\nAUTH_USER=admin\n"
	if err := os.WriteFile(env, []byte(orig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := setDemoModeInEnv(env, true); err != nil {
		t.Fatalf("setDemoModeInEnv(true): %v", err)
	}
	got, _ := os.ReadFile(env)
	s := string(got)
	if !strings.Contains(s, "DEMO_MODE=1") {
		t.Errorf("esperaba DEMO_MODE=1, got:\n%s", s)
	}
	if strings.Contains(s, "DEMO_MODE=0") {
		t.Errorf("no debería quedar DEMO_MODE=0, got:\n%s", s)
	}
	if !strings.Contains(s, "PORT=3000") || !strings.Contains(s, "AUTH_USER=admin") {
		t.Errorf("se perdieron otras claves, got:\n%s", s)
	}

	// Caso 2: apagar (1 → 0).
	if err := setDemoModeInEnv(env, false); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(env)
	if !strings.Contains(string(got), "DEMO_MODE=0") {
		t.Errorf("esperaba DEMO_MODE=0, got:\n%s", got)
	}

	// Caso 3: sin .env previo → se crea con la clave.
	env2 := filepath.Join(dir, ".env2")
	if err := setDemoModeInEnv(env2, true); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(env2)
	if strings.TrimSpace(string(got)) != "DEMO_MODE=1" {
		t.Errorf(".env nuevo inesperado: %q", got)
	}
}
