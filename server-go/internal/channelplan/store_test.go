package channelplan_test

import (
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/channelplan"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

func TestSaveAndRecentScans(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	st := channelplan.NewStore(d.DB)
	scans := []probe.ScanResult{
		{Iface: "wlan0", BSSID: "00:11:22:33:44:55", SSID: "vecino", Channel: 6, Freq: 2437, Signal: -62},
	}
	if err := st.SaveScan("rt1", time.Now().Unix(), scans); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := st.RecentScans("rt1", time.Hour)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("esperaba 1 scan, got %d", len(got))
	}
	if got[0].BSSID != "00:11:22:33:44:55" || got[0].Signal != -62 {
		t.Errorf("scan incorrecto: %+v", got[0])
	}
}

// TestRecommendDevuelveScores (#1070/#1076): el informe puntúa TODOS los
// bloques de la banda a su ancho, DFS incluidos (informativos); los
// recomendables son no-DFS y el mejor de ellos es el Recommended.
func TestRecommendDevuelveScores(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	st := channelplan.NewStore(d.DB)
	recs, err := st.Recommend("rt1", []probe.Radio{{Name: "5 GHz", Channel: 36, WidthMhz: 80}}, time.Hour)
	if err != nil {
		t.Fatalf("recommend: %v", err)
	}
	if len(recs) != 1 || len(recs[0].Scores) == 0 {
		t.Fatalf("esperaba scores, got %+v", recs)
	}
	byCh := map[int]channelplan.ChannelScore{}
	var dfsCount int
	for _, c := range recs[0].Scores {
		if c.DFS {
			dfsCount++
		}
		byCh[c.Channel] = c
	}
	if dfsCount == 0 {
		t.Errorf("a 80 MHz deben puntuarse también bloques DFS: %+v", recs[0].Scores)
	}
	rec := recs[0]
	// #1214: sin scans que la tienda conozca, el motor MANTIENE el canal
	// actual (no hay dato con el que opinar).
	if rec.Recommended != rec.Channel {
		t.Errorf("sin datos se debe mantener el canal actual: Recommended=%d, canal=%d", rec.Recommended, rec.Channel)
	}
	// #1076: el rango 68-92 (5350-5470 MHz) no es RLAN en ETSI: se puntúa
	// pero nunca se recomienda.
	if b, ok := byCh[68]; ok && b.Recommendable {
		t.Errorf("el bloque 68 no debe ser recomendable (no RLAN ETSI): %+v", b)
	}
	if b, ok := byCh[52]; !ok || !b.DFS {
		t.Errorf("el bloque 52-64 debe marcarse DFS: %+v", b)
	}
}

// TestRecommendScores24GHzTodosLosCanales (#1076): a 2.4 GHz se puntúan los
// 13 canales (no solo los no solapados 1/6/11), todos recomendables.
func TestRecommendScores24GHzTodosLosCanales(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	st := channelplan.NewStore(d.DB)
	recs, err := st.Recommend("rt1", []probe.Radio{{Name: "2.4 GHz", Channel: 6, WidthMhz: 20}}, time.Hour)
	if err != nil {
		t.Fatalf("recommend: %v", err)
	}
	if len(recs) != 1 || len(recs[0].Scores) != 13 {
		t.Fatalf("esperaba 13 scores en 2.4 GHz, got %+v", recs)
	}
	for _, s := range recs[0].Scores {
		want := s.Channel == 1 || s.Channel == 6 || s.Channel == 11
		if s.Recommendable != want || s.DFS {
			t.Errorf("recommendable solo en 1/6/11 (#631): %+v", s)
		}
	}
}

// TestRecentScansOwnPorSemillaExacta (#1082): un BSSID que el agente
// reporta como propio (dawn/usteer local=true) se marca own aunque no case
// ni por prefijo ni por transitividad de SSID.
func TestRecentScansOwnPorSemillaExacta(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	st := channelplan.NewStore(d.DB)
	st.SetOwnSeeds(func() []string { return []string{"1e:bf:ce:02:7f:48"} })
	now := time.Now().Unix()
	scans := []probe.ScanResult{
		{Iface: "wlan0", BSSID: "1E:BF:CE:02:7F:48", SSID: "Casa-2.4", Channel: 1, Freq: 2412, Signal: -53},
		{Iface: "wlan0", BSSID: "00:11:22:33:44:55", SSID: "vecino", Channel: 6, Freq: 2437, Signal: -60},
	}
	if err := st.SaveScan("rt1", now, scans); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := st.RecentScans("rt1", time.Hour)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	bySSID := map[string]bool{}
	for _, r := range got {
		bySSID[r.SSID] = r.Own
	}
	if !bySSID["Casa-2.4"] {
		t.Errorf("la semilla exacta debe marcar own: %+v", got)
	}
	if bySSID["vecino"] {
		t.Errorf("vecino NO debe ser own: %+v", got)
	}
}

