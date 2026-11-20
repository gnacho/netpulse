// dns_api_test.go — contrato de los endpoints de DNS por dispositivo y red (#2078).
package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

type dnsDomain struct {
	Domain  string `json:"domain"`
	Queries int64  `json:"queries"`
	Blocked int64  `json:"blocked"`
}

type dnsBody struct {
	Status       string      `json:"status"`
	Window       string      `json:"window"`
	GapTs        int64       `json:"gap_ts"`
	TotalQueries int64       `json:"total_queries"`
	TotalBlocked int64       `json:"total_blocked"`
	Top          []dnsDomain `json:"top"`
	NewDomains   []struct {
		Domain string `json:"domain"`
		MAC    string `json:"mac"`
	} `json:"new_domains"`
}

func getDNS(t *testing.T, ts *testServer, path string) (int, dnsBody) {
	t.Helper()
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	req, _ := http.NewRequest("GET", ts.URL+path, nil)
	req.Header.Set("Cookie", "session="+cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	var out dnsBody
	if res.StatusCode == http.StatusOK {
		if err := json.Unmarshal(body, &out); err != nil {
			t.Fatalf("decode %s: %v (%s)", path, err, body)
		}
	}
	return res.StatusCode, out
}

func seedDNS(t *testing.T, ts *testServer) time.Time {
	t.Helper()
	now := time.Now()
	hour := now.Truncate(time.Hour)
	// La ingesta guarda la MAC tal como está en device_seen (mayúsculas);
	// la ruta del endpoint llega en minúsculas.
	_, err := ts.db.Exec(`INSERT INTO dns_activity (mac, domain, hour_ts, queries, blocked, first_seen, last_seen)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, "AA:BB:CC:DD:EE:01", "video.example", hour.UnixMilli(), 12, 3,
		now.Add(-2*time.Minute).UnixMilli(), now.UnixMilli())
	if err != nil {
		t.Fatalf("seed dns_activity: %v", err)
	}
	_, err = ts.db.Exec(`INSERT INTO dns_domain_first (domain, first_seen, mac) VALUES (?, ?, ?)`,
		"video.example", now.Add(-time.Minute).UnixMilli(), "AA:BB:CC:DD:EE:01")
	if err != nil {
		t.Fatalf("seed dns_domain_first: %v", err)
	}
	if _, err := ts.db.Exec(`INSERT INTO kv (key, value) VALUES ('adguard_querylog_state', 'ok')
		ON CONFLICT(key) DO UPDATE SET value = 'ok'`); err != nil {
		t.Fatalf("seed state: %v", err)
	}
	return now
}

func TestDeviceDNSContract(t *testing.T) {
	ts := makeTestServer(t)
	seedDNS(t, ts)

	code, body := getDNS(t, ts, "/api/devices/aa:bb:cc:dd:ee:01/dns?window=1h")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if body.Status != "ok" || body.Window != "1h" {
		t.Errorf("status/window = %q/%q", body.Status, body.Window)
	}
	if body.TotalQueries != 12 || body.TotalBlocked != 3 {
		t.Errorf("totales = %d/%d", body.TotalQueries, body.TotalBlocked)
	}
	if len(body.Top) != 1 || body.Top[0].Domain != "video.example" || body.Top[0].Queries != 12 {
		t.Errorf("top = %+v", body.Top)
	}
}

func TestDeviceDNSEmptyIsNotAnError(t *testing.T) {
	ts := makeTestServer(t)
	code, body := getDNS(t, ts, "/api/devices/aa:bb:cc:dd:ee:09/dns?window=24h")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if body.Top == nil || len(body.Top) != 0 {
		t.Errorf("top = %+v, want lista vacía (no null)", body.Top)
	}
	if body.Status != "unavailable" {
		t.Errorf("status = %q, want unavailable sin ingesta", body.Status)
	}
}

func TestDeviceDNSRejectsBadMAC(t *testing.T) {
	ts := makeTestServer(t)
	if code, _ := getDNS(t, ts, "/api/devices/no-es-una-mac/dns"); code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", code)
	}
}

func TestDNSOverviewContract(t *testing.T) {
	ts := makeTestServer(t)
	seedDNS(t, ts)

	code, body := getDNS(t, ts, "/api/dns/overview?window=24h")
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(body.Top) != 1 || body.Top[0].Domain != "video.example" {
		t.Errorf("top = %+v", body.Top)
	}
	if len(body.NewDomains) != 1 || body.NewDomains[0].Domain != "video.example" {
		t.Errorf("new_domains = %+v", body.NewDomains)
	}
	if body.NewDomains[0].MAC != "AA:BB:CC:DD:EE:01" {
		t.Errorf("mac del dominio nuevo = %q", body.NewDomains[0].MAC)
	}
}
