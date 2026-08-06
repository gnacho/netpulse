// reports_test.go — contrato de GET /api/reports/weekly: agregación por
// router y semana ISO desde metrics_daily, validación de weeks y upPct.
package httpapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

// insertDaily siembra un daily del router r para una fecha dada.
func insertDaily(t *testing.T, ts *testServer, routerID, date string, upCount int, lat, rx, tx, cpu, ram float64) {
	t.Helper()
	_, err := ts.db.Exec(
		"INSERT OR REPLACE INTO metrics_daily (router_id, date, n, cpu_avg, ram_avg, temp_avg, lat_avg, rx_avg, tx_avg, rx_total, tx_total, up_min, up_count) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)",
		routerID, date, 288, cpu, ram, 40, lat, rx, tx, rx*288, tx*288, int64(upCount)*5, upCount)
	if err != nil {
		t.Fatalf("insert daily %s/%s: %v", routerID, date, err)
	}
}

// weeklyReportEntry espeja la respuesta (solo los campos que usa el test).
type weeklyReportEntry struct {
	RouterID string  `json:"routerId"`
	Week     string  `json:"week"`
	Days     int     `json:"days"`
	UpMin    int64   `json:"upMin"`
	UpPct    float64 `json:"upPct"`
}

func getWeekly(t *testing.T, ts *testServer, weeks string) (int, []weeklyReportEntry) {
	t.Helper()
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test1234")
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
	insertDaily(t, ts, "gw", "2026-08-03", 288, 1.2, 1e8, 5e7, 20, 60) // lunes completo
	insertDaily(t, ts, "gw", "2026-08-04", 288, 1.4, 1e8, 5e7, 22, 61)
	insertDaily(t, ts, "ap2", "2026-08-03", 144, 2.0, 1e7, 1e7, 10, 40) // media semana

	// Otra semana distinta (2026-W31 = 2026-07-27..08-02) para el mismo router.
	insertDaily(t, ts, "gw", "2026-07-27", 288, 1.0, 9e7, 4e7, 19, 59)

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

	// gw en W32: 2 días completos = 288*2 buckets * 5 min = 2880 min, 100%.
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
	if ap2W32.Days != 1 || ap2W32.UpMin != 720 {
		t.Fatalf("ap2 W32: days=%d upMin=%d, esperaba 1/720", ap2W32.Days, ap2W32.UpMin)
	}
	if ap2W32.UpPct < 49.9 || ap2W32.UpPct > 50.1 {
		t.Fatalf("ap2 W32 upPct=%v, esperaba ~50 (media jornada: 720 de 1440 min)", ap2W32.UpPct)
	}
	if gwW31.Days != 1 || gwW31.UpMin != 1440 {
		t.Fatalf("gw W31: days=%d upMin=%d, esperaba 1/1440", gwW31.Days, gwW31.UpMin)
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
