package rintegration

import (
	"database/sql"
	"time"
)

// Row is one row of the router_integrations table. The pusher password is
// never stored here: it lives only in the on-disk environment file written by
// the Manager.
type Row struct {
	Slug        string
	Vendor      string
	Mode        string
	Host        string
	Username    string
	Version     string
	Asset       string
	Arch        string
	IntervalS   int
	Enabled     bool
	State       string
	LastError   string
	LastStartMs int64
	CreatedAt   int64
	UpdatedAt   int64
}

// Store wraps the router_integrations table (created by db.schemaSQL).
type Store struct {
	db *sql.DB
}

// NewStore builds a Store over an open *sql.DB.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

const integrationCols = "slug, vendor, mode, host, username, version, asset, arch, interval_s, enabled, state, last_error, last_start_ms, created_at, updated_at"

func scanIntegration(row interface{ Scan(...any) error }) (Row, error) {
	var r Row
	var enabled int
	if err := row.Scan(
		&r.Slug, &r.Vendor, &r.Mode, &r.Host, &r.Username, &r.Version, &r.Asset,
		&r.Arch, &r.IntervalS, &enabled, &r.State, &r.LastError, &r.LastStartMs,
		&r.CreatedAt, &r.UpdatedAt,
	); err != nil {
		return Row{}, err
	}
	r.Enabled = enabled == 1
	return r, nil
}

// List returns all integrations ordered by creation time.
func (st *Store) List() ([]Row, error) {
	rows, err := st.db.Query("SELECT " + integrationCols + " FROM router_integrations ORDER BY created_at ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Row{}
	for rows.Next() {
		r, err := scanIntegration(rows)
		if err != nil {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Get returns the integration with the given slug, or ok=false.
func (st *Store) Get(slug string) (Row, bool) {
	r, err := scanIntegration(st.db.QueryRow("SELECT "+integrationCols+" FROM router_integrations WHERE slug = ?", slug))
	if err != nil {
		return Row{}, false
	}
	return r, true
}

// Upsert inserts or fully updates an integration row. created_at is preserved
// on conflict; every other column is replaced.
func (st *Store) Upsert(r Row) error {
	now := time.Now().UnixMilli()
	enabled := 0
	if r.Enabled {
		enabled = 1
	}
	if r.CreatedAt == 0 {
		r.CreatedAt = now
	}
	if r.UpdatedAt == 0 {
		r.UpdatedAt = now
	}
	_, err := st.db.Exec(
		`INSERT INTO router_integrations
			(slug, vendor, mode, host, username, version, asset, arch, interval_s, enabled, state, last_error, last_start_ms, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(slug) DO UPDATE SET
			vendor=excluded.vendor, mode=excluded.mode, host=excluded.host,
			username=excluded.username, version=excluded.version, asset=excluded.asset,
			arch=excluded.arch, interval_s=excluded.interval_s, enabled=excluded.enabled,
			state=excluded.state, last_error=excluded.last_error,
			last_start_ms=excluded.last_start_ms, updated_at=excluded.updated_at`,
		r.Slug, r.Vendor, r.Mode, r.Host, r.Username, r.Version, r.Asset,
		r.Arch, r.IntervalS, enabled, r.State, r.LastError, r.LastStartMs,
		r.CreatedAt, r.UpdatedAt,
	)
	return err
}

// SetState updates state and last_error.
func (st *Store) SetState(slug, state, lastError string) error {
	_, err := st.db.Exec(
		"UPDATE router_integrations SET state = ?, last_error = ?, updated_at = ? WHERE slug = ?",
		state, lastError, time.Now().UnixMilli(), slug,
	)
	return err
}

// SetEnabled updates the enabled flag.
func (st *Store) SetEnabled(slug string, enabled bool) error {
	e := 0
	if enabled {
		e = 1
	}
	_, err := st.db.Exec(
		"UPDATE router_integrations SET enabled = ?, updated_at = ? WHERE slug = ?",
		e, time.Now().UnixMilli(), slug,
	)
	return err
}

// SetVersion records the release that is installed.
func (st *Store) SetVersion(slug, version, asset, arch string) error {
	_, err := st.db.Exec(
		"UPDATE router_integrations SET version = ?, asset = ?, arch = ?, updated_at = ? WHERE slug = ?",
		version, asset, arch, time.Now().UnixMilli(), slug,
	)
	return err
}

// SetLastStart records the last successful process start timestamp (epoch ms).
func (st *Store) SetLastStart(slug string, ms int64) error {
	_, err := st.db.Exec(
		"UPDATE router_integrations SET last_start_ms = ?, updated_at = ? WHERE slug = ?",
		ms, time.Now().UnixMilli(), slug,
	)
	return err
}

// Delete removes the row with the given slug.
func (st *Store) Delete(slug string) error {
	_, err := st.db.Exec("DELETE FROM router_integrations WHERE slug = ?", slug)
	return err
}
