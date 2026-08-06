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
	"net/http"
	"strconv"
	"time"
)

// weeklyReportEntry es una fila del informe: un router durante una semana.
type weeklyReportEntry struct {
	RouterID string   `json:"routerId"`
	Week     string   `json:"week"`  // semana ISO, formato "2026-W31" (lunes-domingo)
	Days     int      `json:"days"`  // días con datos en esa semana (≤7)
	UpMin    int64    `json:"upMin"` // minutos de recolección en la semana
	UpPct    float64  `json:"upPct"` // % de disponibilidad sobre los días con datos
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
