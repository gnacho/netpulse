package adapters

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"testing"
	"time"
)

type fakeRDNS struct {
	mu      sync.Mutex
	ptr     map[string][]string
	forward map[string][]string
	calls   map[string]int
}

func (f *fakeRDNS) LookupAddr(_ context.Context, addr string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[addr]++
	if names, ok := f.ptr[addr]; ok {
		return names, nil
	}
	return nil, errors.New("no such host")
}

func (f *fakeRDNS) LookupNetIP(_ context.Context, _, host string) ([]netip.Addr, error) {
	ips, ok := f.forward[host]
	if !ok {
		return nil, errors.New("no such host")
	}
	out := []netip.Addr{}
	for _, s := range ips {
		out = append(out, netip.MustParseAddr(s))
	}
	return out, nil
}

func (f *fakeRDNS) count(addr string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[addr]
}

func newTestRDNS(f *fakeRDNS, now func() time.Time) *rdnsCache {
	c := newRDNSCache()
	c.enabled = true
	c.resolver = f
	c.now = now
	return c
}

// resolved asks once (queues the lookup), waits for it, and asks again.
func resolved(c *rdnsCache, ip string) string {
	if n := c.Name(ip); n != "" {
		return n
	}
	c.wg.Wait()
	return c.Name(ip)
}

func TestRDNSName(t *testing.T) {
	f := &fakeRDNS{
		ptr: map[string][]string{
			"172.16.16.4":   {"switch-garage.home.arpa."},
			"172.16.17.202": {"pihole.home.arpa."},
			"192.168.8.217": {"192-168-8-217.0a1b2c3d4e5f.stremio.rocks."},
			"10.0.0.5":      {"ip-10-0-0-5.ec2.internal."},
			"172.16.16.30":  {"stale.home.arpa."},
			"172.16.16.31":  {"printer-ptr-only."},
			"172.16.16.40":  {"192-168-8-217.example.net.", "nas.home.arpa."},
			"fd00::10":      {"nas6.home.arpa."},
			"203.0.113.1":   {"gateway.isp.example."},
		},
		forward: map[string][]string{
			"switch-garage.home.arpa":                  {"172.16.16.4"},
			"pihole.home.arpa":                         {"172.16.17.202"},
			"192-168-8-217.0a1b2c3d4e5f.stremio.rocks": {"192.168.8.217"},
			"ip-10-0-0-5.ec2.internal":                 {"10.0.0.5"},
			"stale.home.arpa":                          {"172.16.16.99"},
			"nas.home.arpa":                            {"172.16.16.40"},
			"nas6.home.arpa":                           {"fd00::10"},
			"gateway.isp.example":                      {"203.0.113.1"},
		},
	}
	c := newTestRDNS(f, time.Now)
	cases := []struct {
		ip, want, why string
	}{
		{"172.16.16.4", "switch-garage", "static switch, forward-confirmed"},
		{"172.16.17.202", "pihole", "static container"},
		{"192.168.8.217", "", "wildcard PTR that embeds the address"},
		{"10.0.0.5", "", "cloud-style ip-a-b-c-d name"},
		{"172.16.16.30", "", "PTR that does not resolve back"},
		{"172.16.16.31", "", "PTR target with no forward record"},
		{"172.16.16.40", "nas", "skips the synthesized name, takes the real one"},
		{"fd00::10", "nas6", "IPv6 ULA"},
		{"203.0.113.1", "", "public address is never looked up"},
		{"172.16.16.50", "", "no PTR at all"},
		{"not-an-ip", "", "garbage input"},
	}
	for _, tc := range cases {
		if got := resolved(c, tc.ip); got != tc.want {
			t.Errorf("%s (%s): got %q, want %q", tc.ip, tc.why, got, tc.want)
		}
	}
	if n := f.count("203.0.113.1"); n != 0 {
		t.Errorf("public address was looked up %d times", n)
	}
}

