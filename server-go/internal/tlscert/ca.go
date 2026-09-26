package tlscert

// FORK: a private certificate authority for this server.
//
// Browsers only register a service worker - Web Push, installing the PWA -
// on a certificate they trust outright; an exception accepted for a
// self-signed certificate is not enough. So the server can run its own root
// CA, which the admin installs once on their devices, and serve a leaf it
// signs. Agents pin the root (tlspin accepts a CA pin), so the leaf can be
// re-issued - on renewal, or when the server's addresses change - without
// breaking any of them.
//
// A root the admin's devices trust is a powerful thing to keep on a server.
// Three properties bound it:
//
//   - The root carries Name Constraints: private address ranges and
//     local DNS names only. A copy of its key cannot be used to impersonate
//     any public site to a device that trusts it.
//   - Its key is only ever used to sign this server's own leaf, stays in a
//     0700 directory in a 0600 file owned by the service user, and the server
//     refuses to start if either has been opened up.
//   - It is created once and never replaced implicitly: a missing half of the
//     pair is an error, not a reason to mint a new root the devices do not
//     trust.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	caCertFile   = "ca.pem"
	caKeyFile    = "ca-key.pem"
	leafCertFile = "leaf.pem"
	leafKeyFile  = "leaf-key.pem"

	rootValidity = 10 * 365 * 24 * time.Hour
	rootBackdate = 365 * 24 * time.Hour
	// Short-lived leaves limit how long a mis-issued one would be accepted;
	// renewal is automatic and the key does not change, so it costs nothing.
	leafValidity    = 90 * 24 * time.Hour
	leafRenewBefore = 30 * 24 * time.Hour
	// How often Run re-checks the leaf: for expiry, and for a change in the
	// server's addresses.
	caCheckEvery = 10 * time.Minute
)

// Address ranges a leaf may name: private, carrier-grade NAT (which overlay
// VPNs use), loopback and IPv6 unique-local.
var permittedIPRanges = []string{
	"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10",
	"127.0.0.0/8", "::1/128", "fc00::/7",
}

// DNS suffixes that are local by definition or by convention. The host's own
// names and search domains are added when the root is created.
var permittedLocalSuffixes = []string{"lan", "home.arpa", "internal", "local"}

// CAOptions configures OpenCA. The function fields exist for tests; nil
// means the real system.
type CAOptions struct {
	Dir        string   // where the CA and leaf live; created 0700
	Names      []string // extra names or addresses for the leaf (NETPULSE_TLS_NAMES)
	PublicHost string   // host of NETPULSE_PUBLIC_URL, if set

	Addrs         func() ([]net.IP, error)
	Hostname      func() (string, error)
	SearchDomains func() []string
	Now           func() time.Time
	Logf          func(format string, args ...any)
}

// CA is the server's private CA and the leaf it serves.
type CA struct {
	opts    CAOptions
	root    *x509.Certificate
	rootKey *ecdsa.PrivateKey
	leafKey *ecdsa.PrivateKey

	mu          sync.Mutex // serializes Refresh
	lastDropped string
	uncovered   []string // names and addresses the root cannot vouch for
	cur         atomic.Pointer[tls.Certificate]
}

