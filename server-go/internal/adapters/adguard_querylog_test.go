// adguard_querylog_test.go — parseo y lectura del query log (#2078).
package adapters

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const sampleQueryLog = `{
  "oldest": "2026-11-16T10:00:00Z",
  "data": [
    {
      "time": "2026-11-16T10:59:58.123Z",
      "client": "192.168.1.42",
      "client_proto": "",
      "client_info": {"name": "laptop"},
      "question": {"name": "YouTube.COM.", "type": "A"},
      "status": "NOERROR",
      "reason": "NotFilteredNotFound",
      "rules": [],
      "answer": [{"type": "A", "value": "142.250.1.1"}]
    },
    {
      "time": "2026-11-16T10:59:59Z",
      "client": "192.168.1.43",
      "client_proto": "doh",
      "client_info": {"name": "phone"},
      "question": {"name": "ads.example.com", "type": "AAAA"},
      "status": "NOERROR",
      "reason": "FilteredBlackList",
      "rules": [{"text": "||ads.example.com^"}]
    },
    {
      "time": "2026-11-16T10:59:00Z",
      "client": "192.168.1.44",
      "question": {"name": "", "type": "A"},
      "status": "NOERROR",
      "reason": "NotFilteredNotFound"
    }
  ]
}`

func TestParseQueryLogNormalizesAndFilters(t *testing.T) {
	page, err := parseQueryLogJSON([]byte(sampleQueryLog))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(page.Queries) != 2 {
		t.Fatalf("queries = %d, want 2 (la entrada sin dominio se descarta)", len(page.Queries))
	}
	q := page.Queries[0]
	if q.Domain != "youtube.com" {
		t.Errorf("domain = %q, want youtube.com", q.Domain)
	}
	if q.BlockedByFilter() {
		t.Error("NotFilteredNotFound no es bloqueo")
	}
	if q.ClientName != "laptop" || q.QType != "A" {
		t.Errorf("cliente/tipo = %q/%q", q.ClientName, q.QType)
	}
	if !page.Queries[1].BlockedByFilter() {
		t.Error("FilteredBlackList debe contar como bloqueo")
	}
	if page.Queries[1].Rule != "||ads.example.com^" {
		t.Errorf("rule = %q", page.Queries[1].Rule)
	}
	if page.Queries[1].Proto != "doh" {
		t.Errorf("proto = %q, want doh", page.Queries[1].Proto)
	}
	if !page.Oldest.Equal(time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("oldest = %v", page.Oldest)
	}
}

func TestBlockedByFilterReasons(t *testing.T) {
	cases := map[string]bool{
		"FilteredBlackList":      true,
		"FilteredSafeBrowsing":   true,
		"FilteredParental":       true,
		"FilteredBlockedService": true,
		"FilteredSafeSearch":     false, // reescribe, no bloquea
		"Rewrite":                false,
		"NotFilteredNotFound":    false,
		"":                       false,
	}
	for reason, want := range cases {
		if got := (DNSQuery{Reason: reason}).BlockedByFilter(); got != want {
			t.Errorf("reason %q → %v, want %v", reason, got, want)
		}
	}
}

func TestQueryLogHTTPUsesOlderThanAndLimit(t *testing.T) {
	var gotPath, gotOlder, gotLimit string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotOlder = r.URL.Query().Get("older_than")
		gotLimit = r.URL.Query().Get("limit")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleQueryLog))
	}))
	defer srv.Close()

	c := NewAdGuardClient(srv.URL, "u", "p")
	page, err := c.QueryLog(context.Background(), "2026-11-16T09:00:00Z", 500)
	if err != nil {
		t.Fatalf("QueryLog: %v", err)
	}
	if gotPath != "/control/querylog" {
		t.Errorf("path = %q", gotPath)
	}
	if gotOlder != "2026-11-16T09:00:00Z" {
		t.Errorf("older_than = %q", gotOlder)
	}
	if gotLimit != "500" {
		t.Errorf("limit = %q", gotLimit)
	}
	if len(page.Queries) != 2 {
		t.Errorf("queries = %d", len(page.Queries))
	}
}

func TestQueryLogConfigParses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/control/querylog/config" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"enabled": true, "anonymize_client_ip": true, "interval": 86400000,
		})
	}))
	defer srv.Close()

	c := NewAdGuardClient(srv.URL, "u", "p")
	cfg, err := c.QueryLogConfig(context.Background())
	if err != nil {
		t.Fatalf("QueryLogConfig: %v", err)
	}
	if !cfg.Enabled || !cfg.AnonymizeClientIP {
		t.Errorf("cfg = %+v", cfg)
	}
	if cfg.IntervalMS != 86400000 {
		t.Errorf("interval = %v", cfg.IntervalMS)
	}
}
