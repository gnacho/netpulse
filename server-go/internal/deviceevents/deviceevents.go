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

// --- Presence timeline (#771) ------------------------------------------------

// Interval es un tramo de presencia continua de una MAC: conectada a un
// router desde StartMs hasta EndMs. EndMs == nil ⇒ sigue conectada ahora.
type Interval struct {
	RouterID string `json:"routerId"`
	StartMs  int64  `json:"startMs"`
	EndMs    *int64 `json:"endMs"`
}

// Intervals construye los tramos de presencia de una MAC a partir de
// device_events (online/offline del poller, #184) fundidos con los
// AP-STA-CONNECTED de roam_events (#14.5): un CONNECTED en otro router
// mientras hay tramo abierto cierra ese tramo y abre uno nuevo (roaming).
// Los eventos se recorren en ascendente; un online en el mismo router que el
// tramo abierto es no-op (el poller repite online por reconexiones rápidas).
func Intervals(db *sql.DB, mac string, sinceMs int64) ([]Interval, error) {
	devEv, err := listEventsAsc(db, mac, sinceMs)
	if err != nil {
		return nil, err
	}
	roamEv, err := listConnectedAsc(db, mac, sinceMs)
	if err != nil {
		return nil, err
	}
	out := []Interval{}
	curIdx := -1 // índice del tramo abierto en out (-1 = ninguno)
	closeAt := func(ts int64) {
		if curIdx >= 0 {
			out[curIdx].EndMs = &ts
			curIdx = -1
		}
	}
	i, j := 0, 0
	for i < len(devEv) || j < len(roamEv) {
		var ts int64
		var kind string // "online" | "offline" | "connected"
		var router string
		switch {
		case j >= len(roamEv) || (i < len(devEv) && devEv[i].TsMs <= roamEv[j].TsMs):
			ts, kind, router = devEv[i].TsMs, devEv[i].State, devEv[i].RouterID
			i++
		default:
			ts, kind, router = roamEv[j].TsMs, "connected", roamEv[j].RouterID
			j++
		}
		switch kind {
		case StateOnline, "connected":
			if curIdx < 0 {
				out = append(out, Interval{RouterID: router, StartMs: ts})
				curIdx = len(out) - 1
			} else if router != "" && out[curIdx].RouterID != router {
				closeAt(ts)
				out = append(out, Interval{RouterID: router, StartMs: ts})
				curIdx = len(out) - 1
			}
		case StateOffline:
			closeAt(ts)
		}
	}
	return out, nil
}

// listEventsAsc devuelve los device_events de la MAC en ascendente.
func listEventsAsc(db *sql.DB, mac string, sinceMs int64) ([]Event, error) {
	rows, err := db.Query(`SELECT ts_ms, COALESCE(router_id,''), state FROM device_events
		WHERE mac = ? AND ts_ms >= ? ORDER BY ts_ms ASC`, mac, sinceMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var ev Event
		if err := rows.Scan(&ev.TsMs, &ev.RouterID, &ev.State); err == nil {
			out = append(out, ev)
		}
	}
	return out, nil
}

// listConnectedAsc devuelve los roam_events 'connected' de la MAC en
// ascendente (el feed hostapd/DAWN tiene la MAC tal cual la loguea hostapd:
// mayúsculas con ':').
func listConnectedAsc(db *sql.DB, mac string, sinceMs int64) ([]Event, error) {
	rows, err := db.Query(`SELECT ts_ms, router_id FROM roam_events
		WHERE mac = ? AND type = 'connected' AND ts_ms >= ? ORDER BY ts_ms ASC`, mac, sinceMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var ev Event
		if err := rows.Scan(&ev.TsMs, &ev.RouterID); err == nil {
			out = append(out, ev)
		}
	}
	return out, nil
}

// RoamCount resume la actividad de roaming de una MAC en la ventana pedida.
type RoamCount struct {
	MAC      string `json:"mac"`
	Connects int    `json:"connects"`
}

// RoamCounts devuelve las MACs con más eventos 'connected' en la ventana
// (una MAC estable muestra 1; cada salto de AP suma uno más). Salud de
// roaming de la flota: las primeras entradas son las que más rebotan.
func RoamCounts(db *sql.DB, sinceMs int64, limit int) ([]RoamCount, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := db.Query(`SELECT mac, COUNT(*) FROM roam_events
		WHERE type = 'connected' AND ts_ms >= ? AND mac IS NOT NULL AND mac != ''
		GROUP BY mac HAVING COUNT(*) > 1 ORDER BY COUNT(*) DESC LIMIT ?`, sinceMs, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RoamCount{}
	for rows.Next() {
		var rc RoamCount
		if err := rows.Scan(&rc.MAC, &rc.Connects); err == nil {
			out = append(out, rc)
		}
	}
	return out, nil
}

// CountConnects cuenta los eventos 'connected' de una MAC en la ventana.
func CountConnects(db *sql.DB, mac string, sinceMs int64) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM roam_events
		WHERE mac = ? AND type = 'connected' AND ts_ms >= ?`, mac, sinceMs).Scan(&n)
	return n, err
}

// Prune borra los eventos de presencia y roaming anteriores al corte. Es la
// retención configurable del #771: sin ella ambas tablas crecen sin límite.
func Prune(db *sql.DB, cutoffMs int64) (int64, error) {
	r1, err := db.Exec("DELETE FROM device_events WHERE ts_ms < ?", cutoffMs)
	if err != nil {
		return 0, err
	}
	n1, _ := r1.RowsAffected()
	r2, err := db.Exec("DELETE FROM roam_events WHERE ts_ms < ?", cutoffMs)
	if err != nil {
		return n1, err
	}
	n2, _ := r2.RowsAffected()
	return n1 + n2, nil
}
