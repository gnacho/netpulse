// reports_test.go — contrato de GET /api/reports/weekly: agregación por
// router y semana ISO desde metrics_daily, validación de weeks y upPct.
package httpapi_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"
)

// insertDaily siembra un daily del router r para una fecha dada. nSamples es
// el nº de muestras recolectadas (poll 5 s; día completo ≈ 17280 → 1440 min).
func insertDaily(t *testing.T, ts *testServer, routerID, date string, nSamples int, lat sqlNull, rx, tx, cpu, ram float64) {
	t.Helper()
	var latV any
	if lat.valid {
		latV = lat.f
	}
	_, err := ts.db.Exec(
		"INSERT OR REPLACE INTO metrics_daily (router_id, date, n, cpu_avg, ram_avg, temp_avg, lat_avg, rx_avg, tx_avg, rx_total, tx_total, up_min, up_count) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)",
		routerID, date, nSamples, cpu, ram, 40, latV, rx, tx, rx*float64(nSamples), tx*float64(nSamples), 0, nSamples)
	if err != nil {
		t.Fatalf("insert daily %s/%s: %v", routerID, date, err)
	}
}

// sqlNull emula sql.NullFloat64 para el test (sin importar database/sql).
type sqlNull struct {
	valid bool
	f     float64
}

// weeklyReportEntry espeja la respuesta (solo los campos que usa el test).
type weeklyReportEntry struct {
	RouterID string   `json:"routerId"`
	Week     string   `json:"week"`
	Days     int      `json:"days"`
	UpMin    int64    `json:"upMin"`
	UpPct    float64  `json:"upPct"`
	LatAvg   *float64 `json:"latAvg"`
}

func getWeekly(t *testing.T, ts *testServer, weeks string) (int, []weeklyReportEntry) {
	t.Helper()
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")
	url := ts.URL + "/api/reports/weekly"
	if weeks != "" {
		url += "?weeks=" + weeks
	}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Cookie", "session="+cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET weekly: %v", err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	var env struct {
		Items []weeklyReportEntry `json:"items"`
	}
	if res.StatusCode == http.StatusOK {
		if err := json.Unmarshal(body, &env); err != nil {
			t.Fatalf("decode: %v (%s)", err, body)
		}
	}
	return res.StatusCode, env.Items
}

func TestWeeklyReportAgrupaPorRouterYSemana(t *testing.T) {
	ts := makeTestServer(t)

	// Semanas relativas a HOY: el handler filtra por date >= now-7*weeks,
	// así que usar semanas ISO fijas haría el test dependiente de la fecha
	// (fallaba al avanzar la semana actual, CI rojo sin relación con cambios).
	// Semana A = la actual (lunes), Semana B = la anterior.
	now := time.Now().UTC()
	mondayA := mondayOf(now)
	mondayB := mondayA.AddDate(0, 0, -7)
	weekA := isoWeek(mondayA)
	weekB := isoWeek(mondayB)
	dateA := mondayA.Format("2006-01-02")
	dateA2 := mondayA.AddDate(0, 0, 1).Format("2006-01-02")
	dateB := mondayB.Format("2006-01-02")

	// Dos routers en la misma semana A. Día completo = 17280 muestras =
	// 1440 min. latAvg null en prod real.
	insertDaily(t, ts, "gw", dateA, 17280, sqlNull{true, 1.2}, 1e8, 5e7, 20, 60)
	insertDaily(t, ts, "gw", dateA2, 17280, sqlNull{true, 1.4}, 1e8, 5e7, 22, 61)
	insertDaily(t, ts, "ap2", dateA, 8640, sqlNull{false, 0}, 1e7, 1e7, 10, 40) // medio día, sin latencia

	// Otra semana distinta (B, la anterior) para el mismo router.
	insertDaily(t, ts, "gw", dateB, 17280, sqlNull{true, 1.0}, 9e7, 4e7, 19, 59)

	status, items := getWeekly(t, ts, "4")
	if status != http.StatusOK {
		t.Fatalf("status %d, esperaba 200", status)
	}

	// 3 filas: gw en 2 semanas + ap2 en 1.
	if len(items) != 3 {
		t.Fatalf("items = %d, esperaba 3: %+v", len(items), items)
	}

	// La semana más reciente primero (ORDER BY week DESC).
	if items[0].Week != weekA {
		t.Fatalf("items[0].week = %q, esperaba %s", items[0].Week, weekA)
	}
	if items[0].RouterID != "ap2" { // orden alfabético dentro de la semana: ap2 < gw
		t.Fatalf("items[0].routerId = %q, esperaba ap2", items[0].RouterID)
	}

	// gw en semana A: 2 días completos = 2*1440 = 2880 min, 100%.
	var gwA, gwB, ap2A *weeklyReportEntry
	for i := range items {
		switch {
		case items[i].RouterID == "gw" && items[i].Week == weekA:
			gwA = &items[i]
		case items[i].RouterID == "gw" && items[i].Week == weekB:
			gwB = &items[i]
		case items[i].RouterID == "ap2" && items[i].Week == weekA:
			ap2A = &items[i]
		}
	}
	if gwA == nil || gwB == nil || ap2A == nil {
		t.Fatalf("faltan filas esperadas: gwA=%v gwB=%v ap2A=%v", gwA, gwB, ap2A)
	}
	if gwA.Days != 2 || gwA.UpMin != 2880 {
		t.Fatalf("gw semana A: days=%d upMin=%d, esperaba 2/2880", gwA.Days, gwA.UpMin)
	}
	if gwA.UpPct < 99.9 || gwA.UpPct > 100.1 {
		t.Fatalf("gw semana A upPct=%v, esperaba ~100", gwA.UpPct)
	}
	if gwA.LatAvg == nil || *gwA.LatAvg < 1.29 || *gwA.LatAvg > 1.31 {
		t.Fatalf("gw semana A latAvg=%v, esperaba ~1.3 (media de 1.2/1.4)", gwA.LatAvg)
	}
	// ap2: medio día = 720 min sobre 1 día → 50%; latencia null (sin datos).
	if ap2A.Days != 1 || ap2A.UpMin != 720 {
		t.Fatalf("ap2 semana A: days=%d upMin=%d, esperaba 1/720", ap2A.Days, ap2A.UpMin)
	}
	if ap2A.UpPct < 49.9 || ap2A.UpPct > 50.1 {
		t.Fatalf("ap2 semana A upPct=%v, esperaba ~50 (medio día: 720 de 1440 min)", ap2A.UpPct)
	}
	if ap2A.LatAvg != nil {
		t.Fatalf("ap2 semana A latAvg=%v, esperaba null (sin datos de latencia)", *ap2A.LatAvg)
	}
	if gwB.Days != 1 || gwB.UpMin != 1440 {
		t.Fatalf("gw semana B: days=%d upMin=%d, esperaba 1/1440", gwB.Days, gwB.UpMin)
	}
}

