package clientbw

import (
	"database/sql"
	"log"
	"time"
)

const (
	RawRetentionMS    = 7 * 24 * 60 * 60 * 1000
	BucketRetentionMS = 365 * 24 * 60 * 60 * 1000
	BucketMS          = 5 * 60 * 1000
)

// client_bw_raw guarda una muestra por (mac, router_id, ts): los bytes del
// INTERVALO (delta entre contadores absolutos de dos sondeos) y el rate medio
// de ese intervalo (rx_bps = delta*8/dt). El server calcula ambos en la
// ingesta desde los contadores acumulados que reporta la fuente (nlbwmon o
// hostapd bytes por estación, issue #551).
const schemaSQL = `
CREATE TABLE IF NOT EXISTS client_bw_raw (
  mac TEXT NOT NULL,
  router_id TEXT NOT NULL,
  ts INTEGER NOT NULL,
  rx_bytes INTEGER NOT NULL DEFAULT 0,
  tx_bytes INTEGER NOT NULL DEFAULT 0,
  rx_bps REAL NOT NULL DEFAULT 0,
  tx_bps REAL NOT NULL DEFAULT 0,
  PRIMARY KEY (mac, router_id, ts)
);
CREATE INDEX IF NOT EXISTS idx_client_bw_raw_ts ON client_bw_raw(ts);

CREATE TABLE IF NOT EXISTS client_bw_5m (
  mac TEXT NOT NULL,
  router_id TEXT NOT NULL,
  bucket_ts INTEGER NOT NULL,
  n INTEGER NOT NULL,
  rx_bytes INTEGER NOT NULL DEFAULT 0,
  tx_bytes INTEGER NOT NULL DEFAULT 0,
  rx_bps_min REAL NOT NULL DEFAULT 0,
  rx_bps_max REAL NOT NULL DEFAULT 0,
  rx_bps_avg REAL NOT NULL DEFAULT 0,
  tx_bps_min REAL NOT NULL DEFAULT 0,
  tx_bps_max REAL NOT NULL DEFAULT 0,
  tx_bps_avg REAL NOT NULL DEFAULT 0,
  PRIMARY KEY (mac, router_id, bucket_ts)
);
CREATE INDEX IF NOT EXISTS idx_client_bw_5m_ts ON client_bw_5m(bucket_ts);

CREATE TABLE IF NOT EXISTS client_bw_daily (
  mac TEXT NOT NULL,
  router_id TEXT NOT NULL,
  date TEXT NOT NULL,
  n INTEGER NOT NULL,
  rx_bytes INTEGER NOT NULL DEFAULT 0,
  tx_bytes INTEGER NOT NULL DEFAULT 0,
  rx_bps_avg REAL NOT NULL DEFAULT 0,
  tx_bps_avg REAL NOT NULL DEFAULT 0,
  PRIMARY KEY (mac, router_id, date)
);
`

// Sample es una muestra de tráfico de UN cliente en UN router: bytes del
// intervalo (delta) y rate medio del intervalo en bps.
type Sample struct {
	MAC      string
	RouterID string
	TS       time.Time
	RxBytes  uint64
	TxBytes  uint64
	RxBps    float64
	TxBps    float64
}

type Point struct {
	TS      time.Time `json:"ts"`
	RxBytes uint64    `json:"rxBytes"`
	TxBytes uint64    `json:"txBytes"`
	RxBps   float64   `json:"rxBps"`
	TxBps   float64   `json:"txBps"`
}

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) (*Store, error) {
	if db != nil {
		if _, err := db.Exec(schemaSQL); err != nil {
			return nil, err
		}
		// Migración tolerante: DBs creadas antes del #551 no tienen las
		// columnas bps en raw (el esquema viejo las ignoraba).
		ensureColumn(db, "client_bw_raw", "rx_bps", "ALTER TABLE client_bw_raw ADD COLUMN rx_bps REAL NOT NULL DEFAULT 0")
		ensureColumn(db, "client_bw_raw", "tx_bps", "ALTER TABLE client_bw_raw ADD COLUMN tx_bps REAL NOT NULL DEFAULT 0")
	}
	return &Store{db: db}, nil
}

func ensureColumn(db *sql.DB, table, column, alterSQL string) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err == nil && name == column {
			return
		}
	}
	_, _ = db.Exec(alterSQL)
}

