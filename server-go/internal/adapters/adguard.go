// adguard.go — Cliente AdGuard Home estándar (API HTTP /control con Basic
// auth; port de src/adapters/adguard.js, SPEC §7.3). Timeout corto y errores
// controlados: si AdGuard no responde, el caller degrada y el poller sigue.
package adapters

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const adguardHTTPTimeout = 4 * time.Second

// AdGuardClient es el cliente estándar (ADGUARD_URL/USER/PASS).
type AdGuardClient struct {
	url  string // sin barra final
	auth string // "Basic …"
	hc   *http.Client
}

// NewAdGuardClient crea el cliente estándar.
func NewAdGuardClient(rawURL, user, pass string) *AdGuardClient {
	return &AdGuardClient{
		url:  rawURL,
		auth: "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass)),
		hc:   &http.Client{Timeout: adguardHTTPTimeout},
	}
}

func (c *AdGuardClient) get(ctx context.Context, path string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.url+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.auth)
	req.Header.Set("Accept", "application/json")
	res, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("AdGuard %s → HTTP %d", path, res.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(dst)
}

// adGuardStatus: /control/status
type adGuardStatus struct {
	ProtectionEnabled bool `json:"protection_enabled"`
	Running           bool `json:"running"`
}

// adGuardStatsRaw: /control/stats (top_blocked_domains acepta los dos
// formatos: [{domain:count}] o [["domain",count]]).
type adGuardStatsRaw struct {
	NumDNSQueries        int64           `json:"num_dns_queries"`
	NumBlockedFiltering  int64           `json:"num_blocked_filtering"`
	NumReplacedSafebrows *int64          `json:"num_replaced_safebrowsing"`
	NumReplacedParental  int64           `json:"num_replaced_parental"`
	AvgProcessingTime    float64         `json:"avg_processing_time"`
	NumClients           int64           `json:"num_clients"`
	TopBlockedDomains    json.RawMessage `json:"top_blocked_domains"`
}

// GetStats devuelve AdGuardStats (shape del contrato). Error si caído.
func (c *AdGuardClient) GetStats(ctx context.Context) (*AdGuardStats, error) {
	ctx, cancel := context.WithTimeout(ctx, adguardHTTPTimeout)
	defer cancel()

	var status adGuardStatus
	if err := c.get(ctx, "/control/status", &status); err != nil {
		return nil, err
	}
	var stats adGuardStatsRaw
	if err := c.get(ctx, "/control/stats", &stats); err != nil {
		return nil, err
	}
	var filtering struct {
		Filters []struct {
			Enabled    bool `json:"enabled"`
			RulesCount int  `json:"rules_count"`
		} `json:"filters"`
	}
	_ = c.get(ctx, "/control/filtering/status", &filtering) // best-effort (null en JS)

	queries := stats.NumDNSQueries
	blocked := stats.NumBlockedFiltering

	u, _ := url.Parse(c.url)
	host := u.Hostname()
	port := 80
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			port = n
		}
	}

	blockedPct := 0.0
	if queries > 0 {
		blockedPct = math.Round(float64(blocked)/float64(queries)*100*10) / 10
	}

	trackers := blocked
	if stats.NumReplacedSafebrows != nil {
		trackers = blocked - (*stats.NumReplacedSafebrows + stats.NumReplacedParental)
	}

	status2 := "inactive"
	if status.ProtectionEnabled && status.Running {
		status2 = "active"
	}

	filterLists := 0
	rules := 0
	for _, f := range filtering.Filters {
		if f.Enabled {
			filterLists++
			rules += f.RulesCount
		}
	}

	return &AdGuardStats{
		Host: host, Port: port, Status: status2,
		Queries24h: queries, Blocked24h: blocked, BlockedPct: blockedPct,
		TrackersBlocked: trackers,
		DNSLatencyMs:    int(math.Round(stats.AvgProcessingTime * 1000)),
		ClientsUsing:    int(stats.NumClients), ClientsTotal: int(stats.NumClients),
		TopBlocked:      parseTopBlocked(stats.TopBlockedDomains, 5),
		FilterLists:     filterLists, Rules: rules,
	}, nil
}

// parseTopBlocked: top 5; acepta [{domain: count}] y [["domain", count]].
func parseTopBlocked(raw json.RawMessage, limit int) []TopBlocked {
	out := []TopBlocked{}
	if len(raw) == 0 {
		return out
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return out
	}
	for i, item := range arr {
		if i >= limit {
			break
		}
		// Formato viejo: ["domain", count]
		var pair []json.RawMessage
		if err := json.Unmarshal(item, &pair); err == nil && len(pair) == 2 {
			var domain string
			var count int64
			if json.Unmarshal(pair[0], &domain) == nil && json.Unmarshal(pair[1], &count) == nil {
				out = append(out, TopBlocked{Domain: domain, Count: count})
				continue
			}
		}
		// Formato nuevo: {domain: count}
		var m map[string]int64
		if err := json.Unmarshal(item, &m); err == nil {
			for k, v := range m { // una sola clave por objeto
				out = append(out, TopBlocked{Domain: k, Count: v})
				break
			}
		}
	}
	return out
}
