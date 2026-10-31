// data_reasons_test.go — #1703: EnrichOverview inyecta el motivo de caída de
// las unidades empujadas por integraciones solo cuando están offline o
// unreachable; las online no llevan motivo.
package httpapi_test

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/httpapi"
)

func TestEnrichOverviewAppliesPusherReasons(t *testing.T) {
	// EnrichOverview lee kv: necesita una base real (panic con nil).
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	handle := d.DB
	ov := &adapters.Overview{
		Routers: []adapters.Router{
			{ID: "hex", Type: "external", Status: "offline"},
			{ID: "hex-ok", Type: "external", Status: "online"},
			{ID: "gw", Type: "glinet", Status: "unreachable"},
		},
	}
	reasons := map[string]string{
		"hex":    "pusher_auth_failed",
		"gw":     "pusher_stopped",
		"hex-ok": "pusher_auth_failed", // unidad online: no debe aplicarse
	}
	httpapi.EnrichOverview(handle, nil, ov, reasons)

	if got := ov.Routers[0].StatusReason; got != "pusher_auth_failed" {
		t.Errorf("hex: StatusReason = %q, want pusher_auth_failed", got)
	}
	if got := ov.Routers[1].StatusReason; got != "" {
		t.Errorf("hex-ok (online): StatusReason = %q, want empty", got)
	}
	if got := ov.Routers[2].StatusReason; got != "pusher_stopped" {
		t.Errorf("gw (unreachable): StatusReason = %q, want pusher_stopped", got)
	}

	// Sin motivos (demo / sin integraciones): nada cambia.
	ov2 := &adapters.Overview{Routers: []adapters.Router{{ID: "hex", Type: "external", Status: "offline"}}}
	httpapi.EnrichOverview(handle, nil, ov2, nil)
	if ov2.Routers[0].StatusReason != "" {
		t.Errorf("nil reasons: StatusReason = %q, want empty", ov2.Routers[0].StatusReason)
	}
}
