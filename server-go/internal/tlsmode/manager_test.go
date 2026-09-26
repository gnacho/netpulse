package tlsmode

// FORK: tests for the HTTPS manager. Addresses are from the documentation
// and private ranges, names invented.

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	agentruntime "github.com/gnacho/netpulse/agent/runtime"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/tlscert"
)

type rig struct {
	t    *testing.T
	m    *Manager
	db   *db.DB
	dir  string
	now  time.Time
	port int
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func newRig(t *testing.T, tweak func(*Options)) *rig {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	r := &rig{t: t, db: d, dir: dir, now: time.Now(), port: freePort(t)}
	opts := Options{
		DB: d, DataDir: dir, Port: r.port,
		NewServer: func(addr string, h http.Handler) *http.Server {
			return &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second}
		},
		Now: func() time.Time { return r.now },
		CAOptions: func(o tlscert.CAOptions) tlscert.CAOptions {
			o.Addrs = func() ([]net.IP, error) { return []net.IP{net.ParseIP("192.168.50.2")}, nil }
			o.Hostname = func() (string, error) { return "monitor-box", nil }
			o.SearchDomains = func() []string { return []string{"example.lan"} }
			return o
		},
	}
	if tweak != nil {
		tweak(&opts)
	}
	r.m = New(opts)
	// As the real API does, the handler sets the HSTS policy the mode calls
	// for; the plain port's TLS side serves this same handler.
	if err := r.m.Start(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if v := r.m.HSTS(req.TLS != nil); v != "" {
			w.Header().Set("Strict-Transport-Security", v)
		}
		fmt.Fprintf(w, "tls=%v", req.TLS != nil)
	})); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(r.m.Close)
	return r
}

