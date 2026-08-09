package orchestr

import (
	"errors"
	"strings"
	"testing"

	"github.com/gnacho/netpulse/agent/executor"
)

// validateAll ejecuta executor.Validate sobre cada op y falla el test si
// alguna no pasa el allowlist del agente. Garantiza que el plan generado por
// orchestr es ejecutable (contrato orchestr→executor).
func validateAll(t *testing.T, ops []executor.Op) {
	t.Helper()
	for i, op := range ops {
		if err := executor.Validate(op); err != nil {
			t.Errorf("op[%d] kind=%s NO pasa Validate: %v\n  args=%v", i, op.Kind, err, op.Args)
		}
	}
}

func TestAdGuardOpsAbortaSiForkFabricante(t *testing.T) {
	sc := AdGuardScenario{ManagedByFirmware: true}
	_, err := AdGuardOps(AdGuardDesired{Enabled: true}, sc)
	if !errors.Is(err, ErrManagedByFirmware) {
		t.Fatalf("esperaba ErrManagedByFirmware, got %v", err)
	}
}

func TestAdGuardOpsDisable(t *testing.T) {
	ops, err := AdGuardOps(AdGuardDesired{Enabled: false}, AdGuardScenario{})
	if err != nil {
		t.Fatalf("disable inesperado error: %v", err)
	}
	// Sin ops de install/download (solo limpia DNS + para servicio).
	for _, op := range ops {
		if op.Kind == "install" || op.Kind == "apk_install" || op.Kind == "download" {
			t.Errorf("disable no debe instalar: encontró %s", op.Kind)
		}
	}
	// Debe tener uci_delete server + service stop.
	hasDel, hasStop := false, false
	for _, op := range ops {
		if op.Kind == "uci_delete" && op.Args["option"] == "server" {
			hasDel = true
		}
		if op.Kind == "service" && op.Args["action"] == "stop" {
			hasStop = true
		}
	}
	if !hasDel || !hasStop {
		t.Errorf("disable incompleto: hasDel=%v hasStop=%v", hasDel, hasStop)
	}
	validateAll(t, ops)
}

func TestAdGuardOpsEscenarioApk(t *testing.T) {
	sc := AdGuardScenario{ApkAvailable: true, Arch: "x86_64", AdguardSuffix: "amd64"}
	ops, err := AdGuardOps(AdGuardDesired{Enabled: true, Port: "5300", UpstreamDNS: "9.9.9.9"}, sc)
	if err != nil {
		t.Fatalf("apk inesperado error: %v", err)
	}
	if ops[0].Kind != "apk_install" || ops[0].Args["package"] != "adguard-home" {
		t.Errorf("primera op debe ser apk_install adguard-home, got %+v", ops[0])
	}
	// Sin download (usa gestor de paquetes).
	for _, op := range ops {
		if op.Kind == "download" || op.Kind == "extract_tarball" || op.Kind == "mv" {
			t.Errorf("apk no debe descargar binario: encontró %s", op.Kind)
		}
	}
	// El puerto deseado se aplica.
	hasForward := false
	for _, op := range ops {
		if op.Kind == "uci_set" && op.Args["option"] == "server" && op.Args["value"] == "127.0.0.1#5300" {
			hasForward = true
		}
	}
	if !hasForward {
		t.Error("apk debe forwardear DNS al puerto deseado 5300")
	}
	validateAll(t, ops)
}

func TestAdGuardOpsEscenarioOpkg(t *testing.T) {
	sc := AdGuardScenario{OpkgAvailable: true, Arch: "mips", AdguardSuffix: "mips"}
	ops, err := AdGuardOps(AdGuardDesired{Enabled: true}, sc)
	if err != nil {
		t.Fatalf("opkg inesperado error: %v", err)
	}
	if ops[0].Kind != "install" || ops[0].Args["package"] != "adguard-home" {
		t.Errorf("primera op debe ser install (opkg) adguard-home, got %+v", ops[0])
	}
	validateAll(t, ops)
}

func TestAdGuardOpsEscenarioNoneBinarioYaPresente(t *testing.T) {
	sc := AdGuardScenario{BinaryPresent: true, Arch: "aarch64", AdguardSuffix: "arm64"}
	ops, err := AdGuardOps(AdGuardDesired{Enabled: true}, sc)
	if err != nil {
		t.Fatalf("none inesperado error: %v", err)
	}
	// Sin install ni download (binario ya está).
	for _, op := range ops {
		if op.Kind == "install" || op.Kind == "apk_install" || op.Kind == "download" {
			t.Errorf("none no debe instalar/descargar: encontró %s", op.Kind)
		}
	}
	validateAll(t, ops)
}

func TestAdGuardOpsEscenarioBinary(t *testing.T) {
	sc := AdGuardScenario{Arch: "aarch64", AdguardSuffix: "arm64"}
	ops, err := AdGuardOps(AdGuardDesired{Enabled: true, Port: "3000", UpstreamDNS: "1.1.1.1"}, sc)
	if err != nil {
		t.Fatalf("binary inesperado error: %v", err)
	}
	// Secuencia esperada: download → extract → mv → chmod → write_file config
	// → write_file init.d → chmod init.d → enable → start → tcp_check UI →
	// uci_set ×2 → commit → dnsmasq restart.
	kinds := make([]string, len(ops))
	for i, op := range ops {
		kinds[i] = op.Kind
	}
	wantSeq := []string{"download", "extract_tarball", "mv", "chmod", "write_file", "write_file", "chmod", "service", "service", "tcp_check", "uci_set", "uci_set", "uci_commit", "service"}
	if len(kinds) != len(wantSeq) {
		t.Fatalf("binary plan length: got %d want %d\nkinds: %v", len(kinds), len(wantSeq), kinds)
	}
	for i, want := range wantSeq {
		if kinds[i] != want {
			t.Errorf("binary step %d: got %s want %s\nfull: %v", i, kinds[i], want, kinds)
		}
	}
	validateAll(t, ops)
}

func TestAdGuardOpsBinaryURLContieneVersionYSuffix(t *testing.T) {
	sc := AdGuardScenario{Arch: "aarch64", AdguardSuffix: "arm64"}
	ops, err := AdGuardOps(AdGuardDesired{Enabled: true}, sc)
	if err != nil {
		t.Fatalf("binary inesperado error: %v", err)
	}
	if len(ops) == 0 || ops[0].Kind != "download" {
		t.Fatal("primera op debe ser download")
	}
	url := ops[0].Args["url"]
	if !strings.Contains(url, adguardVersion) {
		t.Errorf("URL no contiene versión %s: %s", adguardVersion, url)
	}
	if !strings.Contains(url, "_arm64.tar.gz") {
		t.Errorf("URL no contiene suffix arm64: %s", url)
	}
	// Validate pasa (allowlist de URL).
	if err := executor.Validate(ops[0]); err != nil {
		t.Errorf("download op no valida: %v", err)
	}
}

func TestAdGuardOpsBinaryFallbackSuffix(t *testing.T) {
	// Arch desconocida → fallback "arm64" en la URL (no rompe, el plan sigue
	// siendo válido; fallaría en apply-time si la arch real no es arm64).
	sc := AdGuardScenario{Arch: "exotic", AdguardSuffix: ""}
	ops, err := AdGuardOps(AdGuardDesired{Enabled: true}, sc)
	if err != nil {
		t.Fatalf("binary fallback inesperado error: %v", err)
	}
	if !strings.Contains(ops[0].Args["url"], "_arm64.tar.gz") {
		t.Errorf("fallback URL debe usar arm64: %s", ops[0].Args["url"])
	}
	validateAll(t, ops)
}
