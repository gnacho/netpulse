// reports.go - GET /api/reports/weekly y GET /api/reports/availability:
// informes de disponibilidad por router.
//
// Calculados de metrics_daily (rollup nocturno de Fase 8.3) más el raw del
// día en curso. El corte temporal (semana ISO lunes-domingo) se normaliza
// aquí para que el frontend no haga aritmética de fechas.
//
// Semántica de disponibilidad (issue #987, verificada con datos reales de
// prod):
//   - Base: buckets de 5 min con al menos una muestra (metrics_daily.up_count;
//     288 = día completo). Es agnóstica del intervalo de poll: la fórmula
//     vieja (SUM(n) * 5 s) asumía poll de 5 s y, con el poll real de 30 s de
//     prod, contaba el tiempo entre muestras como tiempo caído: 17 días
//     fantasma en una ventana de 4 semanas en routers online 24/7.
//   - upMin = buckets con datos * 5 (minutos con datos, resolución de 5 min).
//   - El día en curso NO se lee del daily: el rollup es nocturno y llega con
//     hasta 24 h de retraso, así que el daily de hoy está vacío o casi vacío
//     y marcaría el día como caído. Se mide del raw (retención 7 días)
//     contando buckets de 5 min distintos con muestras hoy.
//   - Divisor: días con datos * 1440, pero el día en curso solo cuenta los
//     minutos transcurridos (no penaliza). Clamp a 100 (#207): un bucket
//     sobre-poblado no puede superar el 100%.
//   - Lo medido es "minutos con datos recibidos": un hueco suele ser el
//     router caído, pero también puede ser el monitor sin datos. La UI lo
//     etiqueta como tal y nunca afirma más downtime que tiempo sin datos.
package httpapi

import (
	"database/sql"
	"encoding/csv"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

// minPerBucket: minutos que cubre un bucket de agregación (db.BucketMS).
const minPerBucket = 5

// todayBucketCoverage devuelve, por router, cuántos buckets de 5 min tienen
// al menos una muestra raw desde medianoche UTC de hoy. El daily de hoy aún
// no existe (rollup nocturno), así que el día en curso se mide del raw.
func (s *server) todayBucketCoverage(midnightMS int64) (map[string]int64, error) {
	rows, err := s.db.Query(`
		SELECT router_id, COUNT(DISTINCT ts / ?)
		FROM metrics
		WHERE ts >= ?
		GROUP BY router_id`, db.BucketMS, midnightMS)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id string
		var n int64
		if err := rows.Scan(&id, &n); err != nil {
			continue
		}
		out[id] = n
	}
	return out, rows.Err()
}

