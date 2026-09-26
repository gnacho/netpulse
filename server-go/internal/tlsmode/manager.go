// Package tlsmode runs HTTPS with the server's private CA and decides what
// plain HTTP may still do, as settings an admin changes from the web UI
// without a restart.
//
// FORK: see docs/https.md. In short:
//
//   - Enabling HTTPS only adds a TLS listener (NETPULSE_TLS_PORT, 3443 by
//     default) served with a leaf of the private CA. Plain HTTP keeps working
//     exactly as before (mode "full"), so enabling it cannot lock anyone out.
//   - Mode "migrate" stops plain HTTP from carrying credentials - logins,
//     sessions, API tokens - and redirects browsers to HTTPS, while agents
//     keep reporting over it until each one has moved.
//   - Mode "redirect" leaves plain HTTP with nothing but redirects, the root
//     certificate and a loopback health check.
//
// A stricter mode only takes effect once confirmed from a page loaded over
// HTTPS, within a few minutes: the admin has then proved they can reach and
// log in over HTTPS before plain HTTP stops accepting logins.
//
// The environment always wins over the UI (NETPULSE_TLS_ENABLED with
// NETPULSE_TLS_CA, NETPULSE_HTTP_MODE): a setting fixed there is shown as
// locked, and is how to recover over SSH if the UI cannot be reached.
package tlsmode

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/tlscert"
)

// Mode is what plain HTTP may still do once HTTPS is on.
type Mode string

const (
	Full     Mode = "full"
	Migrate  Mode = "migrate"
	Redirect Mode = "redirect"
)

// ParseMode accepts the three modes; anything else is an error.
func ParseMode(s string) (Mode, error) {
	switch m := Mode(s); m {
	case Full, Migrate, Redirect:
		return m, nil
	}
	return "", fmt.Errorf("unknown HTTP mode %q (want full, migrate or redirect)", s)
}

const (
	kvEnabled = "settings.https.enabled"
	kvMode    = "settings.https.mode"

	// ConfirmWindow is how long a staged mode change waits for confirmation
	// from an HTTPS page before it is dropped.
	ConfirmWindow = 5 * time.Minute
)

// KV is the part of the database the manager uses.
type KV interface {
	QueryRow(query string, args ...any) *sql.Row
	Exec(query string, args ...any) (sql.Result, error)
}

// Options configures New.
type Options struct {
	DB        KV
	DataDir   string
	Port      int      // the TLS listener's port
	Names     []string // NETPULSE_TLS_NAMES
	PublicURL string   // NETPULSE_PUBLIC_URL

	// EnvEnabled is non-nil when the environment decides whether HTTPS with
	// the private CA is on; EnvMode is non-empty when it fixes the mode.
	EnvEnabled *bool
	EnvMode    Mode
	// Unavailable, when not empty, says why this server's HTTPS is managed
	// elsewhere (on-box, or a certificate of the admin's own), and the
	// manager stays off.
	Unavailable string

	// NewServer builds the TLS listener's server with the same timeouts as
	// the plain one.
	NewServer func(addr string, h http.Handler) *http.Server

	Logf func(format string, args ...any)
	Now  func() time.Time
	// CAOptions adjusts the CA's options; tests use it to fake the host.
	CAOptions func(tlscert.CAOptions) tlscert.CAOptions
}

// Manager is the HTTPS state of the running server.
type Manager struct {
	opts Options

	mu       sync.Mutex
	handler  http.Handler
	enabled  bool
	ca       *tlscert.CA
	srv      *http.Server
	caStop   chan struct{}
	lastErr  string
	pending  *pendingMode
	mode     atomic.Value // Mode
	caLoaded atomic.Pointer[tlscert.CA]

	sniff *sniffServer
}

type pendingMode struct {
	mode    Mode
	code    string
	expires time.Time
	by      string
}

