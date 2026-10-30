package adapters

import (
	"context"
	"net"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"
)

// rdns.go — reverse DNS fallback for device names.
//
// Hosts with a static address never take a DHCP lease, so a network made of
// fixed-IP servers, switches and access points shows most of them as bare
// MACs even when the local DNS already knows every name. When nothing else
// named a device (lease, reservation, alias, override, mDNS) its PTR record
// is the last source tried.
//
// buildDevices runs on every poll and must never wait on DNS, so lookups are
// asynchronous: Name answers from the cache and queues a background lookup
// on a miss; the name shows up on the next poll. Only private and link-local
// addresses are looked up, so the server does not send PTR queries for
// public addresses to an upstream resolver. NETPULSE_RDNS=0 turns it off.

const (
	rdnsTTL         = time.Hour
	rdnsNegativeTTL = 15 * time.Minute
	rdnsTimeout     = 3 * time.Second
	rdnsConcurrency = 4
)

type rdnsResolver interface {
	LookupAddr(ctx context.Context, addr string) ([]string, error)
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

type rdnsEntry struct {
	name    string
	expires time.Time
}

type rdnsCache struct {
	enabled  bool
	resolver rdnsResolver
	now      func() time.Time

	mu       sync.Mutex
	entries  map[string]rdnsEntry
	inflight map[string]bool
	sem      chan struct{}
	wg       sync.WaitGroup
}

func newRDNSCache() *rdnsCache {
	c := &rdnsCache{
		enabled:  true,
		resolver: net.DefaultResolver,
		now:      time.Now,
		entries:  map[string]rdnsEntry{},
		inflight: map[string]bool{},
		sem:      make(chan struct{}, rdnsConcurrency),
	}
	if v, ok := os.LookupEnv("NETPULSE_RDNS"); ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "0", "false", "off", "no":
			c.enabled = false
		}
	}
	return c
}

// Name returns the cached PTR name for ip, or "" when there is none yet. A
// missing or expired entry queues one background lookup.
func (c *rdnsCache) Name(ip string) string {
	if c == nil || !c.enabled {
		return ""
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ""
	}
	// An IPv4-mapped IPv6 address (::ffff:172.16.16.4) has to be unmapped
	// before it is used as a cache key and compared with the forward lookup,
	// which returns plain IPv4 addresses.
	addr = addr.Unmap()
	if !rdnsEligible(addr) {
		return ""
	}
	key := addr.String()
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if ok && c.now().Before(e.expires) {
		return e.name
	}
	if !c.inflight[key] {
		// Take a worker slot before spawning, so a network full of unnamed
		// hosts queues lookups for later polls instead of one goroutine per
		// address waiting on the semaphore.
		select {
		case c.sem <- struct{}{}:
			c.inflight[key] = true
			c.wg.Add(1)
			go c.resolve(addr)
		default:
		}
	}
	// A stale name is still better than a MAC while the refresh runs.
	return e.name
}

func (c *rdnsCache) resolve(addr netip.Addr) {
	defer c.wg.Done()
	// The worker slot was taken by Name before this goroutine started.
	defer func() { <-c.sem }()
	name := c.lookup(addr)
	ttl := rdnsTTL
	if name == "" {
		ttl = rdnsNegativeTTL
	}
	key := addr.String()
	c.mu.Lock()
	c.entries[key] = rdnsEntry{name: name, expires: c.now().Add(ttl)}
	delete(c.inflight, key)
	c.mu.Unlock()
}

// lookup returns the short name of the first PTR target that resolves back
// to addr (forward-confirmed), skipping names generated from the address.
func (c *rdnsCache) lookup(addr netip.Addr) string {
	ctx, cancel := context.WithTimeout(context.Background(), rdnsTimeout)
	defer cancel()
	names, err := c.resolver.LookupAddr(ctx, addr.String())
	if err != nil {
		return ""
	}
	for _, n := range names {
		fqdn := strings.TrimSuffix(n, ".")
		if fqdn == "" || rdnsEmbedsAddr(fqdn, addr) {
			continue
		}
		ips, err := c.resolver.LookupNetIP(ctx, "ip", fqdn)
		if err != nil {
			continue
		}
		for _, ip := range ips {
			if ip.Unmap() == addr {
				return rdnsShortName(fqdn)
			}
		}
	}
	return ""
}

// rdnsEligible keeps lookups on the local resolver's own zones: private,
// shared CGNAT and link-local space only.
func rdnsEligible(addr netip.Addr) bool {
	addr = addr.Unmap()
	if addr.IsPrivate() || addr.IsLinkLocalUnicast() {
		return true
	}
	return rdnsCGNAT.Contains(addr)
}

var rdnsCGNAT = netip.MustParsePrefix("100.64.0.0/10")

// rdnsEmbedsAddr rejects synthesized PTRs such as "192-168-8-217.example.net"
// or "ip-10-0-0-5.ec2.internal": they name the address, not the device, and
// some public wildcard zones answer for private ranges.
func rdnsEmbedsAddr(fqdn string, addr netip.Addr) bool {
	host := strings.ToLower(fqdn)
	s := addr.String()
	return strings.Contains(host, strings.NewReplacer(".", "-", ":", "-").Replace(s)) ||
		strings.Contains(host, s)
}

// rdnsShortName drops the domain, matching the bare hostnames DHCP leases
// report ("switch-garage.home.arpa" -> "switch-garage").
func rdnsShortName(fqdn string) string {
	if i := strings.IndexByte(fqdn, '.'); i > 0 {
		return fqdn[:i]
	}
	return fqdn
}
