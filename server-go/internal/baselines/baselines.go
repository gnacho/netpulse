package baselines

import (
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

const (
	baselinesKey = "baselines.v1"
	alpha        = 0.05
	buckets      = 168
)

type Bucket struct {
	Mean float64 `json:"mean"`
	Var  float64 `json:"var"`
	N    int     `json:"n"`
}

type MetricBaseline struct {
	Buckets [buckets]Bucket `json:"buckets"`
}

type Store struct {
	mu   sync.RWMutex
	data map[string]map[string]*MetricBaseline
	db   *db.DB
}

func NewStore(d *db.DB) *Store {
	s := &Store{
		data: map[string]map[string]*MetricBaseline{},
		db:   d,
	}
	s.load()
	return s
}

func (s *Store) load() {
	if s.db == nil {
		return
	}
	var raw string
	if err := s.db.QueryRow("SELECT value FROM kv WHERE key = ?", baselinesKey).Scan(&raw); err != nil {
		return
	}
	var loaded map[string]map[string]*MetricBaseline
	if json.Unmarshal([]byte(raw), &loaded) == nil {
		s.data = loaded
	}
}

func (s *Store) save() {
	if s.db == nil {
		return
	}
	raw, err := json.Marshal(s.data)
	if err != nil {
		return
	}
	_, _ = s.db.Exec(
		"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value",
		baselinesKey, string(raw))
}

func hourOfWeek(t time.Time) int {
	return int(t.Weekday())*24 + t.Hour()
}

func (s *Store) Observe(routerID, metric string, value float64, t time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.data[routerID] == nil {
		s.data[routerID] = map[string]*MetricBaseline{}
	}
	mb := s.data[routerID][metric]
	if mb == nil {
		mb = &MetricBaseline{}
		s.data[routerID][metric] = mb
	}

	b := hourOfWeek(t)
	bucket := &mb.Buckets[b]
	bucket.N++

	if bucket.N == 1 {
		bucket.Mean = value
		bucket.Var = 0
		return
	}

	delta := value - bucket.Mean
	bucket.Mean += alpha * delta
	bucket.Var = (1-alpha)*bucket.Var + alpha*delta*delta
}

func (s *Store) Check(routerID, metric string, value float64, t time.Time) (anomaly bool, sigma float64) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	mb := s.data[routerID]
	if mb == nil {
		return false, 0
	}
	b := mb[metric]
	if b == nil {
		return false, 0
	}

	bucket := &b.Buckets[hourOfWeek(t)]
	if bucket.N < 10 {
		return false, 0
	}

	stdDev := math.Sqrt(bucket.Var)
	if stdDev < 1e-9 {
		return false, 0
	}

	zScore := (value - bucket.Mean) / stdDev
	return zScore > 3, zScore
}

func (s *Store) Snapshot() map[string]map[string]map[string]Bucket {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := map[string]map[string]map[string]Bucket{}
	for rid, metrics := range s.data {
		out[rid] = map[string]map[string]Bucket{}
		for metric, mb := range metrics {
			out[rid][metric] = map[string]Bucket{}
			for i := 0; i < buckets; i++ {
				if mb.Buckets[i].N > 0 {
					key := fmt.Sprintf("%d", i)
					out[rid][metric][key] = mb.Buckets[i]
				}
			}
		}
	}
	return out
}
