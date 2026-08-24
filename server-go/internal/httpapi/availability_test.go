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
	insertDaily(t, ts, "gw", "2026-08-06", 17280, sqlNull{true, 1.0}, 1e8, 5e7, 20, 60) // día completo
	insertDaily(t, ts, "gw", "2026-08-05", 8640, sqlNull{true, 2.0}, 1e8, 5e7, 20, 60)  // medio día

	status, items := getAvailability(t, ts, "?range=day&n=30")
	if status != http.StatusOK {
		t.Fatalf("status %d, esperaba 200", status)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, esperaba 2 (un daily por día): %+v", len(items), items)
	}
	// Orden: bucket DESC → 2026-08-06 primero.
	if items[0].Bucket != "2026-08-06" {
		t.Fatalf("items[0].bucket = %q, esperaba 2026-08-06", items[0].Bucket)
	}
	byDate := map[string]availabilityEntry{}
	for _, it := range items {
		byDate[it.Bucket] = it
	}
	if byDate["2026-08-06"].UpPct < 99.9 {
		t.Fatalf("día completo upPct=%v, esperaba ~100", byDate["2026-08-06"].UpPct)
	}
	// 2026-08-05 es un día pasado completo → 8640 muestras = 720 min de 1440 → 50%.
	if byDate["2026-08-05"].UpPct < 49.9 || byDate["2026-08-05"].UpPct > 50.1 {
		t.Fatalf("medio día pasado upPct=%v, esperaba ~50", byDate["2026-08-05"].UpPct)
	}
}

// TestAvailabilityMonthAgrupaPorMes: agrupa dailies del mismo mes en una fila.
func TestAvailabilityMonthAgrupaPorMes(t *testing.T) {
	ts := makeTestServer(t)
	// Tres días de 2026-07 para el mismo router → un bucket "2026-07".
	insertDaily(t, ts, "gw", "2026-07-10", 17280, sqlNull{false, 0}, 0, 0, 20, 60)
	insertDaily(t, ts, "gw", "2026-07-11", 17280, sqlNull{false, 0}, 0, 0, 20, 60)
	insertDaily(t, ts, "gw", "2026-07-12", 17280, sqlNull{false, 0}, 0, 0, 20, 60)

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
	insertDaily(t, ts, "gw", "2026-08-03", 17280, sqlNull{true, 1.2}, 1e8, 5e7, 20, 60)

	_, wItems := getWeekly(t, ts, "4")
	_, aItems := getAvailability(t, ts, "?range=week&n=4")

	if len(wItems) != len(aItems) {
		t.Fatalf("weekly=%d filas, availability week=%d filas (deberían coincidir)", len(wItems), len(aItems))
	}
	if len(aItems) == 0 || aItems[0].Bucket != wItems[0].Week {
		t.Fatalf("weekly week=%q vs availability bucket=%q", wItems[0].Week, aItems[0].Bucket)
	}
}

// TestAvailabilityDayActualNoPenaliza: el día de hoy con datos parciales se
// normaliza por los minutos transcurridos, no por 1440.
func TestAvailabilityDayActualNoPenaliza(t *testing.T) {
	ts := makeTestServer(t)
	today := time.Now().UTC().Format("2006-01-02")
	// 360 min de recolección hoy (medio día "de trabajo"). Si el divisor fuera
	// 1440, daría 25% (parece caído). Con minutos transcurridos, refleja la
	// cobertura real hasta ahora.
	insertDaily(t, ts, "gw", today, 360*12, sqlNull{false, 0}, 0, 0, 20, 60) // 360 min = 4320 muestras

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
	nowMin := time.Now().UTC().Hour()*60 + time.Now().UTC().Minute()
	want := float64(today_.UpMin) / float64(nowMin) * 100
	if nowMin == 0 {
		want = 100 // evitar div/0 a medianoche
	}
	// El endpoint clamp a 100 (mismo tope que el report semanal, #207): si
	// nowMin < upMin el ratio bruto excede 100 y el clamp lo lleva a 100.
	if want > 100 {
		want = 100
	}
	if today_.UpPct < want-0.5 || today_.UpPct > want+0.5 {
		t.Fatalf("día actual upPct=%v, esperaba ~%.1f (upMin=%d / nowMin=%d)", today_.UpPct, want, today_.UpMin, nowMin)
	}
	// Y no debe verse como "caído": si ahora mismo hay cobertura, upPct cerca del 100%.
	if today_.UpPct > 100.1 {
		t.Fatalf("upPct > 100 sin tope: %v", today_.UpPct)
	}
}

// TestAvailabilityValidaParametros: range inválido y n fuera de rango → 400.
func TestAvailabilityValidaParametros(t *testing.T) {
	ts := makeTestServer(t)
	for _, q := range []string{"?range=bogus", "?range=day&n=0", "?range=day&n=999", "?range=month&n=abc", "?range=week&n=-1"} {
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
}
