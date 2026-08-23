// config_test.go — Fase 9 R4: NETPULSE_ONBOX y la obligatoriedad condicional
// de AUTH_PASS en Load. Incluye la regresión del comportamiento actual
// (sin ONBOX, AUTH_PASS sigue siendo fail-fast).
package config

import (
	"strings"
	"testing"
)

func TestLoadAuthPassRequeridaSinOnbox(t *testing.T) {
	_, err := Load(map[string]string{}, t.TempDir())
	if err == nil {
		t.Fatal("Load sin AUTH_PASS debe fallar (fail-fast)")
	}
	if !strings.Contains(err.Error(), "AUTH_PASS") {
		t.Fatalf("el error debe señalar AUTH_PASS: %v", err)
	}
}

func TestLoadOnboxAuthPassOpcional(t *testing.T) {
	cfg, err := Load(map[string]string{"NETPULSE_ONBOX": "1"}, t.TempDir())
	if err != nil {
		t.Fatalf("Load con ONBOX=1 y sin AUTH_PASS no debe fallar: %v", err)
	}
	if !cfg.Onbox {
		t.Fatal("cfg.Onbox debe ser true con NETPULSE_ONBOX=1")
	}
	if cfg.AuthPass != "" {
		t.Fatalf("AuthPass = %q, esperaba vacía (el bootstrap la genera después)", cfg.AuthPass)
	}
}

func TestLoadOnboxConAuthPass(t *testing.T) {
	cfg, err := Load(map[string]string{"NETPULSE_ONBOX": "1", "AUTH_PASS": "segura-y-larga"}, t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Onbox || cfg.AuthPass != "segura-y-larga" {
		t.Fatalf("Onbox=%v AuthPass=%q: ONBOX con AUTH_PASS explícita debe conservarla", cfg.Onbox, cfg.AuthPass)
	}
}

func TestLoadAuthPassMinLength(t *testing.T) {
	_, err := Load(map[string]string{"AUTH_PASS": "corta"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "AUTH_PASS") {
		t.Fatalf("AUTH_PASS corto debe fallar señalando la variable: %v", err)
	}
	cfg, err := Load(map[string]string{"AUTH_PASS": "diez-carac-"}, t.TempDir())
	if err != nil {
		t.Fatalf("AUTH_PASS de 10 caracteres no debe fallar: %v", err)
	}
	if cfg.AuthPass != "diez-carac-" {
		t.Fatalf("AuthPass = %q, esperaba diez-carac-", cfg.AuthPass)
	}
}

func TestLoadOnboxEnumInvalido(t *testing.T) {
	_, err := Load(map[string]string{"NETPULSE_ONBOX": "2", "AUTH_PASS": "segura-y-larga"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "NETPULSE_ONBOX") {
		t.Fatalf("NETPULSE_ONBOX=2 debe fallar señalando la variable: %v", err)
	}
}

func TestLoadOnboxCeroEquivaleAHoy(t *testing.T) {
	cfg, err := Load(map[string]string{"NETPULSE_ONBOX": "0", "AUTH_PASS": "segura-y-larga"}, t.TempDir())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Onbox {
		t.Fatal("NETPULSE_ONBOX=0 debe dejar Onbox=false (comportamiento actual)")
	}
}