// OpenCA loads the CA in opts.Dir, creating it on first use, and makes sure
// a current leaf is ready to serve.
func OpenCA(opts CAOptions) (*CA, error) {
	if opts.Dir == "" {
		return nil, errors.New("tls: no directory for the CA")
	}
	if opts.Addrs == nil {
		opts.Addrs = interfaceAddrs
	}
	if opts.Hostname == nil {
		opts.Hostname = os.Hostname
	}
	if opts.SearchDomains == nil {
		opts.SearchDomains = resolvSearchDomains
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	if err := ensurePrivateDir(opts.Dir); err != nil {
		return nil, err
	}
	ca := &CA{opts: opts}
	if err := ca.openRoot(); err != nil {
		return nil, err
	}
	key, err := loadOrCreateKey(filepath.Join(opts.Dir, leafKeyFile))
	if err != nil {
		return nil, fmt.Errorf("tls: leaf key: %w", err)
	}
	ca.leafKey = key
	if err := ca.loadLeaf(); err != nil {
		opts.Logf("[netpulse] TLS: the stored leaf cannot be used (%v); issuing a new one", err)
	}
	if _, err := ca.Refresh(); err != nil {
		return nil, err
	}
	return ca, nil
}

func (ca *CA) openRoot() error {
	certPath := filepath.Join(ca.opts.Dir, caCertFile)
	keyPath := filepath.Join(ca.opts.Dir, caKeyFile)
	haveCert, haveKey := fileExists(certPath), fileExists(keyPath)
	switch {
	case haveCert && haveKey:
		key, err := loadKey(keyPath)
		if err != nil {
			return fmt.Errorf("tls: CA key: %w", err)
		}
		cert, err := loadCert(certPath)
		if err != nil {
			return fmt.Errorf("tls: CA certificate: %w", err)
		}
		if !cert.IsCA || !key.PublicKey.Equal(cert.PublicKey) {
			return fmt.Errorf("tls: %s and %s are not a CA and its key", certPath, keyPath)
		}
		ca.root, ca.rootKey = cert, key
		return nil
	case haveCert || haveKey:
		// Never mint a replacement: every device that trusts the old root
		// would silently stop trusting the server.
		return fmt.Errorf("tls: only one of %s and %s exists; restore the other from a backup, "+
			"or remove both to create a new CA (every device will need the new root installed)", certPath, keyPath)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	tmpl, err := ca.rootTemplate()
	if err != nil {
		return err
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("tls: create CA: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return err
	}
	// Key first: a crash between the two writes leaves a key without a
	// certificate, which openRoot refuses loudly, rather than a certificate
	// devices might already trust whose key is lost.
	if err := writeKey(keyPath, key); err != nil {
		return fmt.Errorf("tls: write CA key: %w", err)
	}
	if err := writeAtomic(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return fmt.Errorf("tls: write CA certificate: %w", err)
	}
	ca.root, ca.rootKey = cert, key
	ca.opts.Logf("[netpulse] TLS: created a private CA; install it on your devices and check its fingerprint: %s", ca.RootSHA256())
	return nil
}

func (ca *CA) rootTemplate() (*x509.Certificate, error) {
	serial, err := randomSerial()
	if err != nil {
		return nil, err
	}
	var ranges []*net.IPNet
	for _, r := range permittedIPRanges {
		_, n, err := net.ParseCIDR(r)
		if err != nil {
			return nil, err
		}
		ranges = append(ranges, n)
	}
	domains := slices.Clone(permittedLocalSuffixes)
	host, _ := ca.opts.Hostname()
	domains = append(domains, hostNames(host)...)
	// Only this host under each search domain, never the whole domain: a
	// search domain can be a company's or a public one, and permitting it
	// would let a copy of the CA key vouch for everything in it.
	if short := firstLabel(host); short != "" {
		for _, d := range ca.opts.SearchDomains() {
			domains = append(domains, short+"."+d)
		}
	}
	for _, n := range append(slices.Clone(ca.opts.Names), ca.opts.PublicHost) {
		if n = normName(n); n != "" && net.ParseIP(n) == nil {
			// A name is permitted with everything below it - name
			// constraints cannot say "this name only" - so a public one
			// widens what the root may vouch for. Allowed, since it was
			// asked for, but said.
			if !underLocalSuffix(n) {
				ca.opts.Logf("[netpulse] TLS: the CA may also vouch for everything under %s, "+
					"which is not a local name (NETPULSE_TLS_NAMES / NETPULSE_PUBLIC_URL)", n)
			}
			domains = append(domains, n)
		}
	}
	slices.Sort(domains)
	domains = slices.Compact(domains)
	now := ca.opts.Now()
	cn := "NetPulse local CA"
	if short := firstLabel(host); short != "" {
		cn += " (" + short + ")"
	}
	// Backdated well past any clock skew: the root is never replaced, so one
	// created while the clock ran fast must not stay "not yet valid" for
	// months.
	notBefore := now.Add(-rootBackdate)
	// The name constraints are not marked critical: mbedTLS - OpenWrt's TLS
	// library, behind its wget and curl - refuses to load a certificate with
	// a critical extension it does not implement, and it does not implement
	// this one, so routers could not verify the server at all. Go, OpenSSL,
	// Chrome and Firefox enforce them either way; the CA/B Baseline
	// Requirements allowed them non-critical for the same reason.
	return &x509.Certificate{
		SerialNumber:                serial,
		Subject:                     pkix.Name{CommonName: cn, Organization: []string{"NetPulse"}},
		NotBefore:                   notBefore,
		NotAfter:                    now.Add(rootValidity),
		IsCA:                        true,
		BasicConstraintsValid:       true,
		MaxPathLen:                  0,
		MaxPathLenZero:              true,
		KeyUsage:                    x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		PermittedDNSDomainsCritical: false,
		PermittedDNSDomains:         domains,
		PermittedIPRanges:           ranges,
	}, nil
}

func (ca *CA) loadLeaf() error {
	path := filepath.Join(ca.opts.Dir, leafCertFile)
	if !fileExists(path) {
		return nil
	}
	leaf, err := loadCert(path)
	if err != nil {
		return err
	}
	ca.store(leaf)
	return nil
}

func (ca *CA) store(leaf *x509.Certificate) {
	ca.cur.Store(&tls.Certificate{
		Certificate: [][]byte{leaf.Raw, ca.root.Raw},
		PrivateKey:  ca.leafKey,
		Leaf:        leaf,
	})
}

// Refresh re-issues the leaf if it is missing, near expiry, not this CA's,
// not for the leaf key, or names a different set of addresses than the
// server now has. It reports whether it issued one. On failure the leaf being
// served is kept.
func (ca *CA) Refresh() (bool, error) {
	ca.mu.Lock()
	defer ca.mu.Unlock()
	dns, ips := ca.desiredNames()
	now := ca.opts.Now()
	if cur := ca.cur.Load(); cur != nil && !ca.needsReissue(cur.Leaf, dns, ips, now) {
		return false, nil
	}
	serial, err := randomSerial()
	if err != nil {
		return false, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "NetPulse"},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(leafValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     dns,
		IPAddresses:  ips,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.root, &ca.leafKey.PublicKey, ca.rootKey)
	if err != nil {
		return false, fmt.Errorf("tls: issue leaf: %w", err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return false, err
	}
	if err := writeAtomic(filepath.Join(ca.opts.Dir, leafCertFile),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return false, fmt.Errorf("tls: write leaf: %w", err)
	}
	ca.store(leaf)
	ca.opts.Logf("[netpulse] TLS: issued a certificate for %s, valid until %s",
		strings.Join(sanList(dns, ips), ", "), leaf.NotAfter.Format(time.DateOnly))
	return true, nil
}

func (ca *CA) needsReissue(leaf *x509.Certificate, dns []string, ips []net.IP, now time.Time) bool {
	if leaf == nil || leaf.CheckSignatureFrom(ca.root) != nil {
		return true
	}
	if pub, ok := leaf.PublicKey.(*ecdsa.PublicKey); !ok || !pub.Equal(&ca.leafKey.PublicKey) {
		return true
	}
	if now.Add(leafRenewBefore).After(leaf.NotAfter) || now.Before(leaf.NotBefore) {
		return true
	}
	return !slices.Equal(sanList(leaf.DNSNames, leaf.IPAddresses), sanList(dns, ips))
}

// desiredNames is what the leaf should name now: every address the server
// can be reached at and every name it goes by, limited to what the root may
// vouch for.
func (ca *CA) desiredNames() ([]string, []net.IP) {
	var names []string
	ips := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}
	if addrs, err := ca.opts.Addrs(); err == nil {
		ips = append(ips, addrs...)
	} else {
		ca.opts.Logf("[netpulse] TLS: cannot list this host's addresses: %v", err)
	}
	host, _ := ca.opts.Hostname()
	names = append(names, hostNames(host)...)
	if short := firstLabel(host); short != "" {
		for _, d := range ca.opts.SearchDomains() {
			names = append(names, short+"."+d)
		}
	}
	for _, n := range append(slices.Clone(ca.opts.Names), ca.opts.PublicHost) {
		n = normName(n)
		if n == "" {
			continue
		}
		if ip := net.ParseIP(n); ip != nil {
			ips = append(ips, ip)
		} else {
			names = append(names, n)
		}
	}

	var dns, dropped []string
	var keep []net.IP
	for _, n := range names {
		if ca.permitsName(n) {
			dns = append(dns, n)
		} else {
			dropped = append(dropped, n)
		}
	}
	for _, ip := range ips {
		if ip.IsLinkLocalUnicast() || ip.IsMulticast() || ip.IsUnspecified() {
			continue
		}
		if ca.permitsIP(ip) {
			keep = append(keep, ip)
		} else {
			dropped = append(dropped, ip.String())
		}
	}
	slices.Sort(dns)
	dns = slices.Compact(dns)
	slices.SortFunc(keep, func(a, b net.IP) int { return strings.Compare(a.String(), b.String()) })
	keep = slices.CompactFunc(keep, func(a, b net.IP) bool { return a.Equal(b) })

	// Said once per change, not on every check.
	slices.Sort(dropped)
	dropped = slices.Compact(dropped)
	ca.uncovered = dropped
	if d := strings.Join(dropped, ", "); d != ca.lastDropped {
		ca.lastDropped = d
		if d != "" {
			ca.opts.Logf("[netpulse] TLS: the certificate cannot name %s: the CA only vouches for private "+
				"addresses, and for local names and the names it was created with. A name can be added by "+
				"creating a new CA with it in NETPULSE_TLS_NAMES (docs/https.md, \"Starting over\"); a "+
				"public address cannot be covered.", d)
		}
	}
	return dns, keep
}

func (ca *CA) permitsName(n string) bool {
	for _, d := range ca.root.PermittedDNSDomains {
		if n == d || strings.HasSuffix(n, "."+d) {
			return true
		}
	}
	return false
}

func (ca *CA) permitsIP(ip net.IP) bool {
	for _, r := range ca.root.PermittedIPRanges {
		if r.Contains(ip) {
			return true
		}
	}
	return false
}

// Run re-checks the leaf until done is closed.
func (ca *CA) Run(done <-chan struct{}) {
	t := time.NewTicker(caCheckEvery)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			if _, err := ca.Refresh(); err != nil {
				ca.opts.Logf("[netpulse] TLS: %v (still serving the previous certificate)", err)
			}
		}
	}
}

// TLSConfig serves the current leaf with the root attached, so a client that
// pins the root can verify the chain.
func (ca *CA) TLSConfig() *tls.Config {
	return &tls.Config{
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			return ca.cur.Load(), nil
		},
		MinVersion: tls.VersionTLS12,
		// TLS 1.2 is limited to forward-secret AEAD suites; the key is ECDSA,
		// so only ECDSA suites can be used anyway. TLS 1.3 suites are all
		// sound and not configurable.
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
		},
		CurvePreferences: []tls.CurveID{tls.X25519, tls.CurveP256},
	}
}

