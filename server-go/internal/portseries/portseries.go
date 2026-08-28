// Package portseries: per-port time series with tiered retention (issue #302).
// raw (7d) -> buckets 5m (1 year) -> daily (forever).
package portseries

import (
	"database/sql"
	"fmt"
	"log"
	"time"
)

const (
	RawRetentionMS     = 7 * 24 * 60 * 60 * 1000
	BucketRetentionMS  = 365 * 24 * 60 * 60 * 1000
	BucketMS           = 5 * 60 * 1000
)

// PortSample is a single data point for a port.
type PortSample struct {
	RouterID  string
	PortID    string
	TS        time.Time
	RxBytes   uint64
	TxBytes   uint64
	RxErrors  uint64
	TxErrors  uint64
	RxFrames  uint64
	TxFrames  uint64
	RxBps     float64
	TxBps     float64
	SpeedMbps int
}

// PortPoint is a time-series point returned by GetSeries.
type PortPoint struct {
	TS        time.Time `json:"ts"`
	RxBytes   uint64    `json:"rxBytes"`
	TxBytes   uint64    `json:"txBytes"`
	RxErrors  uint64    `json:"rxErrors"`
	TxErrors  uint64    `json:"txErrors"`
	RxBps     float64   `json:"rxBps"`
	TxBps     float64   `json:"txBps"`
	SpeedMbps int       `json:"speedMbps"`
}

// SchemaSQL returns the DDL for the port series tables.
func SchemaSQL() string {
	return `
CREATE TABLE IF NOT EXISTS port_series_raw (
  router_id TEXT NOT NULL,
  port_id TEXT NOT NULL,
  ts INTEGER NOT NULL,
  rx_bytes INTEGER NOT NULL DEFAULT 0,
  tx_bytes INTEGER NOT NULL DEFAULT 0,
  rx_errors INTEGER NOT NULL DEFAULT 0,
  tx_errors INTEGER NOT NULL DEFAULT 0,
  rx_frames INTEGER NOT NULL DEFAULT 0,
  tx_frames INTEGER NOT NULL DEFAULT 0,
  rx_bps REAL NOT NULL DEFAULT 0,
  tx_bps REAL NOT NULL DEFAULT 0,
  speed_mbps INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (router_id, port_id, ts)
);
CREATE INDEX IF NOT EXISTS idx_port_series_raw_ts ON port_series_raw(ts);
CREATE INDEX IF NOT EXISTS idx_port_series_raw_router_port_ts ON port_series_raw(router_id, port_id, ts);

CREATE TABLE IF NOT EXISTS port_series_5m (
  router_id TEXT NOT NULL,
  port_id TEXT NOT NULL,
  bucket_ts INTEGER NOT NULL,
  n INTEGER NOT NULL,
  rx_bytes INTEGER NOT NULL DEFAULT 0,
  tx_bytes INTEGER NOT NULL DEFAULT 0,
  rx_errors INTEGER NOT NULL DEFAULT 0,
  tx_errors INTEGER NOT NULL DEFAULT 0,
  rx_frames INTEGER NOT NULL DEFAULT 0,
  tx_frames INTEGER NOT NULL DEFAULT 0,
  rx_bps_min REAL NOT NULL DEFAULT 0,
  rx_bps_max REAL NOT NULL DEFAULT 0,
  rx_bps_avg REAL NOT NULL DEFAULT 0,
  tx_bps_min REAL NOT NULL DEFAULT 0,
  tx_bps_max REAL NOT NULL DEFAULT 0,
  tx_bps_avg REAL NOT NULL DEFAULT 0,
  speed_mbps INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (router_id, port_id, bucket_ts)
);
CREATE INDEX IF NOT EXISTS idx_port_series_5m_ts ON port_series_5m(bucket_ts);

CREATE TABLE IF NOT EXISTS port_series_daily (
  router_id TEXT NOT NULL,
  port_id TEXT NOT NULL,
  date TEXT NOT NULL,
  n INTEGER NOT NULL,
  rx_bytes INTEGER NOT NULL DEFAULT 0,
  tx_bytes INTEGER NOT NULL DEFAULT 0,
  rx_errors INTEGER NOT NULL DEFAULT 0,
  tx_errors INTEGER NOT NULL DEFAULT 0,
  rx_frames INTEGER NOT NULL DEFAULT 0,
  tx_frames INTEGER NOT NULL DEFAULT 0,
  rx_bps_avg REAL NOT NULL DEFAULT 0,
  tx_bps_avg REAL NOT NULL DEFAULT 0,
  speed_mbps INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (router_id, port_id, date)
);
`
}