// TestRecentScansOwnPorSSIDTransitable (#1076): si un BSSID de un SSID es
// propio por prefijo MAC, el resto de filas del mismo SSID también se
// marcan propias aunque su MAC no case (guest de la misma unidad).
func TestRecentScansOwnPorSSIDTransitable(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()
	if _, err := d.DB.Exec(`INSERT INTO routers (id, name, host, type, mac, is_gateway, created_at)
		VALUES ('rt2', 'RT2 AX6', '192.168.1.2', 'openwrt', '8C:DE:F9:33:71:58', 0, ?)`,
		time.Now().UnixMilli()); err != nil {
		t.Fatalf("insert router: %v", err)
	}

	st := channelplan.NewStore(d.DB)
	now := time.Now().Unix()
	// La main BSS de la malla casa con el prefijo; la guest (misma unidad,
	// MAC distinta) no.
	scans := []probe.ScanResult{
		{Iface: "wlan0", BSSID: "8C:DE:F9:33:71:59", SSID: "casa-guest", Channel: 6, Freq: 2437, Signal: -30},
		{Iface: "wlan0", BSSID: "1E:BF:CE:02:7F:48", SSID: "casa-guest", Channel: 6, Freq: 2437, Signal: -53},
		{Iface: "wlan0", BSSID: "00:11:22:33:44:55", SSID: "vecino", Channel: 11, Freq: 2462, Signal: -80},
	}
	if err := st.SaveScan("rt1", now, scans); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := st.RecentScans("rt1", time.Hour)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	bySSID := map[string]bool{}
	for _, r := range got {
		bySSID[r.SSID] = r.Own
	}
	if !bySSID["casa-guest"] {
		t.Errorf("toda fila de casa-guest debe ser own (transitividad): %+v", got)
	}
	if bySSID["vecino"] {
		t.Errorf("vecino NO debe ser own: %+v", got)
	}
}

