// Package wgseries: contadores de tráfico por peer de WireGuard con ventana
// real de 30 días (issue #2019).
//
// WireGuard no guarda historia: `wg show <iface> dump` solo expone el
// acumulado por peer desde que el peer (o la interfaz) se creó. Para poder
// decir "transferido en los últimos 30 días" hay que persistir cada muestra y
// sumar los DELTAS, porque el acumulado se reinicia a cero cuando el peer o la
// interfaz se recrean.
//
// Escalera de retención, igual que port_series: raw 7 días (una fila por peer
// y muestra) -> daily (una fila por peer y día, con la suma de deltas de ese
// día) que se conserva. La ventana de la UI se calcula con los días completos
// de daily más el día en curso desde raw, sin solape: el rollup solo consolida
// días ya cerrados.
package wgseries

import (
	"database/sql"
	"fmt"
	"log"
	"time"
)

const (
	// RawRetentionMS: 7 días de muestras crudas. Con un intervalo de muestra
	// de 5 minutos y una docena de peers son ~24k filas, muy por debajo de
	// port_series.
	RawRetentionMS = 7 * 24 * 60 * 60 * 1000
	// DefaultWindowDays es la ventana que pide el issue.
	DefaultWindowDays = 30
)

// PeerSample es el acumulado de un peer en una muestra.
type PeerSample struct {
	// Key es el identificador estable del peer (#1441): la pubkey en
	// base64url (adapters.WGPeer.Key). El ID de device no sirve para
	// persistir porque depende de NetPulse.
	Key     string
	RxBytes int64
	TxBytes int64
}

// Totals es el resultado de la ventana.
type Totals struct {
	Rx    uint64
	Tx    uint64
	Peers int
	// First es la muestra más antigua que cubre la ventana. Zero si no hay
	// ninguna, que es el estado vacío de una instalación nueva.
	First time.Time
}

// Store guarda las series de peers de WireGuard.
type Store struct {
	db *sql.DB
}

// SchemaSQL devuelve el DDL de las dos tablas.
func SchemaSQL() string {
	return `
CREATE TABLE IF NOT EXISTS wg_peer_series_raw (
  router_id TEXT NOT NULL,
  peer_key TEXT NOT NULL,
  ts INTEGER NOT NULL,
  rx_bytes INTEGER NOT NULL DEFAULT 0,
  tx_bytes INTEGER NOT NULL DEFAULT 0,
  d_rx INTEGER NOT NULL DEFAULT 0,
  d_tx INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (router_id, peer_key, ts)
);
CREATE INDEX IF NOT EXISTS idx_wg_peer_series_raw_ts ON wg_peer_series_raw(ts);

CREATE TABLE IF NOT EXISTS wg_peer_series_daily (
  router_id TEXT NOT NULL,
  peer_key TEXT NOT NULL,
  date TEXT NOT NULL,
  n INTEGER NOT NULL DEFAULT 0,
  rx_bytes INTEGER NOT NULL DEFAULT 0,
  tx_bytes INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (router_id, peer_key, date)
);
`
}

// NewStore crea el store y aplica el esquema.
func NewStore(db *sql.DB) (*Store, error) {
	if _, err := db.Exec(SchemaSQL()); err != nil {
		return nil, fmt.Errorf("wg_peer_series schema: %w", err)
	}
	return &Store{db: db}, nil
}

// delta devuelve cuánto creció un contador entre dos muestras. Un valor menor
// que el anterior significa que el contador se reinició (peer o interfaz
// recreados): se cuenta desde cero y no se pierde lo posterior al reinicio.
func delta(prev, cur int64) int64 {
	if cur <= 0 {
		return 0
	}
	if prev < 0 || cur < prev {
		return cur
	}
	return cur - prev
}