// Store wraps a *sql.DB for port series operations.
type Store struct {
	db *sql.DB
}

// NewStore creates a Store and ensures tables exist.
func NewStore(db *sql.DB) (*Store, error) {
	if _, err := db.Exec(SchemaSQL()); err != nil {
		return nil, fmt.Errorf("port_series schema: %w", err)
	}
	return &Store{db: db}, nil
}

// RecordSample inserts a raw sample.
func (s *Store) RecordSample(sample PortSample) error {
	if sample.RouterID == "" || sample.PortID == "" {
		return nil
	}
	tsMs := sample.TS.UnixMilli()
	_, err := s.db.Exec(`INSERT OR REPLACE INTO port_series_raw
		(router_id, port_id, ts, rx_bytes, tx_bytes, rx_errors, tx_errors,
		 rx_frames, tx_frames, rx_bps, tx_bps, speed_mbps)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sample.RouterID, sample.PortID, tsMs,
		sample.RxBytes, sample.TxBytes, sample.RxErrors, sample.TxErrors,
		sample.RxFrames, sample.TxFrames, sample.RxBps, sample.TxBps,
		sample.SpeedMbps)
	return err
}

// Resolution selects the tier based on time range.
func Resolution(from, to time.Time) string {
	dur := to.Sub(from)
	if dur <= 24*time.Hour {
		return "raw"
	}
	if dur <= 30*24*time.Hour {
		return "5m"
	}
	return "daily"
}

// GetSeries reads points for a port in [from, to] at the given resolution.
func (s *Store) GetSeries(routerID, portID string, from, to time.Time, resolution string) ([]PortPoint, error) {
	fromMs := from.UnixMilli()
	toMs := to.UnixMilli()
	switch resolution {
	case "raw":
		return s.getRaw(routerID, portID, fromMs, toMs)
	case "5m":
		return s.get5m(routerID, portID, fromMs, toMs)
	case "daily":
		return s.getDaily(routerID, portID, from, to)
	default:
		return nil, fmt.Errorf("unknown resolution: %s", resolution)
	}
}

func (s *Store) getRaw(routerID, portID string, fromMs, toMs int64) ([]PortPoint, error) {
	rows, err := s.db.Query(`SELECT ts, rx_bytes, tx_bytes, rx_errors, tx_errors,
		rx_bps, tx_bps, speed_mbps FROM port_series_raw
		WHERE router_id=? AND port_id=? AND ts>=? AND ts<=?
		ORDER BY ts`, routerID, portID, fromMs, toMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PortPoint
	for rows.Next() {
		var tsMs int64
		var p PortPoint
		if err := rows.Scan(&tsMs, &p.RxBytes, &p.TxBytes, &p.RxErrors, &p.TxErrors,
			&p.RxBps, &p.TxBps, &p.SpeedMbps); err != nil {
			return nil, err
		}
		p.TS = time.UnixMilli(tsMs)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) get5m(routerID, portID string, fromMs, toMs int64) ([]PortPoint, error) {
	rows, err := s.db.Query(`SELECT bucket_ts, rx_bytes, tx_bytes, rx_errors, tx_errors,
		rx_bps_avg, tx_bps_avg, speed_mbps FROM port_series_5m
		WHERE router_id=? AND port_id=? AND bucket_ts>=? AND bucket_ts<=?
		ORDER BY bucket_ts`, routerID, portID, fromMs, toMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PortPoint
	for rows.Next() {
		var tsMs int64
		var p PortPoint
		if err := rows.Scan(&tsMs, &p.RxBytes, &p.TxBytes, &p.RxErrors, &p.TxErrors,
			&p.RxBps, &p.TxBps, &p.SpeedMbps); err != nil {
			return nil, err
		}
		p.TS = time.UnixMilli(tsMs)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) getDaily(routerID, portID string, from, to time.Time) ([]PortPoint, error) {
	fromDate := from.UTC().Format("2006-01-02")
	toDate := to.UTC().Format("2006-01-02")
	rows, err := s.db.Query(`SELECT date, rx_bytes, tx_bytes, rx_errors, tx_errors,
		rx_bps_avg, tx_bps_avg, speed_mbps FROM port_series_daily
		WHERE router_id=? AND port_id=? AND date>=? AND date<=?
		ORDER BY date`, routerID, portID, fromDate, toDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PortPoint
	for rows.Next() {
		var dateStr string
		var p PortPoint
		if err := rows.Scan(&dateStr, &p.RxBytes, &p.TxBytes, &p.RxErrors, &p.TxErrors,
			&p.RxBps, &p.TxBps, &p.SpeedMbps); err != nil {
			return nil, err
		}
		t, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}
		p.TS = t
		out = append(out, p)
	}
	return out, rows.Err()
}

// RollupRawTo5m aggregates raw samples into 5-min buckets.
func (s *Store) RollupRawTo5m(window time.Duration) error {
	since := time.Now().Add(-window).UnixMilli()
	_, err := s.db.Exec(`INSERT OR REPLACE INTO port_series_5m
		(router_id, port_id, bucket_ts, n,
		 rx_bytes, tx_bytes, rx_errors, tx_errors, rx_frames, tx_frames,
		 rx_bps_min, rx_bps_max, rx_bps_avg,
		 tx_bps_min, tx_bps_max, tx_bps_avg, speed_mbps)
		SELECT router_id, port_id,
			(ts / ?) * ?,
			COUNT(*),
			MAX(rx_bytes), MAX(tx_bytes), MAX(rx_errors), MAX(tx_errors),
			MAX(rx_frames), MAX(tx_frames),
			MIN(rx_bps), MAX(rx_bps), AVG(rx_bps),
			MIN(tx_bps), MAX(tx_bps), AVG(tx_bps),
			MAX(speed_mbps)
		FROM port_series_raw
		WHERE ts >= ?
		GROUP BY router_id, port_id, (ts / ?) * ?`,
		BucketMS, BucketMS, since, BucketMS, BucketMS)
	return err
}

// Rollup5mToDaily aggregates 5-m buckets into daily rows.
func (s *Store) Rollup5mToDaily(window time.Duration) error {
	since := time.Now().Add(-window).UnixMilli()
	_, err := s.db.Exec(`INSERT OR REPLACE INTO port_series_daily
		(router_id, port_id, date, n,
		 rx_bytes, tx_bytes, rx_errors, tx_errors, rx_frames, tx_frames,
		 rx_bps_avg, tx_bps_avg, speed_mbps)
		SELECT router_id, port_id,
			strftime('%Y-%m-%d', bucket_ts / 1000, 'unixepoch'),
			SUM(n),
			MAX(rx_bytes), MAX(tx_bytes), MAX(rx_errors), MAX(tx_errors),
			MAX(rx_frames), MAX(tx_frames),
			AVG(rx_bps_avg), AVG(tx_bps_avg),
			MAX(speed_mbps)
		FROM port_series_5m
		WHERE bucket_ts >= ?
		GROUP BY router_id, port_id, strftime('%Y-%m-%d', bucket_ts / 1000, 'unixepoch')`,
		since)
	return err
}

// PurgeRaw deletes raw samples older than retention.
func (s *Store) PurgeRaw() (int64, error) {
	cutoff := time.Now().UnixMilli() - RawRetentionMS
	res, err := s.db.Exec("DELETE FROM port_series_raw WHERE ts < ?", cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Purge5m deletes 5-m buckets older than retention.
func (s *Store) Purge5m() (int64, error) {
	cutoff := time.Now().UnixMilli() - BucketRetentionMS
	res, err := s.db.Exec("DELETE FROM port_series_5m WHERE bucket_ts < ?", cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// NightlyJob runs the full rollup + purge cycle.
func (s *Store) NightlyJob() {
	start := time.Now()
	log.Printf("[netpulse:portseries] nightly rollup: start")

	if err := s.RollupRawTo5m(48 * time.Hour); err != nil {
		log.Printf("[netpulse:portseries] rollup raw->5m error: %v", err)
	}
	if err := s.Rollup5mToDaily(35 * 24 * time.Hour); err != nil {
		log.Printf("[netpulse:portseries] rollup 5m->daily error: %v", err)
	}
	if n, err := s.PurgeRaw(); err != nil {
		log.Printf("[netpulse:portseries] purge raw error: %v", err)
	} else if n > 0 {
		log.Printf("[netpulse:portseries] purged %d raw samples", n)
	}
	if n, err := s.Purge5m(); err != nil {
		log.Printf("[netpulse:portseries] purge 5m error: %v", err)
	} else if n > 0 {
		log.Printf("[netpulse:portseries] purged %d 5m buckets", n)
	}

	log.Printf("[netpulse:portseries] nightly rollup: done (%s)", time.Since(start).Round(time.Millisecond))
}
