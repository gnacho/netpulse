package httpapi

import (
	"context"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/rack"
)

type rackHintsStubAdapter struct {
	adapters.Snapshotter
	devices []adapters.Device
	uplinks map[string]adapters.FdbUplink
}

func (a *rackHintsStubAdapter) GetDevices(context.Context) []adapters.Device {
	return a.devices
}

func (a *rackHintsStubAdapter) FleetFdbUplinks(string, string) map[string]adapters.FdbUplink {
	return a.uplinks
}

// TestBuildRackHintsSymmetricDedup comprueba que los dos bucles de
// buildRackHints comparten una deduplicación simétrica (A↔B == B↔A), tanto
// dentro del bucle de clientes como entre clientes y uplinks de flota, y que
// omiten puertos multi-MAC y equipos no montados. El adaptador real no lista
// los routers de la flota como clientes, así que hoy el bucle de clientes no
// ve un par en los dos sentidos; el stub lo fuerza para fijar el contrato.
func TestBuildRackHintsSymmetricDedup(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer d.Close()
	if _, err := d.Exec(`INSERT INTO kv (key, value) VALUES ('rack_uplink_unit', 'sw1') ON CONFLICT(key) DO UPDATE SET value = excluded.value`); err != nil {
		t.Fatalf("sembrar rack_uplink_unit: %v", err)
	}

	const (
		macR1  = "AA:BB:CC:00:00:01"
		macSW1 = "AA:BB:CC:00:00:02"
		macDev = "AA:BB:CC:00:00:03"
		macAgg = "AA:BB:CC:00:00:04"
		macAg2 = "AA:BB:CC:00:00:05"
		macOff = "AA:BB:CC:00:00:06"
	)

	stub := &rackHintsStubAdapter{
		Snapshotter: adapters.NewDemo(),
		devices: []adapters.Device{
			// R1 y SW1 se ven mutuamente en bocas de MAC única (forzado por el
			// stub): solo debe salir 1 hint.
			{MAC: macSW1, RouterID: "r1", Band: "cable", Port: "lan1"},
			{MAC: macR1, RouterID: "sw1", Band: "cable", Port: "lan8"},
			// Cliente normal montado en boca única de SW1.
			{MAC: macDev, RouterID: "sw1", Band: "cable", Port: "lan2"},
			// Boca multi-MAC (lan3 de SW1): no debe generar hints.
			{MAC: macAgg, RouterID: "sw1", Band: "cable", Port: "lan3"},
			{MAC: macAg2, RouterID: "sw1", Band: "cable", Port: "lan3"},
			// Cliente no montado: ignorado.
			{MAC: macOff, RouterID: "sw1", Band: "cable", Port: "lan4"},
		},
		uplinks: map[string]adapters.FdbUplink{
			// Mismo par SW1↔R1 en orden inverso: también debe deduplicarse.
			"r1": {ParentID: "sw1", Port: "lan8", ChildPort: "lan1"},
		},
	}

	ov := &adapters.Overview{
		Routers: []adapters.Router{
			{ID: "r1", MAC: "AA:BB:CC:00:00:01"},
			{ID: "sw1", MAC: "AA:BB:CC:00:00:02"},
		},
	}
	s := &server{
		db:      d,
		adapter: stub,
		lastOv:  func() *adapters.Overview { return ov },
	}
	mounts := []rack.MountRow{
		{Mount: rack.Mount{ID: "m1", DeviceMAC: macR1}},
		{Mount: rack.Mount{ID: "m2", DeviceMAC: macSW1}},
		{Mount: rack.Mount{ID: "m3", DeviceMAC: macDev}},
		{Mount: rack.Mount{ID: "m4", DeviceMAC: macAgg}},
		{Mount: rack.Mount{ID: "m5", DeviceMAC: macAg2}},
	}

	hints := s.buildRackHints(context.Background(), mounts)
	if len(hints) != 2 {
		t.Fatalf("hints = %+v, esperaba exactamente 2 hints deduplicados", hints)
	}
	// Se busca cada hint por su par (sin importar el sentido ni el orden), para
	// no depender de qué bucle de buildRackHints lo emite primero.
	find := func(a, b string) *rack.CableHint {
		for i := range hints {
			h := &hints[i]
			if (h.FromDeviceID == a && h.ToDeviceID == b) || (h.FromDeviceID == b && h.ToDeviceID == a) {
				return h
			}
		}
		return nil
	}
	if h := find(macR1, macSW1); h == nil {
		t.Errorf("falta el hint %s <-> %s en %+v", macR1, macSW1, hints)
	} else if h.FromPortHint == "" && h.ToPortHint == "" {
		t.Errorf("hint %s <-> %s sin ninguna pista de puerto: %+v", macR1, macSW1, *h)
	}
	if h := find(macSW1, macDev); h == nil {
		t.Errorf("falta el hint %s <-> %s en %+v", macSW1, macDev, hints)
	} else if h.FromDeviceID != macSW1 || h.FromPortHint != "lan2" {
		t.Errorf("hint = %+v, esperaba %s(lan2) -> %s", *h, macSW1, macDev)
	}
}
