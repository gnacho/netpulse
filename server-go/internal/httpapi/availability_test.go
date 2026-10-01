// availability_test.go — contrato de GET /api/reports/availability (Fase 15.1):
// agregación por día, semana ISO y mes desde metrics_daily, y normalización
// del día actual parcial por minutos transcurridos.
package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

// availabilityEntry espeja la respuesta (campos que usa el test).
type availabilityEntry struct {
	RouterID string   `json:"routerId"`
	Bucket   string   `json:"bucket"`
	Days     int      `json:"days"`
	UpMin    int64    `json:"upMin"`
	UpPct    float64  `json:"upPct"`
	LatAvg   *float64 `json:"latAvg"`
}

func getAvailability(t *testing.T, ts *testServer, query string) (int, []availabilityEntry) {
	t.Helper()
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	url := ts.URL + "/api/reports/availability" + query
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Cookie", "session="+cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET availability: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	var env struct {
		Items []availabilityEntry `json:"items"`
	}
	if res.StatusCode == http.StatusOK {
		if err := json.Unmarshal(body, &env); err != nil {
			t.Fatalf("decode: %v (%s)", err, body)
		}
	}
	return res.StatusCode, env.Items
}

// TestAvailabilityDayAgrupaPorDia: una fila por router y día; upPct = upMin/1440.
func TestAvailabilityDayAgrupaPorDia(t *testing.T) {
	ts := makeTestServer(t)
	// Fechas relativas a "hoy" (UTC) para que siempre caigan dentro de
	// range=day&n=30 independientemente del día en que corra el CI
	// (antes usaba fechas fijas 2026-08-05 que quedaban fuera de rango
	// y hacían el test flaky según el calendario).
	dayBefore := time.Now().UTC().AddDate(0, 0, -2).Format("2006-01-02")
	yesterday := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	insertDaily(t, ts, "gw", yesterday, 17280, 288, sqlNull{true, 1.0}, 1e8, 5e7, 20, 60) // día completo
	insertDaily(t, ts, "gw", dayBefore, 8640, 144, sqlNull{true, 2.0}, 1e8, 5e7, 20, 60)  // medio día

	status, items := getAvailability(t, ts, "?range=day&n=30")
	if status != http.StatusOK {
		t.Fatalf("status %d, esperaba 200", status)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, esperaba 2 (un daily por día): %+v", len(items), items)
	}
	// Orden: bucket DESC → el día más reciente primero.
	if items[0].Bucket != yesterday {
		t.Fatalf("items[0].bucket = %q, esperaba %s", items[0].Bucket, yesterday)
	}
	byDate := map[string]availabilityEntry{}
	for _, it := range items {
		byDate[it.Bucket] = it
	}
	if byDate[yesterday].UpPct < 99.9 {
		t.Fatalf("día completo upPct=%v, esperaba ~100", byDate[yesterday].UpPct)
	}
	// El otro día es pasado completo → 144 buckets = 720 min de 1440 → 50%.
	if byDate[dayBefore].UpPct < 49.9 || byDate[dayBefore].UpPct > 50.1 {
		t.Fatalf("medio día pasado upPct=%v, esperaba ~50", byDate[dayBefore].UpPct)
	}
}

// TestAvailabilityMonthAgrupaPorMes: agrupa dailies del mismo mes en una fila.
func TestAvailabilityMonthAgrupaPorMes(t *testing.T) {
	ts := makeTestServer(t)
	// Tres días de 2026-07 para el mismo router → un bucket "2026-07".
	insertDaily(t, ts, "gw", "2026-07-10", 17280, 288, sqlNull{false, 0}, 0, 0, 20, 60)
	insertDaily(t, ts, "gw", "2026-07-11", 17280, 288, sqlNull{false, 0}, 0, 0, 20, 60)
	insertDaily(t, ts, "gw", "2026-07-12", 17280, 288, sqlNull{false, 0}, 0, 0, 20, 60)

	status, items := getAvailability(t, ts, "?range=month&n=12")
	if status != http.StatusOK {
		t.Fatalf("status %d, esperaba 200", status)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, esperaba 1 (un bucket 2026-07): %+v", len(items), items)
	}
	if items[0].Bucket != "2026-07" {
		t.Fatalf("bucket = %q, esperaba 2026-07", items[0].Bucket)
	}
	if items[0].Days != 3 {
		t.Fatalf("days = %d, esperaba 3", items[0].Days)
	}
	// 3 días completos = 4320 min / (3*1440) = 100%.
	if items[0].UpPct < 99.9 {
		t.Fatalf("3 días completos upPct=%v, esperaba ~100", items[0].UpPct)
	}
}

