package internethealth

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

const (
	schemaSQL = `
CREATE TABLE IF NOT EXISTS internet_probes (
  ts INTEGER NOT NULL,
  target TEXT NOT NULL,
  latency_ms REAL NOT NULL,
  loss_pct REAL NOT NULL,
  success INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_internet_probes_ts ON internet_probes(ts DESC);

CREATE TABLE IF NOT EXISTS internet_outages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  started_at INTEGER NOT NULL,
  ended_at INTEGER,
  duration_s INTEGER,
  targets_down TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_internet_outages_ts ON internet_outages(started_at DESC);
`
	retentionDays = 30
	maxProbes     = 10000
)

type ProbeResult struct {
	Ts        int64   `json:"ts"`
	Target    string  `json:"target"`
	LatencyMs float64 `json:"latencyMs"`
	LossPct   float64 `json:"lossPct"`
	Success   bool    `json:"success"`
}

type Outage struct {
	ID         int64    `json:"id"`
	StartedAt  int64    `json:"startedAt"`
	EndedAt    *int64   `json:"endedAt"`
	DurationS  *int64   `json:"durationS"`
	TargetsDown []string `json:"targetsDown"`
}

type Summary struct {
	LastProbe  *ProbeResult `json:"lastProbe"`
	CurrentOutage *Outage   `json:"currentOutage"`
	TotalOutages24h int     `json:"totalOutages24h"`
	AvgLatency24h   float64 `json:"avgLatency24h"`
}

type Store struct {
	mu   sync.Mutex
	db   *db.DB
	outage *Outage
}

func NewStore(d *db.DB) (*Store, error) {
	if d != nil {
		if _, err := d.Exec(schemaSQL); err != nil {
			return nil, fmt.Errorf("internet_health schema: %w", err)
		}
	}
	s := &Store{db: d}
	if d != nil {
		s.loadActiveOutage()
	}
	return s, nil
}

func (s *Store) loadActiveOutage() {
	var id int64
	var startedAt int64
	var targetsDown string
	err := s.db.QueryRow(
		"SELECT id, started_at, targets_down FROM internet_outages WHERE ended_at IS NULL ORDER BY id DESC LIMIT 1",
	).Scan(&id, &startedAt, &targetsDown)
	if err != nil {
		return
	}
	var targets []string
	_ = json.Unmarshal([]byte(targetsDown), &targets)
	s.outage = &Outage{ID: id, StartedAt: startedAt, TargetsDown: targets}
}

func (s *Store) RecordProbe(p ProbeResult) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db != nil {
		_, _ = s.db.Exec(
			"INSERT INTO internet_probes (ts, target, latency_ms, loss_pct, success) VALUES (?,?,?,?,?)",
			p.Ts, p.Target, p.LatencyMs, p.LossPct, boolToInt(p.Success))
	}

	if !p.Success && s.outage == nil {
		s.startOutage(p)
	} else if p.Success && s.outage != nil {
		s.endOutage(p.Ts)
	}
}

func (s *Store) startOutage(p ProbeResult) {
	now := p.Ts
	targets := []string{p.Target}
	targetsJSON, _ := json.Marshal(targets)

	if s.db != nil {
		res, err := s.db.Exec(
			"INSERT INTO internet_outages (started_at, targets_down) VALUES (?,?)",
			now, string(targetsJSON))
		if err == nil {
			id, _ := res.LastInsertId()
			s.outage = &Outage{ID: id, StartedAt: now, TargetsDown: targets}
		}
	} else {
		s.outage = &Outage{ID: 0, StartedAt: now, TargetsDown: targets}
	}
}

func (s *Store) endOutage(ts int64) {
	if s.outage == nil {
		return
	}
	dur := ts - s.outage.StartedAt
	if s.db != nil {
		_, _ = s.db.Exec(
			"UPDATE internet_outages SET ended_at = ?, duration_s = ? WHERE id = ?",
			ts, dur, s.outage.ID)
	}
	s.outage = nil
}

func (s *Store) Summary() Summary {
	s.mu.Lock()
	defer s.mu.Unlock()

	sum := Summary{}
	if s.db == nil {
		return sum
	}

	var ts int64
	var target string
	var lat, loss float64
	var success int
	err := s.db.QueryRow(
		"SELECT ts, target, latency_ms, loss_pct, success FROM internet_probes ORDER BY ts DESC LIMIT 1",
	).Scan(&ts, &target, &lat, &loss, &success)
	if err == nil {
		sum.LastProbe = &ProbeResult{Ts: ts, Target: target, LatencyMs: lat, LossPct: loss, Success: success == 1}
	}

	sum.CurrentOutage = s.outage

	cutoff := time.Now().Add(-24 * time.Hour).UnixMilli()
	var count int
	var avgLat sql.NullFloat64
	_ = s.db.QueryRow(
		"SELECT COUNT(*), AVG(latency_ms) FROM internet_probes WHERE ts >= ? AND success = 1",
		cutoff,
	).Scan(&count, &avgLat)
	if avgLat.Valid {
		sum.AvgLatency24h = avgLat.Float64
	}

	var outages int
	_ = s.db.QueryRow(
		"SELECT COUNT(*) FROM internet_outages WHERE started_at >= ?",
		cutoff,
	).Scan(&outages)
	sum.TotalOutages24h = outages

	return sum
}

func (s *Store) RecentProbes(limit int) []ProbeResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return nil
	}
	rows, err := s.db.Query(
		"SELECT ts, target, latency_ms, loss_pct, success FROM internet_probes ORDER BY ts DESC LIMIT ?",
		limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []ProbeResult
	for rows.Next() {
		var p ProbeResult
		var success int
		if rows.Scan(&p.Ts, &p.Target, &p.LatencyMs, &p.LossPct, &success) == nil {
			p.Success = success == 1
			out = append(out, p)
		}
	}
	return out
}

func (s *Store) RecentOutages(limit int) []Outage {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return nil
	}
	rows, err := s.db.Query(
		"SELECT id, started_at, ended_at, duration_s, targets_down FROM internet_outages ORDER BY started_at DESC LIMIT ?",
		limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var out []Outage
	for rows.Next() {
		var o Outage
		var endedAt sql.NullInt64
		var dur sql.NullInt64
		var targetsJSON string
		if rows.Scan(&o.ID, &o.StartedAt, &endedAt, &dur, &targetsJSON) == nil {
			if endedAt.Valid {
				o.EndedAt = &endedAt.Int64
			}
			if dur.Valid {
				o.DurationS = &dur.Int64
			}
			_ = json.Unmarshal([]byte(targetsJSON), &o.TargetsDown)
			out = append(out, o)
		}
	}
	return out
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