// TestRecommendGuestSSIDNoCongestiona (#1080): el scoring excluye las redes
// propias por el flag Own (prefijo MAC + transitividad SSID): el guest de una
// unidad, con MAC que no casa pero mismo SSID que una BSS de flota, no debe
// ensuciar su canal ni empujar la recomendación a otro.
func TestRecommendGuestSSIDNoCongestiona(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()
	if _, err := d.DB.Exec(`INSERT INTO routers (id, name, host, type, mac, is_gateway, created_at)
		VALUES ('rt2', 'RT2 AX6', '192.168.1.2', 'openwrt', '8C:DE:F9:33:71:58', 0, ?)`,
		time.Now().UnixMilli()); err != nil {
		t.Fatalf("insert router: %v", err)
	}

	st := channelplan.NewStore(d.DB)
	now := time.Now().Unix()
	scans := []probe.ScanResult{
		{Iface: "wlan0", BSSID: "8C:DE:F9:33:71:59", SSID: "casa", Channel: 11, Freq: 2462, Signal: -30},
		{Iface: "wlan0", BSSID: "1E:BF:CE:02:7F:48", SSID: "casa", Channel: 1, Freq: 2412, Signal: -30},
		{Iface: "wlan0", BSSID: "00:11:22:33:44:55", SSID: "vecino", Channel: 6, Freq: 2437, Signal: -60},
	}
	if err := st.SaveScan("rt1", now, scans); err != nil {
		t.Fatalf("save: %v", err)
	}
	recs, err := st.Recommend("rt1", []probe.Radio{{Name: "2.4 GHz", Channel: 6, WidthMhz: 20}}, time.Hour)
	if err != nil {
		t.Fatalf("recommend: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("esperaba 1 radio, got %+v", recs)
	}
	// El guest de "casa" en ch1 no congestiona: con ch6 ocupada por el vecino
	// (-60) y ch11 por la propia, el 1 (limpio y ortodoxo) es el sugerido.
	if recs[0].Recommended != 1 {
		t.Fatalf("el guest propio no debe congestinar: rec %+v", recs[0])
	}
}

// TestRecommendFuerzaSobreCantidad (#1080): con ponderación en dominio de
// potencia, UNA vecina fuerte (-50) ensucia su canal más que CUATRO de
// juguete (-85): el motor sugiere el de las débiles.
func TestRecommendFuerzaSobreCantidad(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	st := channelplan.NewStore(d.DB)
	now := time.Now().Unix()
	// ch6: UNA vecina fuerte (-50). ch11: CUATRO de juguete (-85).
	// ch1: dos mediocres (-70) que no alteran el orden 11 < 1 << 6.
	scans := []probe.ScanResult{
		{Iface: "wlan0", BSSID: "AA:AA:AA:AA:AA:01", SSID: "fuerte", Channel: 6, Freq: 2437, Signal: -50},
		{Iface: "wlan0", BSSID: "CC:CC:CC:CC:CC:01", SSID: "m1", Channel: 1, Freq: 2412, Signal: -70},
		{Iface: "wlan0", BSSID: "CC:CC:CC:CC:CC:02", SSID: "m2", Channel: 1, Freq: 2412, Signal: -70},
		{Iface: "wlan0", BSSID: "BB:BB:BB:BB:BB:01", SSID: "d1", Channel: 11, Freq: 2462, Signal: -85},
		{Iface: "wlan0", BSSID: "BB:BB:BB:BB:BB:02", SSID: "d2", Channel: 11, Freq: 2462, Signal: -85},
		{Iface: "wlan0", BSSID: "BB:BB:BB:BB:BB:03", SSID: "d3", Channel: 11, Freq: 2462, Signal: -85},
		{Iface: "wlan0", BSSID: "BB:BB:BB:BB:BB:04", SSID: "d4", Channel: 11, Freq: 2462, Signal: -85},
	}
	if err := st.SaveScan("rt1", now, scans); err != nil {
		t.Fatalf("save: %v", err)
	}
	recs, err := st.Recommend("rt1", []probe.Radio{{Name: "2.4 GHz", Channel: 1, WidthMhz: 20}}, time.Hour)
	if err != nil {
		t.Fatalf("recommend: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("esperaba 1 radio, got %+v", recs)
	}
	if recs[0].Recommended != 11 {
		t.Fatalf("una vecina fuerte debe pesar mas que cuatro de juguete: rec %+v", recs[0])
	}
}

// TestRecentScansMarcaMallaPropia (#1070): los BSSIDs de la propia flota
// deben llegar a la UI marcados (own) para destacarse en el informe.
func TestRecentScansMarcaMallaPropia(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()
	if _, err := d.DB.Exec(`INSERT INTO routers (id, name, host, type, mac, is_gateway, created_at)
		VALUES ('rt2', 'RT2 AX6', '192.168.1.2', 'openwrt', '8C:DE:F9:33:71:58', 0, ?)`,
		time.Now().UnixMilli()); err != nil {
		t.Fatalf("insert router: %v", err)
	}

	st := channelplan.NewStore(d.DB)
	now := time.Now().Unix()
	scans := []probe.ScanResult{
		{Iface: "wlan0", BSSID: "8C:DE:F9:33:71:59", SSID: "propia", Channel: 1, Freq: 2412, Signal: -30},
		{Iface: "wlan0", BSSID: "00:11:22:33:44:55", SSID: "vecino", Channel: 6, Freq: 2437, Signal: -62},
	}
	if err := st.SaveScan("rt1", now, scans); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := st.RecentScans("rt1", time.Hour)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("esperaba 2 scans, got %d", len(got))
	}
	for _, r := range got {
		if r.SSID == "propia" && !r.Own {
			t.Errorf("la malla propia debe marcarse own: %+v", r)
		}
		if r.SSID == "vecino" && r.Own {
			t.Errorf("el vecino ajeno NO debe marcarse own: %+v", r)
		}
	}
}

// TestRecentScansDedupBSSID (#475): cada push reinserta los vecinos; la
// lectura debe devolver UNA fila por BSSID (la observación más reciente).
func TestRecentScansDedupBSSID(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	st := channelplan.NewStore(d.DB)
	now := time.Now().Unix()
	// Push 1: vecino A (-70) y vecino B (-50).
	if err := st.SaveScan("rt1", now-60, []probe.ScanResult{
		{Iface: "wlan0", BSSID: "AA:AA:AA:AA:AA:01", SSID: "a", Channel: 1, Freq: 2412, Signal: -70},
		{Iface: "wlan0", BSSID: "BB:BB:BB:BB:BB:02", SSID: "b", Channel: 6, Freq: 2437, Signal: -50},
	}); err != nil {
		t.Fatalf("save1: %v", err)
	}
	// Push 2 (30 s después): el vecino A ahora se oye más fuerte (-55).
	if err := st.SaveScan("rt1", now-30, []probe.ScanResult{
		{Iface: "wlan0", BSSID: "AA:AA:AA:AA:AA:01", SSID: "a", Channel: 1, Freq: 2412, Signal: -55},
		{Iface: "wlan0", BSSID: "BB:BB:BB:BB:BB:02", SSID: "b", Channel: 6, Freq: 2437, Signal: -50},
	}); err != nil {
		t.Fatalf("save2: %v", err)
	}

	got, err := st.RecentScans("rt1", time.Hour)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("esperaba 2 vecinos dedup, got %d: %+v", len(got), got)
	}
	for _, r := range got {
		if r.BSSID == "AA:AA:AA:AA:AA:01" && r.Signal != -55 {
			t.Errorf("el vecino A debe reportar la señal MÁS RECIENTE (-55), got %d", r.Signal)
		}
	}
}

