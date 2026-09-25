package tlspin

// FORK: tests for CA pins and pin lists. Addresses are from the
// documentation ranges, names invented.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func newKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func sign(t *testing.T, tmpl, parent *x509.Certificate, pub any, signer *ecdsa.PrivateKey) *x509.Certificate {
	t.Helper()
	tmpl.SerialNumber, _ = rand.Int(rand.Reader, big.NewInt(1<<62))
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, pub, signer)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// newCA makes a root; isCA=false makes a certificate that looks like one to a
// careless check but is not.
func newCA(t *testing.T, isCA bool) testCA {
	t.Helper()
	k := newKey(t)
	tmpl := &x509.Certificate{
		Subject:               pkix.Name{CommonName: "test root"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		BasicConstraintsValid: true,
		IsCA:                  isCA,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	return testCA{cert: sign(t, tmpl, tmpl, &k.PublicKey, k), key: k}
}

type leafOpts struct {
	ips       []string
	names     []string
	notBefore time.Time
	notAfter  time.Time
	eku       []x509.ExtKeyUsage
}

func (ca testCA) leaf(t *testing.T, o leafOpts) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	k := newKey(t)
	if o.notBefore.IsZero() {
		o.notBefore = time.Now().Add(-time.Hour)
	}
	if o.notAfter.IsZero() {
		o.notAfter = time.Now().Add(time.Hour)
	}
	if o.eku == nil {
		o.eku = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	}
	tmpl := &x509.Certificate{
		Subject:     pkix.Name{CommonName: "test server"},
		NotBefore:   o.notBefore,
		NotAfter:    o.notAfter,
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: o.eku,
		DNSNames:    o.names,
	}
	for _, ip := range o.ips {
		tmpl.IPAddresses = append(tmpl.IPAddresses, net.ParseIP(ip))
	}
	return sign(t, tmpl, ca.cert, &k.PublicKey, ca.key), k
}

func TestVerifyCAPins(t *testing.T) {
	ca := newCA(t, true)
	other := newCA(t, true)
	notCA := newCA(t, false)
	now := time.Now()
	good, _ := ca.leaf(t, leafOpts{ips: []string{"192.0.2.10"}, names: []string{"netpulse.lan"}})
	byOther, _ := other.leaf(t, leafOpts{ips: []string{"192.0.2.10"}})
	byNotCA, _ := notCA.leaf(t, leafOpts{ips: []string{"192.0.2.10"}})
	expired, _ := ca.leaf(t, leafOpts{ips: []string{"192.0.2.10"},
		notBefore: now.Add(-48 * time.Hour), notAfter: now.Add(-24 * time.Hour)})
	notYet, _ := ca.leaf(t, leafOpts{ips: []string{"192.0.2.10"},
		notBefore: now.Add(24 * time.Hour), notAfter: now.Add(48 * time.Hour)})
	clientOnly, _ := ca.leaf(t, leafOpts{ips: []string{"192.0.2.10"},
		eku: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})

	for _, tc := range []struct {
		name    string
		chain   []*x509.Certificate
		host    string
		pins    []string
		wantErr string // "" = accepted
	}{
		{"CA pin, by IP", []*x509.Certificate{good, ca.cert}, "192.0.2.10", []string{spki(ca.cert)}, ""},
		{"CA pin, by name", []*x509.Certificate{good, ca.cert}, "netpulse.lan", []string{spki(ca.cert)}, ""},
		{"a list with one match", []*x509.Certificate{good, ca.cert}, "192.0.2.10",
			[]string{spki(other.cert), spki(ca.cert)}, ""},
		{"leaf pin keeps its old meaning", []*x509.Certificate{good, ca.cert}, "198.51.100.1",
			[]string{spki(good)}, ""},
		{"wrong CA pinned", []*x509.Certificate{good, ca.cert}, "192.0.2.10", []string{spki(other.cert)}, "mismatch"},
		{"a name the leaf does not carry", []*x509.Certificate{good, ca.cert}, "198.51.100.1",
			[]string{spki(ca.cert)}, "does not name"},
		{"expired leaf", []*x509.Certificate{expired, ca.cert}, "192.0.2.10", []string{spki(ca.cert)}, "clock"},
		{"leaf not valid yet", []*x509.Certificate{notYet, ca.cert}, "192.0.2.10", []string{spki(ca.cert)}, "clock"},
		{"pinned key on a certificate that is not a CA", []*x509.Certificate{byNotCA, notCA.cert}, "192.0.2.10",
			[]string{spki(notCA.cert)}, "not a CA"},
		// Our root is public: an impostor can send it along with a leaf of
		// its own. The leaf must still have been signed by the pinned key.
		{"an impostor's leaf sent with our root", []*x509.Certificate{byOther, ca.cert}, "192.0.2.10",
			[]string{spki(ca.cert)}, "not issued"},
		{"a leaf not meant for servers", []*x509.Certificate{clientOnly, ca.cert}, "192.0.2.10",
			[]string{spki(ca.cert)}, "not issued"},
		{"no certificate", nil, "192.0.2.10", []string{spki(ca.cert)}, "sin certificados"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Verify(tc.chain, tc.host, tc.pins, now)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("refused: %v", err)
			case tc.wantErr != "" && err == nil:
				t.Fatal("accepted")
			case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
				t.Fatalf("error %q does not say %q", err, tc.wantErr)
			}
		})
	}
}

