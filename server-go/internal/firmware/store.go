// Package firmware — metadatos y estado de upgrades de firmware OpenWrt (#453).
package firmware

import (
	"database/sql"
	"time"
)

// Target es la información de firmware configurada para un router.
type Target struct {
	RouterID       string `json:"routerId"`
	Model          string `json:"model"`
	CurrentVersion string `json:"currentVersion"`
	TargetVersion  string `json:"targetVersion"`
	TargetURL      string `json:"targetUrl"`
	Checksum       string `json:"checksum"`
	UpdatedAt      int64  `json:"updatedAt"`
}

// Upgrade es el estado de un upgrade iniciado.
type Upgrade struct {
	ID            int64   `json:"id"`
	RouterID      string  `json:"routerId"`
	TargetVersion string  `json:"targetVersion"`
	TargetURL     string  `json:"targetUrl"`
	Checksum      string  `json:"checksum"`
	Status        string  `json:"status"`
	Error         string  `json:"error,omitempty"`
	BackupPath    string  `json:"backupPath,omitempty"`
	StartedAt     int64   `json:"startedAt"`
	FinishedAt    *int64  `json:"finishedAt,omitempty"`
}

// Store persiste targets y upgrades.
type Store struct {
	db *sql.DB
}

// NewStore crea el store.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// SetTarget guarda o actualiza el target de firmware para un router.
func (s *Store) SetTarget(t Target) error {
	_, err := s.db.Exec(`
		INSERT INTO firmware_targets (router_id, model, current_version, target_version, target_url, checksum, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(router_id) DO UPDATE SET
			model=excluded.model,
			current_version=excluded.current_version,
			target_version=excluded.target_version,
			target_url=excluded.target_url,
			checksum=excluded.checksum,
			updated_at=excluded.updated_at
	`, t.RouterID, t.Model, t.CurrentVersion, t.TargetVersion, t.TargetURL, t.Checksum, time.Now().Unix())
	return err
}

// GetTarget devuelve el target de un router o nil si no existe.
func (s *Store) GetTarget(routerID string) (*Target, error) {
	var t Target
	err := s.db.QueryRow(`
		SELECT router_id, model, current_version, target_version, target_url, checksum, updated_at
		FROM firmware_targets WHERE router_id = ?
	`, routerID).Scan(&t.RouterID, &t.Model, &t.CurrentVersion, &t.TargetVersion, &t.TargetURL, &t.Checksum, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// ListTargets devuelve todos los targets.
func (s *Store) ListTargets() ([]Target, error) {
	rows, err := s.db.Query(`
		SELECT router_id, model, current_version, target_version, target_url, checksum, updated_at
		FROM firmware_targets ORDER BY router_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Target
	for rows.Next() {
		var t Target
		if err := rows.Scan(&t.RouterID, &t.Model, &t.CurrentVersion, &t.TargetVersion, &t.TargetURL, &t.Checksum, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// BeginUpgrade crea un registro de upgrade en estado 'requested'.
func (s *Store) BeginUpgrade(routerID, targetVersion, targetURL, checksum string) (int64, error) {
	res, err := s.db.Exec(`
		INSERT INTO firmware_upgrades (router_id, target_version, target_url, checksum, status, started_at)
		VALUES (?, ?, ?, ?, 'requested', ?)
	`, routerID, targetVersion, targetURL, checksum, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// SetStatus actualiza el estado de un upgrade.
func (s *Store) SetStatus(id int64, status, errMsg, backupPath string) error {
	var finished interface{}
	if status == "done" || status == "failed" {
		finished = time.Now().Unix()
	} else {
		finished = nil
	}
	_, err := s.db.Exec(`
		UPDATE firmware_upgrades
		SET status = ?, error = ?, backup_path = ?, finished_at = coalesce(?, finished_at)
		WHERE id = ?
	`, status, errMsg, backupPath, finished, id)
	return err
}

// LatestUpgrade devuelve el upgrade más reciente de un router.
func (s *Store) LatestUpgrade(routerID string) (*Upgrade, error) {
	var u Upgrade
	var finished sql.NullInt64
	err := s.db.QueryRow(`
		SELECT id, router_id, target_version, target_url, checksum, status, error, backup_path, started_at, finished_at
		FROM firmware_upgrades
		WHERE router_id = ?
		ORDER BY started_at DESC
		LIMIT 1
	`, routerID).Scan(&u.ID, &u.RouterID, &u.TargetVersion, &u.TargetURL, &u.Checksum, &u.Status, &u.Error, &u.BackupPath, &u.StartedAt, &finished)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if finished.Valid {
		u.FinishedAt = &finished.Int64
	}
	return &u, nil
}

// GetUpgradeByID busca un upgrade por ID (para validar propiedad del agente).
func (s *Store) GetUpgradeByID(id int64) (*Upgrade, error) {
	var u Upgrade
	var finished sql.NullInt64
	err := s.db.QueryRow(`
		SELECT id, router_id, target_version, target_url, checksum, status, error, backup_path, started_at, finished_at
		FROM firmware_upgrades WHERE id = ?
	`, id).Scan(&u.ID, &u.RouterID, &u.TargetVersion, &u.TargetURL, &u.Checksum, &u.Status, &u.Error, &u.BackupPath, &u.StartedAt, &finished)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if finished.Valid {
		u.FinishedAt = &finished.Int64
	}
	return &u, nil
}
