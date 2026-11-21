package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/rack"
	"github.com/gnacho/netpulse/server-go/internal/rintegration"
)

// TestBuildRackHintsRaceWithHandleOverviewEnrich: handleOverview hace una
// copia superficial del snapshot del poller (out := *ov), así que out.Routers
// comparte su array con lastOverview. Con una integración parada,
// EnrichOverview escribía r.StatusReason en ese array mientras buildRackHints
// (GET /api/racks) lo recorría. La escritura debe ir a una copia.
func TestBuildRackHintsRaceWithHandleOverviewEnrich(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()

	// Integración parada: pusherStatusReasons devuelve un motivo para "ext1".
	now := time.Now().UnixMilli()
	if _, err := d.Exec(
		`INSERT INTO router_integrations
			(slug, vendor, mode, host, username, arch, interval_s, enabled, state, created_at, updated_at)
		 VALUES ('ext1', 'mikrotik', 'pusher', '192.168.1.50', 'admin', 'amd64', 30, 1, 'stopped', ?, ?)`,
		now, now,
	); err != nil {
		t.Fatalf("sembrar router_integrations: %v", err)
	}

	sharedOv := &adapters.Overview{
		Routers: []adapters.Router{
			{ID: "ext1", MAC: "AA:BB:CC:DD:EE:01", Status: "offline"},
		},
	}
	s := &server{
		db:           d,
		adapter:      adapters.NewDemo(),
		integrations: rintegration.NewManager(t.TempDir()),
		lastOv:       func() *adapters.Overview { return sharedOv },
		started:      time.Now(),
	}

	mounts := []rack.MountRow{
		{Mount: rack.Mount{ID: "m1", DeviceMAC: "AA:BB:CC:DD:EE:01"}},
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ctx := context.Background()
		for {
			select {
			case <-stop:
				return
			default:
				_ = s.buildRackHints(ctx, mounts)
			}
		}
	}()

	var rec *httptest.ResponseRecorder
	for i := 0; i < 40; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/overview", nil)
		rec = httptest.NewRecorder()
		s.handleOverview(rec, req)
	}

	close(stop)
	wg.Wait()

	if got := sharedOv.Routers[0].StatusReason; got != "" {
		t.Fatalf("el snapshot compartido cambió: StatusReason=%q", got)
	}
	var body adapters.Overview
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decodificar /api/overview: %v", err)
	}
	if len(body.Routers) != 1 || body.Routers[0].StatusReason == "" {
		t.Fatalf("la respuesta no lleva el motivo de caída: %+v", body.Routers)
	}
}
