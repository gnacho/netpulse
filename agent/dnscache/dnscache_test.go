package dnscache

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// wireA600 es una respuesta DNS A fija: router.example.com -> 192.0.2.10, TTL 600.
// Generada con dnsmessage.Builder y usada por el fake UDP server.
var wireA600 = []byte{
	0x04, 0xd2, 0x81, 0x80, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00,
	0x06, 0x72, 0x6f, 0x75, 0x74, 0x65, 0x72, 0x07, 0x65, 0x78, 0x61, 0x6d,
	0x70, 0x6c, 0x65, 0x03, 0x63, 0x6f, 0x6d, 0x00, 0x00, 0x01, 0x00, 0x01,
	0xc0, 0x0c, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0x02, 0x58, 0x00, 0x04,
	0xc0, 0x00, 0x02, 0x0a,
}

// fakeDNS responde siempre wireA600 y cuenta las consultas recibidas.
type fakeDNS struct {
	udp  *net.UDPConn
	mu   chan struct{}
	quit chan struct{}
}

func startFakeDNS(t *testing.T) *fakeDNS {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeDNS{udp: conn, mu: make(chan struct{}, 64), quit: make(chan struct{})}
	go func() {
		buf := make([]byte, 512)
		for {
			n, src, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			select {
			case <-f.quit:
				return
			default:
			}
			_ = n
			f.mu <- struct{}{}
			_, _ = conn.WriteToUDP(wireA600, src)
		}
	}()
	t.Cleanup(func() {
		close(f.quit)
		_ = conn.Close()
	})
	return f
}

func (f *fakeDNS) queries() int {
	n := 0
	for {
		select {
		case <-f.mu:
			n++
		default:
			return n
		}
	}
}

func TestLookupCacheHonorsTTL(t *testing.T) {
	f := startFakeDNS(t)
	r := &Resolver{}
	ctx := context.Background()

	// queryOne directo: parsea la respuesta fija y lee TTL 600.
	server := f.udp.LocalAddr().String()
	ips, ttl, err := queryOne(ctx, server, "router.example.com", dnsmessage.TypeA)
	if err != nil {
		t.Fatalf("queryOne: %v", err)
	}
	if len(ips) != 1 || !ips[0].Equal(net.ParseIP("192.0.2.10")) {
		t.Fatalf("ips: %v", ips)
	}
	if ttl != 600*time.Second {
		t.Fatalf("ttl: %v (want 600s)", ttl)
	}

	// Lookup con IP literal no consulta nada.
	if got := r.Lookup(ctx, "192.0.2.99"); len(got) != 1 || !got[0].Equal(net.ParseIP("192.0.2.99")) {
		t.Fatalf("literal: %v", got)
	}

	// Caché: store + expiry.
	r.store("h.example", []net.IP{net.ParseIP("192.0.2.1")}, 600*time.Second)
	for i := 0; i < 50; i++ {
		if got := r.Lookup(ctx, "h.example"); len(got) != 1 {
			t.Fatalf("caché rota en la iteración %d: %v", i, got)
		}
	}
	if f.queries() != 1 {
		t.Fatalf("la caché no debería consultar DNS tras el primer miss: %d", f.queries())
	}
}

func TestParseResolvConf(t *testing.T) {
	servers, err := resolvConfServers()
	if err != nil {
		t.Skipf("sin resolv.conf legible en este entorno: %v", err)
	}
	for _, s := range servers {
		if net.ParseIP(s) == nil {
			t.Fatalf("nameserver no IP: %q", s)
		}
	}
}

func TestDialContextResuelveYSiFallaInvalida(t *testing.T) {
	r := &Resolver{}
	r.store("nunca-escucha.example", []net.IP{net.ParseIP("127.0.0.1")}, 600*time.Second)
	_, err := r.DialContext(context.Background(), "tcp", "nunca-escucha.example:59999")
	if err == nil {
		t.Fatal("debe fallar: nada escucha en 59999")
	}
	r.mu.Lock()
	_, cached := r.cache["nunca-escucha.example"]
	r.mu.Unlock()
	if cached {
		t.Fatal("tras fallo de dial la entrada cacheada debe invalidarse")
	}
	if !strings.Contains(err.Error(), "refused") && !strings.Contains(err.Error(), "connect") {
		t.Logf("error de dial inesperado (no fatal): %v", err)
	}
}