// TestAvailabilityWeekIgualQueWeekly: range=week devuelve los mismos buckets
// que /api/reports/weekly para los mismos datos.
func TestAvailabilityWeekIgualQueWeekly(t *testing.T) {
	ts := makeTestServer(t)
	// Usar una fecha dentro de las ultimas 4 semanas para que weekly?weeks=4
	// la incluya independientemente de la fecha actual en CI.
	date := time.Now().UTC().AddDate(0, 0, -7).Format("2006-01-02")
	insertDaily(t, ts, "gw", date, 17280, 288, sqlNull{true, 1.2}, 1e8, 5e7, 20, 60)

	_, wItems := getWeekly(t, ts, "4")
	_, aItems := getAvailability(t, ts, "?range=week&n=4")

	if len(wItems) != len(aItems) {
		t.Fatalf("weekly=%d filas, availability week=%d filas (deberían coincidir)", len(wItems), len(aItems))
	}
	if len(aItems) == 0 || aItems[0].Bucket != wItems[0].Week {
		t.Fatalf("weekly week=%q vs availability bucket=%q", wItems[0].Week, aItems[0].Bucket)
	}
}

// insertRaw siembra muestras raw del router r: una al inicio de cada uno de
// los primeros nBuckets buckets de 5 min del día UTC de hoy. El día en curso
// se mide del raw (el daily llega con el rollup nocturno, #987).
func insertRaw(t *testing.T, ts *testServer, routerID string, nBuckets int) {
	t.Helper()
	now := time.Now().UTC()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	for i := 0; i < nBuckets; i++ {
		tsMs := midnight.Add(time.Duration(i) * 5 * time.Minute).UnixMilli()
		_, err := ts.db.Exec(
			"INSERT INTO metrics (router_id, ts, cpu, ram, temp, latency_ms, rx_bps, tx_bps) VALUES (?,?,?,?,?,?,?,?)",
			routerID, tsMs, 20, 60, 40, 1.5, 1e6, 5e5)
		if err != nil {
			t.Fatalf("insert raw %s: %v", routerID, err)
		}
	}
}

// TestAvailabilityDayActualNoPenaliza: el día de hoy se mide del raw (el
// daily llega con el rollup nocturno y marcaría el día como caído) y se
// normaliza por los minutos transcurridos, no por 1440. Un router online
// desde medianoche da ~100% hoy, sin downtime inventado (#987).
func TestAvailabilityDayActualNoPenaliza(t *testing.T) {
	ts := makeTestServer(t)
	now := time.Now().UTC()
	today := now.Format("2006-01-02")
	nowMin := now.Hour()*60 + now.Minute()
	if nowMin == 0 {
		t.Skip("medianoche UTC: el día en curso aún no tiene minutos que medir")
	}
	// Cobertura continua desde medianoche hasta ahora: una muestra al inicio
	// de cada bucket de 5 min transcurrido (un router online 24/7, #987).
	insertRaw(t, ts, "gw", nowMin/5+1)

	_, items := getAvailability(t, ts, "?range=day&n=5")
	var today_ *availabilityEntry
	for i := range items {
		if items[i].Bucket == today {
			today_ = &items[i]
		}
	}
	if today_ == nil {
		t.Fatalf("no se devolvió fila para hoy %q: %+v", today, items)
	}
	// Cobertura continua → upMin = minutos transcurridos (el bucket actual
	// parcial se tope al divisor) y upPct = 100, no un día "caído". Margen de
	// ±1 min por si el reloj cruza un minuto entre test y handler.
	if today_.UpMin < int64(nowMin)-1 || today_.UpMin > int64(nowMin)+1 {
		t.Fatalf("día actual upMin=%d, esperaba ~%d (minutos transcurridos)", today_.UpMin, nowMin)
	}
	if today_.UpPct < 99.9 || today_.UpPct > 100.1 {
		t.Fatalf("día actual upPct=%v, esperaba ~100 (router online, #987)", today_.UpPct)
	}
}

