package tlsmode

// FORK: #1756 - a certificate from disk replaces the private CA. Names and
// paths are invented.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writePair leaves a self-signed certificate and its key in dir.
func writePair(t *testing.T, dir, name string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(7),
		Subject:               pkix.Name{CommonName: name},
		DNSNames:              []string{name},
		IPAddresses:           []net.IP{net.ParseIP("192.168.50.2")},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(dir, name+".crt")
	keyPath := filepath.Join(dir, name+".key")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

func TestACertificateFromDiskReplacesTheCA(t *testing.T) {
	r := newRig(t, nil)
	if err := r.m.SetEnabled(true, "test"); err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := writePair(t, t.TempDir(), "monitor-box")
	if err := r.m.SetCertificate(certPath, keyPath, "test"); err != nil {
		t.Fatal(err)
	}
	st := r.m.Status()
	if st.CertSource != "custom" || st.CertPath != certPath || st.KeyPath != keyPath {
		t.Fatalf("status does not point at the pair: %+v", st)
	}
	if st.RootSHA256 != "" || len(st.Uncovered) != 0 {
		t.Errorf("a certificate from disk has no root of ours to hand out: %+v", st)
	}
	if len(st.Names) != 2 || st.Names[0] != "monitor-box" || st.Names[1] != "192.168.50.2" {
		t.Errorf("names of the served leaf: %v", st.Names)
	}
	if st.Fingerprint == "" || st.Fingerprint != r.m.Fingerprint() {
		t.Errorf("pin: status %q, manager %q", st.Fingerprint, r.m.Fingerprint())
	}
	// The listener serves it: the rig pins whatever the manager reports, so
	// a handshake only succeeds if the served leaf is the one on disk.
	if body, err := r.getTLS("/"); err != nil {
		t.Fatalf("the listener does not serve the new pair: %v", err)
	} else if body != "tls=true" {
		t.Fatalf("body %q", body)
	}
	// Going back to the CA brings its root back.
	if err := r.m.SetCertificate("", "", "test"); err != nil {
		t.Fatal(err)
	}
	if st := r.m.Status(); st.CertSource != "ca" || st.RootSHA256 == "" {
		t.Fatalf("back to the CA: %+v", st)
	}
	if _, err := r.getTLS("/"); err != nil {
		t.Fatalf("the CA listener did not come back: %v", err)
	}
}

func TestABadPairLeavesTheListenerAlone(t *testing.T) {
	r := newRig(t, nil)
	if err := r.m.SetEnabled(true, "test"); err != nil {
		t.Fatal(err)
	}
	before, beforeStatus := r.m.Fingerprint(), r.m.Status()

	for name, err := range map[string]error{
		"missing file":    r.m.SetCertificate(filepath.Join(t.TempDir(), "nope.crt"), filepath.Join(t.TempDir(), "nope.key"), "test"),
		"relative path":   r.m.SetCertificate("cert.pem", "key.pem", "test"),
		"only one of two": r.m.SetCertificate(filepath.Join(t.TempDir(), "nope.crt"), "", "test"),
	} {
		if err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if got := r.m.Fingerprint(); got != before {
		t.Errorf("the pin changed after a rejected pair: %q -> %q", before, got)
	}
	if st := r.m.Status(); st.CertSource != beforeStatus.CertSource {
		t.Errorf("the source changed after a rejected pair: %q", st.CertSource)
	}
	if _, err := r.getTLS("/"); err != nil {
		t.Fatalf("the listener stopped serving: %v", err)
	}
}

func TestAgentTrustWithACertificateFromDisk(t *testing.T) {
	r := newRig(t, nil)
	if err := r.m.SetEnabled(true, "test"); err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := writePair(t, t.TempDir(), "monitor-box")
	if err := r.m.SetCertificate(certPath, keyPath, "test"); err != nil {
		t.Fatal(err)
	}
	tr := AgentTrust(r.m, func() string { return "legacy-pin" }, "http://192.168.50.2:3000")
	if tr.ServerURL == "" {
		t.Fatal("no HTTPS URL with a certificate from disk")
	}
	if tr.ServerFP != r.m.Fingerprint() {
		t.Errorf("pin %q, want the served leaf %q", tr.ServerFP, r.m.Fingerprint())
	}
	if len(tr.CAPEM) != 0 {
		t.Errorf("there is no root to hand out, got %d bytes", len(tr.CAPEM))
	}
}

func TestRenewRefusesACertificateFromDisk(t *testing.T) {
	r := newRig(t, nil)
	if err := r.m.SetEnabled(true, "test"); err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := writePair(t, t.TempDir(), "monitor-box")
	if err := r.m.SetCertificate(certPath, keyPath, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.Renew("test"); err == nil || !strings.Contains(err.Error(), "file") {
		t.Fatalf("renew: %v", err)
	}
}

// The pair survives a restart: a new manager on the same database serves it
// without going through the UI again.
func TestACertificateFromDiskSurvivesARestart(t *testing.T) {
	r := newRig(t, nil)
	if err := r.m.SetEnabled(true, "test"); err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := writePair(t, t.TempDir(), "monitor-box")
	if err := r.m.SetCertificate(certPath, keyPath, "test"); err != nil {
		t.Fatal(err)
	}
	want := r.m.Fingerprint()
	r.m.Close()

	opts := Options{
		DB: r.db, DataDir: r.dir, Port: r.port,
		NewServer: func(addr string, h http.Handler) *http.Server {
			return &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second}
		},
		Now: func() time.Time { return r.now },
	}
	m := New(opts)
	if err := m.Start(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	if st := m.Status(); st.CertSource != "custom" || st.CertPath != certPath {
		t.Fatalf("after a restart: %+v", st)
	}
	if got := m.Fingerprint(); got != want {
		t.Errorf("pin after a restart: %q, want %q", got, want)
	}
}
