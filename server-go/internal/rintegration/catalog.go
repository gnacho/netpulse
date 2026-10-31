// catalog.go - vendor catalog discovery (#1697). The list of supported
// integrations is no longer compiled into the binary: it is fetched from
// vendors.json in gnacho/netpulse-integrations (the same repository that
// publishes the releases), cached in the kv table with a TTL, and served with
// an embedded seed so a fresh offline install still has something to show.
// A failed fetch or a malformed catalog never empties the vendor list: the
// previous cache (or the seed) keeps being served.
package rintegration

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"sync"
	"time"
)

const (
	// CatalogRepo is the GitHub repository that publishes the vendor catalog
	// and every integration release.
	CatalogRepo = "gnacho/netpulse-integrations"
	// rawCatalogURL is the raw URL of the catalog on the main branch.
	rawCatalogURL = "https://raw.githubusercontent.com/" + CatalogRepo + "/main/vendors.json"
	// catalogTTL is how long a fetched catalog is considered fresh. The
	// issue leaves the value open; 24h keeps GitHub raw traffic negligible
	// while new vendors still show up the same day they are published.
	catalogTTL = 24 * time.Hour
	// catalogFetchTimeout bounds every catalog fetch attempt.
	catalogFetchTimeout = 10 * time.Second
	// catalogRetryCooldown is the minimum wait between fetch attempts after
	// a failure, so an offline instance does not hammer GitHub raw on every
	// page view.
	catalogRetryCooldown = 10 * time.Minute
	// catalogMaxBytes caps the catalog body (it is a small JSON file).
	catalogMaxBytes = 1 << 20
	// kvCatalogKey persists the last fetched catalog plus its timestamp.
	kvCatalogKey = "integrations.vendor_catalog"
)

//go:embed vendors_seed.json
var seedJSON []byte

// seedCatalog is the embedded copy of the current vendors.json, generated
// from netpulse-integrations main. Regenerate it from that file when the
// upstream catalog gains the first vendor that must work out of the box.
var seedCatalog = parseSeed()

func parseSeed() []Vendor {
	var f catalogFile
	if err := json.Unmarshal(seedJSON, &f); err != nil {
		log.Printf("[netpulse] aviso: la semilla del catálogo de vendors no parsea: %v", err)
		return nil
	}
	return validVendors(f.Vendors)
}

// catalogEntry mirrors the vendors.json schema (netpulse-integrations PR #24):
// id, mode, arches, tagPrefix, binary, target, description.
type catalogEntry struct {
	ID          string   `json:"id"`
	Mode        string   `json:"mode"`
	Arches      []string `json:"arches"`
	TagPrefix   string   `json:"tagPrefix"`
	Binary      string   `json:"binary"`
	Target      string   `json:"target"`
	Description string   `json:"description"`
}

type catalogFile struct {
	Vendors []catalogEntry `json:"vendors"`
}

// assetPrefixes holds execution knowledge the catalog schema does not carry:
// vendor ids whose release asset name prefix differs from the vendor id. The
// integrations release workflow names assets "<name>_<version>_linux_<arch>.
// tar.gz"; bbox ships the shared integrations-supervisor binary instead of a
// per-vendor one. A vendor following the default convention needs no entry.
var assetPrefixes = map[string]string{
	"bbox": "integrations-supervisor",
}

// initSystems holds the on-router layout of ssh vendors (how to install, not
// who exists). Empty means procd (OpenWrt); asuswrt ships a Merlin layout
// (#1681).
var initSystems = map[string]string{
	"asuswrt": "merlin",
}

var catalogIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// validVendors validates catalog entries and converts them to Vendors.
// Invalid entries are dropped individually (a bad vendor must never break the
// rest of the catalog); an empty result means the whole catalog is unusable.
func validVendors(entries []catalogEntry) []Vendor {
	seen := map[string]bool{}
	out := make([]Vendor, 0, len(entries))
	for _, e := range entries {
		v, ok := vendorFromEntry(e, seen)
		if !ok {
			log.Printf("[netpulse] aviso: entrada del catálogo de vendors descartada por no válida: id=%q mode=%q", e.ID, e.Mode)
			continue
		}
		out = append(out, v)
	}
	return out
}

