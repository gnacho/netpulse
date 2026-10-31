// catalog_test.go - #1697: descubrimiento del catálogo de vendors. Todos los
// tests corren sin red: el fetch se inyecta con FetchOverride y el reloj con
// Catalog.Now.
package rintegration

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

// seedVendor devuelve un vendor de la semilla embebida (helper de tests).
func seedVendor(t *testing.T, id string) Vendor {
	t.Helper()
	v, ok := (&Catalog{}).Lookup(id)
	if !ok {
		t.Fatalf("vendor %q missing from the embedded seed", id)
	}
	return v
}

// fakeClock es un reloj inyectable para probar el TTL sin esperar.
type fakeClock struct {
	t time.Time
}

func (c *fakeClock) Now() time.Time { return c.t }
func (c *fakeClock) Add(d time.Duration) {
	c.t = c.t.Add(d)
}

// overrideOf devuelve un FetchOverride que sirve los cuerpos en orden y
// cuenta las llamadas; al agotarse repite el último.
func overrideOf(t *testing.T, calls *atomic.Int32, bodies ...string) func(string) ([]byte, error) {
	t.Helper()
	return func(string) ([]byte, error) {
		n := int(calls.Add(1)) - 1
		if n >= len(bodies) {
			n = len(bodies) - 1
		}
		return []byte(bodies[n]), nil
	}
}

func failingOverride(calls *atomic.Int32) func(string) ([]byte, error) {
	return func(string) ([]byte, error) {
		calls.Add(1)
		return nil, errors.New("dial tcp: connection refused")
	}
}

const labVendorA = `{"vendors":[{"id":"labvendor","mode":"local","arches":["amd64"],"tagPrefix":"lab/v","binary":"lab-pusher","target":"lab/cmd/lab-pusher","description":"Lab vendor"}]}`

const labVendorB = `{"vendors":[{"id":"labvendor","mode":"local","arches":["amd64","arm64"],"tagPrefix":"lab/v","binary":"lab-pusher","target":"lab/cmd/lab-pusher","description":"Lab vendor with two arches"}]}`

func newTestCatalogDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// TestSeedCatalogServesCurrentVendors: la semilla embebida (generada del
// vendors.json real, con el campo target) cubre los cuatro vendors
// publicados, y la resolución deriva Repo/AssetTmpl/Init que el schema no
// lleva: bbox usa el prefijo del binario compartido y asuswrt es merlin.
func TestSeedCatalogServesCurrentVendors(t *testing.T) {
	want := []struct {
		id      string
		mode    string
		arches  string
		prefix  string
		binary  string
		init    string
		version string
		arch    string
		asset   string
	}{
		{"routeros", "local", "[amd64]", "routeros/v", "routeros-pusher", "procd", "1.2.3", "amd64", "routeros_1.2.3_linux_amd64.tar.gz"},
		{"asuswrt", "ssh", "[arm64]", "asuswrt/v", "asuswrt-pusher", "merlin", "1.0.0", "arm64", "asuswrt_1.0.0_linux_arm64.tar.gz"},
		{"bbox", "local", "[amd64 arm64]", "bbox/v", "netpulse-integrations", "procd", "0.1.0", "amd64", "integrations-supervisor_0.1.0_linux_amd64.tar.gz"},
		{"unifi", "local", "[amd64]", "unifi/v", "unifi-pusher", "procd", "1.0.0", "amd64", "unifi_1.0.0_linux_amd64.tar.gz"},
	}
	c := NewCatalog(nil)
	if got := len(c.Current()); got != len(want) {
		t.Fatalf("seed serves %d vendors, want %d", got, len(want))
	}
	for _, w := range want {
		v, ok := c.Lookup(w.id)
		if !ok {
			t.Fatalf("seed vendor %q missing", w.id)
		}
		if v.Mode != w.mode || v.TagPrefix != w.prefix || v.Binary != w.binary {
			t.Fatalf("%s: %+v", w.id, v)
		}
		if fmt.Sprint(v.Arches) != w.arches {
			t.Fatalf("%s arches = %v, want %s", w.id, v.Arches, w.arches)
		}
		if got := v.InitSystem(); got != w.init {
			t.Fatalf("%s init = %q, want %q", w.id, got, w.init)
		}
		if v.Repo != CatalogRepo {
			t.Fatalf("%s repo = %q, want %q", w.id, v.Repo, CatalogRepo)
		}
		if got := v.AssetName(w.version, w.arch); got != w.asset {
			t.Fatalf("%s asset = %q, want %q", w.id, got, w.asset)
		}
	}
	if _, ok := c.Lookup("nope"); ok {
		t.Fatal("unknown vendor must not resolve")
	}
}