// Fingerprint is the root's SPKI fingerprint: the pin agents use.
func (ca *CA) Fingerprint() string { return Fingerprint(ca.root) }

// RootSHA256 is the SHA-256 of the root certificate, in the colon-separated
// form phones and browsers show when installing it - what an admin compares.
func (ca *CA) RootSHA256() string {
	sum := sha256.Sum256(ca.root.Raw)
	parts := make([]string, len(sum))
	for i, b := range sum {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, ":")
}

// RootDER and RootPEM are the root certificate, for devices to install.
func (ca *CA) RootDER() []byte { return slices.Clone(ca.root.Raw) }
func (ca *CA) RootPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.root.Raw})
}

// Uncovered lists the host's names and addresses the certificate leaves out
// because the root may not vouch for them, as of the last check.
func (ca *CA) Uncovered() []string {
	ca.mu.Lock()
	defer ca.mu.Unlock()
	return slices.Clone(ca.uncovered)
}

// Leaf is the certificate being served now.
func (ca *CA) Leaf() *x509.Certificate {
	if cur := ca.cur.Load(); cur != nil {
		return cur.Leaf
	}
	return nil
}

// --- names and addresses ---

func interfaceAddrs() ([]net.IP, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	var ips []net.IP
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok {
			ips = append(ips, n.IP)
		}
	}
	return ips, nil
}