func vendorFromEntry(e catalogEntry, seen map[string]bool) (Vendor, bool) {
	if !catalogIDRe.MatchString(e.ID) || seen[e.ID] {
		return Vendor{}, false
	}
	if e.Mode != "local" && e.Mode != "ssh" {
		return Vendor{}, false
	}
	if e.TagPrefix == "" || e.Binary == "" || len(e.Arches) == 0 {
		return Vendor{}, false
	}
	for _, a := range e.Arches {
		if a == "" {
			return Vendor{}, false
		}
	}
	seen[e.ID] = true
	prefix := e.ID
	if p := assetPrefixes[e.ID]; p != "" {
		prefix = p
	}
	return Vendor{
		ID:          e.ID,
		Mode:        e.Mode,
		Repo:        CatalogRepo,
		TagPrefix:   e.TagPrefix,
		AssetTmpl:   prefix + "_{version}_linux_{arch}.tar.gz",
		Arches:      append([]string(nil), e.Arches...),
		Binary:      e.Binary,
		Init:        initSystems[e.ID],
		Description: e.Description,
	}, true
}

// kvCatalog is the persisted form of the last fetched catalog: the raw body
// (so future schema evolution re-validates from the source format) plus the
// timestamp of the successful fetch.
type kvCatalog struct {
	FetchedAtMs int64  `json:"fetchedAtMs"`
	Body        string `json:"body"`
}

// Catalog discovers and serves the vendor catalog. The zero-value knobs
// (Client, RawURL, Now, FetchOverride) mirror the Fetcher style: unset fields
// get production defaults; tests set them to stay offline. A Catalog built
// without a db (demo, nil fallback in tests) never fetches: it serves the
// embedded seed only.
type Catalog struct {
	Client        *http.Client // default: 10s timeout
	RawURL        string       // default: rawCatalogURL
	Now           func() time.Time
	FetchOverride func(url string) ([]byte, error) // test injection; nil = real fetch

	db *sql.DB

	mu         sync.Mutex
	loaded     bool
	vendors    []Vendor
	fetchedAt  time.Time // zero = never fetched from the network
	retryAfter time.Time // cooldown after a failed fetch
	lastErr    string
}

// NewCatalog builds a catalog. A nil db keeps it in memory (seed only, no
// network); a real db enables the kv cache and the TTL auto-fetch.
func NewCatalog(db *sql.DB) *Catalog {
	return &Catalog{db: db}
}

func (c *Catalog) client() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: catalogFetchTimeout}
}

func (c *Catalog) url() string {
	if c.RawURL != "" {
		return c.RawURL
	}
	return rawCatalogURL
}

func (c *Catalog) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Current returns the last known catalog (cache or seed) without any network
// access. Use it when freshness is not worth a fetch.
func (c *Catalog) Current() []Vendor {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ensureLoadedLocked()
	return append([]Vendor(nil), c.vendors...)
}

// Lookup resolves one vendor from the last known catalog (cache or seed),
// with no network access, so create/edit/start/stop/update/check keep working
// offline exactly as they did against the compiled manifest.
func (c *Catalog) Lookup(id string) (Vendor, bool) {
	for _, v := range c.Current() {
		if v.ID == id {
			return v, true
		}
	}
	return Vendor{}, false
}

// LastRefreshedAt reports the last successful fetch from the network (zero
// when only the seed has ever been served).
func (c *Catalog) LastRefreshedAt() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ensureLoadedLocked()
	return c.fetchedAt
}

// LastRefreshedAtMs is LastRefreshedAt as unix milliseconds, 0 when the seed
// has never been replaced by a successful fetch. It is the shape the API and
// the UI use for timestamps.
func (c *Catalog) LastRefreshedAtMs() int64 {
	t := c.LastRefreshedAt()
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// LastError reports the last fetch or validation error, for diagnostics.
func (c *Catalog) LastError() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastErr
}