// weeklyReportEntry es una fila del informe: un router durante una semana.
type weeklyReportEntry struct {
	RouterID string   `json:"routerId"`
	Week     string   `json:"week"`   // semana ISO, formato "2026-W31" (lunes-domingo)
	Days     int      `json:"days"`   // días con datos en esa semana (≤7)
	UpMin    int64    `json:"upMin"`  // minutos con datos en la semana (buckets de 5 min * 5)
	UpPct    float64  `json:"upPct"`  // % de cobertura sobre los minutos medibles
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
	// recorta con la fecha UTC de hoy; el daily de hoy se EXCLUYE (rollup
	// nocturno, llega con retraso) y se fusiona después desde el raw.
	now := time.Now().UTC()
	since := now.AddDate(0, 0, -7*weeks).Format("2006-01-02")
	today := now.Format("2006-01-02")
	nowMin := now.Hour()*60 + now.Minute()

	rows, err := s.db.Query(`
		SELECT
			router_id,
			strftime('%G-W%V', date) AS week,
			COUNT(*),
			SUM(up_count) * ?,
			AVG(lat_avg),
			SUM(rx_total),
			SUM(tx_total),
			AVG(cpu_avg),
			AVG(ram_avg)
		FROM metrics_daily
		WHERE date >= ? AND date < ?
		GROUP BY router_id, strftime('%G-W%V', date)`, minPerBucket, since, today)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	defer rows.Close()

	out := []weeklyReportEntry{}
	idx := map[string]int{} // routerId|week -> posición en out
	hasToday := map[int]bool{}
	for rows.Next() {
		var e weeklyReportEntry
		var lat sql.NullFloat64
		if err := rows.Scan(&e.RouterID, &e.Week, &e.Days, &e.UpMin,
			&lat, &e.RxTotal, &e.TxTotal, &e.CPUAvg, &e.RAMAvg); err != nil {
			continue
		}
		if lat.Valid {
			e.LatAvg = &lat.Float64
		}
		idx[e.RouterID+"|"+e.Week] = len(out)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	// Día en curso desde el raw (ver cabecera). A medianoche UTC exacta no hay
	// minutos transcurridos que medir y se omite.
	if nowMin > 0 {
		midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).UnixMilli()
		cov, err := s.todayBucketCoverage(midnight)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error")
			return
		}
		y, wn := now.ISOWeek()
		todayWeek := fmt.Sprintf("%d-W%02d", y, wn)
		for rid, buckets := range cov {
			key := rid + "|" + todayWeek
			if i, ok := idx[key]; ok {
				out[i].Days++
				out[i].UpMin += buckets * minPerBucket
				hasToday[i] = true
			} else {
				idx[key] = len(out)
				hasToday[len(out)] = true
				out = append(out, weeklyReportEntry{
					RouterID: rid, Week: todayWeek, Days: 1,
					UpMin: buckets * minPerBucket,
				})
			}
		}
	}

	// Divisor: días con datos * 1440; el día en curso solo los minutos
	// transcurridos. UpMin se tope al divisor (el bucket actual parcial puede
	// contarse entero) y upPct se clava a 100 (#207).
	for i := range out {
		divisor := float64(out[i].Days) * 1440
		if hasToday[i] {
			divisor = float64(out[i].Days-1)*1440 + float64(nowMin)
		}
		if divisor <= 0 {
			continue
		}
		if float64(out[i].UpMin) > divisor {
			out[i].UpMin = int64(divisor)
		}
		out[i].UpPct = float64(out[i].UpMin) / divisor * 100
		if out[i].UpPct > 100 {
			out[i].UpPct = 100
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Week != out[j].Week {
			return out[i].Week > out[j].Week
		}
		return out[i].RouterID < out[j].RouterID
	})

	if r.URL.Query().Get("format") == "csv" {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=netpulse-weekly.csv")
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"routerId", "week", "days", "upMin", "upPct", "latAvg", "rxTotal", "txTotal", "cpuAvg", "ramAvg"})
		for _, e := range out {
			lat := ""
			if e.LatAvg != nil {
				lat = fmt.Sprintf("%.2f", *e.LatAvg)
			}
			_ = cw.Write([]string{
				e.RouterID, e.Week, strconv.Itoa(e.Days),
				strconv.FormatInt(e.UpMin, 10), fmt.Sprintf("%.2f", e.UpPct),
				lat, fmt.Sprintf("%.2f", e.RxTotal), fmt.Sprintf("%.2f", e.TxTotal),
				fmt.Sprintf("%.2f", e.CPUAvg), fmt.Sprintf("%.2f", e.RAMAvg),
			})
		}
		cw.Flush()
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "weeks": weeks})
}

// ---------------------------------------------------------------------------
// GET /api/reports/availability?range=hour|day|week|month&n=N - Fase 15.1
//
// Disponibilidad por router agregada por hora, día, semana ISO o mes, sobre
// los últimos N buckets (hour: 24, day: 30, week: 8, month: 12 por defecto).
// Reutiliza la semántica de buckets del weekly (ver cabecera del fichero,
// #987). El rango hour (#1032) no puede salir de metrics_daily (granularidad
// día): se calcula del raw y tiene su propia semántica de huecos (ver
// availabilityHour).
// ---------------------------------------------------------------------------