func TestRDNSIPv4MappedAddress(t *testing.T) {
	f := &fakeRDNS{
		ptr: map[string][]string{
			"172.16.16.6": {"ap-hall.home.arpa."},
		},
		forward: map[string][]string{
			"ap-hall.home.arpa": {"172.16.16.6"},
		},
	}
	c := newTestRDNS(f, time.Now)
	// A mapped address has to be unmapped before the forward comparison,
	// otherwise LookupNetIP returns a plain IPv4 and the match never happens.
	if got := resolved(c, "::ffff:172.16.16.6"); got != "ap-hall" {
		t.Fatalf("mapped address: got %q, want ap-hall", got)
	}
	if n := f.count("172.16.16.6"); n != 1 {
		t.Fatalf("lookup used %v, want the unmapped 172.16.16.6", f.calls)
	}
}

func TestRDNSDoesNotQueueBeyondConcurrency(t *testing.T) {
	f := &fakeRDNS{ptr: map[string][]string{}, forward: map[string][]string{}}
	c := newTestRDNS(f, time.Now)
	for i := 0; i < rdnsConcurrency; i++ {
		c.sem <- struct{}{}
	}
	c.Name("172.16.16.61")
	c.mu.Lock()
	queued := len(c.inflight)
	c.mu.Unlock()
	if queued != 0 {
		t.Fatalf("queued %d lookups while every worker was busy", queued)
	}
	for i := 0; i < rdnsConcurrency; i++ {
		<-c.sem
	}
	// With a slot free the next poll queues it normally.
	c.Name("172.16.16.61")
	c.wg.Wait()
	c.mu.Lock()
	queued = len(c.inflight)
	c.mu.Unlock()
	if queued != 0 {
		t.Fatalf("%d lookups still in flight after the worker finished", queued)
	}
	if _, ok := c.entries["172.16.16.61"]; !ok {
		t.Fatal("lookup was never resolved once a slot was free")
	}
}

func TestRDNSCachesAndExpires(t *testing.T) {
	f := &fakeRDNS{
		ptr:     map[string][]string{"172.16.16.4": {"switch-garage.home.arpa."}},
		forward: map[string][]string{"switch-garage.home.arpa": {"172.16.16.4"}},
	}
	now := time.Unix(1_800_000_000, 0)
	c := newTestRDNS(f, func() time.Time { return now })

	if got := c.Name("172.16.16.4"); got != "" {
		t.Fatalf("first call must not block on DNS, got %q", got)
	}
	c.wg.Wait()
	for i := 0; i < 5; i++ {
		if got := c.Name("172.16.16.4"); got != "switch-garage" {
			t.Fatalf("cached name: got %q", got)
		}
	}
	if n := f.count("172.16.16.4"); n != 1 {
		t.Fatalf("lookups within TTL: got %d, want 1", n)
	}

	// After the TTL the stale name keeps showing while one refresh runs.
	now = now.Add(rdnsTTL + time.Second)
	if got := c.Name("172.16.16.4"); got != "switch-garage" {
		t.Fatalf("stale name during refresh: got %q", got)
	}
	c.wg.Wait()
	if n := f.count("172.16.16.4"); n != 2 {
		t.Fatalf("lookups after TTL: got %d, want 2", n)
	}
}

func TestRDNSNegativeCache(t *testing.T) {
	f := &fakeRDNS{}
	now := time.Unix(1_800_000_000, 0)
	c := newTestRDNS(f, func() time.Time { return now })
	resolved(c, "172.16.16.50")
	resolved(c, "172.16.16.50")
	if n := f.count("172.16.16.50"); n != 1 {
		t.Fatalf("misses within negative TTL: got %d lookups, want 1", n)
	}
	now = now.Add(rdnsNegativeTTL + time.Second)
	resolved(c, "172.16.16.50")
	if n := f.count("172.16.16.50"); n != 2 {
		t.Fatalf("after negative TTL: got %d lookups, want 2", n)
	}
}

func TestRDNSDisabled(t *testing.T) {
	t.Setenv("NETPULSE_RDNS", "0")
	c := newRDNSCache()
	f := &fakeRDNS{ptr: map[string][]string{"172.16.16.4": {"switch-garage.home.arpa."}}}
	c.resolver = f
	if got := c.Name("172.16.16.4"); got != "" {
		t.Fatalf("disabled cache returned %q", got)
	}
	c.wg.Wait()
	if n := f.count("172.16.16.4"); n != 0 {
		t.Fatalf("disabled cache looked up %d times", n)
	}
	var nilCache *rdnsCache
	if got := nilCache.Name("172.16.16.4"); got != "" {
		t.Fatalf("nil cache returned %q", got)
	}
}
