package tlsmode

import (
	"net"
	"strconv"
	"strings"

	"github.com/gnacho/netpulse/server-go/internal/reinstall"
)

// HTTPSURL is base moved to this server's HTTPS port - same host if the
// certificate names it, else one it does - or "" while HTTPS is off. base is
// whatever address routers were given (NETPULSE_PUBLIC_URL, or the one the
// admin browsed to).
func (m *Manager) HTTPSURL(base string) string {
	leaf := m.Leaf()
	if leaf == nil {
		return ""
	}
	host := urlHost(base)
	if host == "" {
		host = hostOnly(base)
	}
	// A loopback address names the server only to itself; the router, or
	// any other device given this URL, would dial its own loopback.
	if !leafNames(leaf.DNSNames, leaf.IPAddresses, host) || isLoopbackHost(host) {
		host = firstReachable(leaf.IPAddresses)
	}
	if host == "" {
		return ""
	}
	return "https://" + net.JoinHostPort(host, strconv.Itoa(m.currentPort()))
}

// AgentTrust is how an agent (re)installed with base as its server should
// reach this server. With HTTPS on, it moves to the HTTPS address, pins the
// root and verifies its downloads against it. Otherwise an https base keeps
// working with legacyFP (the pin of a self-signed or user certificate), and
// a plain one is left as it is.
func AgentTrust(m *Manager, legacyFP func() string, base string) reinstall.Trust {
	if m != nil {
		if url := m.HTTPSURL(base); url != "" {
			if m.CertSource() == "custom" {
				// #1756: a certificate from disk has no root of ours to
				// hand out, so the agent pins the leaf itself.
				return reinstall.Trust{ServerURL: url, ServerFP: m.Fingerprint()}
			}
			ca := m.CA()
			return reinstall.Trust{ServerURL: url, ServerFP: ca.Fingerprint(), CAPEM: ca.RootPEM()}
		}
	}
	if strings.HasPrefix(base, "https://") && legacyFP != nil {
		return reinstall.Trust{ServerFP: legacyFP()}
	}
	return reinstall.Trust{}
}

func isLoopbackHost(host string) bool {
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return host == "localhost"
}
