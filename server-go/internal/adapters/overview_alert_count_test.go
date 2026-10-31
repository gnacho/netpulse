// overview_alert_count_test.go - la línea "N important alerts" de la Overview
// debe contar lo mismo que la lista de alertas (#1707). Contaba el breakdown
// del health score (computeHealth), así que una penalización de salud sin
// alerta visible (p. ej. vpn en nivel "none", o el aviso ya resuelto con la
// condición viva) dejaba un contador fantasma que no se podía quitar.
package adapters

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

// Repro del desajuste #1707: un peer urgente caído penaliza la salud
// (breakdown con 1 entrada), pero con vpn en "none" el motor no registra la
// alerta: la lista (List) y el contador de la campana (UnreadCount) están a
// cero. La métrica vieja de la Overview decía "1 important alert" y la lista
// no mostraba nada que resolver.
func TestHealthPenaltyWithoutVisibleAlertIsThePhantomCount(t *testing.T) {
	l := liveTestLive()
	if err := l.engine.SetConfig(map[string]string{"vpn": "none"}); err != nil {
		t.Fatal(err)
	}
	wg := &WireGuardStats{Interface: "wg0", Status: "active", Peers: []WGPeer{
		{Key: "k1", Name: "Site B", Active: false, Urgent: true},
	}}
	h := computeHealth(nil, nil, wg)
	if len(h.Breakdown) != 1 {
		t.Fatalf("breakdown de salud: %d entradas, esperaba 1 (peer urgente caído): %+v", len(h.Breakdown), h.Breakdown)
	}
	// La emisión real del motor con la categoría en "none" no deja fila.
	l.engine.Emit(alerts.AlertEvent{
		ID: "alert-wgdown-site-b", Category: alerts.CatVPN, Urgent: true,
		Severity: "critical", Title: "Site B down", RouterID: "flint2", Time: "just now",
	})
	if got := l.engine.List(); len(got) != 0 {
		t.Fatalf("una categoría en none no debe asomar en la lista: %+v", got)
	}
	if got := l.engine.UnreadCount(); got != 0 {
		t.Fatalf("unread con la categoría en none: %d, esperaba 0", got)
	}
}

// El contador de la Overview vive en HeroStrip.tsx y debe usar unreadAlerts
// (misma fuente que la campana: server truth con los mismos filtros que
// List), nunca el breakdown del health score. Se comprueba sobre la fuente
// porque la app no tiene tests de JS (patrón de frontend_defaults_test.go).
func TestHeroStripImportantAlertsUseAlertListSemantics(t *testing.T) {
	const rel = "../../../app/src/sections/HeroStrip.tsx"
	raw, err := os.ReadFile(filepath.Join(rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	src := string(raw)
	if !strings.Contains(src, "unreadAlerts") {
		t.Fatalf("%s: el contador de la Overview no usa unreadAlerts (debe compartir semántica con la lista de alertas)", rel)
	}
	const oldCount = "healthScore.breakdown?.length ?? 0"
	if strings.Contains(src, oldCount) {
		t.Fatalf("%s: el contador vuelve a derivarse del breakdown de salud (%q): reproduce el contador fantasma del #1707", rel, oldCount)
	}
}
