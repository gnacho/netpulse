// reports.go — GET /api/reports/weekly: informe de disponibilidad semanal.
// Calculado de metrics_daily (rollup nocturno de Fase 8.3): un daily por
// router y día con el nº de muestras recolectadas (n), medias de
// latencia/cpu/ram y totales de tráfico. El corte temporal (semana ISO
// lunes-domingo) se normaliza aquí para que el frontend no haga aritmética
// de fechas.
//
// Semántica de disponibilidad (verificada contra datos reales de prod):
//   - `n` = suma de muestras del día (poll de 5 s; día completo ≈ 17280).
//   - minutos de recolección = n * 5 / 60.
//   - upPct = minutos / (días con datos * 1440). La semana en curso queda
//     parcial y se normaliza solo sobre los días que tienen datos.
//   - `up_count` del daily NO es fiable como base (es COUNT(*) de filas
//     bucket, que en prod coincide con muestras porque el rollup no agrega);
//     `up_min` del daily es MIN(min_ts) (timestamp), no minutos.
package httpapi

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// weeklyReportEntry es una fila del informe: un router durante una semana.
type weeklyReportEntry struct {
	RouterID string   `json:"routerId"`
	Week     string   `json:"week"`   // semana ISO, formato "2026-W31" (lunes-domingo)
	Days     int      `json:"days"`   // días con datos en esa semana (≤7)
	UpMin    int64    `json:"upMin"`  // minutos de recolección en la semana
	UpPct    float64  `json:"upPct"`  // % de disponibilidad sobre los días con datos
	LatAvg   *float64 `json:"latAvg"` // media de latencia (null si no hay datos)
	RxTotal  float64  `json:"rxTotal"`
	TxTotal  float64  `json:"txTotal"`
	CPUAvg   float64  `json:"cpuAvg"`
	RAMAvg   float64  `json:"ramAvg"`
}

