package snmp

import (
	"testing"

	"github.com/gosnmp/gosnmp"
)

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in   string
		want gosnmp.SnmpVersion
	}{
		{"1", gosnmp.Version1},
		{"2c", gosnmp.Version2c},
		{"3", gosnmp.Version3},
		{"", gosnmp.Version2c},
		{"basura", gosnmp.Version2c},
	}
	for _, c := range cases {
		if got := ParseVersion(c.in); got != c.want {
			t.Errorf("ParseVersion(%q) = %v; want %v", c.in, got, c.want)
		}
	}
}

func TestBuildSessionV2cKeepsCommunity(t *testing.T) {
	s, err := buildSession(Config{Host: "10.0.0.2", Version: gosnmp.Version2c, Community: "s3cret"})
	if err != nil {
		t.Fatalf("buildSession: %v", err)
	}
	if s.Community != "s3cret" {
		t.Fatalf("community = %q", s.Community)
	}
	if s.Version != gosnmp.Version2c {
		t.Fatalf("version = %v", s.Version)
	}
	if s.Port != DefaultPort {
		t.Fatalf("port = %d; want %d", s.Port, DefaultPort)
	}
}

func TestBuildSessionV2cDefaultsCommunityToPublic(t *testing.T) {
	s, err := buildSession(Config{Host: "10.0.0.2"})
	if err != nil {
		t.Fatalf("buildSession: %v", err)
	}
	if s.Community != "public" {
		t.Fatalf("community = %q; want public", s.Community)
	}
}

func TestBuildSessionV3AuthPriv(t *testing.T) {
	s, err := buildSession(Config{
		Host:            "10.0.0.3",
		Version:         gosnmp.Version3,
		V3User:          "monitor",
		V3SecurityLevel: "authPriv",
		V3AuthProto:     "SHA256",
		V3AuthPass:      "authpass123",
		V3PrivProto:     "AES256",
		V3PrivPass:      "privpass123",
	})
	if err != nil {
		t.Fatalf("buildSession: %v", err)
	}
	if s.Version != gosnmp.Version3 {
		t.Fatalf("version = %v", s.Version)
	}
	if s.SecurityModel != gosnmp.UserSecurityModel {
		t.Fatalf("security model = %v", s.SecurityModel)
	}
	if s.MsgFlags&gosnmp.AuthPriv != gosnmp.AuthPriv {
		t.Fatalf("msg flags = %v; want authPriv", s.MsgFlags)
	}
	if s.Community != "" {
		t.Fatalf("community must be empty in v3, got %q", s.Community)
	}
	usm, ok := s.SecurityParameters.(*gosnmp.UsmSecurityParameters)
	if !ok {
		t.Fatalf("security parameters = %T; want *UsmSecurityParameters", s.SecurityParameters)
	}
	if usm.UserName != "monitor" {
		t.Fatalf("user = %q", usm.UserName)
	}
	if usm.AuthenticationProtocol != gosnmp.SHA256 {
		t.Fatalf("auth proto = %v; want SHA256", usm.AuthenticationProtocol)
	}
	if usm.PrivacyProtocol != gosnmp.AES256 {
		t.Fatalf("priv proto = %v; want AES256", usm.PrivacyProtocol)
	}
	if usm.AuthenticationPassphrase != "authpass123" || usm.PrivacyPassphrase != "privpass123" {
		t.Fatal("passphrases not propagated")
	}
}

func TestBuildSessionV3Defaults(t *testing.T) {
	// Sin nivel ni protocolos: authPriv + SHA + AES, que es lo que ofrece la
	// UI por defecto y lo que espera un switch gestionado moderno.
	s, err := buildSession(Config{
		Host:       "10.0.0.4",
		Version:    gosnmp.Version3,
		V3User:     "monitor",
		V3AuthPass: "authpass123",
		V3PrivPass: "privpass123",
	})
	if err != nil {
		t.Fatalf("buildSession: %v", err)
	}
	usm := s.SecurityParameters.(*gosnmp.UsmSecurityParameters)
	if usm.AuthenticationProtocol != gosnmp.SHA {
		t.Fatalf("auth proto = %v; want SHA", usm.AuthenticationProtocol)
	}
	if usm.PrivacyProtocol != gosnmp.AES {
		t.Fatalf("priv proto = %v; want AES", usm.PrivacyProtocol)
	}
	if s.MsgFlags&gosnmp.AuthPriv != gosnmp.AuthPriv {
		t.Fatalf("msg flags = %v; want authPriv", s.MsgFlags)
	}
}

