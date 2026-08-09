package orchestr

import (
	"strings"
	"testing"

	"github.com/gnacho/netpulse/agent/executor"
)

// Fixture: router apk con ddns-scripts instalado y sección activa.
// Formato de `uci show ddns` (no de fichero config).
const ddnsOutApkActivo = `===PKG_MGR===
apk
===INSTALLED===
yes
===DDNS_UCI===
ddns.global=ddns
ddns.global.ddns_dateformat='%F %R'
ddns.global.ddns_loglines='250'
ddns.global.upd_privateip='0'
ddns.myddns=service
ddns.myddns.enabled='1'
ddns.myddns.service_name='duckdns.org'
ddns.myddns.lookup_host='mi.duckdns.org'
ddns.myddns.domain='mi.duckdns.org'
ddns.myddns.username='token'
ddns.myddns.password='token123'
ddns.myddns.ip_source='network'
ddns.myddns.ip_network='wan'
===END===`

// TestParseDdnsApkActivo: detecta apk, instalado y sección activa.
func TestParseDdnsApkActivo(t *testing.T) {
	sc := parseDdns(ddnsOutApkActivo)
	if sc.Manager != "apk" {
		t.Errorf("Manager: got %q want apk", sc.Manager)
	}
	if !sc.Installed {
		t.Error("Installed: esperaba true")
	}
	if !sc.SectionExists {
		t.Error("SectionExists: esperaba true")
	}
	if !sc.ActiveSection {
		t.Error("ActiveSection: esperaba true (enabled='1')")
	}
}

// TestParseDdnsNoInstalado: sin ddns-scripts ni sección.
func TestParseDdnsNoInstalado(t *testing.T) {
	out := `===PKG_MGR===
opkg
===INSTALLED===
no
===DDNS_UCI===
===END===`
	sc := parseDdns(out)
	if sc.Manager != "opkg" {
		t.Errorf("Manager: got %q want opkg", sc.Manager)
	}
	if sc.Installed {
		t.Error("Installed: esperaba false")
	}
	if sc.SectionExists || sc.ActiveSection {
		t.Error("no debería haber sección activa sin ddns-scripts")
	}
}

// TestDdnsOpsEnableInstalaYConfigura: enable instala + configura + arranca.
func TestDdnsOpsEnableInstalaYConfigura(t *testing.T) {
	sc := parseDdns(`===PKG_MGR===
apk
===INSTALLED===
no
===DDNS_UCI===
===END===`)
	ops := DdnsOps(DdnsDesired{
		Enabled: true, ServiceName: "duckdns.org",
		Domain: "mi.duckdns.org", Username: "token", Password: "token123",
	}, sc)
	if err := validateDdnsOps(ops); err != nil {
		t.Fatalf("ops enable no validan en executor: %v", err)
	}
	// Debe instalar con apk_install y crear la sección con uci_set_named.
	kinds := map[string]bool{}
	for _, o := range ops {
		kinds[o.Kind] = true
	}
	if !kinds["apk_install"] {
		t.Error("enable sin apk_install (ddns-scripts no instalado)")
	}
	if !kinds["uci_set_named"] {
		t.Error("enable sin uci_set_named (sección ddns a crear)")
	}
	if !kinds["service"] {
		t.Error("enable sin service (start/enable ddns)")
	}
	// Verificar que configura los campos clave.
	found := false
	for _, o := range ops {
		if o.Kind == "uci_set" && o.Args["option"] == "domain" && o.Args["value"] == "mi.duckdns.org" {
			found = true
		}
	}
	if !found {
		t.Error("enable sin uci_set del dominio")
	}
}

// TestDdnsOpsEnableYaInstalado: no re-instala si ya está.
func TestDdnsOpsEnableYaInstalado(t *testing.T) {
	sc := parseDdns(ddnsOutApkActivo)
	ops := DdnsOps(DdnsDesired{
		Enabled: true, ServiceName: "duckdns.org",
		Domain: "mi.duckdns.org", Username: "token", Password: "token123",
	}, sc)
	if err := validateDdnsOps(ops); err != nil {
		t.Fatalf("ops no validan: %v", err)
	}
	for _, o := range ops {
		if o.Kind == "apk_install" || o.Kind == "install" {
			t.Error("no debería re-instalar ddns-scripts si ya está")
		}
		if o.Kind == "uci_set_named" {
			t.Error("no debería recrear la sección si ya existe")
		}
	}
}

// TestDdnsOpsDisable: desactiva (enabled=0 + stop + disable) sin desinstalar.
func TestDdnsOpsDisable(t *testing.T) {
	sc := parseDdns(ddnsOutApkActivo)
	ops := DdnsOps(DdnsDesired{Enabled: false}, sc)
	if err := validateDdnsOps(ops); err != nil {
		t.Fatalf("ops disable no validan: %v", err)
	}
	joined := strings.Join(kindsOf(ops), ",")
	if !strings.Contains(joined, "uci_set") {
		t.Error("disable sin uci_set enabled=0")
	}
	if !strings.Contains(joined, "service") {
		t.Error("disable sin service stop/disable")
	}
}

func kindsOf(ops []executor.Op) []string {
	out := make([]string, len(ops))
	for i, o := range ops {
		out[i] = o.Kind
	}
	return out
}