// New reads the saved settings. Nothing listens until Start.
func New(opts Options) *Manager {
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	m := &Manager{opts: opts}
	m.mode.Store(Full)
	if opts.Unavailable != "" {
		return m
	}
	m.enabled = m.savedEnabled()
	if opts.EnvEnabled != nil {
		m.enabled = *opts.EnvEnabled
	}
	mode := m.savedMode()
	if opts.EnvMode != "" {
		mode = opts.EnvMode
	}
	if !m.enabled {
		mode = Full
	}
	m.mode.Store(mode)
	return m
}

// Start begins serving HTTPS if it is enabled. h is the API handler, served
// on the TLS listener exactly as on the plain one. An error is returned only
// when the environment asked for HTTPS; a UI setting that cannot be applied
// is reported in the status instead, and plain HTTP keeps working.
func (m *Manager) Start(h http.Handler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handler = h
	m.sniff = newSniffServer(m)
	if !m.enabled {
		// A CA from an earlier run is opened even with HTTPS off, so the
		// plain port can still answer browsers holding an HSTS policy (see
		// SniffListener). Failing here only loses that.
		if fileExists(filepath.Join(m.opts.DataDir, "tls", "ca.pem")) {
			if err := m.openCALocked(); err != nil {
				m.opts.Logf("[netpulse] TLS: CA not opened: %v", err)
			}
		}
		return nil
	}
	if err := m.startLocked(); err != nil {
		m.enabled = false
		m.mode.Store(Full)
		m.lastErr = err.Error()
		if m.opts.EnvEnabled != nil && *m.opts.EnvEnabled {
			return err
		}
		m.opts.Logf("[netpulse] HTTPS could not start, plain HTTP only: %v", err)
	}
	return nil
}

func (m *Manager) openCALocked() error {
	if m.ca != nil {
		return nil
	}
	copts := tlscert.CAOptions{
		Dir:        filepath.Join(m.opts.DataDir, "tls"),
		Names:      m.opts.Names,
		PublicHost: urlHost(m.opts.PublicURL),
		Logf:       m.opts.Logf,
	}
	if m.opts.CAOptions != nil {
		copts = m.opts.CAOptions(copts)
	}
	ca, err := tlscert.OpenCA(copts)
	if err != nil {
		return err
	}
	m.ca = ca
	m.caLoaded.Store(ca)
	// Renewal runs for as long as the CA is loaded - with HTTPS off too,
	// since the plain port's TLS side still serves the leaf.
	m.caStop = make(chan struct{})
	go ca.Run(m.caStop)
	m.opts.Logf("[netpulse] TLS: root certificate SHA-256 %s", ca.RootSHA256())
	m.opts.Logf("[netpulse] TLS: agents pin %s", ca.Fingerprint())
	return nil
}

func (m *Manager) startLocked() error {
	if err := m.openCALocked(); err != nil {
		return err
	}
	addr := fmt.Sprintf(":%d", m.opts.Port)
	// Turning HTTPS off and on again quickly can find the previous listener
	// still closing (stopLocked shuts it down in the background).
	var ln net.Listener
	var err error
	for i := 0; i < 20; i++ {
		if ln, err = net.Listen("tcp", addr); err == nil || !errors.Is(err, syscall.EADDRINUSE) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		return fmt.Errorf("HTTPS listener on %s: %w", addr, err)
	}
	srv := m.opts.NewServer(addr, m.handler)
	srv.TLSConfig = m.ca.TLSConfig()
	m.srv = srv
	go func() {
		if err := srv.ServeTLS(ln, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			m.opts.Logf("[netpulse] HTTPS listener stopped: %v", err)
		}
	}()
	m.lastErr = ""
	m.opts.Logf("[netpulse] HTTPS on :%d (private CA)", m.opts.Port)
	return nil
}

func (m *Manager) stopLocked() {
	if srv := m.srv; srv != nil {
		m.srv = nil
		// Shutdown closes the listener at once - the port is free to be
		// opened again - and lets requests in flight finish, including the
		// one that turned HTTPS off, whose answer would otherwise be lost.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := srv.Shutdown(ctx); err != nil {
				_ = srv.Close()
			}
		}()
	}
}