// TestCatalogServesSeedWithoutNetwork: un catálogo sin base de datos (demo,
// fallback de tests) no intenta fetch nunca y sirve la semilla.
func TestCatalogServesSeedWithoutNetwork(t *testing.T) {
	c := NewCatalog(nil)
	c.FetchOverride = func(url string) ([]byte, error) {
		t.Fatalf("fetch must not be attempted without a db (url %s)", url)
		return nil, nil
	}
	vs := c.Vendors(context.Background())
	if len(vs) != 4 {
		t.Fatalf("seed vendors = %d, want 4", len(vs))
	}
	if !c.LastRefreshedAt().IsZero() {
		t.Fatal("seed-only catalog must have zero lastRefreshedAt")
	}
}

// TestCatalogFetchPersistsAndReloads: el catálogo descargado se sirve, se
// persiste en kv y una segunda apertura sobre la misma base lo recupera sin
// tocar la red.
func TestCatalogFetchPersistsAndReloads(t *testing.T) {
	d := newTestCatalogDB(t)
	clock := &fakeClock{t: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)}
	var calls atomic.Int32

	c := NewCatalog(d.DB)
	c.Now = clock.Now
	c.FetchOverride = overrideOf(t, &calls, labVendorA)
	vs := c.Vendors(context.Background())
	if len(vs) != 1 || vs[0].ID != "labvendor" {
		t.Fatalf("fetched catalog = %+v", vs)
	}
	if calls.Load() != 1 {
		t.Fatalf("fetch calls = %d, want 1", calls.Load())
	}
	if !c.LastRefreshedAt().Equal(clock.t) {
		t.Fatalf("lastRefreshedAt = %v, want %v", c.LastRefreshedAt(), clock.t)
	}
	if _, ok := kvGet(d.DB, kvCatalogKey); !ok {
		t.Fatal("catalog not persisted in kv")
	}

	// Reapertura sobre la misma base: sin red, desde kv.
	c2 := NewCatalog(d.DB)
	c2.Now = clock.Now
	c2.FetchOverride = func(url string) ([]byte, error) {
		t.Fatalf("reopened catalog must serve from kv without fetching (url %s)", url)
		return nil, nil
	}
	got, ok := c2.Lookup("labvendor")
	if !ok || got.Binary != "lab-pusher" {
		t.Fatalf("reloaded vendor = %+v ok=%v", got, ok)
	}
	if !c2.LastRefreshedAt().Equal(clock.t) {
		t.Fatalf("reloaded lastRefreshedAt = %v, want %v", c2.LastRefreshedAt(), clock.t)
	}
}