// resolvSearchDomains reads the local DNS domains from /etc/resolv.conf, so
// the leaf names the host as the network's DNS knows it.
func resolvSearchDomains() []string {
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || (f[0] != "search" && f[0] != "domain") {
			continue
		}
		for _, d := range f[1:] {
			if d = normName(d); d != "" && d != "." {
				out = append(out, d)
			}
		}
	}
	return out
}

// hostNames is the host name as given and its first label, lower-cased.
func hostNames(host string) []string {
	host = normName(host)
	if host == "" {
		return nil
	}
	if short := firstLabel(host); short != host {
		return []string{host, short}
	}
	return []string{host}
}

// underLocalSuffix reports whether n is one of the local suffixes or below
// one.
func underLocalSuffix(n string) bool {
	for _, d := range permittedLocalSuffixes {
		if n == d || strings.HasSuffix(n, "."+d) {
			return true
		}
	}
	return false
}

func firstLabel(host string) string {
	host = normName(host)
	if i := strings.IndexByte(host, '.'); i >= 0 {
		return host[:i]
	}
	return host
}

func normName(n string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(n)), ".")
}

func sanList(dns []string, ips []net.IP) []string {
	out := slices.Clone(dns)
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	slices.Sort(out)
	return out
}

// --- files ---

// ensurePrivateDir creates dir 0700, or checks that an existing one is not
// open to other users.
func ensurePrivateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("tls: %w", err)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("tls: %w", err)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("tls: %s is open to other users (mode %04o); it holds the CA key. Run: chmod 700 %s",
			dir, fi.Mode().Perm(), dir)
	}
	if !ownedByMe(fi) {
		return fmt.Errorf("tls: %s is not owned by the user the server runs as", dir)
	}
	return nil
}