// Close stops the TLS listeners and the leaf's renewal.
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
	if m.caStop != nil {
		close(m.caStop)
		m.caStop = nil
	}
	if m.sniff != nil {
		m.sniff.close()
	}
}

// ErrLocked is returned for a change to a setting the environment fixes.
var ErrLocked = errors.New("this setting is fixed in the server's environment")

// ErrNotFull is returned for turning HTTPS off in a stricter mode.
var ErrNotFull = errors.New("choose \"Everything\" first: browsers told to always use HTTPS in a stricter mode " +
	"need it answering for a while to be told to stop")

// SetEnabled turns HTTPS on or off, live. Turning it off also returns plain
// HTTP to full service: without HTTPS there is nowhere to redirect to.
func (m *Manager) SetEnabled(on bool, by string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.opts.Unavailable != "" || m.opts.EnvEnabled != nil {
		return ErrLocked
	}
	if on == m.enabled {
		return nil
	}
	if on {
		if err := m.startLocked(); err != nil {
			m.lastErr = err.Error()
			return err
		}
	} else {
		if m.opts.EnvMode != "" && m.opts.EnvMode != Full {
			return fmt.Errorf("%w: NETPULSE_HTTP_MODE=%s needs HTTPS", ErrLocked, m.opts.EnvMode)
		}
		// Only from full: browsers that got an HSTS policy in a stricter
		// mode need a while of full, where HTTPS answers with max-age=0,
		// to be told to drop it.
		if m.Mode() != Full {
			return ErrNotFull
		}
		m.stopLocked()
		m.pending = nil
	}
	m.enabled = on
	m.saveEnabled(on)
	m.opts.Logf("[netpulse] HTTPS turned %s by %s", onOff(on), by)
	return nil
}

// RequestMode changes what plain HTTP may do. Going back to full takes effect
// at once; a stricter mode is staged and returns the code to confirm it with
// from an HTTPS page (Confirm) before ConfirmWindow runs out.
func (m *Manager) RequestMode(mode Mode, by string) (code string, expires time.Time, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.opts.Unavailable != "" || m.opts.EnvMode != "" {
		return "", time.Time{}, ErrLocked
	}
	if !m.enabled && mode != Full {
		return "", time.Time{}, errors.New("turn HTTPS on first")
	}
	if mode == Full {
		m.pending = nil
		m.applyModeLocked(Full, by)
		return "", time.Time{}, nil
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", time.Time{}, err
	}
	p := &pendingMode{mode: mode, code: hex.EncodeToString(b), expires: m.opts.Now().Add(ConfirmWindow), by: by}
	m.pending = p
	m.opts.Logf("[netpulse] HTTP mode %s staged by %s; waiting for confirmation over HTTPS", mode, by)
	return p.code, p.expires, nil
}

// ErrConfirm is returned for a confirmation that does not apply.
var ErrConfirm = errors.New("no such change is waiting for confirmation; it may have expired")

// Confirm applies a staged mode change. It must arrive over TLS: the point
// is proving HTTPS works for the admin before plain HTTP is restricted.
func (m *Manager) Confirm(code string, overTLS bool, by string) (Mode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !overTLS {
		return "", errors.New("confirm from the HTTPS address, not over plain HTTP")
	}
	p := m.pending
	if p == nil || m.opts.Now().After(p.expires) ||
		subtle.ConstantTimeCompare([]byte(code), []byte(p.code)) != 1 {
		return "", ErrConfirm
	}
	m.pending = nil
	m.applyModeLocked(p.mode, by)
	return p.mode, nil
}

func (m *Manager) applyModeLocked(mode Mode, by string) {
	old := m.Mode()
	m.mode.Store(mode)
	m.saveMode(mode)
	if old != mode {
		m.opts.Logf("[netpulse] HTTP mode %s -> %s by %s", old, mode, by)
	}
}