func TestRecommendPrefiereCanalLibre(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	st := channelplan.NewStore(d.DB)
	now := time.Now().Unix()
	// Canal 6 con vecino fuerte, canal 1 y 11 libres.
	scans := []probe.ScanResult{
		{Iface: "wlan0", BSSID: "00:11:22:33:44:55", SSID: "A", Channel: 6, Freq: 2437, Signal: -50},
	}
	if err := st.SaveScan("rt1", now, scans); err != nil {
		t.Fatalf("save: %v", err)
	}

	radios := []probe.Radio{{Name: "2.4 GHz", Channel: 6, WidthMhz: 20}}
	recs, err := st.Recommend("rt1", radios, time.Hour)
	if err != nil {
		t.Fatalf("recommend: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("esperaba 1 recomendación, got %d", len(recs))
	}
	if recs[0].Recommended == 6 {
		t.Errorf("no debería recomendar el canal 6 ocupado: %+v", recs[0])
	}
	if recs[0].Recommended != 1 && recs[0].Recommended != 11 {
		t.Errorf("debería recomendar 1 o 11, got %d", recs[0].Recommended)
	}
}

func TestRecommendSinVecinos(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	st := channelplan.NewStore(d.DB)
	radios := []probe.Radio{{Name: "5 GHz", Channel: 36, WidthMhz: 80}}
	recs, err := st.Recommend("rt1", radios, time.Hour)
	if err != nil {
		t.Fatalf("recommend: %v", err)
	}
	if len(recs) != 1 || recs[0].Recommended != 36 {
		t.Fatalf("sin vecinos debería mantener canal actual: %+v", recs)
	}
}

func TestPrune(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	st := channelplan.NewStore(d.DB)
	old := time.Now().Add(-48 * time.Hour).Unix()
	if err := st.SaveScan("rt1", old, []probe.ScanResult{{Iface: "wlan0", BSSID: "00:11:22:33:44:55", Channel: 6, Freq: 2437, Signal: -60}}); err != nil {
		t.Fatalf("save old: %v", err)
	}
	if err := st.Prune(24 * time.Hour); err != nil {
		t.Fatalf("prune: %v", err)
	}
	got, err := st.RecentScans("rt1", 72*time.Hour)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("esperaba 0 tras prune, got %d", len(got))
	}
}

