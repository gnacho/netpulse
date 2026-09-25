package tlscert

// FORK: tests for the private CA. Addresses are from the documentation and
// private ranges, names invented.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	agentruntime "github.com/gnacho/netpulse/agent/runtime"
)

// fakeHost is the system as the CA sees it.
type fakeHost struct {
	addrs []net.IP
	now   time.Time
	logs  []string
}

func (h *fakeHost) opts(dir string) CAOptions {
	return CAOptions{
		Dir:           dir,
		Addrs:         func() ([]net.IP, error) { return h.addrs, nil },
		Hostname:      func() (string, error) { return "monitor-box", nil },
		SearchDomains: func() []string { return []string{"example.lan"} },
		Now:           func() time.Time { return h.now },
		Logf:          func(f string, a ...any) { h.logs = append(h.logs, fmt.Sprintf(f, a...)) },
	}
}

func newHost() *fakeHost {
	return &fakeHost{
		addrs: []net.IP{net.ParseIP("192.168.50.2"), net.ParseIP("fe80::1")},
		now:   time.Now(),
	}
}

func openTestCA(t *testing.T, h *fakeHost, dir string) *CA {
	t.Helper()
	ca, err := OpenCA(h.opts(dir))
	if err != nil {
		t.Fatalf("OpenCA: %v", err)
	}
	return ca
}

func verifyFor(t *testing.T, ca *CA, leaf *x509.Certificate, name string) error {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(ca.root)
	_, err := leaf.Verify(x509.VerifyOptions{
		DNSName: name, Roots: roots, CurrentTime: time.Now(),
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	})
	return err
}

func TestTheRootIsConstrainedAndPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tls")
	ca := openTestCA(t, newHost(), dir)
	r := ca.root
	if !r.IsCA || !r.MaxPathLenZero || r.MaxPathLen != 0 || r.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Fatalf("root is not a path-length-0 CA: isCA=%v pathlen=%d", r.IsCA, r.MaxPathLen)
	}
	// Non-critical on purpose: mbedTLS (OpenWrt) refuses a critical one.
	if r.PermittedDNSDomainsCritical || len(r.PermittedIPRanges) == 0 || len(r.PermittedDNSDomains) == 0 {
		t.Fatal("root lacks its name constraints, or marks them critical")
	}
	for _, want := range []string{"lan", "home.arpa", "monitor-box", "example.lan"} {
		if !ca.permitsName(want) {
			t.Errorf("root does not permit %q", want)
		}
	}
	for path, want := range map[string]os.FileMode{
		dir:                             0o700,
		filepath.Join(dir, caKeyFile):   0o600,
		filepath.Join(dir, leafKeyFile): 0o600,
	} {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != want {
			t.Errorf("%s mode %04o, want %04o", filepath.Base(path), fi.Mode().Perm(), want)
		}
	}
}

func TestTheLeafNamesTheHostAndVerifies(t *testing.T) {
	ca := openTestCA(t, newHost(), tlsDir(t))
	leaf := ca.Leaf()
	for _, name := range []string{"192.168.50.2", "127.0.0.1", "monitor-box", "monitor-box.example.lan"} {
		if err := verifyFor(t, ca, leaf, name); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, ip := range leaf.IPAddresses {
		if ip.IsLinkLocalUnicast() {
			t.Errorf("leaf names link-local %s", ip)
		}
	}
}

// The point of the constraints: even a certificate signed with the CA's own
// key is refused for a public name or address.
func TestTheRootCannotVouchForPublicNames(t *testing.T) {
	ca := openTestCA(t, newHost(), tlsDir(t))
	for _, tc := range []struct {
		dns string
		ip  string
	}{
		{dns: "bank.example.com"},
		{ip: "203.0.113.7"},
	} {
		k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		serial, _ := randomSerial()
		tmpl := &x509.Certificate{
			SerialNumber: serial, Subject: pkix.Name{CommonName: "forged"},
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
			KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}
		name := tc.dns
		if tc.dns != "" {
			tmpl.DNSNames = []string{tc.dns}
		} else {
			tmpl.IPAddresses = []net.IP{net.ParseIP(tc.ip)}
			name = tc.ip
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.root, &k.PublicKey, ca.rootKey)
		if err != nil {
			t.Fatal(err)
		}
		forged, _ := x509.ParseCertificate(der)
		if err := verifyFor(t, ca, forged, name); err == nil {
			t.Errorf("a certificate for %s signed by the CA verified", name)
		}
	}
}

// A public address the host has is left out of the leaf, and said so once.
func TestAddressesOutsideTheConstraintsAreLeftOut(t *testing.T) {
	h := newHost()
	h.addrs = append(h.addrs, net.ParseIP("203.0.113.7"))
	ca := openTestCA(t, h, tlsDir(t))
	for _, ip := range ca.Leaf().IPAddresses {
		if ip.Equal(net.ParseIP("203.0.113.7")) {
			t.Fatal("the leaf names a public address")
		}
	}
	if _, err := ca.Refresh(); err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, l := range h.logs {
		if strings.Contains(l, "203.0.113.7") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the dropped address was logged %d times, want once", n)
	}
}

func TestReopeningKeepsTheRootAndTheLeaf(t *testing.T) {
	h := newHost()
	dir := tlsDir(t)
	first := openTestCA(t, h, dir)
	second := openTestCA(t, h, dir)
	if first.Fingerprint() != second.Fingerprint() {
		t.Fatal("reopening created a new root")
	}
	if first.Leaf().SerialNumber.Cmp(second.Leaf().SerialNumber) != 0 {
		t.Fatal("reopening re-issued a leaf that was still good")
	}
}

func TestTheLeafFollowsTheHostsAddressesWithTheSameKey(t *testing.T) {
	h := newHost()
	ca := openTestCA(t, h, tlsDir(t))
	before := ca.Leaf()
	h.addrs = []net.IP{net.ParseIP("10.20.30.40")}
	issued, err := ca.Refresh()
	if err != nil || !issued {
		t.Fatalf("an address change did not re-issue the leaf (issued=%v, err=%v)", issued, err)
	}
	after := ca.Leaf()
	if err := verifyFor(t, ca, after, "10.20.30.40"); err != nil {
		t.Fatalf("new leaf does not name the new address: %v", err)
	}
	if !after.PublicKey.(*ecdsa.PublicKey).Equal(before.PublicKey) {
		t.Fatal("re-issuing changed the leaf key")
	}
	if issued, _ := ca.Refresh(); issued {
		t.Fatal("an unchanged host re-issued the leaf again")
	}
}

func TestTheLeafIsRenewedBeforeItExpires(t *testing.T) {
	h := newHost()
	ca := openTestCA(t, h, tlsDir(t))
	h.now = h.now.Add(leafValidity - leafRenewBefore + time.Hour)
	if issued, err := ca.Refresh(); err != nil || !issued {
		t.Fatalf("a leaf inside the renewal window was not renewed (issued=%v, err=%v)", issued, err)
	}
	if !ca.Leaf().NotAfter.After(h.now.Add(leafRenewBefore)) {
		t.Fatal("the renewed leaf is not valid for long enough")
	}
}

func TestAFailedWriteKeepsServingThePreviousLeaf(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	h := newHost()
	dir := tlsDir(t)
	ca := openTestCA(t, h, dir)
	before := ca.Leaf()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)
	h.addrs = []net.IP{net.ParseIP("10.20.30.40")}
	if _, err := ca.Refresh(); err == nil {
		t.Fatal("writing into a read-only directory succeeded")
	}
	if ca.Leaf() != before {
		t.Fatal("a failed re-issue replaced the served leaf")
	}
}

func TestOpenKeyFilesAreRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		mess func(dir string) error
		want string
	}{
		{"a readable CA key", func(dir string) error { return os.Chmod(filepath.Join(dir, caKeyFile), 0o644) }, "chmod 600"},
		{"a readable directory", func(dir string) error { return os.Chmod(dir, 0o755) }, "chmod 700"},
		{"a CA certificate without its key", func(dir string) error { return os.Remove(filepath.Join(dir, caKeyFile)) }, "only one of"},
		{"a CA key without its certificate", func(dir string) error { return os.Remove(filepath.Join(dir, caCertFile)) }, "only one of"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost()
			dir := filepath.Join(t.TempDir(), "tls")
			openTestCA(t, h, dir)
			if err := tc.mess(dir); err != nil {
				t.Fatal(err)
			}
			_, err := OpenCA(h.opts(dir))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("OpenCA error %v, want one saying %q", err, tc.want)
			}
		})
	}
}

// End to end: an agent that pins the root reaches the server over TLS.
func TestAnAgentPinningTheRootConnects(t *testing.T) {
	ca := openTestCA(t, newHost(), tlsDir(t))
	// A listener with exactly the CA's config: httptest would add its own
	// certificate, which Go serves instead of GetCertificate's to a client
	// that sends no SNI - as one dialling an address does.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "ok")
	})}
	go srv.Serve(tls.NewListener(ln, ca.TLSConfig()))
	defer srv.Close()
	url := "https://" + ln.Addr().String()

	tr, err := agentruntime.ServerTransport(url, ca.Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	res, err := (&http.Client{Transport: tr, Timeout: 5 * time.Second}).Get(url)
	if err != nil {
		t.Fatalf("an agent pinning the root could not connect: %v", err)
	}
	res.Body.Close()
	if res.TLS == nil || res.TLS.Version < tls.VersionTLS12 {
		t.Fatal("not a TLS 1.2+ connection")
	}
}

func TestRootSHA256IsWhatDevicesShow(t *testing.T) {
	ca := openTestCA(t, newHost(), tlsDir(t))
	fp := ca.RootSHA256()
	if len(fp) != 32*3-1 || strings.Count(fp, ":") != 31 || strings.ToUpper(fp) != fp {
		t.Fatalf("RootSHA256 = %q, want 32 upper-case hex pairs joined by colons", fp)
	}
}

// tlsDir is a directory the CA creates itself, as DATA_DIR/tls is.
func tlsDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "tls")
}

// A search domain lets the root vouch for this host under it, never for the
// whole domain, which can be a company's or a public one.
func TestASearchDomainOnlyCoversThisHost(t *testing.T) {
	h := newHost()
	opts := h.opts(tlsDir(t))
	opts.SearchDomains = func() []string { return []string{"corp.example"} }
	ca, err := OpenCA(opts)
	if err != nil {
		t.Fatal(err)
	}
	if !ca.permitsName("monitor-box.corp.example") {
		t.Fatal("the host under its search domain is not covered")
	}
	for _, n := range []string{"corp.example", "mail.corp.example"} {
		if ca.permitsName(n) {
			t.Errorf("the root may vouch for %s", n)
		}
	}
}

// Created with a clock running fast, the root is still valid once the clock
// is corrected.
func TestTheRootToleratesAFastClockAtCreation(t *testing.T) {
	h := newHost()
	h.now = time.Now().Add(90 * 24 * time.Hour)
	ca := openTestCA(t, h, tlsDir(t))
	if !ca.root.NotBefore.Before(time.Now()) {
		t.Fatalf("root not valid before %v", ca.root.NotBefore)
	}
}