// TestAvailabilityAgenteOnlineNoInventaDowntime (#987): un router sondeado
// cada 30 s (poll real de prod) o que reporta por agente tiene una n diaria
// muy inferior a las 17280 muestras del poll de 5 s. La fórmula vieja
// (n * 5 s) lo marcaba como caído el 83% del tiempo (17 días fantasma en 4
// semanas). Con la semántica de buckets (up_count) una semana completa con
// datos da ~100% y cero downtime inventado.
func TestAvailabilityAgenteOnlineNoInventaDowntime(t *testing.T) {
	ts := makeTestServer(t)
	// 7 días completos la semana pasada con poll de 30 s: n = 2880 muestras
	// (la fórmula vieja daba upMin = 240 → upPct 16,7%), up_count = 288
	// buckets (día completo con datos).
	monday := mondayOf(time.Now().UTC()).AddDate(0, 0, -7)
	for i := 0; i < 7; i++ {
		date := monday.AddDate(0, 0, i).Format("2006-01-02")
		insertDaily(t, ts, "agent1", date, 2880, 288, sqlNull{true, 2.0}, 1e8, 5e7, 20, 60)
	}

	status, items := getAvailability(t, ts, "?range=week&n=4")
	if status != http.StatusOK {
		t.Fatalf("status %d, esperaba 200", status)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, esperaba 1 (una semana): %+v", len(items), items)
	}
	if items[0].UpMin != 7*1440 {
		t.Fatalf("upMin = %d, esperaba %d (semana completa con datos)", items[0].UpMin, 7*1440)
	}
	if items[0].UpPct < 99.9 {
		t.Fatalf("upPct = %v, esperaba ~100: un router online no puede salir caído (#987)", items[0].UpPct)
	}

	// El endpoint weekly (API/CSV) aplica la misma semántica.
	_, wItems := getWeekly(t, ts, "4")
	var w *weeklyReportEntry
	for i := range wItems {
		if wItems[i].RouterID == "agent1" {
			w = &wItems[i]
		}
	}
	if w == nil {
		t.Fatalf("weekly sin fila para agent1: %+v", wItems)
	}
	if w.UpPct < 99.9 {
		t.Fatalf("weekly upPct = %v, esperaba ~100 (#987)", w.UpPct)
	}
}

// TestAvailabilityValidaParametros: range inválido y n fuera de rango → 400.
func TestAvailabilityValidaParametros(t *testing.T) {
	ts := makeTestServer(t)
	for _, q := range []string{"?range=bogus", "?range=day&n=0", "?range=day&n=999", "?range=month&n=abc", "?range=week&n=-1", "?range=hour&n=0", "?range=hour&n=49"} {
		status, _ := getAvailability(t, ts, q)
		if status != http.StatusBadRequest {
			t.Fatalf("query %q → status %d, esperaba 400", q, status)
		}
	}
	// range válido sin n → 200 (default).
	status, _ := getAvailability(t, ts, "?range=day")
	if status != http.StatusOK {
		t.Fatalf("range=day sin n → status %d, esperaba 200", status)
	}
	if status, _ := getAvailability(t, ts, "?range=hour"); status != http.StatusOK {
		t.Fatalf("range=hour sin n → status %d, esperaba 200", status)
	}
}

