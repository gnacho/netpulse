package wifisle

import (
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func setup(t *testing.T) *Store {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := EnsureSchema(d); err != nil {
		t.Fatal(err)
	}
	return NewStore(d)
}

func TestInsertAndSummary(t *testing.T) {
	s := setup(t)
	now := db.NowMS()
	for i := 0; i < 5; i++ {
		err := s.Insert(SLEReport{
			RouterID: "patio", TS: now - int64(i)*3600000,
			ConnectCount: 10, AvgConnectMs: 150 + float64(i*10),
			DHCPRequests: 20, DHCPAcks: 19, DNSQueries: 100, AvgDNSMs: 5.5,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	sc, err := s.Summary("patio", 24)
	if err != nil {
		t.Fatal(err)
	}
	if sc.ConnectCount != 50 {
		t.Fatalf("expected 50 connects, got %d", sc.ConnectCount)
	}
	if sc.DHCPSuccessPct < 94 || sc.DHCPSuccessPct > 96 {
		t.Fatalf("unexpected DHCP success: %.1f%%", sc.DHCPSuccessPct)
	}
	if sc.Score < 80 {
		t.Fatalf("score too low: %d", sc.Score)
	}
}

func TestSummaryEmpty(t *testing.T) {
	s := setup(t)
	sc, err := s.Summary("nonexistent", 24)
	if err != nil {
		t.Fatal(err)
	}
	if sc.ConnectCount != 0 {
		t.Fatalf("expected 0 connects, got %d", sc.ConnectCount)
	}
	if sc.DHCPSuccessPct != 100 {
		t.Fatalf("expected 100%% DHCP (no data), got %.1f%%", sc.DHCPSuccessPct)
	}
}

func TestSeries(t *testing.T) {
	s := setup(t)
	now := db.NowMS()
	for i := 0; i < 3; i++ {
		_ = s.Insert(SLEReport{
			RouterID: "patio", TS: now - int64(i)*3600000,
			ConnectCount: 5, AvgConnectMs: 100, DHCPRequests: 10, DHCPAcks: 10,
		})
	}
	series, err := s.Series("patio", now-7200000, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 3 {
		t.Fatalf("expected 3 points, got %d", len(series))
	}
}

func TestPurge(t *testing.T) {
	s := setup(t)
	now := db.NowMS()
	_ = s.Insert(SLEReport{RouterID: "r1", TS: now - 86400000*30, ConnectCount: 1})
	_ = s.Insert(SLEReport{RouterID: "r1", TS: now, ConnectCount: 1})
	n, err := s.Purge(7 * 24 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 purged, got %d", n)
	}
}

func TestComputeScore(t *testing.T) {
	tests := []struct {
		name     string
		sc       Summary
		minScore int
	}{
		{"perfect", Summary{AvgConnectMs: 100, DHCPSuccessPct: 100, AvgDNSMs: 5}, 95},
		{"slow connect", Summary{AvgConnectMs: 2500, DHCPSuccessPct: 100, AvgDNSMs: 5}, 70},
		{"dhcp failures", Summary{AvgConnectMs: 100, DHCPSuccessPct: 85, AvgDNSMs: 5}, 60},
		{"slow dns", Summary{AvgConnectMs: 100, DHCPSuccessPct: 100, AvgDNSMs: 150}, 70},
		{"all bad", Summary{AvgConnectMs: 3000, DHCPSuccessPct: 80, AvgDNSMs: 200}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := computeScore(tt.sc)
			if score < tt.minScore {
				t.Fatalf("score %d < min %d", score, tt.minScore)
			}
		})
	}
}