func (s *Store) Insert(sample Sample) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO client_bw_raw (mac, router_id, ts, rx_bytes, tx_bytes, rx_bps, tx_bps)
		 VALUES (?,?,?,?,?,?,?)`,
		sample.MAC, sample.RouterID, sample.TS.UnixMilli(), sample.RxBytes, sample.TxBytes,
		sample.RxBps, sample.TxBps)
	return err
}

func (s *Store) GetSeries(mac, routerID string, from, to time.Time, resolution string) ([]Point, error) {
	if s.db == nil {
		return nil, nil
	}
	fromMs, toMs := from.UnixMilli(), to.UnixMilli()

	switch resolution {
	case "raw":
		return s.queryRaw(mac, routerID, fromMs, toMs)
	case "5m":
		return s.query5m(mac, routerID, fromMs, toMs)
	case "daily":
		return s.queryDaily(mac, routerID, from, to)
	default:
		return s.queryRaw(mac, routerID, fromMs, toMs)
	}
}

// GetMACSeries devuelve la serie de un cliente (MAC) agregando a través de
// TODOS los routers: un dispositivo puede verse en varios APs a lo largo del
// día (roaming), y la UI del detalle quiere el total, no el de un router.
func (s *Store) GetMACSeries(mac string, from, to time.Time, resolution string) ([]Point, error) {
	if s.db == nil {
		return nil, nil
	}
	fromMs, toMs := from.UnixMilli(), to.UnixMilli()
	switch resolution {
	case "raw":
		return s.queryRawMAC(mac, fromMs, toMs)
	case "5m":
		return s.query5mMAC(mac, fromMs, toMs)
	case "daily":
		return s.queryDailyMAC(mac, from, to)
	default:
		return s.queryRawMAC(mac, fromMs, toMs)
	}
}

func (s *Store) queryRawMAC(mac string, fromMs, toMs int64) ([]Point, error) {
	rows, err := s.db.Query(
		`SELECT ts, SUM(rx_bytes), SUM(tx_bytes), AVG(rx_bps), AVG(tx_bps)
		 FROM client_bw_raw
		 WHERE mac = ? AND ts >= ? AND ts <= ?
		 GROUP BY ts ORDER BY ts`, mac, fromMs, toMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Point
	for rows.Next() {
		var p Point
		var ts int64
		if err := rows.Scan(&ts, &p.RxBytes, &p.TxBytes, &p.RxBps, &p.TxBps); err == nil {
			p.TS = time.UnixMilli(ts)
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

func (s *Store) query5mMAC(mac string, fromMs, toMs int64) ([]Point, error) {
	rows, err := s.db.Query(
		`SELECT bucket_ts, SUM(rx_bytes), SUM(tx_bytes), AVG(rx_bps_avg), AVG(tx_bps_avg)
		 FROM client_bw_5m
		 WHERE mac = ? AND bucket_ts >= ? AND bucket_ts <= ?
		 GROUP BY bucket_ts ORDER BY bucket_ts`, mac, fromMs, toMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Point
	for rows.Next() {
		var p Point
		var ts int64
		if err := rows.Scan(&ts, &p.RxBytes, &p.TxBytes, &p.RxBps, &p.TxBps); err == nil {
			p.TS = time.UnixMilli(ts)
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

func (s *Store) queryDailyMAC(mac string, from, to time.Time) ([]Point, error) {
	fromDate := from.Format("2006-01-02")
	toDate := to.Format("2006-01-02")
	rows, err := s.db.Query(
		`SELECT date, SUM(rx_bytes), SUM(tx_bytes), AVG(rx_bps_avg), AVG(tx_bps_avg)
		 FROM client_bw_daily
		 WHERE mac = ? AND date >= ? AND date <= ?
		 GROUP BY date ORDER BY date`, mac, fromDate, toDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Point
	for rows.Next() {
		var p Point
		var date string
		if err := rows.Scan(&date, &p.RxBytes, &p.TxBytes, &p.RxBps, &p.TxBps); err == nil {
			p.TS, _ = time.Parse("2006-01-02", date)
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

func (s *Store) queryRaw(mac, routerID string, fromMs, toMs int64) ([]Point, error) {
	rows, err := s.db.Query(
		`SELECT ts, rx_bytes, tx_bytes, rx_bps, tx_bps FROM client_bw_raw
		 WHERE mac = ? AND router_id = ? AND ts >= ? AND ts <= ?
		 ORDER BY ts`, mac, routerID, fromMs, toMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Point
	for rows.Next() {
		var p Point
		var ts int64
		if err := rows.Scan(&ts, &p.RxBytes, &p.TxBytes, &p.RxBps, &p.TxBps); err == nil {
			p.TS = time.UnixMilli(ts)
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

func (s *Store) query5m(mac, routerID string, fromMs, toMs int64) ([]Point, error) {
	rows, err := s.db.Query(
		`SELECT bucket_ts, rx_bytes, tx_bytes, rx_bps_avg, tx_bps_avg FROM client_bw_5m
		 WHERE mac = ? AND router_id = ? AND bucket_ts >= ? AND bucket_ts <= ?
		 ORDER BY bucket_ts`, mac, routerID, fromMs, toMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Point
	for rows.Next() {
		var p Point
		var ts int64
		if err := rows.Scan(&ts, &p.RxBytes, &p.TxBytes, &p.RxBps, &p.TxBps); err == nil {
			p.TS = time.UnixMilli(ts)
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

func (s *Store) queryDaily(mac, routerID string, from, to time.Time) ([]Point, error) {
	fromDate := from.Format("2006-01-02")
	toDate := to.Format("2006-01-02")
	rows, err := s.db.Query(
		`SELECT date, rx_bytes, tx_bytes, rx_bps_avg, tx_bps_avg FROM client_bw_daily
		 WHERE mac = ? AND router_id = ? AND date >= ? AND date <= ?
		 ORDER BY date`, mac, routerID, fromDate, toDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Point
	for rows.Next() {
		var p Point
		var date string
		if err := rows.Scan(&date, &p.RxBytes, &p.TxBytes, &p.RxBps, &p.TxBps); err == nil {
			p.TS, _ = time.Parse("2006-01-02", date)
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

func (s *Store) RollupRawTo5m(window time.Duration) error {
	if s.db == nil {
		return nil
	}
	since := time.Now().Add(-window).UnixMilli()
	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO client_bw_5m
			(mac, router_id, bucket_ts, n, rx_bytes, tx_bytes,
			 rx_bps_min, rx_bps_max, rx_bps_avg, tx_bps_min, tx_bps_max, tx_bps_avg)
		SELECT
			mac, router_id, (ts / ?) * ?, COUNT(*),
			SUM(rx_bytes), SUM(tx_bytes),
			MIN(rx_bps), MAX(rx_bps), AVG(rx_bps),
			MIN(tx_bps), MAX(tx_bps), AVG(tx_bps)
		FROM client_bw_raw
		WHERE ts >= ?
		GROUP BY mac, router_id, (ts / ?)`,
		BucketMS, BucketMS, since, BucketMS)
	return err
}

