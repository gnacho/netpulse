// data_internal_test.go — EnrichOverview (#849): el campo plan del overview
// (que pinta la topología en el enlace WAN) se rellena con la velocidad
// contratada cuando el poller lo dejó vacío ("—"), típico del modo live.
package httpapi

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
)

// TestEnrichOverviewPlanFromContract: sin plan pero con contrato → plan
// formateado "600/100 Mbps".
func TestEnrichOverviewPlanFromContract(t *testing.T) {
	db := newTestKV(t)
	if err := kvSetFloat(db, wanSpeedDownKey, 600); err != nil {
		t.Fatalf("set down: %v", err)
	}
	if err := kvSetFloat(db, wanSpeedUpKey, 100); err != nil {
		t.Fatalf("set up: %v", err)
	}
	out := &adapters.Overview{WAN: adapters.WAN{Plan: "—"}}
	EnrichOverview(db, nil, out)
	if out.WAN.Plan != "600/100 Mbps" {
		t.Fatalf("plan = %q, esperado %q", out.WAN.Plan, "600/100 Mbps")
	}
	if out.WAN.ContractDownMbps == nil || *out.WAN.ContractDownMbps != 600 {
		t.Fatalf("ContractDownMbps = %v", out.WAN.ContractDownMbps)
	}
}

// TestEnrichOverviewPlanKeepsDecimals: decimales con un solo decimal.
func TestEnrichOverviewPlanKeepsDecimals(t *testing.T) {
	db := newTestKV(t)
	_ = kvSetFloat(db, wanSpeedDownKey, 94.9)
	_ = kvSetFloat(db, wanSpeedUpKey, 9.4)
	out := &adapters.Overview{WAN: adapters.WAN{Plan: "—"}}
	EnrichOverview(db, nil, out)
	if out.WAN.Plan != "94.9/9.4 Mbps" {
		t.Fatalf("plan = %q, esperado %q", out.WAN.Plan, "94.9/9.4 Mbps")
	}
}

// TestEnrichOverviewPlanNotClobbered: si el poller ya trajo un plan (demo o
// futuro autodetección), no se pisa con el contrato.
func TestEnrichOverviewPlanNotClobbered(t *testing.T) {
	db := newTestKV(t)
	_ = kvSetFloat(db, wanSpeedDownKey, 600)
	_ = kvSetFloat(db, wanSpeedUpKey, 100)
	out := &adapters.Overview{WAN: adapters.WAN{Plan: "600/600 Mbps"}}
	EnrichOverview(db, nil, out)
	if out.WAN.Plan != "600/600 Mbps" {
		t.Fatalf("plan = %q, no debe pisarse con el contrato", out.WAN.Plan)
	}
}

// TestEnrichOverviewPlanSinContrato: sin velocidad contratada configurada,
// el plan se queda como estaba.
func TestEnrichOverviewPlanSinContrato(t *testing.T) {
	db := newTestKV(t)
	out := &adapters.Overview{WAN: adapters.WAN{Plan: "—"}}
	EnrichOverview(db, nil, out)
	if out.WAN.Plan != "—" {
		t.Fatalf("plan = %q, esperado sin cambios", out.WAN.Plan)
	}
}
