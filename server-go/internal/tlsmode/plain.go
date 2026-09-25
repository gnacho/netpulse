package tlsmode

import (
	"bufio"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/auth"
)

// PlainHandler wraps the handler served on the plain HTTP port with what the
// current mode allows there. Requests that arrived over TLS - on that port,
// through the sniffing listener - are not restricted.
func (m *Manager) PlainHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Secure is a TLS connection, or one a trusted TLS-terminating proxy
		// forwarded as https - the same rule the session cookie follows. A
		// client that fakes the proxy's header only sends its own
		// credentials in clear.
		if auth.IsSecureRequest(r) || !m.Enabled() {
			next.ServeHTTP(w, r)
			return
		}
		switch m.Mode() {
		case Migrate:
			if auth.IsAgentRequest(r) || isPublicPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			dropPlainSession(w)
			if m.redirectBrowser(w, r) {
				return
			}
			// Refused, not redirected: a redirect cannot protect a password
			// or token that has already been sent in clear.
			auth.WriteError(w, http.StatusForbidden, "use_https",
				"this server only accepts sign-ins and API calls over HTTPS")
		case Redirect:
			if isPublicPath(r.URL.Path) && r.URL.Path != "/api/health" && r.URL.Path != "/health" ||
				isHealthPath(r.URL.Path) && fromLoopback(r) {
				next.ServeHTTP(w, r)
				return
			}
			dropPlainSession(w)
			if m.redirectBrowser(w, r) {
				return
			}
			w.Header().Set("Upgrade", "TLS/1.2, HTTP/1.1")
			w.Header().Set("Connection", "Upgrade")
			auth.WriteError(w, http.StatusUpgradeRequired, "use_https",
				"this server only answers over HTTPS")
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// dropPlainSession deletes the session cookie of a sign-in made over plain
// HTTP. A browser sends it with every http:// request - an old bookmark,
// access by address, which HSTS never covers - in clear, until it expires;
// once plain HTTP is no longer for people, it is only a liability.
func dropPlainSession(w http.ResponseWriter) {
	w.Header().Add("Set-Cookie", auth.ClearSessionCookie)
}

// redirectBrowser sends a page load to the HTTPS address. API calls are not
// redirected: a client that would follow one has usually sent its
// credentials already.
func (m *Manager) redirectBrowser(w http.ResponseWriter, r *http.Request) bool {
	if (r.Method != http.MethodGet && r.Method != http.MethodHead) || strings.HasPrefix(r.URL.Path, "/api/") {
		return false
	}
	target, ok := m.RedirectTarget(r)
	if !ok {
		return false
	}
	http.Redirect(w, r, target, http.StatusPermanentRedirect)
	return true
}

// isPublicPath is what carries no credential and is safe over plain HTTP: the
// health checks, and the root certificate and fingerprint a device needs
// before it can trust HTTPS at all.
func isPublicPath(p string) bool {
	return isHealthPath(p) || p == "/fingerprint" || p == "/netpulse-ca.crt" || p == "/netpulse-ca.pem"
}

func isHealthPath(p string) bool { return p == "/api/health" || p == "/health" }

// fromLoopback looks at the connection, never at forwarded headers.
func fromLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// --- one port, both protocols ---

// peekTimeout bounds how long a new connection may take to send its first
// byte before it is dropped, so a client that sends nothing cannot hold a
// goroutine forever.
const peekTimeout = 10 * time.Second

// SniffListener wraps the plain port's listener. Once the CA exists, a
// connection that opens with a TLS handshake is served as HTTPS on the same
// port: a browser holding an HSTS policy for the server's name upgrades
// http://name:PORT to https://name:PORT, which would otherwise fail against
// a plain listener and never see the redirect. Before HTTPS was ever turned
// on, connections are passed through untouched, as before.
func (m *Manager) SniffListener(ln net.Listener) net.Listener {
	l := &sniffListener{
		Listener: ln,
		m:        m,
		plain:    make(chan net.Conn),
		errc:     make(chan error, 1),
		done:     make(chan struct{}),
	}
	go l.acceptLoop()
	return l
}

type sniffListener struct {
	net.Listener
	m     *Manager
	plain chan net.Conn
	errc  chan error
	done  chan struct{}
	once  sync.Once
}

func (l *sniffListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.plain:
		return c, nil
	case err := <-l.errc:
		return nil, err
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *sniffListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return l.Listener.Close()
}

func (l *sniffListener) acceptLoop() {
	var delay time.Duration
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			// Only a closed listener ends the loop. Anything else - running
			// out of file descriptors under a burst of connections, say - is
			// waited out: handing it to http.Server would make it stop
			// serving for good while the process, and the port, stay up.
			if errors.Is(err, net.ErrClosed) {
				select {
				case l.errc <- err:
				case <-l.done:
				}
				return
			}
			if delay == 0 {
				delay = 5 * time.Millisecond
			} else if delay *= 2; delay > time.Second {
				delay = time.Second
			}
			select {
			case <-time.After(delay):
			case <-l.done:
				return
			}
			continue
		}
		delay = 0
		go l.route(c)
	}
}

func (l *sniffListener) route(c net.Conn) {
	// Once the CA exists the port answers TLS in every mode, and with HTTPS
	// off: a browser that got an HSTS policy for the name while a stricter
	// mode was on keeps rewriting http://name:PORT to https://name:PORT for
	// as long as the policy lasts. Served here, it gets max-age=0 and the
	// page; refused, the name would be unreachable from it until then.
	if l.m.CA() == nil {
		l.deliver(c)
		return
	}
	_ = c.SetReadDeadline(time.Now().Add(peekTimeout))
	br := bufio.NewReader(c)
	first, err := br.Peek(1)
	_ = c.SetReadDeadline(time.Time{})
	if err != nil {
		c.Close()
		return
	}
	pc := &peekedConn{Conn: c, r: br}
	if first[0] == 0x16 { // a TLS handshake record
		l.m.sniff.serve(pc)
		return
	}
	l.deliver(pc)
}

func (l *sniffListener) deliver(c net.Conn) {
	select {
	case l.plain <- c:
	case <-l.done:
		c.Close()
	}
}

// peekedConn gives back the byte read to tell the protocols apart.
type peekedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *peekedConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// sniffServer serves the TLS connections found on the plain port.
type sniffServer struct {
	m     *Manager
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func newSniffServer(m *Manager) *sniffServer {
	s := &sniffServer{m: m, conns: make(chan net.Conn), done: make(chan struct{})}
	srv := m.opts.NewServer("", m.handler)
	go func() { _ = srv.Serve(chanListener{s}) }()
	go func() {
		<-s.done
		_ = srv.Close()
	}()
	return s
}

func (s *sniffServer) serve(c net.Conn) {
	ca := s.m.CA()
	if ca == nil {
		c.Close()
		return
	}
	select {
	case s.conns <- tls.Server(c, ca.TLSConfig()):
	case <-s.done:
		c.Close()
	}
}

func (s *sniffServer) close() { s.once.Do(func() { close(s.done) }) }

// chanListener hands the sniffing server its connections.
type chanListener struct{ s *sniffServer }

func (l chanListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.s.conns:
		return c, nil
	case <-l.s.done:
		return nil, net.ErrClosed
	}
}
func (chanListener) Close() error   { return nil }
func (chanListener) Addr() net.Addr { return &net.TCPAddr{} }