// mondayOf devuelve el lunes de la semana ISO que contiene t (reloj UTC).
func mondayOf(t time.Time) time.Time {
	wd := int(t.Weekday())
	if wd == 0 { // Sunday
		wd = 7
	}
	daysSinceMonday := wd - 1
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -daysSinceMonday)
}

// isoWeek devuelve la semana ISO de t con formato "2026-W31".
func isoWeek(t time.Time) string {
	y, w := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// TestWeeklyReportUpPctClamp: un bucket sobre-poblado (más muestras que
// minutos del día) no puede superar el 100% de disponibilidad (issue #207).
// El weekly clampa igual que availability: divergía porque availability sí
// clavaba y weekly no.
func TestWeeklyReportUpPctClamp(t *testing.T) {
	ts := makeTestServer(t)
	// Fecha relativa a hoy (mismo patrón anti-dependencia de la fecha del
	// test de agrupación): lunes de la semana actual.
	dateA := mondayOf(time.Now().UTC()).Format("2006-01-02")
	// Día completo = 17280 muestras = 1440 min. El doble (34560) da
	// upMin = 2880 sobre 1440 min → 200% sin el clamp.
	insertDaily(t, ts, "gw", dateA, 34560, sqlNull{false, 0}, 0, 0, 20, 60)

	status, items := getWeekly(t, ts, "4")
	if status != http.StatusOK {
		t.Fatalf("status %d, esperaba 200", status)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, esperaba 1 (un bucket): %+v", len(items), items)
	}
	if items[0].UpPct != 100 {
		t.Fatalf("bucket sobre-poblado upPct=%v, esperaba 100 (clamp)", items[0].UpPct)
	}
}

func TestWeeklyReportValidaWeeks(t *testing.T) {
	ts := makeTestServer(t)
	for _, bad := range []string{"0", "53", "abc", "-1"} {
		status, _ := getWeekly(t, ts, bad)
		if status != http.StatusBadRequest {
			t.Fatalf("weeks=%q → status %d, esperaba 400", bad, status)
		}
	}
	// Por defecto (sin query) → 200.
	status, _ := getWeekly(t, ts, "")
	if status != http.StatusOK {
		t.Fatalf("sin weeks → status %d, esperaba 200", status)
	}
}

func TestWeeklyReportRequiereSesion(t *testing.T) {
	ts := makeTestServer(t)
	res, err := http.Get(ts.URL + "/api/reports/weekly")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer res.Body.Close()
	io.Copy(io.Discard, res.Body)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d, esperaba 401", res.StatusCode)
	}
}