// ---------------------------------------------------------------------------
// range=hour (#1032): disponibilidad por hora desde el raw (metrics), con
// relleno de horas sin datos dentro de la ventana.
// ---------------------------------------------------------------------------

// insertRawAt siembra una muestra raw del router r en el instante tsMs
// (epoch ms). El rango hour se mide del raw: metrics_daily no tiene
// granularidad horaria.
func insertRawAt(t *testing.T, ts *testServer, routerID string, tsMs int64) {
	t.Helper()
	_, err := ts.db.Exec(
		"INSERT INTO metrics (router_id, ts, cpu, ram, temp, latency_ms, rx_bps, tx_bps) VALUES (?,?,?,?,?,?,?,?)",
		routerID, tsMs, 20, 60, 40, 1.5, 1e6, 5e5)
	if err != nil {
		t.Fatalf("insert raw %s@%d: %v", routerID, tsMs, err)
	}
}

// fillHour siembra nBuckets muestras (una al inicio de cada bucket de 5 min)
// a partir del inicio de la hora hourStart. 12 = hora completa con datos.
func fillHour(t *testing.T, ts *testServer, routerID string, hourStart time.Time, nBuckets int) {
	t.Helper()
	for i := 0; i < nBuckets; i++ {
		insertRawAt(t, ts, routerID, hourStart.Add(time.Duration(i)*5*time.Minute).UnixMilli())
	}
}

// skipNearHourFlip evita flakiness cuando el reloj está a punto de cambiar de
// hora: los tests siembran relativo a now.Truncate(hour) y el handler
// recalcula "ahora" al servir; un cambio de hora entre ambos movería las
// etiquetas esperadas (mismo criterio que el skip de medianoche en
// TestAvailabilityDayActualNoPenaliza).
func skipNearHourFlip(t *testing.T) {
	t.Helper()
	if m := time.Now().UTC().Minute(); m == 0 || m >= 58 {
		t.Skipf("minuto %d: demasiado cerca del cambio de hora para aserciones estables", m)
	}
}

// TestAvailabilityHourAgrupaPorHoraYRellenaHuecos: una fila por router y
// hora de la ventana (incluida la en curso); las horas sin muestras se
// emiten con upMin/days/upPct 0 (#1032).
func TestAvailabilityHourAgrupaPorHoraYRellenaHuecos(t *testing.T) {
	ts := makeTestServer(t)
	skipNearHourFlip(t)
	hourStart := time.Now().UTC().Truncate(time.Hour)

	// gw: hora completa hace 3 h (12 buckets = 60 min), media hora hace 1 h
	// (6 buckets = 30 min). La hora de hace 2 h queda vacía → hueco rellenado.
	full := hourStart.Add(-3 * time.Hour)
	half := hourStart.Add(-1 * time.Hour)
	fillHour(t, ts, "gw", full, 12)
	fillHour(t, ts, "gw", half, 6)

	status, items := getAvailability(t, ts, "?range=hour&n=6")
	if status != http.StatusOK {
		t.Fatalf("status %d, esperaba 200", status)
	}
	// 6 horas de ventana (hace 5 h .. hora en curso), todas emitidas.
	if len(items) != 6 {
		t.Fatalf("items = %d, esperaba 6 (ventana rellenada): %+v", len(items), items)
	}
	byBucket := map[string]availabilityEntry{}
	for _, it := range items {
		if it.RouterID != "gw" {
			t.Fatalf("router inesperado %q: %+v", it.RouterID, it)
		}
		byBucket[it.Bucket] = it
	}
	fullKey := full.Format("2006-01-02T15")
	halfKey := half.Format("2006-01-02T15")
	gapKey := hourStart.Add(-2 * time.Hour).Format("2006-01-02T15")

	f := byBucket[fullKey]
	if f.Days != 1 || f.UpMin != 60 || f.UpPct < 99.9 {
		t.Fatalf("hora completa: days=%d upMin=%d upPct=%v, esperaba 1/60/~100", f.Days, f.UpMin, f.UpPct)
	}
	h := byBucket[halfKey]
	if h.Days != 1 || h.UpMin != 30 || h.UpPct < 49.9 || h.UpPct > 50.1 {
		t.Fatalf("media hora: days=%d upMin=%d upPct=%v, esperaba 1/30/~50", h.Days, h.UpMin, h.UpPct)
	}
	g := byBucket[gapKey]
	if g.Days != 0 || g.UpMin != 0 || g.UpPct != 0 {
		t.Fatalf("hueco rellenado: days=%d upMin=%d upPct=%v, esperaba 0/0/0", g.Days, g.UpMin, g.UpPct)
	}
	// Orden: bucket DESC → la hora en curso primero.
	if items[0].Bucket != hourStart.Format("2006-01-02T15") {
		t.Fatalf("items[0].bucket = %q, esperaba la hora en curso %s", items[0].Bucket, hourStart.Format("2006-01-02T15"))
	}
}