// handleWeeklyReport sirve GET /api/reports/weekly?weeks=4. weeks ∈ [1,52].
func (s *server) handleWeeklyReport(w http.ResponseWriter, r *http.Request) {
	weeks := 4
	if raw := r.URL.Query().Get("weeks"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 52 {
			writeError(w, http.StatusBadRequest, "invalid_query", "weeks must be an integer between 1 and 52")
			return
		}
		weeks = n
	}

	// El daily se agrupa por día UTC (unixepoch en el rollup). La ventana se
	// recorta con la fecha UTC de hoy; la semana en curso queda parcial y
	// upPct se calcula solo sobre los días con datos (no penaliza).
	since := time.Now().UTC().AddDate(0, 0, -7*weeks).Format("2006-01-02")

	rows, err := s.db.Query(`
		SELECT
			router_id,
			strftime('%G-W%V', date) AS week,
			COUNT(*),
			SUM(n) * 5 / 60,
			AVG(lat_avg),
			SUM(rx_total),
			SUM(tx_total),
			AVG(cpu_avg),
			AVG(ram_avg)
		FROM metrics_daily
		WHERE date >= ?
		GROUP BY router_id, strftime('%G-W%V', date)
		ORDER BY week DESC, router_id`, since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	defer rows.Close()

	out := []weeklyReportEntry{}
	for rows.Next() {
		var e weeklyReportEntry
		var upMin int64
		var lat sql.NullFloat64
		if err := rows.Scan(&e.RouterID, &e.Week, &e.Days, &upMin,
			&lat, &e.RxTotal, &e.TxTotal, &e.CPUAvg, &e.RAMAvg); err != nil {
			continue
		}
		e.UpMin = upMin
		if lat.Valid {
			e.LatAvg = &lat.Float64
		}
		if e.Days > 0 {
			e.UpPct = float64(upMin) / (float64(e.Days) * 24 * 60) * 100
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "weeks": weeks})
}

// ---------------------------------------------------------------------------
// GET /api/reports/availability?range=day|week|month&n=N — Fase 15.1
//
// Disponibilidad por router agregada por día, semana ISO o mes, sobre los
// últimos N buckets (day: 30, week: 8, month: 12 por defecto). Reutiliza la
// semántica de upMin/upPct del weekly. El día actual parcial se normaliza por
// los minutos transcurridos (no penaliza); el mes en curso, por días con datos.
// ---------------------------------------------------------------------------

type availabilityEntry struct {
	RouterID string   `json:"routerId"`
	Bucket   string   `json:"bucket"` // day "2026-08-07" | week "2026-W31" | month "2026-07"
	Days     int      `json:"days"`   // días con datos en el bucket
	UpMin    int64    `json:"upMin"`  // minutos de recolección en el bucket
	UpPct    float64  `json:"upPct"`  // % de disponibilidad
	LatAvg   *float64 `json:"latAvg"`
	RxTotal  float64  `json:"rxTotal"`
	TxTotal  float64  `json:"txTotal"`
	CPUAvg   float64  `json:"cpuAvg"`
	RAMAvg   float64  `json:"ramAvg"`
}

// handleAvailabilityReport sirve GET /api/reports/availability.
func (s *server) handleAvailabilityReport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rangeParam := q.Get("range")
	if rangeParam == "" {
		rangeParam = "week"
	}

	// Expresión SQL de agrupación + ventana por defecto según el rango.
	var groupExpr string
	nDef, nMax := 8, 52
	switch rangeParam {
	case "day":
		groupExpr = "strftime('%Y-%m-%d', date)"
		nDef, nMax = 30, 90
	case "week":
		groupExpr = "strftime('%G-W%V', date)"
		nDef, nMax = 8, 52
	case "month":
		groupExpr = "strftime('%Y-%m', date)"
		nDef, nMax = 12, 24
	default:
		writeError(w, http.StatusBadRequest, "invalid_query", "range must be day, week or month")
		return
	}

	n := nDef
	if raw := q.Get("n"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > nMax {
			writeError(w, http.StatusBadRequest, "invalid_query",
				fmt.Sprintf("n must be an integer between 1 and %d", nMax))
			return
		}
		n = v
	}

	// Ventana hacia atrás desde hoy (UTC).
	now := time.Now().UTC()
	var since string
	switch rangeParam {
	case "day":
		since = now.AddDate(0, 0, -n).Format("2006-01-02")
	case "week":
		since = now.AddDate(0, 0, -7*n).Format("2006-01-02")
	case "month":
		since = now.AddDate(0, -n, 0).Format("2006-01-02")
	}

	query := fmt.Sprintf(`
		SELECT
			router_id,
			%s AS bucket,
			COUNT(*),
			SUM(n) * 5 / 60,
			AVG(lat_avg),
			SUM(rx_total),
			SUM(tx_total),
			AVG(cpu_avg),
			AVG(ram_avg)
		FROM metrics_daily
		WHERE date >= ?
		GROUP BY router_id, %s
		ORDER BY bucket DESC, router_id`, groupExpr, groupExpr)

	rows, err := s.db.Query(query, since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	defer rows.Close()

	// Para el día actual parcial: divisor = minutos transcurridos del día UTC.
	today := now.Format("2006-01-02")
	nowMin := now.Hour()*60 + now.Minute()

	out := []availabilityEntry{}
	for rows.Next() {
		var e availabilityEntry
		var upMin int64
		var lat sql.NullFloat64
		if err := rows.Scan(&e.RouterID, &e.Bucket, &e.Days, &upMin,
			&lat, &e.RxTotal, &e.TxTotal, &e.CPUAvg, &e.RAMAvg); err != nil {
			continue
		}
		e.UpMin = upMin
		if lat.Valid {
			e.LatAvg = &lat.Float64
		}
		var divisor float64
		if rangeParam == "day" {
			if e.Bucket == today && nowMin > 0 {
				divisor = float64(nowMin)
			} else {
				divisor = 1440
			}
		} else {
			divisor = float64(e.Days) * 1440
		}
		if divisor > 0 {
			e.UpPct = float64(upMin) / divisor * 100
			if e.UpPct > 100 {
				e.UpPct = 100
			}
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "range": rangeParam, "n": n})
}