// Mode is what plain HTTP may do now.
func (m *Manager) Mode() Mode { return m.mode.Load().(Mode) }

// Enabled reports whether the private-CA HTTPS listener is on.
func (m *Manager) Enabled() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enabled
}

// Fingerprint is the pin agents should use: the root's SPKI, or "" while
// HTTPS is off.
func (m *Manager) Fingerprint() string {
	if !m.Enabled() {
		return ""
	}
	if ca := m.caLoaded.Load(); ca != nil {
		return ca.Fingerprint()
	}
	return ""
}

// CA is the private CA once it has been opened, for serving its root.
func (m *Manager) CA() *tlscert.CA { return m.caLoaded.Load() }

// Status is what the settings page shows.
type Status struct {
	Available     bool      `json:"available"`
	Unavailable   string    `json:"unavailable,omitempty"`
	Enabled       bool      `json:"enabled"`
	EnabledLocked bool      `json:"enabledLocked"`
	Mode          Mode      `json:"mode"`
	ModeLocked    bool      `json:"modeLocked"`
	Port          int       `json:"port"`
	RootSHA256    string    `json:"rootSha256,omitempty"`
	Fingerprint   string    `json:"fingerprint,omitempty"`
	Names         []string  `json:"names,omitempty"`
	Uncovered     []string  `json:"uncovered,omitempty"`
	ValidUntil    time.Time `json:"validUntil,omitzero"`
	Pending       Mode      `json:"pending,omitempty"`
	PendingUntil  time.Time `json:"pendingUntil,omitzero"`
	Error         string    `json:"error,omitempty"`
}

// Status reports the current state.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := Status{
		Available:     m.opts.Unavailable == "",
		Unavailable:   m.opts.Unavailable,
		Enabled:       m.enabled,
		EnabledLocked: m.opts.Unavailable != "" || m.opts.EnvEnabled != nil,
		Mode:          m.Mode(),
		ModeLocked:    m.opts.Unavailable != "" || m.opts.EnvMode != "",
		Port:          m.opts.Port,
		Error:         m.lastErr,
	}
	if p := m.pending; p != nil && m.opts.Now().Before(p.expires) {
		st.Pending, st.PendingUntil = p.mode, p.expires
	}
	if m.ca != nil && m.enabled {
		st.RootSHA256 = m.ca.RootSHA256()
		st.Fingerprint = m.ca.Fingerprint()
		st.Uncovered = m.ca.Uncovered()
		if leaf := m.ca.Leaf(); leaf != nil {
			st.Names = append(slices.Clone(leaf.DNSNames), ipStrings(leaf.IPAddresses)...)
			st.ValidUntil = leaf.NotAfter
		}
	}
	return st
}

// Renew re-reads the host's addresses and names now, instead of at the next
// periodic check, and issues a new certificate if they changed. It reports
// whether it issued one. The root, and so every pin, stays the same.
func (m *Manager) Renew(by string) (bool, error) {
	m.mu.Lock()
	ca, on := m.ca, m.enabled
	m.mu.Unlock()
	if ca == nil || !on {
		return false, errors.New("turn HTTPS on first")
	}
	issued, err := ca.Refresh()
	if err == nil {
		m.opts.Logf("[netpulse] TLS: certificate checked by %s (new one issued: %v)", by, issued)
	}
	return issued, err
}

// HSTS is the Strict-Transport-Security value for a response, "" for none.
//
// While plain HTTP still serves everything (full), HTTPS responses send
// max-age=0: that clears any HSTS a browser kept from a stricter mode, so
// going back to full really does make plain HTTP usable again. During
// migrate the policy is short, so undoing it is cheap; only redirect, which
// is meant to stay, gets a year. includeSubDomains is never sent.
func (m *Manager) HSTS(secure bool) string {
	if !secure || m.CA() == nil {
		return ""
	}
	if !m.Enabled() {
		return "max-age=0" // served through the plain port's TLS side
	}
	switch m.Mode() {
	case Migrate:
		return "max-age=86400"
	case Redirect:
		return "max-age=31536000"
	default:
		return "max-age=0"
	}
}

