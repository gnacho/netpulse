package httpapi_test

// FORK: #1756 - setting a certificate from disk through Settings.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTestPair(t *testing.T, dir, name string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(9),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		IPAddresses:  []net.IP{net.ParseIP("192.168.50.2")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
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

func TestACertificateCanBeSetFromSettings(t *testing.T) {
	ts, mgr := makeHTTPSTestServer(t)
	certPath, keyPath := writeTestPair(t, t.TempDir(), "monitor-box")

	st, body := httpsCall(t, ts, "POST", "/api/settings/https",
		fmt.Sprintf(`{"cert":%q,"key":%q,"password":"test123456"}`, certPath, keyPath), true)
	if st != http.StatusOK {
		t.Fatalf("status %d: %v", st, body)
	}
	status, _ := body["status"].(map[string]any)
	if status["certSource"] != "custom" {
		t.Fatalf("status after setting the pair: %v", body)
	}
	if got := mgr.Status(); got.CertPath != certPath || got.KeyPath != keyPath {
		t.Fatalf("the manager kept %+v", got)
	}

	// A pair that does not load is rejected and the current one stays.
	st, body = httpsCall(t, ts, "POST", "/api/settings/https",
		fmt.Sprintf(`{"cert":%q,"key":%q,"password":"test123456"}`,
			filepath.Join(t.TempDir(), "nope.crt"), filepath.Join(t.TempDir(), "nope.key")), true)
	if st != http.StatusBadRequest {
		t.Fatalf("a bad pair: status %d: %v", st, body)
	}
	if got := mgr.Status(); got.CertPath != certPath {
		t.Fatalf("a rejected pair changed the setting: %+v", got)
	}

	// Empty paths go back to the private CA.
	st, body = httpsCall(t, ts, "POST", "/api/settings/https", `{"cert":"","key":"","password":"test123456"}`, true)
	if st != http.StatusOK {
		t.Fatalf("back to the CA: status %d: %v", st, body)
	}
	if got := mgr.Status(); got.CertSource != "ca" {
		t.Fatalf("back to the CA: %+v", got)
	}
}