// TestRecommendExcluyeMallaPropia (#631): un AP de la propia malla (BSSID que
// comparte los 5 primeros octetos con la MAC de un router monitorizado) no
// debe puntuar como vecino, así el canal que ocupa queda libre para
// recomendarlo.
func TestRecommendExcluyeMallaPropia(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()
	if _, err := d.DB.Exec(`INSERT INTO routers (id, name, host, type, mac, is_gateway, created_at)
		VALUES ('rt2', 'RT2 AX6', '192.168.1.2', 'openwrt', '8C:DE:F9:33:71:58', 0, ?)`,
		time.Now().UnixMilli()); err != nil {
		t.Fatalf("insert router: %v", err)
	}

	st := channelplan.NewStore(d.DB)
	now := time.Now().Unix()
	// La propia malla (prefijo 8C:DE:F9:33:71) ocupa el canal 1 con señal
	// fortísima (-30); sin la exclusión penalizaría el 1 y recomendaría 6/11.
	scans := []probe.ScanResult{
		{Iface: "wlan0", BSSID: "8C:DE:F9:33:71:59", SSID: "temiscira", Channel: 1, Freq: 2412, Signal: -30},
	}
	if err := st.SaveScan("rt1", now, scans); err != nil {
		t.Fatalf("save: %v", err)
	}

	recs, err := st.Recommend("rt1", []probe.Radio{{Name: "2.4 GHz", Channel: 1, WidthMhz: 20}}, time.Hour)
	if err != nil {
		t.Fatalf("recommend: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("esperaba 1 radio, got %d", len(recs))
	}
	// Con el AP propio excluido, el canal 1 (ocupado solo por la malla) queda
	// libre y es el primero de la lista no-DFS (1,6,11) con score 0.
	if recs[0].Recommended != 1 {
		t.Fatalf("la propia malla debería excluirse y dejar libre el 1, got %d", recs[0].Recommended)
	}
}

// TestRecommendAnchura80NoCruzaDfs (#631): a 80 MHz el canal 44 ocupa el
// bloque 44-56 que entra en canales DFS (52+), así que no debe ofrecerse como
// candidato; se recomienda el 36, único bloque UNII-1 no-DFS a ese ancho.
func TestRecommendAnchura80NoCruzaDfs(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	st := channelplan.NewStore(d.DB)
	recs, err := st.Recommend("rt1", []probe.Radio{{Name: "5 GHz", Channel: 44, WidthMhz: 80}}, time.Hour)
	if err != nil {
		t.Fatalf("recommend: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("esperaba 1 radio, got %d", len(recs))
	}
	if recs[0].Recommended == 44 {
		t.Fatalf("a 80 MHz el 44 cruza a DFS y no debe recomendarse: %+v", recs[0])
	}
	if recs[0].Recommended != 36 {
		t.Fatalf("a 80 MHz solo el bloque 36-48 es no-DFS: got %d", recs[0].Recommended)
	}
}

func TestRecommendPasaSeccion(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	st := channelplan.NewStore(d.DB)
	radios := []probe.Radio{
		{Name: "2.4 GHz", Channel: 1, WidthMhz: 20, Section: "radio0"},
		{Name: "5 GHz", Channel: 44, WidthMhz: 80},
	}
	recs, err := st.Recommend("rt1", radios, time.Hour)
	if err != nil {
		t.Fatalf("recommend: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("esperaba 2 recomendaciones, got %d", len(recs))
	}
	var sec, nosec *channelplan.Radio
	for i := range recs {
		if recs[i].Section != "" {
			sec = &recs[i]
		} else {
			nosec = &recs[i]
		}
	}
	if sec == nil || nosec == nil {
		t.Fatalf("esperaba una radio con sección y otra sin: %+v", recs)
	}
	if sec.Section != "radio0" || sec.Iface != "radio0" {
		t.Errorf("la sección no pasa al plan: %+v", *sec)
	}
	if nosec.Iface == "" {
		t.Errorf("sin sección el iface debe caer al placeholder: %+v", *nosec)
	}
}

func TestRecommendCurrentDfsChannelScoreNotMaxInt(t *testing.T) {
	// #518: si el canal ACTUAL es DFS (112) y no está en candidateChannels
	// (solo no-DFS), su score debe calcularse igual (es informativo) y no
	// quedarse en MaxInt (la UI pintaba 9223372036854775807).
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	st := channelplan.NewStore(d.DB)
	now := time.Now().Unix()
	scans := []probe.ScanResult{
		{Iface: "wlan1", BSSID: "aa:bb:cc:dd:ee:01", SSID: "V", Channel: 112, Freq: 5560, Signal: -60},
		{Iface: "wlan1", BSSID: "aa:bb:cc:dd:ee:02", SSID: "W", Channel: 44, Freq: 5220, Signal: -55},
		{Iface: "wlan1", BSSID: "aa:bb:cc:dd:ee:03", SSID: "X", Channel: 116, Freq: 5580, Signal: -70},
	}
	if err := st.SaveScan("rt1", now, scans); err != nil {
		t.Fatalf("save: %v", err)
	}

	radios := []probe.Radio{{Name: "5 GHz", Channel: 112, WidthMhz: 80}}
	recs, err := st.Recommend("rt1", radios, time.Hour)
	if err != nil {
		t.Fatalf("recommend: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("esperaba 1 radio, got %d", len(recs))
	}
	rec := recs[0]
	if rec.CurrentScore > 1_000_000_000_000 {
		t.Fatalf("currentScore no debe ser MaxInt (canal DFS): %d", rec.CurrentScore)
	}
	if rec.Recommended == 112 {
		t.Errorf("no debería recomendar el DFS 112 ocupado: %+v", rec)
	}
	if rec.Recommended != 36 {
		t.Errorf("debería recomendar el 36 libre (no-DFS), got %d", rec.Recommended)
	}
}

// TestRecommendConservador (#1214): el motor no sugiere cambios frívolos.
// (a) canal actual limpio → mantiene (0). (b) canal actual sucio con un
// bloque mucho más limpio → sugiere, y el sugerido es recomendable no-DFS.
// (c) empate técnico (ruido similar en el actual y el mejor) → mantiene.
func TestRecommendConservador(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()
	st := channelplan.NewStore(d.DB)
	now := time.Now().Unix()

	// (a) 2.4: una vecina débil en ch11; el actual es ch1 (limpio) → mantiene.
	if err := st.SaveScan("rt1", now, []probe.ScanResult{
		{BSSID: "00:11:22:33:44:01", SSID: "vecino", Channel: 11, Freq: 2462, Signal: -88},
	}); err != nil {
		t.Fatalf("save a: %v", err)
	}
	recs, err := st.Recommend("rt1", []probe.Radio{{Name: "2.4 GHz", Channel: 1, WidthMhz: 20}}, time.Hour)
	if err != nil {
		t.Fatalf("recommend a: %v", err)
	}
	// (a) el canal actual está limpio: se MANTIENE (Recommended = actual).
	if recs[0].Recommended != 1 {
		t.Errorf("(a) canal limpio: se espera mantener 1: %+v", recs[0])
	}

	// (b) ch6 con un vecino FUERTE (-40); 1 y 11 limpios → sugiere 1 o 11.
	if err := st.SaveScan("rt2", now, []probe.ScanResult{
		{BSSID: "00:11:22:33:44:02", SSID: "vecino", Channel: 6, Freq: 2437, Signal: -40},
		{BSSID: "00:11:22:33:44:03", SSID: "vecino", Channel: 11, Freq: 2462, Signal: -88},
	}); err != nil {
		t.Fatalf("save b: %v", err)
	}
	recs, err = st.Recommend("rt2", []probe.Radio{{Name: "2.4 GHz", Channel: 6, WidthMhz: 20}}, time.Hour)
	if err != nil {
		t.Fatalf("recommend b: %v", err)
	}
	if recs[0].Recommended != 1 && recs[0].Recommended != 11 {
		t.Errorf("(b) canal sucio: se esperaba sugerencia a 1 o 11: %+v", recs[0])
	}
	for _, s := range recs[0].Scores {
		if s.Channel == recs[0].Recommended && (s.DFS || !s.Recommendable) {
			t.Errorf("(b) el sugerido debe ser no-DFS recomendable: %+v", s)
		}
	}

	// (c) empate técnico: los tres canales ortodoxos igual de ocupados (-70
	// en 1, 6 y 11) → no hay mejora apreciable → se mantiene el actual.
	if err := st.SaveScan("rt3", now, []probe.ScanResult{
		{BSSID: "00:11:22:33:44:04", SSID: "vecino", Channel: 6, Freq: 2437, Signal: -70},
		{BSSID: "00:11:22:33:44:05", SSID: "vecino", Channel: 1, Freq: 2412, Signal: -70},
		{BSSID: "00:11:22:33:44:06", SSID: "vecino", Channel: 11, Freq: 2462, Signal: -70},
	}); err != nil {
		t.Fatalf("save c: %v", err)
	}
	recs, err = st.Recommend("rt3", []probe.Radio{{Name: "2.4 GHz", Channel: 6, WidthMhz: 20}}, time.Hour)
	if err != nil {
		t.Fatalf("recommend c: %v", err)
	}
	// (c) empate técnico: se mantiene el actual (Recommended = canal actual).
	if recs[0].Recommended != 6 {
		t.Errorf("(c) empate técnico: se espera mantener 6: %+v", recs[0])
	}
}
