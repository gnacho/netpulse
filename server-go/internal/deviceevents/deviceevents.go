// Package deviceevents — persistencia de transiciones offline/online de
// dispositivos detectadas por el poller WiFi (issue #184).
//
// El adapter Live detecta cuándo una MAC wireless deja de verse (tras N ticks
// de polling) y cuándo reaparece, y registra cada transición como un evento
// consultable por la API (GET /api/device-events), espejo de roam_events.
package deviceevents

import "database/sql"

// Estado del dispositivo en el evento.
const (
	StateOffline = "offline"
	StateOnline  = "online"
)

// Event es una fila de device_events (transición de presencia).
type Event struct {
	ID        int64  `json:"id"`
	TsMs      int64  `json:"ts_ms"`
	MAC       string `json:"mac"`
	RouterID  string `json:"router_id,omitempty"`
	State     string `json:"state"`
	SignalDbm *int   `json:"signal_dbm,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

// Insert registra una transición de presencia.
func Insert(db *sql.DB, ev Event) error {
	_, err := db.Exec(
		`INSERT INTO device_events (ts_ms, mac, router_id, state, signal_dbm, detail)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		ev.TsMs, ev.MAC, ev.RouterID, ev.State, ev.SignalDbm, ev.Detail,
	)
	return err
}

// ListEvents lee eventos ordenados por ts DESC. Filtros opcionales: router,
// mac y state. limit se acota a [1,1000] (default 100); sinceMs excluye
// eventos anteriores.
func ListEvents(db *sql.DB, limit int, sinceMs int64, routerID, mac, state string) ([]Event, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	q := "SELECT id, ts_ms, mac, COALESCE(router_id,''), state, signal_dbm, COALESCE(detail,'') FROM device_events WHERE ts_ms >= ?"
	args := []any{sinceMs}
	if routerID != "" {
		q += " AND router_id = ?"
		args = append(args, routerID)
	}
	if mac != "" {
		q += " AND mac = ?"
		args = append(args, mac)
	}
	if state != "" {
		q += " AND state = ?"
		args = append(args, state)
	}
	q += " ORDER BY ts_ms DESC LIMIT ?"
	args = append(args, limit)
	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var ev Event
		if err := rows.Scan(&ev.ID, &ev.TsMs, &ev.MAC, &ev.RouterID, &ev.State, &ev.SignalDbm, &ev.Detail); err != nil {
			continue
		}
		out = append(out, ev)
	}
	return out, nil
}
