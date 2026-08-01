// timeseries.go — módulo de referencia: series temporales en SQLite (Go + modernc.org/sqlite)
// Uso:
//
//	ts, _ := NewTimeSeries("data/metrics.db")
//	ts.RegisterMetric(Metric{Key: "latency_ms", Unit: "ms", Kind: "gauge", MaxValue: ptr(60000.0)})
//	ts.Write("latency_ms", 12.3)          // buffer; flush cada 1s/50 muestras
//	go ts.RunNightlyJob(ctx, 3, 30)       // job diario 03:30 local (rollup+purga+mantenimiento)
//	ts.Series("latency_ms", from, to, 800)
//	ts.Close()                            // SIGTERM: flush + checkpoint (patrón go-collector-stack)
package timeseries

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	rawRetentionS    = 7 * 86400
	bucketRetentionS = 366 * 86400
	bucketS          = 300
	flushBatch       = 50
	// bufferCap acota la memoria si la DB falla (FS RO/disco lleno): drop-oldest.
	bufferCap = 10000
	// dropLogEvery limita el spam del aviso de drops (como mucho 1 aviso/intervalo).
	dropLogEvery = time.Minute
)

type Metric struct {
	ID       int64
	Key      string
	Unit     string
	Kind     string // "gauge" | "counter"
	MaxValue *float64
	MaxRate  *float64
	MaxDaily *float64
}

type sample struct {
	ts       int64
	metricID int64
	value    float64
}

type TimeSeries struct {
	db          *sql.DB
	mu          sync.Mutex
	metrics     map[string]Metric
	lastGood    map[int64]float64
	buffer      []sample
	dropped     int64 // muestras descartadas por buffer lleno (drop-oldest)
	lastDropLog time.Time
	done        chan struct{}
	wg          sync.WaitGroup
}

