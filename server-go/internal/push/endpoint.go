package push

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const maxRedirects = 5

// ValidateEndpoint accepts only HTTPS endpoints whose host is public. It is
// used when a subscription is created and again before every send, so rows
// stored by older versions cannot reach the LAN either.
func ValidateEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("endpoint invalido: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return errors.New("el endpoint debe usar https")
	}
	if u.User != nil {
		return errors.New("el endpoint no puede incluir credenciales")
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("el endpoint no tiene host")
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return errors.New("el endpoint tiene un puerto invalido")
		}
	}
	if ip := net.ParseIP(host); ip != nil {
		if blockedIP(ip) {
			return errors.New("el endpoint apunta a una direccion no publica")
		}
		return nil
	}
	name := strings.ToLower(strings.TrimSuffix(host, "."))
	if name == "localhost" || strings.HasSuffix(name, ".localhost") {
		return errors.New("el endpoint apunta a localhost")
	}
	return nil
}

func blockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		switch {
		case v4[0] == 0:
			return true
		case v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127:
			return true
		case v4[0] == 192 && v4[1] == 0 && v4[2] == 0:
			return true
		case v4[0] == 192 && v4[1] == 0 && v4[2] == 2:
			return true
		case v4[0] == 198 && (v4[1] == 18 || v4[1] == 19):
			return true
		case v4[0] == 198 && v4[1] == 51 && v4[2] == 100:
			return true
		case v4[0] == 203 && v4[1] == 0 && v4[2] == 113:
			return true
		case v4[0] >= 240:
			return true
		}
		return false
	}
	if len(ip) == net.IPv6len {
		// 2001:db8::/32 (documentation) and 64:ff9b::/96 / 64:ff9b:1::/48
		// (NAT64, which can map private IPv4 addresses).
		if ip[0] == 0x20 && ip[1] == 0x01 && ip[2] == 0x0d && ip[3] == 0xb8 {
			return true
		}
		if ip[0] == 0x00 && ip[1] == 0x64 && ip[2] == 0xff && ip[3] == 0x9b {
			return true
		}
	}
	return false
}

func resolvePushHost(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolver %s: %w", host, err)
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, addr := range addrs {
		ips = append(ips, addr.IP)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("resolver %s: sin direcciones", host)
	}
	return ips, nil
}

func safeDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("direccion de push invalida: %w", err)
	}
	ips, err := resolvePushHost(ctx, host)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	var lastErr error
	for _, ip := range ips {
		if blockedIP(ip) {
			continue
		}
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, fmt.Errorf("conectar con %s: %w", host, lastErr)
	}
	return nil, fmt.Errorf("el endpoint %s resuelve a una direccion no publica", host)
}

func newSafeHTTPClient() *http.Client {
	return &http.Client{
		Timeout: sendTimeout,
		Transport: &http.Transport{
			Proxy:               nil,
			DialContext:         safeDialContext,
			ForceAttemptHTTP2:   true,
			TLSHandshakeTimeout: 10 * time.Second,
			MaxIdleConns:        20,
			IdleConnTimeout:     90 * time.Second,
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("demasiadas redirecciones")
			}
			return ValidateEndpoint(req.URL.String())
		},
	}
}
