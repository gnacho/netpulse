package collectorreader

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`
		CREATE TABLE metrics (id INTEGER PRIMARY KEY, key TEXT NOT NULL UNIQUE, unit TEXT NOT NULL, kind TEXT NOT NULL DEFAULT 'gauge', max_value REAL, max_rate REAL, max_daily REAL);
		CREATE TABLE samples (ts INTEGER NOT NULL, metric_id INTEGER NOT NULL REFERENCES metrics(id), value REAL NOT NULL);
		CREATE INDEX idx_samples_metric_ts ON samples(metric_id, ts);
		CREATE TABLE buckets (bucket_ts INTEGER NOT NULL, metric_id INTEGER NOT NULL REFERENCES metrics(id), n INTEGER NOT NULL, min REAL NOT NULL, max REAL NOT NULL, avg REAL NOT NULL, sum REAL NOT NULL, PRIMARY KEY (metric_id, bucket_ts));
		CREATE TABLE daily (date TEXT NOT NULL, metric_id INTEGER NOT NULL REFERENCES metrics(id), n INTEGER NOT NULL, min REAL NOT NULL, max REAL NOT NULL, avg REAL NOT NULL, sum REAL NOT NULL, PRIMARY KEY (metric_id, date));
	`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("INSERT INTO metrics (id, key, unit, kind) VALUES (1, 'tcp_latency_ms.test', 'ms', 'gauge')")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	for i := 0; i < 100; i++ {
		ts := now - int64(100-i)*5
		_, err = db.Exec("INSERT INTO samples (ts, metric_id, value) VALUES (?, 1, ?)", ts, float64(10+i%20))
		if err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestOpenAndList(t *testing.T) {
	path := setupTestDB(t)
	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	metrics, err := r.ListMetrics()
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 1 {
		t.Fatalf("expected 1 metric, got %d", len(metrics))
	}
	if metrics[0].Key != "tcp_latency_ms.test" {
		t.Fatalf("unexpected key: %s", metrics[0].Key)
	}
}

func TestSeries(t *testing.T) {
	path := setupTestDB(t)
	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	now := time.Now().Unix()
	res, data, err := r.Series("tcp_latency_ms.test", now-600, now, 500)
	if err != nil {
		t.Fatal(err)
	}
	if res != "raw" {
		t.Fatalf("expected raw resolution, got %s", res)
	}
	if len(data) == 0 {
		t.Fatal("expected data points")
	}
}

func TestSeriesUnknown(t *testing.T) {
	path := setupTestDB(t)
	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	now := time.Now().Unix()
	_, _, err = r.Series("nonexistent", now-600, now, 500)
	if err == nil {
		t.Fatal("expected error for unknown metric")
	}
}

func TestOpenNonexistent(t *testing.T) {
	_, err := Open("/nonexistent/path/metrics.db")
	if err == nil {
		t.Fatal("expected error for nonexistent db")
	}
}

func TestLTTB(t *testing.T) {
	data := make([]Point, 100)
	for i := range data {
		data[i] = Point{float64(i), float64(i % 10)}
	}
	result := lttb(data, 20)
	if len(result) != 20 {
		t.Fatalf("expected 20 points, got %d", len(result))
	}
	if result[0][0] != data[0][0] {
		t.Fatal("first point should be preserved")
	}
	if result[len(result)-1][0] != data[len(data)-1][0] {
		t.Fatal("last point should be preserved")
	}
}

var _ = os.Remove