// TestCatalogTTLExpiredRefetches: dentro del TTL no hay segunda descarga;
// pasado el TTL la siguiente lectura refresca.
func TestCatalogTTLExpiredRefetches(t *testing.T) {
	d := newTestCatalogDB(t)
	clock := &fakeClock{t: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)}
	var calls atomic.Int32

	c := NewCatalog(d.DB)
	c.Now = clock.Now
	c.FetchOverride = overrideOf(t, &calls, labVendorA, labVendorB)

	if vs := c.Vendors(context.Background()); len(vs[0].Arches) != 1 {
		t.Fatalf("first serve should be body A: %+v", vs)
	}
	clock.Add(catalogTTL - time.Minute)
	if vs := c.Vendors(context.Background()); len(vs[0].Arches) != 1 {
		t.Fatalf("within TTL the cache must still be served: %+v", vs)
	}
	if calls.Load() != 1 {
		t.Fatalf("fetch calls within TTL = %d, want 1", calls.Load())
	}
	clock.Add(2 * time.Minute)
	vs := c.Vendors(context.Background())
	if calls.Load() != 2 {
		t.Fatalf("fetch calls after TTL = %d, want 2", calls.Load())
	}
	if len(vs[0].Arches) != 2 {
		t.Fatalf("after TTL the refreshed catalog must be served: %+v", vs)
	}
}

// TestCatalogFetchFailsKeepsCache: con caché previa, un fetch fallido sirve
// la caché vieja, no cambia lastRefreshedAt y no reintenta dentro del
// cooldown.
func TestCatalogFetchFailsKeepsCache(t *testing.T) {
	d := newTestCatalogDB(t)
	clock := &fakeClock{t: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)}
	var calls atomic.Int32

	c := NewCatalog(d.DB)
	c.Now = clock.Now
	c.FetchOverride = overrideOf(t, &calls, labVendorA)
	if vs := c.Vendors(context.Background()); len(vs) != 1 {
		t.Fatalf("setup fetch failed: %+v", vs)
	}
	firstRefresh := c.LastRefreshedAt()

	clock.Add(catalogTTL + time.Hour)
	c.FetchOverride = failingOverride(&calls)
	vs := c.Vendors(context.Background())
	if len(vs) != 1 || vs[0].ID != "labvendor" {
		t.Fatalf("stale cache must be served after a failed fetch: %+v", vs)
	}
	if calls.Load() != 2 {
		t.Fatalf("fetch calls = %d, want 2 (setup + failed attempt)", calls.Load())
	}
	if !c.LastRefreshedAt().Equal(firstRefresh) {
		t.Fatalf("lastRefreshedAt changed on failure: %v", c.LastRefreshedAt())
	}

	// Cooldown: la siguiente lectura no reintenta.
	if vs := c.Vendors(context.Background()); len(vs) != 1 {
		t.Fatalf("cache must survive: %+v", vs)
	}
	if calls.Load() != 2 {
		t.Fatalf("retry happened within the cooldown (%d calls)", calls.Load())
	}
	if c.LastError() == "" {
		t.Fatal("last error must be recorded for diagnostics")
	}
}

// TestCatalogFetchFailsNoCacheServesSeed: sin caché y sin red, la semilla se
// sirve sin error y la lista nunca queda vacía.
func TestCatalogFetchFailsNoCacheServesSeed(t *testing.T) {
	d := newTestCatalogDB(t)
	var calls atomic.Int32
	c := NewCatalog(d.DB)
	c.FetchOverride = failingOverride(&calls)
	vs := c.Vendors(context.Background())
	if len(vs) != 4 {
		t.Fatalf("seed must be served when the fetch fails without cache: %d vendors", len(vs))
	}
	if _, ok := c.Lookup("routeros"); !ok {
		t.Fatal("seed lookup broken after failed fetch")
	}
	if !c.LastRefreshedAt().IsZero() {
		t.Fatal("lastRefreshedAt must stay zero without a successful fetch")
	}
}

