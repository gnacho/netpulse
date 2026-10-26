// wifi_channelplan_throttle_test.go — anti-martilleo del scan bajo demanda
// (#1561). Con el default de scan periódico en 0, abrir "Análisis de canales"
// es el camino normal para refrescar vecinos, y varias pestañas no deben
// encadenar ciclos que saquen la radio del canal.
package httpapi

import (
	"testing"
	"time"
)

func TestClaimPlanScanRateLimits(t *testing.T) {
	s := &server{}

	retry, ok := s.claimPlanScan("flint2")
	if !ok || retry != 0 {
		t.Fatalf("primer scan: ok=%v retry=%d, quería ok=true retry=0", ok, retry)
	}

	retry, ok = s.claimPlanScan("flint2")
	if ok {
		t.Fatal("segundo scan inmediato: quería rechazo")
	}
	if retry <= 0 || retry > int(planScanMinInterval.Seconds()) {
		t.Fatalf("retryAfterSec=%d fuera de (0, %v]", retry, planScanMinInterval.Seconds())
	}

	// Otra unidad tiene su propio turno.
	if _, ok := s.claimPlanScan("rt2"); !ok {
		t.Fatal("otra unidad: quería turno concedido")
	}
}

func TestClaimPlanScanAfterInterval(t *testing.T) {
	s := &server{lastPlanScan: map[string]time.Time{
		"flint2": time.Now().Add(-planScanMinInterval - time.Second),
	}}
	if _, ok := s.claimPlanScan("flint2"); !ok {
		t.Fatal("pasado el intervalo: quería turno concedido")
	}
}

func TestReleasePlanScanFreesTurn(t *testing.T) {
	s := &server{}
	if _, ok := s.claimPlanScan("flint2"); !ok {
		t.Fatal("primer scan: quería turno")
	}
	// Send falla (agente sin SSE): liberar para que el reintento no espere.
	s.releasePlanScan("flint2")
	if _, ok := s.claimPlanScan("flint2"); !ok {
		t.Fatal("tras release: quería turno concedido")
	}
}