// checkPrivateFile refuses a key file other users could read or replace.
func checkPrivateFile(path string, fi fs.FileInfo) error {
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s is open to other users (mode %04o). Run: chmod 600 %s", path, fi.Mode().Perm(), path)
	}
	if !ownedByMe(fi) {
		return fmt.Errorf("%s is not owned by the user the server runs as", path)
	}
	return nil
}

func loadOrCreateKey(path string) (*ecdsa.PrivateKey, error) {
	if fileExists(path) {
		return loadKey(path)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	return key, writeKey(path, key)
}

func loadKey(path string) (*ecdsa.PrivateKey, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if err := checkPrivateFile(path, fi); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "EC PRIVATE KEY" {
		return nil, fmt.Errorf("%s: not a PEM EC private key", path)
	}
	return x509.ParseECPrivateKey(block.Bytes)
}

func writeKey(path string, key *ecdsa.PrivateKey) error {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	return writeAtomic(path, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), 0o600)
}

func loadCert(path string) (*x509.Certificate, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("%s: not a PEM certificate", path)
	}
	return x509.ParseCertificate(block.Bytes)
}

// writeAtomic replaces path so that a reader - or a crash - sees either the
// old content or the new, never half of it.
func writeAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name) // no-op once renamed
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

func randomSerial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
}