func TestBuildSessionV3NoAuthNoPriv(t *testing.T) {
	s, err := buildSession(Config{
		Host:            "10.0.0.5",
		Version:         gosnmp.Version3,
		V3User:          "monitor",
		V3SecurityLevel: "noAuthNoPriv",
		// Protocolos y passphrases presentes pero irrelevantes en este nivel.
		V3AuthProto: "SHA",
		V3AuthPass:  "authpass123",
		V3PrivProto: "AES",
		V3PrivPass:  "privpass123",
	})
	if err != nil {
		t.Fatalf("buildSession: %v", err)
	}
	if s.MsgFlags&gosnmp.AuthPriv != 0 {
		t.Fatalf("msg flags = %v; want noAuthNoPriv", s.MsgFlags)
	}
	usm := s.SecurityParameters.(*gosnmp.UsmSecurityParameters)
	if usm.AuthenticationProtocol != gosnmp.NoAuth {
		t.Fatalf("auth proto = %v; want NoAuth", usm.AuthenticationProtocol)
	}
	if usm.PrivacyProtocol != gosnmp.NoPriv {
		t.Fatalf("priv proto = %v; want NoPriv", usm.PrivacyProtocol)
	}
}

func TestBuildSessionV3AuthNoPriv(t *testing.T) {
	s, err := buildSession(Config{
		Host:            "10.0.0.6",
		Version:         gosnmp.Version3,
		V3User:          "monitor",
		V3SecurityLevel: "authNoPriv",
		V3AuthPass:      "authpass123",
	})
	if err != nil {
		t.Fatalf("buildSession: %v", err)
	}
	usm := s.SecurityParameters.(*gosnmp.UsmSecurityParameters)
	if usm.PrivacyProtocol != gosnmp.NoPriv {
		t.Fatalf("priv proto = %v; want NoPriv", usm.PrivacyProtocol)
	}
	if s.MsgFlags&gosnmp.AuthNoPriv != gosnmp.AuthNoPriv {
		t.Fatalf("msg flags = %v; want authNoPriv", s.MsgFlags)
	}
}

func TestBuildSessionV3Errors(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"sin usuario", Config{Host: "h", Version: gosnmp.Version3, V3AuthPass: "authpass123", V3PrivPass: "privpass123"}},
		{"pass corta auth", Config{Host: "h", Version: gosnmp.Version3, V3User: "u", V3AuthPass: "corta", V3PrivPass: "privpass123"}},
		{"pass corta priv", Config{Host: "h", Version: gosnmp.Version3, V3User: "u", V3AuthPass: "authpass123", V3PrivPass: "corta"}},
		{"nivel desconocido", Config{Host: "h", Version: gosnmp.Version3, V3User: "u", V3SecurityLevel: "authPrivX", V3AuthPass: "authpass123", V3PrivPass: "privpass123"}},
		{"auth proto desconocido", Config{Host: "h", Version: gosnmp.Version3, V3User: "u", V3AuthProto: "SHA3", V3AuthPass: "authpass123", V3PrivPass: "privpass123"}},
		{"priv proto desconocido", Config{Host: "h", Version: gosnmp.Version3, V3User: "u", V3PrivProto: "CHACHA", V3AuthPass: "authpass123", V3PrivPass: "privpass123"}},
		{"sin host", Config{Version: gosnmp.Version3}},
	}
	for _, c := range cases {
		if _, err := buildSession(c.cfg); err == nil {
			t.Errorf("%s: buildSession sin error", c.name)
		}
	}
}

func TestV3Validators(t *testing.T) {
	for _, p := range V3AuthProtocols {
		if !ValidV3AuthProtocol(p) {
			t.Errorf("ValidV3AuthProtocol(%q) = false", p)
		}
	}
	for _, p := range V3PrivProtocols {
		if !ValidV3PrivProtocol(p) {
			t.Errorf("ValidV3PrivProtocol(%q) = false", p)
		}
	}
	for _, l := range V3SecurityLevels {
		if !ValidV3SecurityLevel(l) {
			t.Errorf("ValidV3SecurityLevel(%q) = false", l)
		}
	}
	for _, bad := range []string{"sha", "SHA-256", "", "AES128"} {
		if ValidV3AuthProtocol(bad) {
			t.Errorf("ValidV3AuthProtocol(%q) = true", bad)
		}
	}
}
