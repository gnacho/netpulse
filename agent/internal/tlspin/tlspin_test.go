package tlspin

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// selfSignedCert genera un cert autofirmado ECDSA P-256 y devuelve el
// tls.Config del servidor + el fingerprint SPKI esperado (mismo algoritmo
// que server-go/internal/tlscert.Fingerprint: SHA-256 del
// RawSubjectPublicKeyInfo en hex).
func selfSignedCert(t *testing.T) (tlsConf *tls.Config, fp string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	template := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	tlsConf = &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: priv}},
		MinVersion:   tls.VersionTLS12,
	}
	sum := sha256.Sum256(leaf.RawSubjectPublicKeyInfo)
	fp = hex.EncodeToString(sum[:])
	return
}

func TestPinAcceptsMatchingSPKI(t *testing.T) {
	srvTLS, fp := selfSignedCert(t)
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "ok")
	}))
	ts.TLS = srvTLS
	ts.StartTLS()
	defer ts.Close()

	transport, err := BuildTransport(ts.URL, fp)
	if err != nil {
		t.Fatalf("BuildTransport: %v", err)
	}
	hc := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	res, err := hc.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET con SPKI correcto falló: %v", err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(body) != "ok" {
		t.Fatalf("body = %q", body)
	}
}

func TestPinRejectsWrongSPKI(t *testing.T) {
	srvTLS, _ := selfSignedCert(t)
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	ts.TLS = srvTLS
	ts.StartTLS()
	defer ts.Close()

	// Fingerprint deliberadamente incorrecto (64 hex chars).
	transport, err := BuildTransport(ts.URL, "de"+strings.Repeat("ad", 31))
	if err != nil {
		t.Fatalf("BuildTransport: %v", err)
	}
	hc := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	_, err = hc.Get(ts.URL)
	if err == nil {
		t.Fatal("GET con SPKI incorrecto debería fallar")
	}
}

func TestFailClosedHTTPSWithoutFP(t *testing.T) {
	_, err := BuildTransport("https://192.168.1.1:3000", "")
	if err == nil {
		t.Fatal("HTTPS sin serverFP debería dar error (fail-closed)")
	}
}

func TestHTTPSkipsTLS(t *testing.T) {
	transport, err := BuildTransport("http://192.168.1.226:3000", "")
	if err != nil {
		t.Fatalf("HTTP sin FP no debería fallar: %v", err)
	}
	// El transporte por defecto puede tener TLSClientConfig (Go lo inicializa),
	// pero NO debe llevar VerifyPeerCertificate (eso es nuestro pinning).
	if transport.TLSClientConfig != nil && transport.TLSClientConfig.VerifyPeerCertificate != nil {
		t.Fatal("HTTP no debería configurar pinning SPKI")
	}
}

func TestNormalizeStripsPrefixes(t *testing.T) {
	got := Normalize("sha256/AB:CD:EF:01")
	want := "abcdef01"
	if got != want {
		t.Fatalf("Normalize = %q, want %q", got, want)
	}
}
