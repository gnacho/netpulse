// Package dnscache resuelve nombres con caché que RESPETA el TTL de los
// registros (#376). Los binarios Go estáticos usan el resolver puro de Go
// (sin la caché de glibc), así que cada dial consulta al resolver: un host
// FQDN sondeado cada pocos segundos martilleaba el DNS del usuario aunque
// sus registros llevaran TTL 600.
//
// Lookup hace una consulta A (y AAAA si aquella no responde nada) con
// dnsmessage contra los nameservers de /etc/resolv.conf, cachea el resultado
// hasta min(TTL de los registros) y devuelve IPs. Cualquier problema
// (resolv.conf sin leerse, timeout del nameserver, respuesta inválida)
// degrada SIEMPRE a net.DefaultResolver (comportamiento anterior).
//
// DialContext adapta la caché a net.Conn para inyectarla en http.Transport
// y en el dial SSH; Install() la instala en http.DefaultTransport para todo
// el proceso.
package dnscache

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

const (
	queryTimeout  = 3 * time.Second
	minCacheTTL   = 5 * time.Second
	maxCacheTTL   = 1 * time.Hour
	negativeTTL   = 5 * time.Second // NXDOMAIN/vacío: no martillar tampoco
	fallbackFloor = 30 * time.Second
)

type entry struct {
	ips     []net.IP
	expires time.Time
}

// Default es la instancia de proceso usada por DialContext e Install.
var Default = &Resolver{}

// Resolver es la caché MAC... de nombres: host → IPs con expiración por TTL.
type Resolver struct {
	mu    sync.Mutex
	cache map[string]entry
}

// Lookup devuelve las IPs de host desde la caché o el DNS. Nunca falla por
// sí sola: si el DNS propio no funciona usa net.DefaultResolver.
func (r *Resolver) Lookup(ctx context.Context, host string) []net.IP {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}
	}
	now := time.Now()
	r.mu.Lock()
	if e, ok := r.cache[host]; ok && now.Before(e.expires) {
		ips := e.ips
		r.mu.Unlock()
		return ips
	}
	r.mu.Unlock()

	ips, ttl, err := r.exchange(ctx, host)
	if err != nil {
		// Degradación honesta al resolver del sistema, con caché corta para
		// no volver a consultar en cada dial tampoco por este camino.
		addrs, lerr := net.DefaultResolver.LookupIPAddr(ctx, host)
		if lerr != nil || len(addrs) == 0 {
			r.store(host, nil, negativeTTL)
			return nil
		}
		for _, a := range addrs {
			ips = append(ips, a.IP)
		}
		r.store(host, ips, fallbackFloor)
		return ips
	}
	r.store(host, ips, ttl)
	return ips
}

func (r *Resolver) store(host string, ips []net.IP, ttl time.Duration) {
	if ttl < minCacheTTL {
		ttl = minCacheTTL
	}
	if ttl > maxCacheTTL {
		ttl = maxCacheTTL
	}
	r.mu.Lock()
	if r.cache == nil {
		r.cache = map[string]entry{}
	}
	r.cache[host] = entry{ips: ips, expires: time.Now().Add(ttl)}
	r.mu.Unlock()
}

// exchange consulta A (luego AAAA) a los nameservers de resolv.conf.
// Devuelve las IPs y el TTL mínimo de los registros.
func (r *Resolver) exchange(ctx context.Context, host string) ([]net.IP, time.Duration, error) {
	servers, err := resolvConfServers()
	if err != nil || len(servers) == 0 {
		return nil, 0, errors.New("sin nameservers en resolv.conf")
	}
	var ips []net.IP
	ttl := maxCacheTTL
	for _, qtype := range []dnsmessage.Type{dnsmessage.TypeA, dnsmessage.TypeAAAA} {
		ans, atTL, err := queryServer(ctx, servers, host, qtype)
		if err != nil {
			return nil, 0, err
		}
		if atTL < ttl {
			ttl = atTL
		}
		ips = append(ips, ans...)
		if len(ips) > 0 {
			break // A contestó: no hace falta AAAA
		}
	}
	if len(ips) == 0 {
		return nil, 0, errors.New("respuesta vacía")
	}
	return ips, ttl, nil
}

// queryServer pregunta a los nameservers en orden (el primero que responda).
func queryServer(ctx context.Context, servers []string, host string, qtype dnsmessage.Type) ([]net.IP, time.Duration, error) {
	var lastErr error
	for _, server := range servers {
		qctx, cancel := context.WithTimeout(ctx, queryTimeout)
		ips, ttl, err := queryOne(qctx, server, host, qtype)
		cancel()
		if err == nil {
			return ips, ttl, nil
		}
		lastErr = err
	}
	return nil, 0, lastErr
}

