// proxmox_gate_test.go - toggle server-side de la integración Proxmox (#968):
// con settings.integrations.proxmox="false" el adapter NO hace ni un solo
// request al PVE; ausente o "true" sondea con normalidad.
package adapters

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/pve"
)

func TestPveInventoryGateDropsRequests(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api2/json/cluster/resources":
			_, _ = w.Write([]byte(`{"data":[{"id":"node/pve1","node":"pve1","type":"node","status":"online"}]}`))
		case "/api2/json/cluster/status":
			_, _ = w.Write([]byte(`{"data":[{"type":"node","name":"pve1","ip":"192.0.2.2","online":1}]}`))
		default:
			_, _ = w.Write([]byte(`{"data":{}}`))
		}
	}))
	defer srv.Close()

	d := openLiveTestDB(t)
	cfg := pve.Config{URL: srv.URL, TokenID: "netpulse@pam!t", Secret: "s"}
	if err := pve.SaveInstances(d.DB, []pve.Instance{{ID: "acasa", Name: "casa", Config: cfg}}); err != nil {
		t.Fatalf("SaveInstances: %v", err)
	}
	l := NewLive(nil, d, nil, nil)

	// Integración OFF: 0 requests aunque haya instancia configurada.
	if _, err := d.Exec("INSERT INTO kv (key, value) VALUES (?, ?)", pveIntegrationKey, "false"); err != nil {
		t.Fatalf("kv off: %v", err)
	}
	if inv := l.pveInventoryCached(); inv != nil {
		t.Fatalf("gate off: inventario %v, esperado nil", inv)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("gate off: %d requests al PVE, esperado 0", n)
	}

	// Integración ON: sondea (resources + status + lo que pida el inventario).
	if _, err := d.Exec("UPDATE kv SET value = ? WHERE key = ?", "true", pveIntegrationKey); err != nil {
		t.Fatalf("kv on: %v", err)
	}
	l.pveClients = nil
	l.pveKey = ""
	if inv := l.pveInventoryCached(); inv == nil {
		t.Fatal("gate on: inventario nil, esperado datos")
	}
	if n := hits.Load(); n == 0 {
		t.Fatal("gate on: 0 requests al PVE, esperado sondeo real")
	}
}

func TestPveInventoryGateDefaultOn(t *testing.T) {
	// Clave ausente (instalación previa a #968) = integración activa.
	d := openLiveTestDB(t)
	l := NewLive(nil, d, nil, nil)
	if !l.pveIntegrationEnabled() {
		t.Fatal("clave ausente: integración desactivada, esperado activa")
	}
	if _, err := d.Exec("INSERT INTO kv (key, value) VALUES (?, ?)", pveIntegrationKey, "false"); err != nil {
		t.Fatalf("kv off: %v", err)
	}
	if l.pveIntegrationEnabled() {
		t.Fatal("clave false: integración activa, esperado desactivada")
	}
}