// get fetches path from the TLS listener, pinning the root as an agent would.
func (r *rig) getTLS(path string) (string, error) {
	tr, err := agentruntime.ServerTransport(fmt.Sprintf("https://127.0.0.1:%d", r.port), r.m.Fingerprint())
	if err != nil {
		return "", err
	}
	var res *http.Response
	for i := 0; i < 50; i++ { // the listener starts in a goroutine
		res, err = (&http.Client{Transport: tr, Timeout: 2 * time.Second}).Get(fmt.Sprintf("https://127.0.0.1:%d%s", r.port, path))
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return string(b), nil
}

func TestEnablingStartsHTTPSAndDisablingStopsIt(t *testing.T) {
	r := newRig(t, nil)
	if r.m.Fingerprint() != "" {
		t.Fatal("a server with HTTPS off hands out a pin")
	}
	if err := r.m.SetEnabled(true, "admin"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	body, err := r.getTLS("/")
	if err != nil || body != "tls=true" {
		t.Fatalf("an agent pinning the root could not reach HTTPS: %q %v", body, err)
	}
	if r.m.Mode() != Full {
		t.Fatal("enabling HTTPS changed what plain HTTP may do")
	}
	if err := r.m.SetEnabled(false, "admin"); err != nil {
		t.Fatal(err)
	}
	closed := false
	for i := 0; i < 50 && !closed; i++ { // closed from a goroutine, see stopLocked
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", r.port), time.Second)
		if err != nil {
			closed = true
			break
		}
		c.Close()
		time.Sleep(20 * time.Millisecond)
	}
	if !closed {
		t.Fatal("the HTTPS listener is still open after turning HTTPS off")
	}
	// And it can be turned on again at once, on the same port.
	if err := r.m.SetEnabled(true, "admin"); err != nil {
		t.Fatalf("re-enable: %v", err)
	}
}

func TestSettingsSurviveARestart(t *testing.T) {
	r := newRig(t, nil)
	if err := r.m.SetEnabled(true, "admin"); err != nil {
		t.Fatal(err)
	}
	code, _, err := r.m.RequestMode(Migrate, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.Confirm(code, true, "admin"); err != nil {
		t.Fatal(err)
	}
	r.m.Close()
	again := New(r.m.opts)
	if !again.Enabled() || again.Mode() != Migrate {
		t.Fatalf("after a restart enabled=%v mode=%s, want on and migrate", again.Enabled(), again.Mode())
	}
}

func TestTheEnvironmentWins(t *testing.T) {
	off := false
	r := newRig(t, func(o *Options) { o.EnvEnabled = &off; o.EnvMode = Full })
	if err := r.m.SetEnabled(true, "admin"); !errors.Is(err, ErrLocked) {
		t.Fatalf("enabling a setting fixed in the environment: %v, want ErrLocked", err)
	}
	if _, _, err := r.m.RequestMode(Migrate, "admin"); !errors.Is(err, ErrLocked) {
		t.Fatalf("changing a mode fixed in the environment: %v, want ErrLocked", err)
	}
	st := r.m.Status()
	if !st.EnabledLocked || !st.ModeLocked {
		t.Fatal("status does not show the settings as locked")
	}
}

func TestAStricterModeWaitsForConfirmationOverHTTPS(t *testing.T) {
	r := newRig(t, nil)
	if _, _, err := r.m.RequestMode(Migrate, "admin"); err == nil {
		t.Fatal("a stricter mode was accepted with HTTPS off")
	}
	if err := r.m.SetEnabled(true, "admin"); err != nil {
		t.Fatal(err)
	}
	code, expires, err := r.m.RequestMode(Redirect, "admin")
	if err != nil || code == "" {
		t.Fatalf("RequestMode: %q %v", code, err)
	}
	if r.m.Mode() != Full {
		t.Fatal("the mode changed before it was confirmed")
	}
	if !expires.Equal(r.now.Add(ConfirmWindow)) {
		t.Fatalf("expires %v, want %v", expires, r.now.Add(ConfirmWindow))
	}
	if _, err := r.m.Confirm(code, false, "admin"); err == nil {
		t.Fatal("confirmed over plain HTTP")
	}
	wrong := "0" + code[1:]
	if code[0] == '0' {
		wrong = "1" + code[1:]
	}
	if _, err := r.m.Confirm(wrong, true, "admin"); !errors.Is(err, ErrConfirm) {
		t.Fatalf("a wrong code: %v, want ErrConfirm", err)
	}
	if mode, err := r.m.Confirm(code, true, "admin"); err != nil || mode != Redirect || r.m.Mode() != Redirect {
		t.Fatalf("confirm: %s %v, mode now %s", mode, err, r.m.Mode())
	}
	if _, err := r.m.Confirm(code, true, "admin"); !errors.Is(err, ErrConfirm) {
		t.Fatal("a code was accepted twice")
	}
}

func TestAnUnconfirmedChangeLapses(t *testing.T) {
	r := newRig(t, nil)
	if err := r.m.SetEnabled(true, "admin"); err != nil {
		t.Fatal(err)
	}
	code, _, _ := r.m.RequestMode(Migrate, "admin")
	r.now = r.now.Add(ConfirmWindow + time.Second)
	if _, err := r.m.Confirm(code, true, "admin"); !errors.Is(err, ErrConfirm) {
		t.Fatalf("a lapsed change was applied: %v", err)
	}
	if r.m.Mode() != Full || r.m.Status().Pending != "" {
		t.Fatal("a lapsed change left something behind")
	}
}

// Going back to full is always immediate: it is the way out.
func TestFullTakesEffectAtOnce(t *testing.T) {
	r := newRig(t, nil)
	_ = r.m.SetEnabled(true, "admin")
	code, _, _ := r.m.RequestMode(Redirect, "admin")
	_, _ = r.m.Confirm(code, true, "admin")
	if err := r.m.SetEnabled(false, "admin"); !errors.Is(err, ErrNotFull) {
		t.Fatalf("turning HTTPS off in redirect: %v, want ErrNotFull", err)
	}
	if _, _, err := r.m.RequestMode(Full, "admin"); err != nil || r.m.Mode() != Full {
		t.Fatalf("back to full: %v, mode %s", err, r.m.Mode())
	}
	if err := r.m.SetEnabled(false, "admin"); err != nil || r.m.Mode() != Full {
		t.Fatalf("turning HTTPS off from full: %v", err)
	}
}

func setMode(t *testing.T, r *rig, mode Mode) {
	t.Helper()
	if err := r.m.SetEnabled(true, "admin"); err != nil {
		t.Fatal(err)
	}
	if mode == Full {
		return
	}
	code, _, err := r.m.RequestMode(mode, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.Confirm(code, true, "admin"); err != nil {
		t.Fatal(err)
	}
}

func TestWhatPlainHTTPMayDo(t *testing.T) {
	type want struct {
		status   int
		location string // prefix, for redirects
	}
	cases := []struct {
		method, path, remote string
		full, migrate, redir want
	}{
		{"POST", "/api/auth/login", "192.168.50.9:5000", want{200, ""}, want{403, ""}, want{426, ""}},
		{"GET", "/api/overview", "192.168.50.9:5000", want{200, ""}, want{403, ""}, want{426, ""}},
		{"POST", "/api/ingest/agent", "192.168.50.9:5000", want{200, ""}, want{200, ""}, want{426, ""}},
		{"GET", "/api/agents/gw/stream", "192.168.50.9:5000", want{200, ""}, want{200, ""}, want{426, ""}},
		{"POST", "/api/config-backup", "192.168.50.9:5000", want{200, ""}, want{200, ""}, want{426, ""}},
		{"GET", "/devices?q=x", "192.168.50.9:5000", want{200, ""}, want{308, "https://192.168.50.2:"}, want{308, "https://192.168.50.2:"}},
		{"POST", "/devices", "192.168.50.9:5000", want{200, ""}, want{403, ""}, want{426, ""}},
		{"GET", "/netpulse-ca.crt", "192.168.50.9:5000", want{200, ""}, want{200, ""}, want{200, ""}},
		{"GET", "/api/health", "192.168.50.9:5000", want{200, ""}, want{200, ""}, want{426, ""}},
		{"GET", "/api/health", "127.0.0.1:5000", want{200, ""}, want{200, ""}, want{200, ""}},
	}
	for _, mode := range []Mode{Full, Migrate, Redirect} {
		t.Run(string(mode), func(t *testing.T) {
			r := newRig(t, nil)
			setMode(t, r, mode)
			h := r.m.PlainHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
			for _, c := range cases {
				w := map[Mode]want{Full: c.full, Migrate: c.migrate, Redirect: c.redir}[mode]
				req := httptest.NewRequest(c.method, "http://192.168.50.2:3000"+c.path, nil)
				req.RemoteAddr = c.remote
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if rec.Code != w.status {
					t.Errorf("%s %s from %s: %d, want %d", c.method, c.path, c.remote, rec.Code, w.status)
				}
				if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, w.location) {
					t.Errorf("%s %s: Location %q, want %q...", c.method, c.path, loc, w.location)
				}
			}
		})
	}
}

// The redirect never takes a host from the request unless the certificate
// names it: a forged Host header cannot send the browser elsewhere.
func TestTheRedirectOnlyGoesToNamesTheCertificateCarries(t *testing.T) {
	r := newRig(t, nil)
	setMode(t, r, Migrate)
	for host, want := range map[string]string{
		"monitor-box.example.lan:3000": "https://monitor-box.example.lan:",
		"192.168.50.2:3000":            "https://192.168.50.2:",
		"attacker.example.com:3000":    "https://192.168.50.2:",
		"198.51.100.1:3000":            "https://192.168.50.2:",
	} {
		req := httptest.NewRequest("GET", "/settings?tab=https", nil)
		req.Host = host
		got, ok := r.m.RedirectTarget(req)
		if !ok || !strings.HasPrefix(got, want) || !strings.HasSuffix(got, "/settings?tab=https") {
			t.Errorf("Host %s: redirected to %q, want %s...", host, got, want)
		}
	}
}

func TestHSTSFollowsTheMode(t *testing.T) {
	r := newRig(t, nil)
	if got := r.m.HSTS(true); got != "" {
		t.Fatalf("HTTPS never on: HSTS %q", got)
	}
	setMode(t, r, Full)
	if got := r.m.HSTS(true); got != "max-age=0" {
		t.Fatalf("full: %q, want max-age=0 (clears an earlier policy)", got)
	}
	if got := r.m.HSTS(false); got != "" {
		t.Fatalf("plain response: %q", got)
	}
	code, _, _ := r.m.RequestMode(Migrate, "admin")
	_, _ = r.m.Confirm(code, true, "admin")
	if got := r.m.HSTS(true); got != "max-age=86400" {
		t.Fatalf("migrate: %q", got)
	}
	code, _, _ = r.m.RequestMode(Redirect, "admin")
	_, _ = r.m.Confirm(code, true, "admin")
	if got := r.m.HSTS(true); got != "max-age=31536000" {
		t.Fatalf("redirect: %q", got)
	}
}

// One port, both protocols. Before HTTPS was ever on the plain port is plain
// only, as before. Once the CA exists it also answers TLS - in every mode,
// and with HTTPS turned off again - so a browser holding an HSTS policy for
// the name can still get in, and there be told max-age=0.
func TestThePlainPortAlsoSpeaksTLSOnceTheCAExists(t *testing.T) {
	for _, tc := range []struct {
		name     string
		setup    func(t *testing.T, r *rig)
		wantTLS  bool
		wantHSTS string
	}{
		{"never enabled", func(*testing.T, *rig) {}, false, ""},
		{"full", func(t *testing.T, r *rig) { setMode(t, r, Full) }, true, "max-age=0"},
		{"migrate", func(t *testing.T, r *rig) { setMode(t, r, Migrate) }, true, "max-age=86400"},
		{"turned off again", func(t *testing.T, r *rig) {
			setMode(t, r, Full)
			if err := r.m.SetEnabled(false, "test"); err != nil {
				t.Fatal(err)
			}
		}, true, "max-age=0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, nil)
			tc.setup(t, r)
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			srv := &http.Server{Handler: r.m.PlainHandler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if v := r.m.HSTS(req.TLS != nil); v != "" {
					w.Header().Set("Strict-Transport-Security", v)
				}
				fmt.Fprintf(w, "tls=%v", req.TLS != nil)
			}))}
			go srv.Serve(r.m.SniffListener(ln))
			defer srv.Close()
			addr := ln.Addr().String()

			res, err := http.Get("http://" + addr + "/api/health")
			if err != nil {
				t.Fatal(err)
			}
			b, _ := io.ReadAll(res.Body)
			res.Body.Close()
			if string(b) != "tls=false" {
				t.Fatalf("plain client got %q", b)
			}

			var pool *x509.CertPool
			if ca := r.m.CA(); ca != nil {
				pool = x509.NewCertPool()
				pool.AppendCertsFromPEM(ca.RootPEM())
			}
			c := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{
				TLSClientConfig: &tls.Config{RootCAs: pool},
			}}
			res, err = c.Get("https://" + addr + "/api/health")
			if !tc.wantTLS {
				if err == nil {
					res.Body.Close()
					t.Fatal("the plain port answered TLS before HTTPS was ever on")
				}
				return
			}
			if err != nil {
				t.Fatalf("TLS on the plain port: %v", err)
			}
			b, _ = io.ReadAll(res.Body)
			res.Body.Close()
			if string(b) != "tls=true" {
				t.Fatalf("TLS client got %q", b)
			}
			if got := res.Header.Get("Strict-Transport-Security"); got != tc.wantHSTS {
				t.Fatalf("HSTS %q, want %q", got, tc.wantHSTS)
			}
		})
	}
}

