package configbackup

import (
	"fmt"
	"sync"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

const (
	schemaSQL = `
CREATE TABLE IF NOT EXISTS config_snapshots (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  router_id TEXT NOT NULL,
  snapshot_id TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  size_bytes INTEGER NOT NULL,
  configs TEXT,
  data BLOB NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_config_snapshots_router ON config_snapshots(router_id, created_at DESC);
`
	maxPerRouter = 30
)

type Snapshot struct {
	ID         int64  `json:"id"`
	RouterID   string `json:"routerId"`
	SnapshotID string `json:"snapshotId"`
	CreatedAt  int64  `json:"createdAt"`
	SizeBytes  int64  `json:"sizeBytes"`
	Configs    string `json:"configs,omitempty"`
}

type Store struct {
	mu sync.Mutex
	db *db.DB
}

func NewStore(d *db.DB) (*Store, error) {
	if d != nil {
		if _, err := d.Exec(schemaSQL); err != nil {
			return nil, fmt.Errorf("config_snapshots schema: %w", err)
		}
	}
	return &Store{db: d}, nil
}

func (s *Store) Save(routerID, snapshotID string, configs string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return nil
	}
	_, err := s.db.Exec(
		"INSERT INTO config_snapshots (router_id, snapshot_id, created_at, size_bytes, configs, data) VALUES (?,?,?,?,?,?)",
		routerID, snapshotID, time.Now().UnixMilli(), len(data), configs, data)
	if err != nil {
		return fmt.Errorf("insert snapshot: %w", err)
	}

	s.db.Exec(
		`DELETE FROM config_snapshots WHERE router_id = ? AND id NOT IN (
			SELECT id FROM config_snapshots WHERE router_id = ? ORDER BY created_at DESC LIMIT ?)`,
		routerID, routerID, maxPerRouter)
	return nil
}

func (s *Store) List(routerID string) ([]Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return nil, nil
	}
	rows, err := s.db.Query(
		"SELECT id, router_id, snapshot_id, created_at, size_bytes, configs FROM config_snapshots WHERE router_id = ? ORDER BY created_at DESC",
		routerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Snapshot
	for rows.Next() {
		var snap Snapshot
		if err := rows.Scan(&snap.ID, &snap.RouterID, &snap.SnapshotID, &snap.CreatedAt, &snap.SizeBytes, &snap.Configs); err != nil {
			continue
		}
		out = append(out, snap)
	}
	return out, nil
}

func (s *Store) ListAll() ([]Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return nil, nil
	}
	rows, err := s.db.Query(
		"SELECT id, router_id, snapshot_id, created_at, size_bytes, configs FROM config_snapshots ORDER BY created_at DESC LIMIT 200")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Snapshot
	for rows.Next() {
		var snap Snapshot
		if err := rows.Scan(&snap.ID, &snap.RouterID, &snap.SnapshotID, &snap.CreatedAt, &snap.SizeBytes, &snap.Configs); err != nil {
			continue
		}
		out = append(out, snap)
	}
	return out, nil
}

func (s *Store) Get(id int64) ([]byte, *Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return nil, nil, fmt.Errorf("no db")
	}
	var snap Snapshot
	var data []byte
	err := s.db.QueryRow(
		"SELECT id, router_id, snapshot_id, created_at, size_bytes, configs, data FROM config_snapshots WHERE id = ?",
		id,
	).Scan(&snap.ID, &snap.RouterID, &snap.SnapshotID, &snap.CreatedAt, &snap.SizeBytes, &snap.Configs, &data)
	if err != nil {
		return nil, nil, err
	}
	return data, &snap, nil
}

func (s *Store) Delete(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.db == nil {
		return nil
	}
	_, err := s.db.Exec("DELETE FROM config_snapshots WHERE id = ?", id)
	return err
}