func NewTimeSeries(path string) (*TimeSeries, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=temp_store(MEMORY)&_pragma=cache_size(-64000)&_pragma=busy_timeout(5000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite: 1 escritor; serializa también lecturas para evitar SQLITE_BUSY
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, err
	}
	t := &TimeSeries{db: db, metrics: map[string]Metric{}, lastGood: map[int64]float64{}, done: make(chan struct{})}
	rows, err := db.Query("SELECT id, key, unit, kind, max_value, max_rate, max_daily FROM metrics")
	if err != nil {
		db.Close()
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m Metric
		if err := rows.Scan(&m.ID, &m.Key, &m.Unit, &m.Kind, &m.MaxValue, &m.MaxRate, &m.MaxDaily); err != nil {
			db.Close() // no fugues la conexión si el scan inicial falla
			return nil, err
		}
		t.metrics[m.Key] = m
	}
	if err := rows.Err(); err != nil {
		db.Close()
		return nil, err
	}
	t.wg.Add(1)
	go t.flushLoop()
	return t, nil
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS metrics (
  id INTEGER PRIMARY KEY, key TEXT NOT NULL UNIQUE, unit TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'gauge',
  max_value REAL, max_rate REAL, max_daily REAL
);
CREATE TABLE IF NOT EXISTS samples (
  ts INTEGER NOT NULL, metric_id INTEGER NOT NULL REFERENCES metrics(id), value REAL NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_samples_metric_ts ON samples(metric_id, ts);
CREATE TABLE IF NOT EXISTS buckets (
  bucket_ts INTEGER NOT NULL, metric_id INTEGER NOT NULL REFERENCES metrics(id),
  n INTEGER NOT NULL, min REAL NOT NULL, max REAL NOT NULL, avg REAL NOT NULL, sum REAL NOT NULL,
  PRIMARY KEY (metric_id, bucket_ts)
);
CREATE TABLE IF NOT EXISTS daily (
  date TEXT NOT NULL, metric_id INTEGER NOT NULL REFERENCES metrics(id),
  n INTEGER NOT NULL, min REAL NOT NULL, max REAL NOT NULL, avg REAL NOT NULL, sum REAL NOT NULL,
  PRIMARY KEY (metric_id, date)
);`

func (t *TimeSeries) RegisterMetric(m Metric) error {
	if _, err := t.db.Exec(`INSERT OR IGNORE INTO metrics (key, unit, kind, max_value, max_rate, max_daily)
	                     VALUES (?, ?, ?, ?, ?, ?)`,
		m.Key, m.Unit, orDefault(m.Kind, "gauge"), m.MaxValue, m.MaxRate, m.MaxDaily); err != nil {
		return err
	}
	if err := t.db.QueryRow("SELECT id FROM metrics WHERE key = ?", m.Key).Scan(&m.ID); err != nil {
		return err
	}
	t.mu.Lock()
	t.metrics[m.Key] = m
	t.mu.Unlock()
	return nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// Write aplica el filtro de sanidad (references/sanidad.md §1) y encola.
func (t *TimeSeries) Write(key string, value float64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	m, ok := t.metrics[key]
	if !ok {
		return false
	}
	if value < 0 {
		slog.Warn("[SANITY] muestra descartada (negativo)", "key", key, "value", value)
		return false
	}
	if m.MaxValue != nil && value > *m.MaxValue {
		slog.Warn("[SANITY] muestra descartada (> max_value)", "key", key, "value", value, "max_value", *m.MaxValue)
		return false
	}
	if m.Kind == "counter" {
		if prev, ok := t.lastGood[m.ID]; ok {
			delta := value - prev
			if delta < 0 {
				slog.Warn("[SANITY] reset de counter, nueva base", "key", key, "prev", prev, "value", value)
			} else if m.MaxRate != nil && delta > *m.MaxRate {
				slog.Warn("[SANITY] muestra descartada (delta > max_rate)", "key", key, "value", value, "delta", delta, "max_rate", *m.MaxRate)
				return false
			}
		}
	}
	t.lastGood[m.ID] = value
	t.buffer = append(t.buffer, sample{ts: time.Now().Unix(), metricID: m.ID, value: value})
	if len(t.buffer) >= flushBatch {
		t.flushLocked()
	}
	t.capBufferLocked()
	return true
}

// capBufferLocked aplica la cota del buffer (drop-oldest): si la DB falla el
// buffer no puede crecer sin límite — crítico en un FS degradado (router).
func (t *TimeSeries) capBufferLocked() {
	if len(t.buffer) <= bufferCap {
		return
	}
	drop := len(t.buffer) - bufferCap
	copy(t.buffer, t.buffer[drop:])
	t.buffer = t.buffer[:bufferCap]
	t.dropped += int64(drop)
	if time.Since(t.lastDropLog) >= dropLogEvery {
		t.lastDropLog = time.Now()
		slog.Warn("buffer lleno: descartando muestras antiguas (drop-oldest)",
			"dropped_total", t.dropped, "cap", bufferCap)
	}
}

// Dropped: total acumulado de muestras descartadas por buffer lleno.
func (t *TimeSeries) Dropped() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.dropped
}

func (t *TimeSeries) flushLoop() {
	defer t.wg.Done()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-t.done:
			return
		case <-tick.C:
			t.mu.Lock()
			t.flushLocked()
			t.mu.Unlock()
		}
	}
}

func (t *TimeSeries) flushLocked() {
	if len(t.buffer) == 0 {
		return
	}
	tx, err := t.db.Begin()
	if err != nil {
		slog.Error("flush: begin", "err", err)
		return
	}
	stmt, err := tx.Prepare("INSERT INTO samples (ts, metric_id, value) VALUES (?, ?, ?)")
	if err != nil {
		slog.Error("flush: prepare", "err", err)
		tx.Rollback()
		return
	}
	for _, s := range t.buffer {
		if _, err := stmt.Exec(s.ts, s.metricID, s.value); err != nil {
			slog.Error("flush: insert", "err", err)
		}
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		slog.Error("flush: commit", "err", err)
		return
	}
	t.buffer = t.buffer[:0]
}

// NightlyJob ejecuta el orden obligatorio (references/jobs.md): rollup → purga →
// checkpoint → optimize → VACUUM condicional.
func (t *TimeSeries) NightlyJob() {
	t.mu.Lock()
	t.flushLocked()
	t.mu.Unlock()
	start := time.Now()
	now := time.Now().Unix()

	// Cada paso loguea su error: un fallo nocturno silencioso es inobservable.
	if _, err := t.db.Exec(fmt.Sprintf(`
	  INSERT OR REPLACE INTO buckets (bucket_ts, metric_id, n, min, max, avg, sum)
	  SELECT (ts / %d) * %d, metric_id, COUNT(*), MIN(value), MAX(value), AVG(value),
	         CASE WHEN (SELECT kind FROM metrics WHERE id = metric_id) = 'counter'
	              THEN MAX(value) - MIN(value) ELSE SUM(value) END
	  FROM samples WHERE ts >= ?
	  GROUP BY metric_id, ts / %d`, bucketS, bucketS, bucketS), now-48*3600); err != nil {
		slog.Error("NightlyJob: rollup 5min", "err", err)
	}

	fromDate := time.Unix(now-35*86400, 0).UTC().Format("2006-01-02")
	if _, err := t.db.Exec(`
	  INSERT OR REPLACE INTO daily (date, metric_id, n, min, max, avg, sum)
	  SELECT date(bucket_ts, 'unixepoch'), metric_id, SUM(n), MIN(min), MAX(max),
	         SUM(sum) * 1.0 / SUM(n), SUM(sum)
	  FROM buckets WHERE date(bucket_ts, 'unixepoch') >= ?
	  GROUP BY metric_id, date(bucket_ts, 'unixepoch')`, fromDate); err != nil {
		slog.Error("NightlyJob: rollup diario", "err", err)
	}

	if _, err := t.db.Exec("DELETE FROM samples WHERE ts < ?", now-rawRetentionS); err != nil {
		slog.Error("NightlyJob: purga raw", "err", err)
	}
	if _, err := t.db.Exec("DELETE FROM buckets WHERE bucket_ts < ?", now-bucketRetentionS); err != nil {
		slog.Error("NightlyJob: purga buckets", "err", err)
	}
	if _, err := t.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		slog.Error("NightlyJob: checkpoint", "err", err)
	}
	if _, err := t.db.Exec("PRAGMA optimize"); err != nil {
		slog.Error("NightlyJob: optimize", "err", err)
	}
	if _, err := t.db.Exec("ANALYZE"); err != nil {
		slog.Error("NightlyJob: analyze", "err", err)
	}
	var freelist int
	if err := t.db.QueryRow("PRAGMA freelist_count").Scan(&freelist); err != nil {
		slog.Error("NightlyJob: freelist_count", "err", err)
	}
	if freelist > 1000 {
		if _, err := t.db.Exec("VACUUM"); err != nil {
			slog.Error("NightlyJob: vacuum", "err", err)
		}
	}
	slog.Info("NightlyJob completado", "duracion", time.Since(start), "freelist", freelist)
}

// RunNightlyJob programa el job a hour:min local cada día hasta ctx.Done().
func (t *TimeSeries) RunNightlyJob(ctx context.Context, hour, min int) {
	t.wg.Add(1)
	defer t.wg.Done()
	for {
		now := time.Now()
		next := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, now.Location())
		if !next.After(now) {
			next = next.Add(24 * time.Hour)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
			t.NightlyJob()
		}
	}
}

type Point [2]float64 // [ts, value]

// Series devuelve la serie downsampleada (fuente según rango, references/series-api.md).
func (t *TimeSeries) Series(key string, from, to int64, points int) (string, []Point, error) {
	t.mu.Lock()
	m, ok := t.metrics[key]
	t.mu.Unlock()
	if !ok {
		return "", nil, fmt.Errorf("métrica desconocida: %s", key)
	}
	var rows *sql.Rows
	var err error
	var resolution string
	switch span := to - from; {
	case span <= 2*86400:
		resolution = "raw"
		rows, err = t.db.Query("SELECT ts, value FROM samples WHERE metric_id = ? AND ts BETWEEN ? AND ? ORDER BY ts", m.ID, from, to)
	case span <= 60*86400:
		resolution = "5m"
		rows, err = t.db.Query("SELECT bucket_ts, avg FROM buckets WHERE metric_id = ? AND bucket_ts BETWEEN ? AND ? ORDER BY bucket_ts", m.ID, from, to)
	default:
		resolution = "1d"
		rows, err = t.db.Query("SELECT CAST(strftime('%s', date) AS INTEGER), avg FROM daily WHERE metric_id = ? AND date BETWEEN date(?, 'unixepoch') AND date(?, 'unixepoch') ORDER BY date", m.ID, from, to)
	}
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	var data []Point
	for rows.Next() {
		var p Point
		if err := rows.Scan(&p[0], &p[1]); err != nil {
			return "", nil, err
		}
		data = append(data, p)
	}
	if points > 2000 {
		points = 2000
	}
	return resolution, LTTB(data, points), nil
}

// Health: tamaño del WAL, freelist y última muestra por métrica (references/jobs.md).
func (t *TimeSeries) Health(dbPath string) map[string]any {
	var walBytes int64
	if st, err := os.Stat(dbPath + "-wal"); err == nil {
		walBytes = st.Size()
	}
	var freelist int
	t.db.QueryRow("PRAGMA freelist_count").Scan(&freelist)
	last := map[int64]int64{}
	rows, err := t.db.Query("SELECT metric_id, MAX(ts) FROM samples GROUP BY metric_id")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id, ts int64
			rows.Scan(&id, &ts)
			last[id] = ts
		}
	}
	t.mu.Lock()
	pending := len(t.buffer)
	dropped := t.dropped
	t.mu.Unlock()
	return map[string]any{
		"wal_bytes": walBytes, "freelist_pages": freelist, "last_sample": last,
		"buffer_pending": pending, "buffer_dropped": dropped,
	}
}

// Close: SIGTERM — para el loop, vuelca el lote pendiente, checkpoint, espera a
// las goroutines (flushLoop/NightlyJob) y SOLO ENTONCES cierra la conexión:
// nadie toca la DB después de db.Close().
func (t *TimeSeries) Close() {
	close(t.done)
	t.mu.Lock()
	t.flushLocked()
	t.mu.Unlock()
	t.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	t.wg.Wait()
	t.db.Close()
}

// LTTB — Largest-Triangle-Three-Buckets; conserva la forma visual de la serie.
func LTTB(data []Point, threshold int) []Point {
	n := len(data)
	if threshold >= n || threshold <= 0 {
		return data
	}
	sampled := make([]Point, 0, threshold)
	sampled = append(sampled, data[0])
	every := float64(n-2) / float64(threshold-2)
	a := 0
	for i := 0; i < threshold-2; i++ {
		rangeStart := int(float64(i+1)*every) + 1
		rangeEnd := int(float64(i+2)*every) + 1
		if rangeEnd > n {
			rangeEnd = n
		}
		var avgX, avgY float64
		for j := rangeStart; j < rangeEnd; j++ {
			avgX += data[j][0]
			avgY += data[j][1]
		}
		avgX /= float64(rangeEnd - rangeStart)
		avgY /= float64(rangeEnd - rangeStart)
		bucketStart := int(float64(i)*every) + 1
		bucketEnd := int(float64(i+1)*every) + 1
		ax, ay := data[a][0], data[a][1]
		maxArea := -1.0
		nextA := bucketStart
		for j := bucketStart; j < bucketEnd; j++ {
			area := abs((ax-avgX)*(data[j][1]-ay) - (ax-data[j][0])*(avgY-ay))
			if area > maxArea {
				maxArea = area
				nextA = j
			}
		}
		sampled = append(sampled, data[nextA])
		a = nextA
	}
	return append(sampled, data[n-1])
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