// RedirectTarget is where a plain HTTP request is sent: the same path and
// query, on the HTTPS port, at a host this server's certificate names. The
// request's Host header is only used when the certificate names it, so a
// forged Host cannot turn this into a redirect elsewhere.
func (m *Manager) RedirectTarget(r *http.Request) (string, bool) {
	ca := m.caLoaded.Load()
	if ca == nil {
		return "", false
	}
	leaf := ca.Leaf()
	if leaf == nil {
		return "", false
	}
	named := func(h string) bool { return h != "" && leafNames(leaf.DNSNames, leaf.IPAddresses, h) }
	// In order: the name the browser used; the address the request arrived
	// on; the public URL's host; any address the certificate names.
	host := strings.ToLower(hostOnly(r.Host))
	if !named(host) {
		host = ""
		if a, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
			if h := hostOnly(a.String()); named(h) {
				host = h
			}
		}
		if h := urlHost(m.opts.PublicURL); host == "" && named(h) {
			host = h
		}
		if host == "" {
			host = firstReachable(leaf.IPAddresses)
		}
	}
	if host == "" {
		return "", false
	}
	u := url.URL{
		Scheme:   "https",
		Host:     net.JoinHostPort(host, fmt.Sprint(m.opts.Port)),
		Path:     r.URL.Path,
		RawQuery: r.URL.RawQuery,
	}
	return u.String(), true
}

// --- persistence ---

func (m *Manager) savedEnabled() bool {
	return m.kvGet(kvEnabled) == "1"
}

func (m *Manager) savedMode() Mode {
	if mode, err := ParseMode(m.kvGet(kvMode)); err == nil {
		return mode
	}
	return Full
}

func (m *Manager) saveEnabled(on bool) {
	v := "0"
	if on {
		v = "1"
	}
	m.kvSet(kvEnabled, v)
}

func (m *Manager) saveMode(mode Mode) {
	if m.opts.EnvMode == "" {
		m.kvSet(kvMode, string(mode))
	}
}

func (m *Manager) kvGet(key string) string {
	if m.opts.DB == nil {
		return ""
	}
	var v string
	_ = m.opts.DB.QueryRow("SELECT value FROM kv WHERE key = ?", key).Scan(&v)
	return v
}

func (m *Manager) kvSet(key, value string) {
	if m.opts.DB == nil {
		return
	}
	if _, err := m.opts.DB.Exec(
		"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value",
		key, value); err != nil {
		m.opts.Logf("[netpulse] HTTPS: could not save %s: %v", key, err)
	}
}

// --- helpers ---

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func urlHost(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func hostOnly(hostport string) string {
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		return h
	}
	return hostport
}

func leafNames(dns []string, ips []net.IP, host string) bool {
	if ip := net.ParseIP(host); ip != nil {
		return slices.ContainsFunc(ips, ip.Equal)
	}
	for _, d := range dns {
		if d == host {
			return true
		}
	}
	return false
}

// firstReachable picks an address other devices can use: IPv4, not
// loopback, and outside 172.16/12 when possible - where container bridges
// usually live, which nothing else on the network can reach.
func firstReachable(ips []net.IP) string {
	_, bridges, _ := net.ParseCIDR("172.16.0.0/12")
	fallback := ""
	for _, ip := range ips {
		if ip.IsLoopback() || ip.To4() == nil {
			continue
		}
		if !bridges.Contains(ip) {
			return ip.String()
		}
		if fallback == "" {
			fallback = ip.String()
		}
	}
	return fallback
}

func ipStrings(ips []net.IP) []string {
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	return out
}

// Available reports whether this server's HTTPS is managed here at all.
func (m *Manager) Available() bool { return m.opts.Unavailable == "" }

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
