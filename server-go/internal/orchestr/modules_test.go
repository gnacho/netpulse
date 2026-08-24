package orchestr

import (
	"encoding/base64"
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
	// El reenvío DNS va al puerto DNS fijo (adGuardDNSPort), NO al puerto de la UI.
	// El puerto deseado (5300) solo afecta a la UI (bind_port/tcp_check).
	hasForward := false
	for _, op := range ops {
		if op.Kind == "uci_set" && op.Args["option"] == "server" && op.Args["value"] == "127.0.0.1#"+adGuardDNSPort {
			hasForward = true
		}
	}
	if !hasForward {
		t.Errorf("apk debe forwardear DNS al puerto DNS fijo %s, no al de la UI", adGuardDNSPort)
	}
	// El tcp_check de la UI usa el puerto deseado, no el del DNS.
	hasTCP := false
	for _, op := range ops {
		if op.Kind == "tcp_check" && op.Args["port"] == "5300" {
			hasTCP = true
		}
	}
	if !hasTCP {
		t.Error("tcp_check de la UI debe apuntar al puerto 5300 (bind_port)")
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

func TestAdGuardOpsDNSForwardMatchConfigBinary(t *testing.T) {
	// Regresión issue #269: el puerto que dnsmasq reenvía (127.0.0.1#X) debe
	// coincidir con el dns.port de la config de AdGuard, y el DNS no debe
	// chocar con la UI (bind_port). Antes reenviaba al puerto de la UI.
	sc := AdGuardScenario{Arch: "aarch64", AdguardSuffix: "arm64"}
	ops, err := AdGuardOps(AdGuardDesired{Enabled: true, Port: "3000", UpstreamDNS: "1.1.1.1"}, sc)
	if err != nil {
		t.Fatalf("binary inesperado error: %v", err)
	}

	// puerto de reenvío dnsmasq
	var fwdPort string
	for _, op := range ops {
		if op.Kind == "uci_set" && op.Args["option"] == "server" {
			fwdPort = strings.TrimPrefix(op.Args["value"], "127.0.0.1#")
		}
	}
	if fwdPort == "" {
		t.Fatal("no se encontró op uci_set server de reenvío")
	}
	if fwdPort != adGuardDNSPort {
		t.Errorf("dnsmasq reenvía a %q, esperado %q", fwdPort, adGuardDNSPort)
	}

	// puerto de la UI (bind_port / tcp_check) debe ser distinto del DNS
	var uiPort string
	for _, op := range ops {
		if op.Kind == "tcp_check" {
			uiPort = op.Args["port"]
		}
	}
	if uiPort == fwdPort {
		t.Errorf("UI y DNS comparten puerto %q: colisión", uiPort)
	}

	// la config embebida debe tener dns.port == fwdPort
	for _, op := range ops {
		if op.Kind == "write_file" && strings.Contains(op.Args["path"], "AdGuardHome.yaml") {
			raw, err := base64.StdEncoding.DecodeString(op.Args["content_b64"])
			if err != nil {
				t.Fatalf("config no es base64 válido: %v", err)
			}
			if !strings.Contains(string(raw), "  port: "+fwdPort) {
				t.Errorf("config dns.port no coincide con reenvío %q\nconfig:\n%s", fwdPort, raw)
			}
			if !strings.Contains(string(raw), "  port: 53") {
				t.Errorf("config no debe usar el puerto fijo 53 (mal):\n%s", raw)
			}
		}
	}
	validateAll(t, ops)
}
