package wifisle

import (
	"database/sql"
	"fmt"
	"math"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

type SLEReport struct {
	RouterID       string  `json:"routerId"`
	TS             int64   `json:"ts"`
	ConnectCount   int     `json:"connectCount"`
	AvgConnectMs   float64 `json:"avgConnectMs"`
	DHCPRequests   int     `json:"dhcpRequests"`
	DHCPAcks       int     `json:"dhcpAcks"`
	DHCPSuccessPct float64 `json:"dhcpSuccessPct"`
	DNSQueries     int     `json:"dnsQueries"`
	AvgDNSMs       float64 `json:"avgDnsMs"`
}

type Summary struct {
	RouterID       string  `json:"routerId"`
	ConnectCount   int     `json:"connectCount"`
	AvgConnectMs   float64 `json:"avgConnectMs"`
	DHCPSuccessPct float64 `json:"dhcpSuccessPct"`
	AvgDNSMs       float64 `json:"avgDnsMs"`
	Score          int     `json:"score"`
	Label          string  `json:"label"`
}

type Store struct {
	db *db.DB
}

func NewStore(d *db.DB) *Store {
	return &Store{db: d}
}

func EnsureSchema(d *db.DB) error {
	_, err := d.Exec(`
		CREATE TABLE IF NOT EXISTS wifi_sles (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			router_id TEXT NOT NULL,
			ts INTEGER NOT NULL,
			connect_count INTEGER NOT NULL DEFAULT 0,
			avg_connect_ms REAL NOT NULL DEFAULT 0,
			dhcp_requests INTEGER NOT NULL DEFAULT 0,
			dhcp_acks INTEGER NOT NULL DEFAULT 0,
			dns_queries INTEGER NOT NULL DEFAULT 0,
			avg_dns_ms REAL NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_wifi_sles_router_ts ON wifi_sles(router_id, ts DESC);
	`)
	return err
}

func (s *Store) Insert(r SLEReport) error {
	_, err := s.db.Exec(
		`INSERT INTO wifi_sles (router_id, ts, connect_count, avg_connect_ms, dhcp_requests, dhcp_acks, dns_queries, avg_dns_ms)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.RouterID, r.TS, r.ConnectCount, r.AvgConnectMs, r.DHCPRequests, r.DHCPAcks, r.DNSQueries, r.AvgDNSMs,
	)
	return err
}

func (s *Store) Summary(routerID string, windowHours int) (*Summary, error) {
	cutoff := db.NowMS() - int64(windowHours)*3600000
	var sc Summary
	sc.RouterID = routerID
	var dhcpReqs, dhcpAcks int
	err := s.db.QueryRow(`
		SELECT COALESCE(SUM(connect_count),0),
		       CASE WHEN SUM(connect_count) > 0 THEN SUM(avg_connect_ms * connect_count) / SUM(connect_count) ELSE 0 END,
		       COALESCE(SUM(dhcp_requests),0),
		       COALESCE(SUM(dhcp_acks),0),
		       CASE WHEN SUM(dns_queries) > 0 THEN SUM(avg_dns_ms * dns_queries) / SUM(dns_queries) ELSE 0 END
		FROM wifi_sles WHERE router_id = ? AND ts >= ?`,
		routerID, cutoff,
	).Scan(&sc.ConnectCount, &sc.AvgConnectMs, &dhcpReqs, &dhcpAcks, &sc.AvgDNSMs)
	if err != nil {
		return nil, err
	}
	if dhcpReqs > 0 {
		sc.DHCPSuccessPct = math.Round(float64(dhcpAcks) / float64(dhcpReqs) * 1000) / 10
	} else {
		sc.DHCPSuccessPct = 100
	}
	sc.Score = computeScore(sc)
	sc.Label = scoreLabel(sc.Score)
	return &sc, nil
}

func (s *Store) AllSummaries(windowHours int) ([]Summary, error) {
	rows, err := s.db.Query("SELECT DISTINCT router_id FROM wifi_sles")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Summary
	for rows.Next() {
		var rid string
		if err := rows.Scan(&rid); err != nil {
			continue
		}
		sc, err := s.Summary(rid, windowHours)
		if err == nil {
			out = append(out, *sc)
		}
	}
	return out, rows.Err()
}

func (s *Store) Series(routerID string, from, to int64) ([]SLEReport, error) {
	rows, err := s.db.Query(
		`SELECT router_id, ts, connect_count, avg_connect_ms, dhcp_requests, dhcp_acks, dns_queries, avg_dns_ms
		 FROM wifi_sles WHERE router_id = ? AND ts BETWEEN ? AND ? ORDER BY ts`,
		routerID, from, to,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SLEReport
	for rows.Next() {
		var r SLEReport
		if err := rows.Scan(&r.RouterID, &r.TS, &r.ConnectCount, &r.AvgConnectMs, &r.DHCPRequests, &r.DHCPAcks, &r.DNSQueries, &r.AvgDNSMs); err != nil {
			return nil, err
		}
		if r.DHCPRequests > 0 {
			r.DHCPSuccessPct = math.Round(float64(r.DHCPAcks) / float64(r.DHCPRequests) * 1000) / 10
		} else {
			r.DHCPSuccessPct = 100
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) Purge(olderThan time.Duration) (int64, error) {
	cutoff := db.NowMS() - olderThan.Milliseconds()
	res, err := s.db.Exec("DELETE FROM wifi_sles WHERE ts < ?", cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func computeScore(sc Summary) int {
	score := 100
	if sc.AvgConnectMs > 2000 {
		score -= 20
	} else if sc.AvgConnectMs > 1000 {
		score -= 10
	} else if sc.AvgConnectMs > 500 {
		score -= 5
	}
	if sc.DHCPSuccessPct < 90 {
		score -= 25
	} else if sc.DHCPSuccessPct < 95 {
		score -= 15
	} else if sc.DHCPSuccessPct < 99 {
		score -= 5
	}
	if sc.AvgDNSMs > 100 {
		score -= 20
	} else if sc.AvgDNSMs > 50 {
		score -= 10
	} else if sc.AvgDNSMs > 20 {
		score -= 5
	}
	if score < 0 {
		score = 0
	}
	return score
}

func scoreLabel(score int) string {
	switch {
	case score >= 90:
		return "Excellent"
	case score >= 70:
		return "Good"
	case score >= 50:
		return "Fair"
	default:
		return "Poor"
	}
}

var _ = sql.ErrNoRows
var _ = fmt.Sprintf