func (s *Store) Rollup5mToDaily(window time.Duration) error {
	if s.db == nil {
		return nil
	}
	since := time.Now().Add(-window).UnixMilli()
	_, err := s.db.Exec(`
		INSERT OR REPLACE INTO client_bw_daily
			(mac, router_id, date, n, rx_bytes, tx_bytes, rx_bps_avg, tx_bps_avg)
		SELECT
			mac, router_id, strftime('%Y-%m-%d', bucket_ts / 1000, 'unixepoch'),
			SUM(n), SUM(rx_bytes), SUM(tx_bytes), AVG(rx_bps_avg), AVG(tx_bps_avg)
		FROM client_bw_5m
		WHERE bucket_ts >= ?
		GROUP BY mac, router_id, strftime('%Y-%m-%d', bucket_ts / 1000, 'unixepoch')`,
		since)
	return err
}

func (s *Store) Purge() error {
	if s.db == nil {
		return nil
	}
	now := time.Now()
	rawCutoff := now.Add(-time.Duration(RawRetentionMS) * time.Millisecond).UnixMilli()
	bucketCutoff := now.Add(-time.Duration(BucketRetentionMS) * time.Millisecond).UnixMilli()
	_, _ = s.db.Exec("DELETE FROM client_bw_raw WHERE ts < ?", rawCutoff)
	_, _ = s.db.Exec("DELETE FROM client_bw_5m WHERE bucket_ts < ?", bucketCutoff)
	return nil
}

// NightlyJob: escalera de rollup + purga (patrón portseries). La agrega
// DB.NightlyJob tras los rollups de métricas.
func (s *Store) NightlyJob() {
	start := time.Now()
	log.Printf("[netpulse:clientbw] nightly rollup: start")
	if err := s.RollupRawTo5m(48 * time.Hour); err != nil {
		log.Printf("[netpulse:clientbw] rollup raw->5m error: %v", err)
	}
	if err := s.Rollup5mToDaily(35 * 24 * time.Hour); err != nil {
		log.Printf("[netpulse:clientbw] rollup 5m->daily error: %v", err)
	}
	if err := s.Purge(); err != nil {
		log.Printf("[netpulse:clientbw] purge error: %v", err)
	}
	log.Printf("[netpulse:clientbw] nightly rollup: done (%s)", time.Since(start).Round(time.Millisecond))
}

func (s *Store) TopClients(routerID string, since time.Time, limit int) ([]TopClient, error) {
	if s.db == nil {
		return nil, nil
	}
	rows, err := s.db.Query(
		`SELECT mac, SUM(rx_bytes) as rx, SUM(tx_bytes) as tx
		 FROM client_bw_raw WHERE router_id = ? AND ts >= ?
		 GROUP BY mac ORDER BY (rx + tx) DESC LIMIT ?`,
		routerID, since.UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TopClient
	for rows.Next() {
		var tc TopClient
		if err := rows.Scan(&tc.MAC, &tc.RxBytes, &tc.TxBytes); err == nil {
			out = append(out, tc)
		}
	}
	return out, rows.Err()
}

type TopClient struct {
	MAC     string `json:"mac"`
	RxBytes uint64 `json:"rxBytes"`
	TxBytes uint64 `json:"txBytes"`
}
