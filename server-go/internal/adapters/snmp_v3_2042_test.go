package adapters

import (
	"testing"

	"github.com/gosnmp/gosnmp"
)

// #2042: el adapter traduce la configuración de la unidad al contrato del
// poller. En v3 no debe arrastrar la comunidad y en v1/v2c no debe mandar
// credenciales USM.
func TestSnmpSessionConfigVersionMapping(t *testing.T) {
	cfg := RouterConfig{
		Host: "10.0.0.9", SnmpPort: 1161, SnmpCommunity: "public",
		SnmpVersion: "3", SnmpV3User: "monitor", SnmpV3SecurityLevel: "authPriv",
		SnmpV3AuthProto: "SHA256", SnmpV3AuthPass: "authpass123",
		SnmpV3PrivProto: "AES256", SnmpV3PrivPass: "privpass123",
	}
	got := snmpSessionConfig(cfg)
	if got.Version != gosnmp.Version3 {
		t.Fatalf("version = %v; want Version3", got.Version)
	}
	if got.Community != "" {
		t.Fatalf("community en v3 = %q; want vacía", got.Community)
	}
	if got.V3User != "monitor" || got.V3AuthProto != "SHA256" || got.V3PrivProto != "AES256" {
		t.Fatalf("credenciales v3 no propagadas: %+v", got)
	}
	if got.V3AuthPass != "authpass123" || got.V3PrivPass != "privpass123" {
		t.Fatal("passphrases v3 no propagadas")
	}

	for _, v := range []string{"", "2c"} {
		cfg.SnmpVersion = v
		got = snmpSessionConfig(cfg)
		if got.Version != gosnmp.Version2c {
			t.Fatalf("%q → version %v; want Version2c", v, got.Version)
		}
		if got.Community != "public" {
			t.Fatalf("%q → community %q; want public", v, got.Community)
		}
		if got.V3User != "" || got.V3AuthPass != "" || got.V3PrivPass != "" {
			t.Fatalf("%q → credenciales v3 coladas: %+v", v, got)
		}
	}

	cfg.SnmpVersion = "1"
	if got = snmpSessionConfig(cfg); got.Version != gosnmp.Version1 {
		t.Fatalf("v1 → version %v", got.Version)
	}
	// El puerto y el host se pasan siempre.
	if got.Host != "10.0.0.9" || got.Port != 1161 {
		t.Fatalf("host/puerto = %q:%d", got.Host, got.Port)
	}
}
