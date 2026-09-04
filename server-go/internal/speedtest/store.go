// Package speedtest — test de velocidad WAN periódico (issue #511).
//
// Mide download/upload/latencia/jitter desde el host del server contra
// speedtest.net (showwin/speedtest-go, MIT), persiste la serie en SQLite y
// alimenta la tarjeta WAN del router gateway con valores medidos en lugar de
// aproximaciones. La ejecución es deliberadamente server-side: el router ya
// dedica su CPU al NAT y un test desde él competiría consigo mismo.
package speedtest

import (
	"database/sql"
	"time"
)

// RawRetention: los tests son discretos (1-4 al día como mucho), la tabla
// crece despacio y el valor está en la tendencia anual de la línea.
const RawRetention = 365 * 24 * time.Hour

const schemaSQL = `
CREATE TABLE IF NOT EXISTS speedtest_results (
  ts INTEGER PRIMARY KEY,
  down_mbps REAL NOT NULL,
  up_mbps REAL NOT NULL,
  ping_ms REAL,
  jitter_ms REAL,
  loss_pct REAL,
  server_name TEXT NOT NULL DEFAULT '',
  server_id TEXT NOT NULL DEFAULT '',
  origin TEXT NOT NULL DEFAULT 'scheduled',
  error TEXT
);
`

// Result es una medición completada. PingMs/JitterMs/LossPct son nil cuando
// el servidor no los aportó (se guardan como NULL y el frontend los omite).
type Result struct {
	TS         time.Time `json:"ts"`
	DownMbps   float64   `json:"downMbps"`
	UpMbps     float64   `json:"upMbps"`
	PingMs     *float64  `json:"pingMs,omitempty"`
	JitterMs   *float64  `json:"jitterMs,omitempty"`
	LossPct    *float64  `json:"lossPct,omitempty"`
	ServerName string    `json:"serverName,omitempty"`
	ServerID   string    `json:"serverId,omitempty"`
	Origin     string    `json:"origin,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// Store envuelve la tabla speedtest_results (patrón clientbw: schema en el
// propio paquete con CREATE TABLE IF NOT EXISTS al construir).
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) (*Store, error) {
	if db != nil {
		if _, err := db.Exec(schemaSQL); err != nil {
			return nil, err
		}
	}
	return &Store{db: db}, nil
}

// Insert guarda un resultado. La serie solo guarda éxitos: los fallos del
// runner no ensucian la gráfica (se exponen por Status).
func (s *Store) Insert(r Result) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO speedtest_results
		 (ts, down_mbps, up_mbps, ping_ms, jitter_ms, loss_pct, server_name, server_id, origin, error)
		 VALUES (?,?,?,?,?,?,?,?,?,?)`,
		r.TS.UnixMilli(), r.DownMbps, r.UpMbps,
		nullFloat(r.PingMs), nullFloat(r.JitterMs), nullFloat(r.LossPct),
		r.ServerName, r.ServerID, r.Origin, r.Error)
	return err
}

// Latest devuelve el resultado más reciente (nil si la serie está vacía).
func (s *Store) Latest() (*Result, error) {
	if s.db == nil {
		return nil, nil
	}
	row := s.db.QueryRow(`SELECT ts, down_mbps, up_mbps, ping_ms, jitter_ms, loss_pct,
	 server_name, server_id, origin, error FROM speedtest_results ORDER BY ts DESC LIMIT 1`)
	return scanResult(row)
}

// History devuelve los resultados de la ventana [from, to] en orden ascendente.
func (s *Store) History(from, to time.Time) ([]Result, error) {
	if s.db == nil {
		return nil, nil
	}
	rows, err := s.db.Query(`SELECT ts, down_mbps, up_mbps, ping_ms, jitter_ms, loss_pct,
	 server_name, server_id, origin, error FROM speedtest_results
	 WHERE ts >= ? AND ts <= ? ORDER BY ts`,
		from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Result
	for rows.Next() {
		r, err := scanResult(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// PruneBefore elimina resultados anteriores al cutoff (lo llama el
// scheduler tras cada test con su reloj inyectable: sin trabajo extra, la
// retención se cumple sola).
func (s *Store) PruneBefore(cutoff time.Time) error {
	if s.db == nil {
		return nil
	}
	_, err := s.db.Exec(`DELETE FROM speedtest_results WHERE ts < ?`, cutoff.UnixMilli())
	return err
}

func nullFloat(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

// scanner cubre QueryRow y Rows (ambos implementan Scan).
type scanner interface{ Scan(dest ...any) error }

func scanResult(sc scanner) (*Result, error) {
	var r Result
	var ts int64
	var ping, jitter, loss sql.NullFloat64
	if err := sc.Scan(&ts, &r.DownMbps, &r.UpMbps, &ping, &jitter, &loss,
		&r.ServerName, &r.ServerID, &r.Origin, &r.Error); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	r.TS = time.UnixMilli(ts)
	if ping.Valid {
		v := ping.Float64
		r.PingMs = &v
	}
	if jitter.Valid {
		v := jitter.Float64
		r.JitterMs = &v
	}
	if loss.Valid {
		v := loss.Float64
		r.LossPct = &v
	}
	return &r, nil
}