type availabilityEntry struct {
	RouterID string   `json:"routerId"`
	Bucket   string   `json:"bucket"` // hour "2026-10-02T14" | day "2026-08-07" | week "2026-W31" | month "2026-07"
	Days     int      `json:"days"`   // unidades con datos en el bucket (en hour: 1 si la hora tiene datos, 0 si es hueco rellenado)
	UpMin    int64    `json:"upMin"`  // minutos con datos en el bucket (buckets de 5 min * 5)
	UpPct    float64  `json:"upPct"`  // % de cobertura sobre los minutos medibles
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
	case "hour":
		nDef, nMax = 24, 48
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
		writeError(w, http.StatusBadRequest, "invalid_query", "range must be hour, day, week or month")
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

	// El rango hour (#1032) se calcula del raw, no del daily: camino propio.
	if rangeParam == "hour" {
		out, err := s.availabilityHour(n)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error")
			return
		}
		writeAvailability(w, q.Get("format") == "csv", rangeParam, n, out)
		return
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
	today := now.Format("2006-01-02")
	nowMin := now.Hour()*60 + now.Minute()

	// Igual que el weekly: el daily de hoy se excluye y se fusiona del raw.
	query := fmt.Sprintf(`
		SELECT
			router_id,
			%s AS bucket,
			COUNT(*),
			SUM(up_count) * ?,
			AVG(lat_avg),
			SUM(rx_total),
			SUM(tx_total),
			AVG(cpu_avg),
			AVG(ram_avg)
		FROM metrics_daily
		WHERE date >= ? AND date < ?
		GROUP BY router_id, %s`, groupExpr, groupExpr)

	rows, err := s.db.Query(query, minPerBucket, since, today)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	defer rows.Close()

	out := []availabilityEntry{}
	idx := map[string]int{} // routerId|bucket -> posición en out
	hasToday := map[int]bool{}
	for rows.Next() {
		var e availabilityEntry
		var lat sql.NullFloat64
		if err := rows.Scan(&e.RouterID, &e.Bucket, &e.Days, &e.UpMin,
			&lat, &e.RxTotal, &e.TxTotal, &e.CPUAvg, &e.RAMAvg); err != nil {
			continue
		}
		if lat.Valid {
			e.LatAvg = &lat.Float64
		}
		idx[e.RouterID+"|"+e.Bucket] = len(out)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	// Día en curso desde el raw; la etiqueta del grupo que contiene hoy
	// depende del rango (día, semana ISO o mes).
	if nowMin > 0 {
		midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).UnixMilli()
		cov, err := s.todayBucketCoverage(midnight)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "db_error")
			return
		}
		var todayBucket string
		switch rangeParam {
		case "day":
			todayBucket = today
		case "week":
			y, wn := now.ISOWeek()
			todayBucket = fmt.Sprintf("%d-W%02d", y, wn)
		case "month":
			todayBucket = now.Format("2006-01")
		}
		for rid, buckets := range cov {
			key := rid + "|" + todayBucket
			if i, ok := idx[key]; ok {
				out[i].Days++
				out[i].UpMin += buckets * minPerBucket
				hasToday[i] = true
			} else {
				idx[key] = len(out)
				hasToday[len(out)] = true
				out = append(out, availabilityEntry{
					RouterID: rid, Bucket: todayBucket, Days: 1,
					UpMin: buckets * minPerBucket,
				})
			}
		}
	}

	// Divisor: días con datos * 1440; el día en curso solo los minutos
	// transcurridos (mismo criterio que el weekly, #987).
	for i := range out {
		divisor := float64(out[i].Days) * 1440
		if hasToday[i] {
			divisor = float64(out[i].Days-1)*1440 + float64(nowMin)
		}
		if divisor <= 0 {
			continue
		}
		if float64(out[i].UpMin) > divisor {
			out[i].UpMin = int64(divisor)
		}
		out[i].UpPct = float64(out[i].UpMin) / divisor * 100
		if out[i].UpPct > 100 {
			out[i].UpPct = 100
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Bucket != out[j].Bucket {
			return out[i].Bucket > out[j].Bucket
		}
		return out[i].RouterID < out[j].RouterID
	})

	writeAvailability(w, q.Get("format") == "csv", rangeParam, n, out)
}

// writeAvailability emite el informe de disponibilidad en JSON o CSV. El
// formato de etiqueta del bucket depende del rango (ver availabilityEntry).
func writeAvailability(w http.ResponseWriter, csvWanted bool, rangeParam string, n int, out []availabilityEntry) {
	if csvWanted {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=netpulse-availability-%s.csv", rangeParam))
		cw := csv.NewWriter(w)
		_ = cw.Write([]string{"routerId", "bucket", "days", "upMin", "upPct", "latAvg", "rxTotal", "txTotal", "cpuAvg", "ramAvg"})
		for _, e := range out {
			lat := ""
			if e.LatAvg != nil {
				lat = fmt.Sprintf("%.2f", *e.LatAvg)
			}
			_ = cw.Write([]string{
				e.RouterID, e.Bucket, strconv.Itoa(e.Days),
				strconv.FormatInt(e.UpMin, 10), fmt.Sprintf("%.2f", e.UpPct),
				lat, fmt.Sprintf("%.2f", e.RxTotal), fmt.Sprintf("%.2f", e.TxTotal),
				fmt.Sprintf("%.2f", e.CPUAvg), fmt.Sprintf("%.2f", e.RAMAvg),
			})
		}
		cw.Flush()
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out, "range": rangeParam, "n": n})
}

