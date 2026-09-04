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
