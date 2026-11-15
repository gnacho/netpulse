package snmp

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

func v3Env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// TestV3AgainstLiveAgent sondea un agente SNMPv3 real de punta a punta. Se
// salta salvo que NETPULSE_SNMP_V3_TEST_HOST apunte a uno: el CI no levanta
// snmpd, pero la verificación manual sí (net-snmp 5.9 con createUser).
//
// Receta:
//
//	printf 'createUser monitor SHA-256 authpass123 AES privpass123\nrouser monitor authPriv\n' > /tmp/snmpd.conf
//	snmpd -C -c /tmp/snmpd.conf -f -Lo udp:127.0.0.1:1161 &
//	NETPULSE_SNMP_V3_TEST_HOST=127.0.0.1 NETPULSE_SNMP_V3_TEST_PORT=1161 \
//	NETPULSE_SNMP_V3_TEST_AUTHPASS=authpass123 NETPULSE_SNMP_V3_TEST_PRIVPASS=privpass123 \
//	go test ./internal/snmp -run TestV3AgainstLiveAgent -v
func TestV3AgainstLiveAgent(t *testing.T) {
	host := os.Getenv("NETPULSE_SNMP_V3_TEST_HOST")
	if host == "" {
		t.Skip("NETPULSE_SNMP_V3_TEST_HOST no definido")
	}
	port := 161
	if v := os.Getenv("NETPULSE_SNMP_V3_TEST_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("puerto: %v", err)
		}
		port = p
	}
	authPass := os.Getenv("NETPULSE_SNMP_V3_TEST_AUTHPASS")
	privPass := os.Getenv("NETPULSE_SNMP_V3_TEST_PRIVPASS")
	if authPass == "" || privPass == "" {
		t.Fatal("faltan NETPULSE_SNMP_V3_TEST_AUTHPASS / _PRIVPASS")
	}
	cfg := Config{
		Host:            host,
		Port:            port,
		Version:         ParseVersion("3"),
		V3User:          v3Env("NETPULSE_SNMP_V3_TEST_USER", "monitor"),
		V3SecurityLevel: v3Env("NETPULSE_SNMP_V3_TEST_LEVEL", "authPriv"),
		V3AuthProto:     v3Env("NETPULSE_SNMP_V3_TEST_AUTHPROTO", "SHA256"),
		V3AuthPass:      authPass,
		V3PrivProto:     v3Env("NETPULSE_SNMP_V3_TEST_PRIVPROTO", "AES"),
		V3PrivPass:      privPass,
	}
	s, err := NewSession(cfg)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer CloseSession(s)

	info, err := PollSystem(s)
	if err != nil {
		t.Fatalf("PollSystem: %v", err)
	}
	if strings.TrimSpace(info.Descr) == "" {
		t.Fatalf("sysDescr vacío: %+v", info)
	}
	t.Logf("v3 authPriv ok: descr=%q name=%q uptime=%ds", info.Descr, info.Name, info.UpTimeSec)
}
