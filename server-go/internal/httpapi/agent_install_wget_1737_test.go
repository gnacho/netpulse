package httpapi_test

import (
	"strings"
	"testing"
)

// #1737: en un router sin curl (BusyBox pelado) el one-liner moría con
// "curl: not found" antes de llegar al instalador. Tiene que haber rama wget.
func TestInstallLineHasAWgetBranch(t *testing.T) {
	ts := makeURLTestServer(t, nil)
	_, _, line := createAgentToken(t, ts, "rt1")

	if !strings.Contains(line, "if command -v curl") {
		t.Fatalf("install line does not check for curl:\n%s", line)
	}
	if !strings.Contains(line, "wget -q -O /tmp/netpulse-install-agent.sh") {
		t.Fatalf("install line does not fall back to wget:\n%s", line)
	}
	// La descarga sigue siendo a fichero y el script solo se ejecuta si el
	// descargador terminó bien (#1577).
	if !strings.Contains(line, "fi && sh /tmp/netpulse-install-agent.sh") {
		t.Fatalf("install line does not gate the run on the download:\n%s", line)
	}
}

// Con HTTPS y CA propia, wget de BusyBox no conoce --ca-certificate: la línea
// intenta con la CA y reintenta sin verificar el certificado, porque el pin
// (--server-fp) y el token de un solo uso siguen protegiendo el flujo.
func TestInstallLineWgetBranchKeepsTheRootWhenPossible(t *testing.T) {
	ts, mgr := makeHTTPSTestServer(t)
	enableHTTPS(t, mgr)
	_, _, line := createAgentToken(t, ts, "rt2")

	for _, want := range []string{
		`wget -q --ca-certificate="$NPCA" -O /tmp/netpulse-install-agent.sh`,
		"wget -q --no-check-certificate -O /tmp/netpulse-install-agent.sh",
		"--server-fp=" + mgr.Fingerprint(),
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("install line lacks %q:\n%s", want, line)
		}
	}
}