// A client that connects and sends nothing does not hold up the next one.
func TestASilentClientDoesNotBlockTheNext(t *testing.T) {
	r := newRig(t, nil)
	setMode(t, r, Migrate)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: r.m.PlainHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "ok")
	}))}
	go srv.Serve(r.m.SniffListener(ln))
	defer srv.Close()
	silent, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()
	c := &http.Client{Timeout: 2 * time.Second}
	res, err := c.Get("http://" + ln.Addr().String() + "/api/health")
	if err != nil {
		t.Fatalf("a silent connection blocked the next client: %v", err)
	}
	res.Body.Close()
}

func TestTheCALivesInItsOwnDirectory(t *testing.T) {
	r := newRig(t, nil)
	if err := r.m.SetEnabled(true, "admin"); err != nil {
		t.Fatal(err)
	}
	st := r.m.Status()
	if st.RootSHA256 == "" || st.Fingerprint == "" || len(st.Names) == 0 {
		t.Fatalf("status lacks the root: %+v", st)
	}
	if _, err := tlscert.OpenCA(tlscert.CAOptions{Dir: filepath.Join(r.dir, "tls")}); err != nil {
		t.Fatalf("the CA is not in DATA_DIR/tls: %v", err)
	}
}

// flakyListener fails its first Accept the way a process out of file
// descriptors does.
type flakyListener struct {
	net.Listener
	failed atomic.Bool
}