func queryOne(ctx context.Context, server, host string, qtype dnsmessage.Type) ([]net.IP, time.Duration, error) {
	name, err := dnsmessage.NewName(canonicalHost(host))
	if err != nil {
		return nil, 0, err
	}
	var buf [512]byte
	msg := dnsmessage.NewBuilder(buf[:0], dnsmessage.Header{ID: 0, RecursionDesired: true})
	msg.StartQuestions()
	if err := msg.Question(dnsmessage.Question{Name: name, Type: qtype, Class: dnsmessage.ClassINET}); err != nil {
		return nil, 0, err
	}
	wire, err := msg.Finish()
	if err != nil {
		return nil, 0, err
	}

	var d net.Dialer
	serverAddr := server
	if !strings.Contains(server, ":") {
		serverAddr = net.JoinHostPort(server, "53")
	}
	conn, err := d.DialContext(ctx, "udp", serverAddr)
	if err != nil {
		return nil, 0, err
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	_ = conn.SetDeadline(deadline)
	if _, err := conn.Write(wire); err != nil {
		return nil, 0, err
	}
	var resp [1500]byte
	n, err := conn.Read(resp[:])
	if err != nil {
		return nil, 0, err
	}

	var p dnsmessage.Parser
	hdr, err := p.Start(resp[:n])
	if err != nil {
		return nil, 0, err
	}
	if hdr.RCode != dnsmessage.RCodeSuccess {
		return nil, 0, fmt.Errorf("dns rcode %v", hdr.RCode)
	}
	if _, err := p.AllQuestions(); err != nil {
		return nil, 0, err
	}
	answers, err := p.AllAnswers()
	if err != nil {
		return nil, 0, err
	}
	var ips []net.IP
	ttl := maxCacheTTL
	for _, a := range answers {
		if a.Header.Type != qtype {
			continue
		}
		switch qtype {
		case dnsmessage.TypeA:
			r, ok := a.Body.(*dnsmessage.AResource)
			if !ok {
				continue
			}
			ips = append(ips, net.IP(r.A[:]))
		case dnsmessage.TypeAAAA:
			r, ok := a.Body.(*dnsmessage.AAAAResource)
			if !ok {
				continue
			}
			ips = append(ips, r.AAAA[:])
		}
		if time.Duration(a.Header.TTL)*time.Second < ttl {
			ttl = time.Duration(a.Header.TTL) * time.Second
		}
	}
	if len(ips) == 0 {
		return nil, 0, errors.New("respuesta vacía")
	}
	return ips, ttl, nil
}

// canonicalHost devuelve un nombre FQDN con punto final, que requiere
// dnsmessage.NewName. Las IP literales no pasan por aquí.
func canonicalHost(host string) string {
	if host == "" || host == "." {
		return "."
	}
	if strings.HasSuffix(host, ".") {
		return host
	}
	return host + "."
}

// resolvConfServers lee los nameservers de /etc/resolv.conf.
func resolvConfServers() ([]string, error) {
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "nameserver" {
			if ip := net.ParseIP(f[1]); ip != nil {
				out = append(out, f[1])
			}
		}
	}
	return out, nil
}

// DialContext resuelve host con la caché y marca el "cache miss" en el
// segundo intento, dialando la IP:puerto. El SNI de TLS lo aporta SIEMPRE
// el host original del llamador (http.Transport/ssh), no este dial.
func (r *Resolver) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = addr, ""
	}
	var d net.Dialer
	if host == "" {
		return d.DialContext(ctx, network, addr)
	}
	ips := r.Lookup(ctx, host)
	if len(ips) == 0 {
		return nil, fmt.Errorf("dnscache: sin IPs para %s", host)
	}
	var lastErr error
	for _, ip := range ips {
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	// Todas las IPs cacheadas fallaron: invalidar y un reintento fresco.
	r.mu.Lock()
	delete(r.cache, host)
	r.mu.Unlock()
	return nil, lastErr
}

// Instala la caché en http.DefaultTransport del proceso (todo lo que clona
// DefaultTransport - tlspin incluido - hereda el dial cacheado).
func Install() {
	t, ok := http.DefaultTransport.(*http.Transport)
	if !ok || t == nil {
		return
	}
	if t.DialContext == nil || isStdDialContext(t.DialContext) {
		t.DialContext = Default.DialContext
	} else {
		t.DialContext = wrapExisting(t.DialContext)
	}
}

func isStdDialContext(f func(ctx context.Context, network, addr string) (net.Conn, error)) bool {
	// El DefaultTransport base lleva nil en DialContext (usa su dialero
	// interno); cualquier valor previo distinto de nil aquí es nuestro.
	return f == nil
}

func wrapExisting(prev func(ctx context.Context, network, addr string) (net.Conn, error)) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, err := net.SplitHostPort(addr)
		if err != nil || net.ParseIP(host) != nil {
			return prev(ctx, network, addr)
		}
		ips := Default.Lookup(ctx, host)
		if len(ips) == 0 {
			return prev(ctx, network, addr)
		}
		var d net.Dialer
		var lastErr error
		for _, ip := range ips {
			conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), mustPort(addr)))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
}

func mustPort(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return ""
	}
	return port
}
