// reports_test.go — contrato de GET /api/reports/weekly: agregación por
// router y semana ISO desde metrics_daily, validación de weeks y upPct.
package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
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

	// Dos routers en la misma semana ISO 2026-W32 (2026-08-03..09).
	// Día completo = 17280 muestras = 1440 min. latAvg null en prod real.
	insertDaily(t, ts, "gw", "2026-08-03", 17280, sqlNull{true, 1.2}, 1e8, 5e7, 20, 60)
	insertDaily(t, ts, "gw", "2026-08-04", 17280, sqlNull{true, 1.4}, 1e8, 5e7, 22, 61)
	insertDaily(t, ts, "ap2", "2026-08-03", 8640, sqlNull{false, 0}, 1e7, 1e7, 10, 40) // medio día, sin latencia

	// Otra semana distinta (2026-W31 = 2026-07-27..08-02) para el mismo router.
	insertDaily(t, ts, "gw", "2026-07-27", 17280, sqlNull{true, 1.0}, 9e7, 4e7, 19, 59)

	status, items := getWeekly(t, ts, "4")
	if status != http.StatusOK {
		t.Fatalf("status %d, esperaba 200", status)
	}

	// 3 filas: gw en 2 semanas + ap2 en 1.
	if len(items) != 3 {
		t.Fatalf("items = %d, esperaba 3: %+v", len(items), items)
	}

	// La semana más reciente primero (ORDER BY week DESC).
	if items[0].Week != "2026-W32" {
		t.Fatalf("items[0].week = %q, esperaba 2026-W32", items[0].Week)
	}
	if items[0].RouterID != "ap2" { // orden alfabético dentro de la semana: ap2 < gw
		t.Fatalf("items[0].routerId = %q, esperaba ap2", items[0].RouterID)
	}

	// gw en W32: 2 días completos = 2*1440 = 2880 min, 100%.
	var gwW32, gwW31, ap2W32 *weeklyReportEntry
	for i := range items {
		switch {
		case items[i].RouterID == "gw" && items[i].Week == "2026-W32":
			gwW32 = &items[i]
		case items[i].RouterID == "gw" && items[i].Week == "2026-W31":
			gwW31 = &items[i]
		case items[i].RouterID == "ap2" && items[i].Week == "2026-W32":
			ap2W32 = &items[i]
		}
	}
	if gwW32 == nil || gwW31 == nil || ap2W32 == nil {
		t.Fatalf("faltan filas esperadas: gwW32=%v gwW31=%v ap2W32=%v", gwW32, gwW31, ap2W32)
	}
	if gwW32.Days != 2 || gwW32.UpMin != 2880 {
		t.Fatalf("gw W32: days=%d upMin=%d, esperaba 2/2880", gwW32.Days, gwW32.UpMin)
	}
	if gwW32.UpPct < 99.9 || gwW32.UpPct > 100.1 {
		t.Fatalf("gw W32 upPct=%v, esperaba ~100", gwW32.UpPct)
	}
	if gwW32.LatAvg == nil || *gwW32.LatAvg < 1.29 || *gwW32.LatAvg > 1.31 {
		t.Fatalf("gw W32 latAvg=%v, esperaba ~1.3 (media de 1.2/1.4)", gwW32.LatAvg)
	}
	// ap2: medio día = 720 min sobre 1 día → 50%; latencia null (sin datos).
	if ap2W32.Days != 1 || ap2W32.UpMin != 720 {
		t.Fatalf("ap2 W32: days=%d upMin=%d, esperaba 1/720", ap2W32.Days, ap2W32.UpMin)
	}
	if ap2W32.UpPct < 49.9 || ap2W32.UpPct > 50.1 {
		t.Fatalf("ap2 W32 upPct=%v, esperaba ~50 (medio día: 720 de 1440 min)", ap2W32.UpPct)
	}
	if ap2W32.LatAvg != nil {
		t.Fatalf("ap2 W32 latAvg=%v, esperaba null (sin datos de latencia)", *ap2W32.LatAvg)
	}
	if gwW31.Days != 1 || gwW31.UpMin != 1440 {
		t.Fatalf("gw W31: days=%d upMin=%d, esperaba 1/1440", gwW31.Days, gwW31.UpMin)
	}
}

// TestWeeklyReportUpPctClamp: un bucket sobre-poblado (más muestras que
// minutos del día) no puede superar el 100% de disponibilidad (issue #207).
// El weekly clampa igual que availability: divergía porque availability sí
// clavaba y weekly no.
func TestWeeklyReportUpPctClamp(t *testing.T) {
	ts := makeTestServer(t)
	// Día completo = 17280 muestras = 1440 min. El doble (34560) da
	// upMin = 2880 sobre 1440 min → 200% sin el clamp.
	insertDaily(t, ts, "gw", "2026-08-03", 34560, sqlNull{false, 0}, 0, 0, 20, 60)

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