// TestAvailabilityHourActualNoPenaliza: la hora en curso se normaliza por
// los minutos transcurridos, no por 60 (mismo criterio que el día en curso,
// #987). Un router online desde el inicio de la hora da ~100%.
func TestAvailabilityHourActualNoPenaliza(t *testing.T) {
	ts := makeTestServer(t)
	skipNearHourFlip(t)
	now := time.Now().UTC()
	hourStart := now.Truncate(time.Hour)
	nowMin := now.Minute()

	// Cobertura continua desde el inicio de la hora: una muestra al inicio
	// de cada bucket de 5 min transcurrido.
	fillHour(t, ts, "gw", hourStart, nowMin/5+1)

	_, items := getAvailability(t, ts, "?range=hour&n=6")
	var cur *availabilityEntry
	for i := range items {
		if items[i].Bucket == hourStart.Format("2006-01-02T15") {
			cur = &items[i]
		}
	}
	if cur == nil {
		t.Fatalf("no se devolvió fila para la hora en curso: %+v", items)
	}
	// upMin se tope a los minutos transcurridos (el bucket actual parcial
	// cuenta entero) y upPct = 100. Margen de ±1 min por si el reloj cruza
	// un minuto entre test y handler.
	if cur.UpMin < int64(nowMin)-1 || cur.UpMin > int64(nowMin)+1 {
		t.Fatalf("hora en curso upMin=%d, esperaba ~%d (minutos transcurridos)", cur.UpMin, nowMin)
	}
	if cur.UpPct < 99.9 || cur.UpPct > 100.1 {
		t.Fatalf("hora en curso upPct=%v, esperaba ~100 (router online)", cur.UpPct)
	}
}

// TestAvailabilityHourRouterSinMuestrasNoAparece: un router sin ninguna
// muestra en TODA la ventana no aparece (igual que en los demás rangos); el
// relleno de huecos solo aplica a routers presentes en la ventana.
func TestAvailabilityHourRouterSinMuestrasNoAparece(t *testing.T) {
	ts := makeTestServer(t)
	skipNearHourFlip(t)
	hourStart := time.Now().UTC().Truncate(time.Hour)

	// old: muestras de hace 3 días (dentro de la retención raw de 7 días,
	// fuera de la ventana de 6 h). gw: activo ahora.
	fillHour(t, ts, "old", hourStart.Add(-72*time.Hour), 12)
	fillHour(t, ts, "gw", hourStart.Add(-1*time.Hour), 12)

	_, items := getAvailability(t, ts, "?range=hour&n=6")
	if len(items) != 6 {
		t.Fatalf("items = %d, esperaba 6 (solo gw, ventana rellenada): %+v", len(items), items)
	}
	for _, it := range items {
		if it.RouterID != "gw" {
			t.Fatalf("router fuera de ventana %q no debería aparecer: %+v", it.RouterID, it)
		}
	}
}
