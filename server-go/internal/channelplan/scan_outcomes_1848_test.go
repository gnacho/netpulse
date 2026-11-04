package channelplan_test

import (
	"testing"

	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/channelplan"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

// #1848: el motivo de un scan rechazado tiene que sobrevivir al push y salir
// tal cual, porque es lo unico que distingue "el driver lo rechazo" de "no
// habia vecinos".
func TestScanOutcomesRoundTrip(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	st := channelplan.NewStore(d.DB)
	out := []probe.ScanOutcome{
		{Iface: "eth6", Phy: 1, BSS: 0, Error: "command failed: Operation not supported (-95)"},
		{Iface: "eth5", Phy: 0, BSS: 16, Error: ""},
	}
	if err := st.SaveScanOutcomes("rt1", 1000, out); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := st.ScanOutcomes("rt1")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("esperaba 2 outcomes, got %d", len(got))
	}
	byIface := map[string]channelplan.ScanOutcomeRow{}
	for _, oc := range got {
		byIface[oc.Iface] = oc
	}
	if oc := byIface["eth6"]; oc.Phy != 1 || oc.BSS != 0 || oc.Error == "" || oc.Ts != 1000 {
		t.Fatalf("outcome del rechazo mal guardado: %+v", oc)
	}
	if oc := byIface["eth5"]; oc.Phy != 0 || oc.BSS != 16 || oc.Error != "" {
		t.Fatalf("outcome del scan correcto mal guardado: %+v", oc)
	}
}

// El ultimo estado manda: dos ciclos del mismo radio no dejan dos filas.
func TestScanOutcomesUpsert(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	st := channelplan.NewStore(d.DB)
	if err := st.SaveScanOutcomes("rt1", 1000, []probe.ScanOutcome{
		{Iface: "eth6", Phy: 1, BSS: 0, Error: "Resource temporarily unavailable (-11)"},
	}); err != nil {
		t.Fatalf("save 1: %v", err)
	}
	if err := st.SaveScanOutcomes("rt1", 2000, []probe.ScanOutcome{
		{Iface: "eth6", Phy: 1, BSS: 9, Error: ""},
	}); err != nil {
		t.Fatalf("save 2: %v", err)
	}

	got, err := st.ScanOutcomes("rt1")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("esperaba 1 fila por interfaz, got %d", len(got))
	}
	if got[0].BSS != 9 || got[0].Error != "" || got[0].Ts != 2000 {
		t.Fatalf("el segundo ciclo no sustituyo al primero: %+v", got[0])
	}
}

// Un agente anterior a 3.0.34 no manda el campo: la tabla queda vacia y la
// vista no debe romperse por eso.
func TestScanOutcomesVacio(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	st := channelplan.NewStore(d.DB)
	if err := st.SaveScanOutcomes("rt1", 1000, []probe.ScanOutcome{{Iface: ""}}); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := st.ScanOutcomes("rt1")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("una interfaz vacia no debe guardarse: %+v", got)
	}
}