// Vendors serves the current catalog, refreshing it first when the TTL has
// expired (or nothing was ever fetched). The fetch is best effort: on failure
// the previously known catalog (or the seed) is served, never an empty list.
func (c *Catalog) Vendors(ctx context.Context) []Vendor {
	c.mu.Lock()
	c.ensureLoadedLocked()
	stale := c.db != nil && (c.fetchedAt.IsZero() || c.now().Sub(c.fetchedAt) >= catalogTTL)
	cooldown := c.now().Before(c.retryAfter)
	c.mu.Unlock()
	if stale && !cooldown {
		// Best effort: Refresh keeps the old catalog on failure.
		_ = c.Refresh(ctx)
	}
	return c.Current()
}

// Refresh forces a fetch from the network regardless of the TTL, validates
// the catalog and persists it. On failure the previous catalog stays in
// place, a cooldown is stamped and the error is returned.
func (c *Catalog) Refresh(ctx context.Context) error {
	body, err := c.fetchRaw(ctx)
	if err == nil {
		var f catalogFile
		if err = json.Unmarshal(body, &f); err == nil {
			vs := validVendors(f.Vendors)
			if len(vs) == 0 {
				err = fmt.Errorf("vendor catalog: no valid entries")
			} else {
				now := c.now()
				c.mu.Lock()
				c.vendors = vs
				c.fetchedAt = now
				c.lastErr = ""
				c.mu.Unlock()
				c.persist(now, body)
				return nil
			}
		}
	}
	c.mu.Lock()
	c.retryAfter = c.now().Add(catalogRetryCooldown)
	c.lastErr = err.Error()
	c.mu.Unlock()
	log.Printf("[netpulse] aviso: no se pudo refrescar el catálogo de vendors (%v); se sigue sirviendo el último conocido", err)
	return err
}

func (c *Catalog) fetchRaw(ctx context.Context) ([]byte, error) {
	if c.FetchOverride != nil {
		return c.FetchOverride(c.url())
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := c.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vendor catalog: %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, catalogMaxBytes))
	if err != nil {
		return nil, err
	}
	return body, nil
}

// persist stores the raw catalog and its timestamp in kv. Failures are logged
// and swallowed: the in-memory catalog already serves this process, and the
// next successful fetch will persist again.
func (c *Catalog) persist(fetchedAt time.Time, body []byte) {
	if c.db == nil {
		return
	}
	raw, err := json.Marshal(kvCatalog{FetchedAtMs: fetchedAt.UnixMilli(), Body: string(body)})
	if err != nil {
		log.Printf("[netpulse] aviso: catálogo de vendors no serializable: %v", err)
		return
	}
	if err := kvSet(c.db, kvCatalogKey, string(raw)); err != nil {
		log.Printf("[netpulse] aviso: no se pudo persistir el catálogo de vendors: %v", err)
	}
}

// ensureLoadedLocked loads the kv cache on first use, falling back to the
// embedded seed. Any corrupt or empty cache silently degrades to the seed.
func (c *Catalog) ensureLoadedLocked() {
	if c.loaded {
		return
	}
	c.loaded = true
	c.vendors = seedCatalog
	if c.db == nil {
		return
	}
	raw, ok := kvGet(c.db, kvCatalogKey)
	if !ok {
		return
	}
	var w kvCatalog
	if err := json.Unmarshal([]byte(raw), &w); err != nil || w.Body == "" {
		log.Printf("[netpulse] aviso: caché del catálogo de vendors corrupta, se usa la semilla")
		return
	}
	var f catalogFile
	if err := json.Unmarshal([]byte(w.Body), &f); err != nil {
		log.Printf("[netpulse] aviso: caché del catálogo de vendors no parsea, se usa la semilla")
		return
	}
	vs := validVendors(f.Vendors)
	if len(vs) == 0 {
		return
	}
	c.vendors = vs
	if w.FetchedAtMs > 0 {
		c.fetchedAt = time.UnixMilli(w.FetchedAtMs)
	}
}

func kvGet(d *sql.DB, key string) (string, bool) {
	var val string
	err := d.QueryRow("SELECT value FROM kv WHERE key = ?", key).Scan(&val)
	if err != nil {
		return "", false
	}
	return val, true
}

func kvSet(d *sql.DB, key, val string) error {
	_, err := d.Exec("INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value", key, val)
	return err
}