func (l *flakyListener) Accept() (net.Conn, error) {
	if !l.failed.Swap(true) {
		return nil, &net.OpError{Op: "accept", Net: "tcp", Err: syscall.EMFILE}
	}
	return l.Listener.Accept()
}

// One temporary accept error must not stop the plain port for good.
func TestThePlainPortSurvivesATemporaryAcceptError(t *testing.T) {
	r := newRig(t, nil)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "ok") })}
	go srv.Serve(r.m.SniffListener(&flakyListener{Listener: ln}))
	defer srv.Close()
	c := &http.Client{Timeout: 3 * time.Second}
	res, err := c.Get("http://" + ln.Addr().String() + "/")
	if err != nil {
		t.Fatalf("after a temporary accept error the port stopped serving: %v", err)
	}
	res.Body.Close()
}

func TestRedirectHostsTheClientCanReach(t *testing.T) {
	r := newRig(t, nil)
	setMode(t, r, Migrate)
	// Host names are case-insensitive.
	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "MONITOR-BOX.Example.LAN:3000"
	if got, _ := r.m.RedirectTarget(req); !strings.HasPrefix(got, "https://monitor-box.example.lan:") {
		t.Errorf("mixed-case host: %q", got)
	}
	// A name the certificate lacks: the address the request arrived on.
	req = httptest.NewRequest("GET", "/", nil)
	req.Host = "localhost:3000"
	req = req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey,
		&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 3000}))
	if got, _ := r.m.RedirectTarget(req); !strings.HasPrefix(got, "https://127.0.0.1:") {
		t.Errorf("localhost: %q", got)
	}
}

