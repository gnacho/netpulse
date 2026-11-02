// integrations_catalog_test.go - #1697: GET /api/integrations/vendors sirve el
// catálogo por descubrimiento (con lastRefreshedAt) y degrada a la semilla sin
// red; POST /api/integrations/vendors/refresh (admin) fuerza el fetch y
// devuelve el catálogo actualizado. Sin red: fetch inyectado.
package httpapi_test

import (
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/rintegration"
)

// seedVendor devuelve un vendor de la semilla embebida (helper de tests).
func seedVendor(t *testing.T, id string) rintegration.Vendor {
	t.Helper()
	v, ok := rintegration.NewCatalog(nil).Lookup(id)
	if !ok {
		t.Fatalf("vendor %q missing from the embedded seed", id)
	}
	return v
}

func post(t *testing.T, base, path, cookie, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", base+path, strings.NewReader(body))
	if cookie != "" {
		req.Header.Set("Cookie", "session="+cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return res
}

const labVendorCatalog = `{"vendors":[{"id":"labvendor","mode":"local","arches":["amd64"],"tagPrefix":"lab/v","binary":"lab-pusher","target":"lab/cmd/lab-pusher","description":"Lab vendor"}]}`

// TestVendorsEndpointServesSeedAndDegradesOffline: con el fetch roto, el GET
// sirve la semilla embebida (nunca una lista vacía), lastRefreshedAt se
// mantiene a cero y el intento fallido no tira la respuesta.
func TestVendorsEndpointServesSeedAndDegradesOffline(t *testing.T) {
	var calls atomic.Int32
	ts, _ := makeIntegrationsTestServerFull(t, nil, func(d *db.DB) *rintegration.Catalog {
		c := rintegration.NewCatalog(d.DB)
		c.FetchOverride = func(string) ([]byte, error) {
			calls.Add(1)
			return nil, errors.New("dial tcp: connection refused")
		}
		return c
	})

	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	res := get(t, ts.URL, "/api/integrations/vendors", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET vendors: status %d", res.StatusCode)
	}
	body := readJSON(t, res)
	rawVendors, ok := body["vendors"].([]any)
	if !ok {
		t.Fatalf("vendors missing: %v", body)
	}
	if len(rawVendors) != 4 {
		t.Fatalf("seed must be served when offline: %d vendors (%v)", len(rawVendors), rawVendors)
	}
	if got := body["lastRefreshedAt"].(float64); got != 0 {
		t.Fatalf("lastRefreshedAt = %v, want 0 without a successful fetch", got)
	}
	if calls.Load() != 1 {
		t.Fatalf("the endpoint must attempt the discovery once (calls %d)", calls.Load())
	}
}

// TestVendorsRefreshEndpointUpdatesCatalog: el refresco admin fuerza el fetch
// y devuelve el catálogo nuevo con lastRefreshedAt actualizado.
func TestVendorsRefreshEndpointUpdatesCatalog(t *testing.T) {
	var calls atomic.Int32
	ts, _ := makeIntegrationsTestServerFull(t, nil, func(d *db.DB) *rintegration.Catalog {
		c := rintegration.NewCatalog(d.DB)
		c.FetchOverride = func(string) ([]byte, error) {
			if calls.Add(1) == 1 {
				// Primer intento (GET): roto, se sirve la semilla.
				return nil, errors.New("connection refused")
			}
			return []byte(labVendorCatalog), nil
		}
		return c
	})

	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	// Estado inicial: el GET de arranque falla y sirve la semilla.
	res0 := get(t, ts.URL, "/api/integrations/vendors", cookie)
	body0 := readJSON(t, res0)
	if rawVendors0, ok := body0["vendors"].([]any); !ok || len(rawVendors0) != 4 {
		t.Fatalf("seed must be served while the fetch fails: %v", body0)
	}

	res := post(t, ts.URL, "/api/integrations/vendors/refresh", cookie, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("POST refresh: status %d", res.StatusCode)
	}
	body := readJSON(t, res)
	rawVendors, ok := body["vendors"].([]any)
	if !ok || len(rawVendors) != 1 {
		t.Fatalf("refreshed catalog = %v", body)
	}
	first, _ := rawVendors[0].(map[string]any)
	if first["id"] != "labvendor" || first["tagPrefix"] != "lab/v" {
		t.Fatalf("refreshed vendor = %v", first)
	}
	if got := body["lastRefreshedAt"].(float64); got <= 0 {
		t.Fatalf("lastRefreshedAt = %v, want > 0 after a successful refresh", got)
	}

	// El GET posterior sirve el catálogo refrescado desde la caché.
	res2 := get(t, ts.URL, "/api/integrations/vendors", cookie)
	body2 := readJSON(t, res2)
	if rawVendors2, ok := body2["vendors"].([]any); !ok || len(rawVendors2) != 1 {
		t.Fatalf("GET after refresh must serve the cached catalog: %v", body2)
	}
}

// TestVendorsRefreshRequiresSession: el refresco del catálogo es admin-only.
func TestVendorsRefreshRequiresSession(t *testing.T) {
	ts, _ := makeIntegrationsTestServerFull(t, nil, func(d *db.DB) *rintegration.Catalog {
		return rintegration.NewCatalog(d.DB)
	})
	res := post(t, ts.URL, "/api/integrations/vendors/refresh", "", "")
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated refresh: status %d, want 401", res.StatusCode)
	}
}

// TestVendorsFallbackCatalogHasNoNetwork: el catálogo por defecto de
// NewHandler (sin Deps.VendorCatalog) sirve la semilla sin intentar fetch
// alguno: los tests y el demo no tocan la red.
func TestVendorsFallbackCatalogHasNoNetwork(t *testing.T) {
	ts, _ := makeIntegrationsTestServer(t)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	res := get(t, ts.URL, "/api/integrations/vendors", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET vendors: status %d", res.StatusCode)
	}
	body := readJSON(t, res)
	rawVendors, ok := body["vendors"].([]any)
	if !ok || len(rawVendors) != 4 {
		t.Fatalf("fallback catalog must serve the seed: %v", body)
	}
	if got := body["lastRefreshedAt"].(float64); got != 0 {
		t.Fatalf("fallback catalog must never fetch: lastRefreshedAt = %v", got)
	}
}
