package pathanalysis

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

type Hop struct {
	Index   int     `json:"index"`
	Host    string  `json:"host"`
	LossPct float64 `json:"lossPct"`
	AvgMs   float64 `json:"avgMs"`
	BestMs  float64 `json:"bestMs"`
	WorstMs float64 `json:"worstMs"`
	StdevMs float64 `json:"stdevMs"`
}

type PathResult struct {
	ID          int64   `json:"id"`
	RouterID    string  `json:"routerId"`
	Destination string  `json:"destination"`
	TS          int64   `json:"ts"`
	Hops        []Hop   `json:"hops"`
	HopCount    int     `json:"hopCount"`
	TotalMs     float64 `json:"totalMs"`
}

type PathSummary struct {
	Destination string  `json:"destination"`
	LastRun     int64   `json:"lastRun"`
	HopCount    int     `json:"hopCount"`
	TotalMs     float64 `json:"totalMs"`
	WorstHop    int     `json:"worstHop"`
	WorstLoss   float64 `json:"worstLoss"`
}

type Store struct {
	db *db.DB
}

func NewStore(d *db.DB) *Store {
	return &Store{db: d}
}

func EnsureSchema(d *db.DB) error {
	_, err := d.Exec(`
		CREATE TABLE IF NOT EXISTS path_results (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			router_id TEXT NOT NULL,
			destination TEXT NOT NULL,
			ts INTEGER NOT NULL,
			hops_json TEXT NOT NULL,
			hop_count INTEGER NOT NULL,
			total_ms REAL NOT NULL DEFAULT 0
		);
		CREATE INDEX IF NOT EXISTS idx_path_results_lookup ON path_results(router_id, destination, ts DESC);
	`)
	return err
}

func (s *Store) Insert(r PathResult) error {
	hopsJSON, err := json.Marshal(r.Hops)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO path_results (router_id, destination, ts, hops_json, hop_count, total_ms)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		r.RouterID, r.Destination, r.TS, string(hopsJSON), r.HopCount, r.TotalMs,
	)
	return err
}

func (s *Store) Latest(routerID, destination string) (*PathResult, error) {
	var r PathResult
	var hopsJSON string
	err := s.db.QueryRow(
		`SELECT id, router_id, destination, ts, hops_json, hop_count, total_ms
		 FROM path_results WHERE router_id = ? AND destination = ?
		 ORDER BY ts DESC LIMIT 1`,
		routerID, destination,
	).Scan(&r.ID, &r.RouterID, &r.Destination, &r.TS, &hopsJSON, &r.HopCount, &r.TotalMs)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(hopsJSON), &r.Hops); err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Store) History(routerID, destination string, limit int) ([]PathResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.Query(
		`SELECT id, router_id, destination, ts, hops_json, hop_count, total_ms
		 FROM path_results WHERE router_id = ? AND destination = ?
		 ORDER BY ts DESC LIMIT ?`,
		routerID, destination, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PathResult
	for rows.Next() {
		var r PathResult
		var hopsJSON string
		if err := rows.Scan(&r.ID, &r.RouterID, &r.Destination, &r.TS, &hopsJSON, &r.HopCount, &r.TotalMs); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(hopsJSON), &r.Hops); err != nil {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) Summaries(routerID string) ([]PathSummary, error) {
	rows, err := s.db.Query(
		`SELECT destination, MAX(ts), hop_count, total_ms
		 FROM path_results WHERE router_id = ?
		 GROUP BY destination ORDER BY destination`,
		routerID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PathSummary
	for rows.Next() {
		var ps PathSummary
		if err := rows.Scan(&ps.Destination, &ps.LastRun, &ps.HopCount, &ps.TotalMs); err != nil {
			continue
		}
		out = append(out, ps)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Cerrar SIEMPRE el primer cursor antes de las queries anidadas: el
	// server corre con MaxOpenConns(1) y una query dentro del bucle de rows
	// abiertos es un deadlock (lo bloqueaba todo, incluido el CI de release).
	rows.Close()
	for i := range out {
		var hopsJSON string
		err := s.db.QueryRow(
			`SELECT hops_json FROM path_results WHERE router_id = ? AND destination = ? ORDER BY ts DESC LIMIT 1`,
			routerID, out[i].Destination,
		).Scan(&hopsJSON)
		if err != nil {
			continue
		}
		var hops []Hop
		if json.Unmarshal([]byte(hopsJSON), &hops) != nil {
			continue
		}
		for _, h := range hops {
			if h.LossPct > out[i].WorstLoss {
				out[i].WorstLoss = h.LossPct
				out[i].WorstHop = h.Index
			}
		}
	}
	return out, nil
}

func (s *Store) AllDestinations() ([]string, error) {
	rows, err := s.db.Query("SELECT DISTINCT destination FROM path_results ORDER BY destination")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			continue
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) Purge(olderThan time.Duration) (int64, error) {
	cutoff := db.NowMS() - olderThan.Milliseconds()
	res, err := s.db.Exec("DELETE FROM path_results WHERE ts < ?", cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

var _ = sql.ErrNoRows
var _ = fmt.Sprintf
