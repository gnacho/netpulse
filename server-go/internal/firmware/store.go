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
	// ScheduledFor: programación desatendida (#494), epoch ms UTC. nil = el
	// upgrade es del flujo manual/inmediato. Nota: StartedAt/FinishedAt siguen
	// en unix SEGUNDOS (convención previa), ScheduledFor en ms.
	ScheduledFor *int64 `json:"scheduledFor,omitempty"`
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

// rowScanner abstrae *sql.Row y *sql.Rows para un único helper de escaneo.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanUpgrade escanea una fila completa de firmware_upgrades (11 columnas,
// incluida scheduled_for #494). Reutilizada por LatestUpgrade, GetUpgradeByID
// y DueScheduled para no duplicar el mapeo de columnas.
func scanUpgrade(sc rowScanner) (*Upgrade, error) {
	var u Upgrade
	var finished, scheduled sql.NullInt64
	var errStr, backup sql.NullString
	if err := sc.Scan(
		&u.ID, &u.RouterID, &u.TargetVersion, &u.TargetURL, &u.Checksum,
		&u.Status, &errStr, &backup, &u.StartedAt, &finished, &scheduled,
	); err != nil {
		return nil, err
	}
	if finished.Valid {
		u.FinishedAt = &finished.Int64
	}
	if scheduled.Valid {
		u.ScheduledFor = &scheduled.Int64
	}
	if errStr.Valid {
		u.Error = errStr.String
	}
	if backup.Valid {
		u.BackupPath = backup.String
	}
	return &u, nil
}

// LatestUpgrade devuelve el upgrade más reciente de un router.
func (s *Store) LatestUpgrade(routerID string) (*Upgrade, error) {
	u, err := scanUpgrade(s.db.QueryRow(`
		SELECT id, router_id, target_version, target_url, checksum, status, error, backup_path, started_at, finished_at, scheduled_for
		FROM firmware_upgrades
		WHERE router_id = ?
		ORDER BY started_at DESC
		LIMIT 1
	`, routerID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

// GetUpgradeByID busca un upgrade por ID (para validar propiedad del agente).
func (s *Store) GetUpgradeByID(id int64) (*Upgrade, error) {
	u, err := scanUpgrade(s.db.QueryRow(`
		SELECT id, router_id, target_version, target_url, checksum, status, error, backup_path, started_at, finished_at, scheduled_for
		FROM firmware_upgrades WHERE id = ?
	`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return u, nil
}

// DismissLatest borra el último intento de upgrade del router si está
// terminado (failed o done). Un upgrade en curso (requested/running) no se
// toca: el aviso de error obsoleto debe poder descartarse (#519) sin poder
// cancelar una operación viva por accidente.
func (s *Store) DismissLatest(routerID string) error {
	up, err := s.LatestUpgrade(routerID)
	if err != nil {
		return err
	}
	if up == nil || (up.Status != "failed" && up.Status != "done") {
		return nil
	}
	_, err = s.db.Exec(`DELETE FROM firmware_upgrades WHERE id = ?`, up.ID)
	return err
}

// ScheduleUpgrade crea o actualiza la fila 'scheduled' del router (#494).
// Una fila por router: si ya hay una pendiente, se actualizan target y hora;
// si no, se inserta. scheduledFor es epoch ms UTC.
func (s *Store) ScheduleUpgrade(routerID, targetVersion, targetURL, checksum string, scheduledFor int64) (int64, error) {
	var id int64
	err := s.db.QueryRow(`
		SELECT id FROM firmware_upgrades WHERE router_id = ? AND status = 'scheduled'
	`, routerID).Scan(&id)
	switch {
	case err == nil:
		_, err = s.db.Exec(`
			UPDATE firmware_upgrades
			SET target_version = ?, target_url = ?, checksum = ?, scheduled_for = ?, started_at = ?
			WHERE id = ?
		`, targetVersion, targetURL, checksum, scheduledFor, time.Now().Unix(), id)
		return id, err
	case err == sql.ErrNoRows:
		res, err := s.db.Exec(`
			INSERT INTO firmware_upgrades (router_id, target_version, target_url, checksum, status, started_at, scheduled_for)
			VALUES (?, ?, ?, ?, 'scheduled', ?, ?)
		`, routerID, targetVersion, targetURL, checksum, time.Now().Unix(), scheduledFor)
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	default:
		return 0, err
	}
}

// CancelScheduled borra la fila 'scheduled' pendiente del router (#494).
// No-op si no hay programación pendiente.
func (s *Store) CancelScheduled(routerID string) error {
	_, err := s.db.Exec(`
		DELETE FROM firmware_upgrades WHERE router_id = ? AND status = 'scheduled'
	`, routerID)
	return err
}

// DueScheduled devuelve las programaciones vencidas (status 'scheduled' y
// scheduled_for <= nowMs), ordenadas por hora para lanzarlas en orden.
func (s *Store) DueScheduled(nowMs int64) ([]Upgrade, error) {
	rows, err := s.db.Query(`
		SELECT id, router_id, target_version, target_url, checksum, status, error, backup_path, started_at, finished_at, scheduled_for
		FROM firmware_upgrades
		WHERE status = 'scheduled' AND scheduled_for <= ?
		ORDER BY scheduled_for ASC
	`, nowMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Upgrade{}
	for rows.Next() {
		u, err := scanUpgrade(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// StartScheduled pasa una fila 'scheduled' vencida a 'requested' (#494).
// Guarded: solo transiciona si sigue en 'scheduled' (single-flight frente a
// un segundo tick o a un cancelado). Devuelve true si la transición se aplicó.
func (s *Store) StartScheduled(id int64) (bool, error) {
	res, err := s.db.Exec(`
		UPDATE firmware_upgrades SET status = 'requested' WHERE id = ? AND status = 'scheduled'
	`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