// hourMS es el tamaño del bucket horario en ms (range=hour, #1032).
const hourMS = 60 * 60 * 1000

// availabilityHour calcula range=hour: disponibilidad por router y hora
// sobre las últimas n horas, incluida la en curso. La fuente es el raw
// (tabla metrics, retención 7 días, de sobra para el máximo de 48 h) porque
// metrics_daily no tiene granularidad horaria. La etiqueta del bucket es la
// hora UTC en formato ISO recortado: "2006-01-02T15" (p.ej. "2026-10-02T14").
//
// Diferencia de semántica con day/week/month (#1032): las horas SIN muestras
// dentro de la ventana se EMITEN con upMin 0, days 0 y upPct 0 (bucket
// rellenado), porque a escala horaria el hueco reciente es justo lo que
// interesa ver; un día sin datos, en cambio, no se emite (puede ser un
// router aún no monitorizado). Solo se rellenan horas de routers con al
// menos una muestra en la ventana: un router sin ninguna muestra en toda la
// ventana no aparece, igual que en los demás rangos.
//
// Divisor de upPct: 60 min por hora cerrada; la hora en curso solo cuenta
// los minutos transcurridos (mismo criterio que el día en curso, #987), con
// upMin tope al divisor y upPct clavado a 100. latAvg/cpuAvg/ramAvg son
// medias de las muestras raw de la hora; rxTotal/txTotal se emiten a 0: el
// raw guarda tasas (bps), no totales, y agregarlas inventaría un dato.
func (s *server) availabilityHour(n int) ([]availabilityEntry, error) {
	now := time.Now().UTC()
	hourStart := now.Truncate(time.Hour)
	sinceMS := hourStart.Add(-time.Duration(n-1) * time.Hour).UnixMilli()

	rows, err := s.db.Query(`
		SELECT
			router_id,
			ts / ?,
			COUNT(DISTINCT ts / ?),
			AVG(latency_ms),
			AVG(cpu),
			AVG(ram)
		FROM metrics
		WHERE ts >= ?
		GROUP BY router_id, ts / ?`, hourMS, db.BucketMS, sinceMS, hourMS)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type hourData struct {
		buckets int64
		lat     *float64
		cpu     float64
		ram     float64
	}
	byRouter := map[string]map[int64]hourData{}
	for rows.Next() {
		var rid string
		var hourEpoch, buckets int64
		var lat, cpu, ram sql.NullFloat64
		if err := rows.Scan(&rid, &hourEpoch, &buckets, &lat, &cpu, &ram); err != nil {
			continue
		}
		m := byRouter[rid]
		if m == nil {
			m = map[int64]hourData{}
			byRouter[rid] = m
		}
		d := hourData{buckets: buckets}
		if lat.Valid {
			d.lat = &lat.Float64
		}
		if cpu.Valid {
			d.cpu = cpu.Float64
		}
		if ram.Valid {
			d.ram = ram.Float64
		}
		m[hourEpoch] = d
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := []availabilityEntry{}
	currentHourEpoch := hourStart.UnixMilli() / hourMS
	for rid, hours := range byRouter {
		for i := 0; i < n; i++ {
			hStart := hourStart.Add(-time.Duration(i) * time.Hour)
			epoch := hStart.UnixMilli() / hourMS
			e := availabilityEntry{
				RouterID: rid,
				Bucket:   hStart.Format("2006-01-02T15"),
			}
			// Divisor: 60 min por hora cerrada; la hora en curso solo los
			// minutos transcurridos (now.Minute(), reloj UTC).
			divisor := float64(60)
			if epoch == currentHourEpoch {
				divisor = float64(now.Minute())
			}
			if d, ok := hours[epoch]; ok {
				e.Days = 1
				e.UpMin = d.buckets * minPerBucket
				e.LatAvg = d.lat
				e.CPUAvg = d.cpu
				e.RAMAvg = d.ram
			}
			if divisor <= 0 {
				// Hora en curso en el minuto 0: aún no hay nada medible.
				e.UpMin = 0
			} else {
				if float64(e.UpMin) > divisor {
					e.UpMin = int64(divisor)
				}
				e.UpPct = float64(e.UpMin) / divisor * 100
				if e.UpPct > 100 {
					e.UpPct = 100
				}
			}
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Bucket != out[j].Bucket {
			return out[i].Bucket > out[j].Bucket
		}
		return out[i].RouterID < out[j].RouterID
	})
	return out, nil
}
