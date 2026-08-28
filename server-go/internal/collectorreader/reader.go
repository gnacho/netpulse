package collectorreader

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type Metric struct {
	ID   int64  `json:"id"`
	Key  string `json:"key"`
	Unit string `json:"unit"`
	Kind string `json:"kind"`
}

type Point [2]float64

type Reader struct {
	db *sql.DB
}

func Open(path string) (*Reader, error) {
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(3000)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &Reader{db: db}, nil
}

func (r *Reader) Close() error {
	return r.db.Close()
}

func (r *Reader) ListMetrics() ([]Metric, error) {
	rows, err := r.db.Query("SELECT id, key, unit, kind FROM metrics ORDER BY key")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Metric
	for rows.Next() {
		var m Metric
		if err := rows.Scan(&m.ID, &m.Key, &m.Unit, &m.Kind); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *Reader) Series(key string, from, to int64, points int) (string, []Point, error) {
	var metricID int64
	if err := r.db.QueryRow("SELECT id FROM metrics WHERE key = ?", key).Scan(&metricID); err != nil {
		return "", nil, fmt.Errorf("unknown metric: %s", key)
	}
	var rows *sql.Rows
	var err error
	var resolution string
	switch span := to - from; {
	case span <= 2*86400:
		resolution = "raw"
		rows, err = r.db.Query("SELECT ts, value FROM samples WHERE metric_id = ? AND ts BETWEEN ? AND ? ORDER BY ts", metricID, from, to)
	case span <= 60*86400:
		resolution = "5m"
		rows, err = r.db.Query("SELECT bucket_ts, avg FROM buckets WHERE metric_id = ? AND bucket_ts BETWEEN ? AND ? ORDER BY bucket_ts", metricID, from, to)
	default:
		resolution = "1d"
		rows, err = r.db.Query("SELECT CAST(strftime('%s', date) AS INTEGER), avg FROM daily WHERE metric_id = ? AND date BETWEEN date(?, 'unixepoch') AND date(?, 'unixepoch') ORDER BY date", metricID, from, to)
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
	if err := rows.Err(); err != nil {
		return "", nil, err
	}
	if points > 2000 {
		points = 2000
	}
	if points > 0 && len(data) > points {
		data = lttb(data, points)
	}
	return resolution, data, nil
}

func lttb(data []Point, threshold int) []Point {
	n := len(data)
	if threshold >= n || threshold <= 0 {
		return data
	}
	out := make([]Point, 0, threshold)
	out = append(out, data[0])
	every := float64(n-2) / float64(threshold-2)
	a := 0
	for i := 0; i < threshold-2; i++ {
		rs := int(float64(i+1)*every) + 1
		re := int(float64(i+2)*every) + 1
		if re > n {
			re = n
		}
		var ax, ay float64
		for j := rs; j < re; j++ {
			ax += data[j][0]
			ay += data[j][1]
		}
		ax /= float64(re - rs)
		ay /= float64(re - rs)
		bs := int(float64(i)*every) + 1
		be := int(float64(i+1)*every) + 1
		px, py := data[a][0], data[a][1]
		maxA := -1.0
		na := bs
		for j := bs; j < be; j++ {
			area := abs((px-ax)*(data[j][1]-py) - (px-data[j][0])*(ay-py))
			if area > maxA {
				maxA = area
				na = j
			}
		}
		out = append(out, data[na])
		a = na
	}
	return append(out, data[n-1])
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