// RecordSamples guarda una muestra por peer y calcula su delta contra la
// última fila de ese peer. Es idempotente por (router, peer, ts): volver a
// escribir el mismo instante recalculate el delta, así que el llamador no debe
// repetir ts con contadores distintos.
func (s *Store) RecordSamples(routerID string, ts time.Time, samples []PeerSample) error {
	if routerID == "" || len(samples) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	prev := map[string][2]int64{}
	rows, err := tx.Query(`SELECT r.peer_key, r.rx_bytes, r.tx_bytes
		FROM wg_peer_series_raw r
		JOIN (SELECT peer_key, MAX(ts) AS mts FROM wg_peer_series_raw
		      WHERE router_id = ? GROUP BY peer_key) m
		  ON r.peer_key = m.peer_key AND r.ts = m.mts
		WHERE r.router_id = ?`, routerID, routerID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var key string
		var rx, txb int64
		if err := rows.Scan(&key, &rx, &txb); err != nil {
			_ = rows.Close()
			return err
		}
		prev[key] = [2]int64{rx, txb}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	tsMs := ts.UnixMilli()
	for _, smp := range samples {
		if smp.Key == "" {
			continue
		}
		var dRx, dTx int64
		if p, ok := prev[smp.Key]; ok {
			dRx = delta(p[0], smp.RxBytes)
			dTx = delta(p[1], smp.TxBytes)
		}
		// Primera muestra del peer: solo abre la ventana, no hay delta que
		// contar (el acumulado previo es de antes de que observáramos nada).
		if _, err := tx.Exec(`INSERT OR REPLACE INTO wg_peer_series_raw
			(router_id, peer_key, ts, rx_bytes, tx_bytes, d_rx, d_tx)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			routerID, smp.Key, tsMs, smp.RxBytes, smp.TxBytes, dRx, dTx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Window suma el tráfico de la ventana que termina en now. El raw manda
// donde hay raw (7 días) y daily solo rellena los días anteriores al más
// antiguo que aún está en raw: así el resultado es correcto aunque el rollup
// nocturno no haya corrido todavía, y no hay solape entre las dos fuentes.
func (s *Store) Window(routerID string, now time.Time, days int) (Totals, error) {
	var out Totals
	if routerID == "" || days <= 0 {
		return out, nil
	}
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -days+1)

	// Día más antiguo que sigue en raw dentro de la ventana.
	var minRawTs int64
	if err := s.db.QueryRow(`SELECT COALESCE(MIN(ts),0) FROM wg_peer_series_raw
		WHERE router_id = ? AND ts >= ?`, routerID, from.UnixMilli()).Scan(&minRawTs); err != nil {
		return out, err
	}
	dailyEnd := from
	if minRawTs > 0 {
		t := time.UnixMilli(minRawTs).In(now.Location())
		dailyEnd = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, now.Location())
	}

	// Días completos anteriores al raw disponible.
	row := s.db.QueryRow(`SELECT COALESCE(SUM(rx_bytes),0), COALESCE(SUM(tx_bytes),0),
		COUNT(DISTINCT peer_key), COALESCE(MIN(date),'')
		FROM wg_peer_series_daily
		WHERE router_id = ? AND date >= ? AND date < ?`,
		routerID, from.Format("2006-01-02"), dailyEnd.Format("2006-01-02"))
	var rx, txb int64
	var peers int
	var minDate string
	if err := row.Scan(&rx, &txb, &peers, &minDate); err != nil {
		return out, err
	}
	out.Rx = uint64(rx)
	out.Tx = uint64(txb)
	out.Peers = peers
	if minDate != "" {
		if t, err := time.ParseInLocation("2006-01-02", minDate, now.Location()); err == nil {
			out.First = t
		}
	}

	// Todo el raw de la ventana. Sus deltas ya vienen calculados, incluidos
	// los resets, así que aquí solo se suman.
	if minRawTs > 0 {
		row = s.db.QueryRow(`SELECT COALESCE(SUM(d_rx),0), COALESCE(SUM(d_tx),0),
			COUNT(DISTINCT peer_key), COALESCE(MIN(ts),0)
			FROM wg_peer_series_raw
			WHERE router_id = ? AND ts >= ?`, routerID, minRawTs)
		var rrx, rtx int64
		var rpeers int
		var rawFirst int64
		if err := row.Scan(&rrx, &rtx, &rpeers, &rawFirst); err != nil {
			return out, err
		}
		out.Rx += uint64(rrx)
		out.Tx += uint64(rtx)
		if rpeers > out.Peers {
			out.Peers = rpeers
		}
		if rawFirst > 0 {
			if t := time.UnixMilli(rawFirst); out.First.IsZero() || t.Before(out.First) {
				out.First = t
			}
		}
	}
	return out, nil
}

// RollupRawToDaily consolida en daily los días ya cerrados que sigan en raw.
// Ventana = retención raw para recubrir caídas largas del daemon (#812).
func (s *Store) RollupRawToDaily(window time.Duration) error {
	now := time.Now()
	since := now.Add(-window).UnixMilli()
	startOfToday := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).UnixMilli()
	_, err := s.db.Exec(`INSERT OR REPLACE INTO wg_peer_series_daily
		(router_id, peer_key, date, n, rx_bytes, tx_bytes)
		SELECT router_id, peer_key,
			strftime('%Y-%m-%d', ts / 1000, 'unixepoch', 'localtime'),
			COUNT(*), SUM(d_rx), SUM(d_tx)
		FROM wg_peer_series_raw
		WHERE ts >= ? AND ts < ?
		GROUP BY router_id, peer_key,
			strftime('%Y-%m-%d', ts / 1000, 'unixepoch', 'localtime')`,
		since, startOfToday)
	return err
}

// PurgeRaw borra las muestras fuera de retención. El delta de la primera
// muestra que quede sigue siendo correcto: se calcula contra el acumulado de
// la fila anterior aunque esa fila ya no exista.
func (s *Store) PurgeRaw() (int64, error) {
	cutoff := time.Now().UnixMilli() - RawRetentionMS
	res, err := s.db.Exec("DELETE FROM wg_peer_series_raw WHERE ts < ?", cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// RecentSamples informa cuántas muestras crudas hay en la ventana. Sirve para
// que el estado de la UI distinga "sin datos" de "cero bytes transferidos".
func (s *Store) RecentSamples(routerID string, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM wg_peer_series_raw
		WHERE router_id = ? AND ts >= ?`, routerID, since.UnixMilli()).Scan(&n)
	return n, err
}

// NightlyJob ejecuta el rollup y la purga.
func (s *Store) NightlyJob() {
	start := time.Now()
	log.Printf("[netpulse:wgseries] nightly rollup: start")
	if err := s.RollupRawToDaily(time.Duration(RawRetentionMS) * time.Millisecond); err != nil {
		log.Printf("[netpulse:wgseries] rollup raw->daily error: %v", err)
	}
	if n, err := s.PurgeRaw(); err != nil {
		log.Printf("[netpulse:wgseries] purge raw error: %v", err)
	} else if n > 0 {
		log.Printf("[netpulse:wgseries] purgadas %d muestras raw", n)
	}
	log.Printf("[netpulse:wgseries] nightly rollup: done (%s)", time.Since(start).Round(time.Millisecond))
}