func TestFirstReachableAvoidsLoopbackAndBridges(t *testing.T) {
	ips := []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("172.17.0.1"), net.ParseIP("192.168.50.2")}
	if got := firstReachable(ips); got != "192.168.50.2" {
		t.Fatalf("got %s", got)
	}
	if got := firstReachable(ips[:2]); got != "172.17.0.1" {
		t.Fatalf("with only a bridge, it is still better than nothing: got %s", got)
	}
}

func TestAgentTrust(t *testing.T) {
	r := newRig(t, nil)
	legacy := func() string { return "legacy-pin" }
	if tr := AgentTrust(r.m, legacy, "http://192.168.50.2:3000"); tr.ServerURL != "" || tr.ServerFP != "" || tr.CAPEM != nil {
		t.Fatalf("HTTPS off, plain base: %+v", tr)
	}
	if tr := AgentTrust(r.m, legacy, "https://192.168.50.2:3443"); tr.ServerFP != "legacy-pin" || tr.ServerURL != "" {
		t.Fatalf("HTTPS off, https base keeps its legacy pin: %+v", tr)
	}
	if tr := AgentTrust(nil, legacy, "http://192.168.50.2:3000"); tr.ServerURL != "" {
		t.Fatalf("no manager: %+v", tr)
	}
	_ = r.m.SetEnabled(true, "test")
	want := fmt.Sprintf("https://192.168.50.2:%d", r.port)
	for _, base := range []string{
		"http://192.168.50.2:3000",
		"http://monitor-box.example.lan:3000", // a name the leaf carries...
		"http://127.0.0.1:3000",               // ...and loopback, which it must not hand out
		"http://198.51.100.1:3000",            // an address it does not name
	} {
		tr := AgentTrust(r.m, legacy, base)
		if base == "http://monitor-box.example.lan:3000" {
			if tr.ServerURL != fmt.Sprintf("https://monitor-box.example.lan:%d", r.port) {
				t.Errorf("%s -> %s", base, tr.ServerURL)
			}
		} else if tr.ServerURL != want {
			t.Errorf("%s -> %s, want %s", base, tr.ServerURL, want)
		}
		if tr.ServerFP != r.m.Fingerprint() || len(tr.CAPEM) == 0 {
			t.Errorf("%s: pin or root missing: %+v", base, tr)
		}
	}
}