// End to end: an agent pinning the CA reaches a server that presents a leaf
// the CA signed, and refuses the same server when it presents a leaf signed
// by another CA - even with the pinned root attached to the chain.
func TestCAPinOverARealConnection(t *testing.T) {
	ca := newCA(t, true)
	other := newCA(t, true)
	for _, tc := range []struct {
		name   string
		signer testCA
		wantOK bool
	}{
		{"leaf signed by the pinned CA", ca, true},
		{"leaf signed by another CA", other, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			leaf, key := tc.signer.leaf(t, leafOpts{ips: []string{"127.0.0.1"}})
			ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, "ok")
			}))
			ts.TLS = &tls.Config{Certificates: []tls.Certificate{{
				Certificate: [][]byte{leaf.Raw, ca.cert.Raw},
				PrivateKey:  key,
			}}}
			ts.StartTLS()
			defer ts.Close()

			tr, err := BuildTransport(ts.URL, spki(ca.cert))
			if err != nil {
				t.Fatal(err)
			}
			res, err := (&http.Client{Transport: tr, Timeout: 5 * time.Second}).Get(ts.URL)
			if err == nil {
				res.Body.Close()
			}
			if (err == nil) != tc.wantOK {
				t.Fatalf("connected = %v, want %v (err %v)", err == nil, tc.wantOK, err)
			}
		})
	}
}

func TestBuildTransportRefusesAMalformedPin(t *testing.T) {
	good := strings.Repeat("ab", 32)
	for _, fps := range []string{"abcd", good + ",zz", good + "," + good[:62]} {
		if _, err := BuildTransport("https://192.0.2.10:3443", fps); err == nil {
			t.Errorf("BuildTransport accepted pins %q", fps)
		}
	}
	if _, err := BuildTransport("https://192.0.2.10:3443", "sha256/"+good+", "+strings.ToUpper(good)); err != nil {
		t.Errorf("a normalizable list was refused: %v", err)
	}
}

// The check must sit in VerifyConnection, which also runs on resumed
// sessions; VerifyPeerCertificate does not.
func TestThePinIsCheckedOnEveryConnection(t *testing.T) {
	tr, err := BuildTransport("https://192.0.2.10:3443", strings.Repeat("ab", 32))
	if err != nil {
		t.Fatal(err)
	}
	c := tr.TLSClientConfig
	if c.VerifyConnection == nil || c.VerifyPeerCertificate != nil {
		t.Fatal("the pin must be checked in VerifyConnection")
	}
}

// A CA made the way OpenSSL makes one by default has no key usage extension.
// It may sign, and must be accepted.
func TestACAWithoutKeyUsageIsAccepted(t *testing.T) {
	k := newKey(t)
	tmpl := &x509.Certificate{
		Subject:               pkix.Name{CommonName: "openssl-style root"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	ca := testCA{cert: sign(t, tmpl, tmpl, &k.PublicKey, k), key: k}
	if ca.cert.KeyUsage != 0 {
		t.Fatal("test root unexpectedly has a key usage")
	}
	leaf, _ := ca.leaf(t, leafOpts{ips: []string{"192.0.2.10"}})
	if err := Verify([]*x509.Certificate{leaf, ca.cert}, "192.0.2.10", []string{spki(ca.cert)}, time.Now()); err != nil {
		t.Fatalf("refused: %v", err)
	}
}

// One pinned certificate that cannot vouch for the leaf does not stop another
// pinned one further up the chain from doing so.
func TestEveryPinnedCertificateInTheChainIsTried(t *testing.T) {
	ca := newCA(t, true)
	notCA := newCA(t, false)
	leaf, _ := ca.leaf(t, leafOpts{ips: []string{"192.0.2.10"}})
	chain := []*x509.Certificate{leaf, notCA.cert, ca.cert}
	if err := Verify(chain, "192.0.2.10", []string{spki(notCA.cert), spki(ca.cert)}, time.Now()); err != nil {
		t.Fatalf("refused: %v", err)
	}
}

func TestBuildTransportRefusesAURLWithoutAHost(t *testing.T) {
	if _, err := BuildTransport("https://:3443", strings.Repeat("ab", 32)); err == nil {
		t.Fatal("a URL with no host was accepted: nothing to check the certificate's names against")
	}
}

func TestNormalizeStripsAnUpperCasePrefix(t *testing.T) {
	good := strings.Repeat("ab", 32)
	pins, err := ParsePins("SHA256/" + strings.ToUpper(good))
	if err != nil || len(pins) != 1 || pins[0] != good {
		t.Fatalf("ParsePins = %v, %v", pins, err)
	}
}
