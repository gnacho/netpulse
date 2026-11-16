// Package dnsactivity agrega el query log de AdGuard Home por dispositivo y
// dominio (#2078).
//
// Reglas de diseño, decididas en la issue y en el PR:
//   - Nunca se guarda el log crudo. Se escribe un agregado horario por
//     (MAC, dominio) con retención de 7 días, más la primera vez que se vio
//     cada dominio (90 días) para la vista de dominios nuevos.
//   - La lectura es incremental con cursores: `older_than` es un timestamp, no
//     un id, y el log de AdGuard rota en memoria. Si el hueco entre el cursor y
//     lo más antiguo disponible supera una hora, se registra y se declara.
//   - La atribución es IP→MAC con la evidencia que ya usa la app
//     (device_seen). Lo que no casa va a la MAC vacía ("sin atribuir"), nunca
//     se descarta.
package dnsactivity

import (
	"context"
	"database/sql"
	"net"
	"sort"
	"strings"
	"time"
)

// State describe por qué la ingesta puede estar parada. Se persiste en kv y se
// expone en los endpoints para que la UI lo diga en vez de mostrar vacío.
type State string

const (
	StateOK        State = "ok"          // ingiriendo
	StateNoConf    State = "unavailable" // sin integración configurada
	StateDisabled  State = "disabled"    // query log deshabilitado en AdGuard
	StateAnonymize State = "anonymized"  // anonymize_client_ip activo
	StateError     State = "error"       // AdGuard no responde
)

// Bucket es una fila del agregado horario.
type Bucket struct {
	MAC       string // vacía = sin atribuir
	Domain    string
	Hour      time.Time
	Queries   int64
	Blocked   int64
	FirstSeen time.Time
	LastSeen  time.Time
}

// DomainRow es una fila de la vista por dominio.
type DomainRow struct {
	Domain    string `json:"domain"`
	Queries   int64  `json:"queries"`
	Blocked   int64  `json:"blocked"`
	FirstSeen int64  `json:"first_seen_ms"`
	LastSeen  int64  `json:"last_seen_ms"`
}

// FirstSeen es una fila de "dominio nuevo en la ventana".
type FirstSeen struct {
	Domain      string `json:"domain"`
	MAC         string `json:"mac"`
	FirstSeenMS int64  `json:"first_seen_ms"`
}

// Store es el acceso SQL del agregado.
type Store struct{ db *sql.DB }

// NewStore envuelve la base de datos.
func NewStore(d *sql.DB) *Store { return &Store{db: d} }