// In the stricter modes a plain-HTTP answer to a person deletes the plain
// session cookie; an agent's is left alone.
func TestStricterModesDropThePlainSessionCookie(t *testing.T) {
	r := newRig(t, nil)
	setMode(t, r, Migrate)
	h := r.m.PlainHandler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	for path, want := range map[string]bool{"/devices": true, "/api/overview": true, "/api/ingest/agent": false} {
		method := "GET"
		if path == "/api/ingest/agent" {
			method = "POST"
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, "http://192.168.50.2:3000"+path, nil))
		got := strings.Contains(strings.Join(rec.Header().Values("Set-Cookie"), ";"), "session=; Path=/")
		if got != want {
			t.Errorf("%s: cookie deleted = %v, want %v", path, got, want)
		}
	}
}

// Renew picks up a changed address at once, keeps the root, and says which
// addresses the root cannot vouch for.
func TestRenewFollowsAChangedAddress(t *testing.T) {
	addrs := []net.IP{net.ParseIP("192.168.50.2")}
	r := newRig(t, func(o *Options) {
		inner := o.CAOptions
		o.CAOptions = func(c tlscert.CAOptions) tlscert.CAOptions {
			c = inner(c)
			c.Addrs = func() ([]net.IP, error) { return addrs, nil }
			return c
		}
	})
	if _, err := r.m.Renew("test"); err == nil {
		t.Fatal("renewed with HTTPS off")
	}
	_ = r.m.SetEnabled(true, "test")
	pin := r.m.Fingerprint()

	addrs = []net.IP{net.ParseIP("10.20.30.40"), net.ParseIP("203.0.113.7")}
	issued, err := r.m.Renew("test")
	if err != nil || !issued {
		t.Fatalf("Renew after an address change: issued=%v err=%v", issued, err)
	}
	st := r.m.Status()
	if !slices.Contains(st.Names, "10.20.30.40") || slices.Contains(st.Names, "192.168.50.2") {
		t.Fatalf("names after renew: %v", st.Names)
	}
	if !slices.Equal(st.Uncovered, []string{"203.0.113.7"}) {
		t.Fatalf("uncovered: %v", st.Uncovered)
	}
	if r.m.Fingerprint() != pin {
		t.Fatal("renewing changed the root agents pin")
	}
	if issued, _ := r.m.Renew("test"); issued {
		t.Fatal("renewing with nothing changed issued a certificate")
	}
}