// TestCatalogInvalidEntriesDropped: las entradas inválidas se descartan una a
// una sin romper el resto; un catálogo sin ninguna entrada válida no se
// aplica y devuelve error conservando el anterior.
func TestCatalogInvalidEntriesDropped(t *testing.T) {
	d := newTestCatalogDB(t)
	clock := &fakeClock{t: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)}
	var calls atomic.Int32

	mixed := `{"vendors":[
		{"id":"badmode","mode":"telnet","arches":["amd64"],"tagPrefix":"bad/v","binary":"b","target":"x","description":"bad mode"},
		{"id":"noduplicates","mode":"local","arches":["amd64"],"tagPrefix":"dup/v","binary":"b","target":"x","description":"first"},
		{"id":"noduplicates","mode":"local","arches":["amd64"],"tagPrefix":"dup/v","binary":"b","target":"x","description":"duplicate id"},
		{"id":"noarches","mode":"local","arches":[],"tagPrefix":"na/v","binary":"b","target":"x","description":"empty arches"},
		{"id":"goodone","mode":"ssh","arches":["arm64"],"tagPrefix":"good/v","binary":"good-pusher","target":"good/cmd/good-pusher","description":"Good one"}
	]}`
	c := NewCatalog(d.DB)
	c.Now = clock.Now
	c.FetchOverride = overrideOf(t, &calls, mixed)
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh with mixed entries: %v", err)
	}
	vs := c.Current()
	if len(vs) != 2 {
		t.Fatalf("valid vendors = %d, want 2 (goodone + noduplicates): %+v", len(vs), vs)
	}
	if _, ok := c.Lookup("goodone"); !ok {
		t.Fatal("good entry must survive invalid siblings")
	}
	if _, ok := c.Lookup("badmode"); ok {
		t.Fatal("entry with unknown mode must be dropped")
	}
	if _, ok := c.Lookup("noarches"); ok {
		t.Fatal("entry with empty arches must be dropped")
	}

	// Todo inválido: error y catálogo anterior intacto.
	empty := `{"vendors":[{"id":"Bad ID","mode":"local","arches":["amd64"],"tagPrefix":"x/v","binary":"b","target":"x","description":"bad id"}]}`
	c.FetchOverride = overrideOf(t, &calls, empty)
	if err := c.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh must fail when no entry is valid")
	}
	if len(c.Current()) != 2 {
		t.Fatalf("previous catalog must survive a fully invalid refresh: %+v", c.Current())
	}
	if !c.LastRefreshedAt().Equal(clock.t) {
		t.Fatal("lastRefreshedAt must not advance on a failed refresh")
	}

	// JSON malformado: mismo trato.
	c.FetchOverride = overrideOf(t, &calls, `{"vendors": [[["bad`)
	if err := c.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh must fail on malformed JSON")
	}
	if len(c.Current()) != 2 {
		t.Fatalf("previous catalog must survive malformed JSON: %+v", c.Current())
	}
}

// TestCatalogRefreshForcesFetch: el refresco admin salta el TTL, actualiza el
// catálogo y lastRefreshedAt, y devuelve el error si el fetch falla sin tocar
// lo servido.
func TestCatalogRefreshForcesFetch(t *testing.T) {
	d := newTestCatalogDB(t)
	clock := &fakeClock{t: time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)}
	var calls atomic.Int32

	c := NewCatalog(d.DB)
	c.Now = clock.Now
	c.FetchOverride = overrideOf(t, &calls, labVendorA, labVendorB)
	_ = c.Vendors(context.Background())
	clock.Add(time.Minute) // fresco: Vendors no refrescaría

	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("forced Refresh: %v", err)
	}
	if calls.Load() != 2 {
		t.Fatalf("forced refresh must fetch even within the TTL (calls %d)", calls.Load())
	}
	if vs := c.Current(); len(vs[0].Arches) != 2 {
		t.Fatalf("refreshed catalog not served: %+v", vs)
	}
	if !c.LastRefreshedAt().Equal(clock.t) {
		t.Fatalf("lastRefreshedAt = %v, want %v", c.LastRefreshedAt(), clock.t)
	}

	c.FetchOverride = failingOverride(&calls)
	if err := c.Refresh(context.Background()); err == nil {
		t.Fatal("failed forced refresh must return the error")
	}
	if vs := c.Current(); len(vs[0].Arches) != 2 {
		t.Fatalf("failed refresh must keep the previous catalog: %+v", vs)
	}
}