// Apply escribe un lote en una sola transacción. Los UPSERT suman: dos ticks
// que solapan la misma hora no deben perder cuentas.
func (s *Store) Apply(ctx context.Context, buckets []Bucket, firsts []FirstSeen) error {
	if len(buckets) == 0 && len(firsts) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if len(buckets) > 0 {
		stmt, err := tx.PrepareContext(ctx, `
INSERT INTO dns_activity (mac, domain, hour_ts, queries, blocked, first_seen, last_seen)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(mac, domain, hour_ts) DO UPDATE SET
  queries = queries + excluded.queries,
  blocked = blocked + excluded.blocked,
  first_seen = MIN(first_seen, excluded.first_seen),
  last_seen = MAX(last_seen, excluded.last_seen)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, b := range buckets {
			if _, err := stmt.ExecContext(ctx,
				b.MAC, b.Domain, b.Hour.UnixMilli(), b.Queries, b.Blocked,
				b.FirstSeen.UnixMilli(), b.LastSeen.UnixMilli()); err != nil {
				return err
			}
		}
	}
	if len(firsts) > 0 {
		stmt, err := tx.PrepareContext(ctx, `
INSERT INTO dns_domain_first (domain, first_seen, mac) VALUES (?, ?, ?)
ON CONFLICT(domain) DO NOTHING`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, f := range firsts {
			if _, err := stmt.ExecContext(ctx, f.Domain, f.FirstSeenMS, f.MAC); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// queryTop agrupa el agregado por dominio en una ventana.
func (s *Store) queryTop(ctx context.Context, mac string, from, to time.Time, limit int) ([]DomainRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 20
	}
	q := `SELECT domain, SUM(queries), SUM(blocked), MIN(first_seen), MAX(last_seen)
FROM dns_activity WHERE hour_ts >= ? AND hour_ts <= ?`
	args := []any{from.Truncate(time.Hour).UnixMilli(), to.UnixMilli()}
	if mac != "" {
		q += " AND mac = ?"
		args = append(args, mac)
	}
	q += " GROUP BY domain ORDER BY SUM(queries) DESC, domain ASC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DomainRow{}
	for rows.Next() {
		var r DomainRow
		if err := rows.Scan(&r.Domain, &r.Queries, &r.Blocked, &r.FirstSeen, &r.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// TopForMAC devuelve los dominios más consultados por un dispositivo.
func (s *Store) TopForMAC(ctx context.Context, mac string, from, to time.Time, limit int) ([]DomainRow, error) {
	return s.queryTop(ctx, mac, from, to, limit)
}

// TopAll devuelve los dominios más consultados de toda la red.
func (s *Store) TopAll(ctx context.Context, from, to time.Time, limit int) ([]DomainRow, error) {
	return s.queryTop(ctx, "", from, to, limit)
}

// totals suma consultas y bloqueos de una ventana (mac vacía = toda la red).
func (s *Store) totals(ctx context.Context, mac string, from, to time.Time) (int64, int64, error) {
	q := `SELECT COALESCE(SUM(queries),0), COALESCE(SUM(blocked),0)
FROM dns_activity WHERE hour_ts >= ? AND hour_ts <= ?`
	args := []any{from.Truncate(time.Hour).UnixMilli(), to.UnixMilli()}
	if mac != "" {
		q += " AND mac = ?"
		args = append(args, mac)
	}
	var queries, blocked int64
	err := s.db.QueryRowContext(ctx, q, args...).Scan(&queries, &blocked)
	return queries, blocked, err
}

// TotalsForMAC suma la ventana de un dispositivo.
func (s *Store) TotalsForMAC(ctx context.Context, mac string, from, to time.Time) (int64, int64, error) {
	return s.totals(ctx, mac, from, to)
}

// TotalsAll suma la ventana de toda la red.
func (s *Store) TotalsAll(ctx context.Context, from, to time.Time) (int64, int64, error) {
	return s.totals(ctx, "", from, to)
}

// NewDomains lista los dominios vistos por primera vez en la ventana.
func (s *Store) NewDomains(ctx context.Context, from, to time.Time, limit int) ([]FirstSeen, error) {
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT domain, mac, first_seen FROM dns_domain_first
WHERE first_seen >= ? AND first_seen <= ?
ORDER BY first_seen DESC LIMIT ?`,
		from.UnixMilli(), to.UnixMilli(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FirstSeen{}
	for rows.Next() {
		var f FirstSeen
		if err := rows.Scan(&f.Domain, &f.MAC, &f.FirstSeenMS); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// resolveMACs construye el mapa IP→MAC con la evidencia de device_seen.
func resolveMACs(ctx context.Context, d *sql.DB) (map[string]string, error) {
	rows, err := d.QueryContext(ctx, `SELECT mac, ip FROM device_seen WHERE ip <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var mac, ip string
		if err := rows.Scan(&mac, &ip); err != nil {
			return nil, err
		}
		if k := canonicalIP(ip); k != "" {
			out[k] = mac
		}
	}
	return out, rows.Err()
}

// canonicalIP normaliza la representación de una IP para usarla como clave.
func canonicalIP(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if ip := net.ParseIP(s); ip != nil {
		return ip.String()
	}
	return s
}

// sortFirsts ordena por primera vez ascendente (determinista en tests).
func sortFirsts(f []FirstSeen) {
	sort.Slice(f, func(i, j int) bool { return f[i].FirstSeenMS < f[j].FirstSeenMS })
}
