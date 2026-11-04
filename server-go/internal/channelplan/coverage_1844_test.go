package channelplan_test

import (
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/channelplan"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

// #1844: el bloque actual se identifica por cobertura, así que un canal ancho
// cuyo primario no es el del bloque cuenta como ese bloque. Antes se comparaba
// por igualdad, ningún bloque coincidía con el 48 y el motor caía en la rama de
// DFS, que mueve el canal sin mirar vecinas. A 80 MHz el 36 y el 48 son el
// mismo bloque: la sugerencia no cambiaba de canal y no había una sola medida
// detrás.
func TestRecommendBloqueActualPorCobertura(t *testing.T) {
	cases := []struct {
		name    string
		radio   probe.Radio
		want    int
		explain string
	}{
		{"5 GHz 20", probe.Radio{Name: "5 GHz", Channel: 48, WidthMhz: 20}, 48, "canal primario de su propio bloque"},
		{"5 GHz 40", probe.Radio{Name: "5 GHz", Channel: 44, WidthMhz: 40}, 44, "el bloque 44 cubre 44-48"},
		{"5 GHz 80", probe.Radio{Name: "5 GHz", Channel: 48, WidthMhz: 80}, 48, "el bloque 36 cubre 36-48"},
		{"5 GHz 160", probe.Radio{Name: "5 GHz", Channel: 48, WidthMhz: 160}, 48, "sin bloque no-DFS, se mantiene"},
		{"6 GHz 80", probe.Radio{Name: "6 GHz", Channel: 9, WidthMhz: 80}, 9, "el bloque 1 cubre 1-13 y a ese ancho es recomendable"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, err := db.Open(t.TempDir())
			if err != nil {
				t.Fatalf("db: %v", err)
			}
			defer d.Close()

			st := channelplan.NewStore(d.DB)
			recs, err := st.Recommend("rt1", []probe.Radio{c.radio}, time.Hour)
			if err != nil {
				t.Fatalf("recommend: %v", err)
			}
			if len(recs) != 1 {
				t.Fatalf("esperaba 1 radio, got %d", len(recs))
			}
			if recs[0].Recommended != c.want {
				t.Fatalf("sin medidas se mantiene el canal (%s): got %d, want %d", c.explain, recs[0].Recommended, c.want)
			}
			if recs[0].CurrentScore != 0 {
				t.Fatalf("sin vecinas el score actual debe ser 0: got %d", recs[0].CurrentScore)
			}
		})
	}
}

// Las reglas que sí deben mover el canal sin medidas siguen en pie: el solape
// de 2.4 GHz (solo 1/6/11 son ortodoxos) y los PSC de 6 GHz a 20 MHz.
func TestRecommendSinMedidasMantieneReglasDeSolape(t *testing.T) {
	cases := []struct {
		name  string
		radio probe.Radio
		want  int
	}{
		{"2.4 GHz en 3", probe.Radio{Name: "2.4 GHz", Channel: 3, WidthMhz: 20}, 1},
		{"6 GHz no PSC en 9", probe.Radio{Name: "6 GHz", Channel: 9, WidthMhz: 20}, 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, err := db.Open(t.TempDir())
			if err != nil {
				t.Fatalf("db: %v", err)
			}
			defer d.Close()

			st := channelplan.NewStore(d.DB)
			recs, err := st.Recommend("rt1", []probe.Radio{c.radio}, time.Hour)
			if err != nil {
				t.Fatalf("recommend: %v", err)
			}
			if recs[0].Recommended != c.want {
				t.Fatalf("got %d, want %d", recs[0].Recommended, c.want)
			}
		})
	}
}

// Con medidas se mantiene el umbral del 30 por ciento, y el motor puede
// sugerir otro bloque aunque el canal actual esté dentro del suyo.
func TestRecommendCoberturaConVecinas(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	st := channelplan.NewStore(d.DB)
	now := time.Now().Unix()
	// Vecina fuerte en el 36, primario del bloque 36-48 que ocupa el radio.
	scans := []probe.ScanResult{
		{Iface: "wlan1", BSSID: "AA:BB:CC:DD:EE:01", SSID: "vecina", Channel: 36, Freq: 5180, Signal: -50},
	}
	if err := st.SaveScan("rt1", now, scans); err != nil {
		t.Fatalf("save: %v", err)
	}

	recs, err := st.Recommend("rt1", []probe.Radio{{Name: "5 GHz", Channel: 48, WidthMhz: 80}}, time.Hour)
	if err != nil {
		t.Fatalf("recommend: %v", err)
	}
	if recs[0].Recommended != 149 {
		t.Fatalf("el bloque 36-48 está ocupado y el 149 limpio: got %d", recs[0].Recommended)
	}
	if recs[0].CurrentScore == 0 {
		t.Fatalf("el score actual debe reflejar la vecina del bloque: %+v", recs[0])
	}
}
