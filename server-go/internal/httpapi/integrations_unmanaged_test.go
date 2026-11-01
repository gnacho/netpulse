package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/rintegration"
)

// externalPayload es un push de un pusher externo: el kind viaja en el payload
// y es lo que distingue una integración de un agente nativo.
func externalPayload(slug, version string, interval int, ts int64) string {
	return fmt.Sprintf(`{"router":%q,"ts":%d,"version":%q,"kind":"external","interval":%d,"data":{"system":{"sysinfo":{"uptime":100,"load":[0,0,0],"memory":{"total":100,"free":50,"buffered":0,"available":50}},"cpu":10,"temp":40},"wireless":{"clients":{}},"dhcp":{"leases":[]},"fdb":{"macs":{}}}}`, slug, ts, version, interval)
}

type unmanagedJSON struct {
	Slug       string `json:"slug"`
	Kind       string `json:"kind"`
	Version    string `json:"version"`
	IntervalS  int    `json:"intervalS"`
	LastPushMs int64  `json:"lastPushMs"`
	Fresh      bool   `json:"fresh"`
}

// getIntegrations devuelve la lista gestionada y la de pushers a mano.
func getIntegrations(t *testing.T, ts *agentTestServer) ([]string, []unmanagedJSON) {
	t.Helper()
	req, _ := http.NewRequest("GET", ts.URL+"/api/integrations", nil)
	req.Header.Set("Cookie", "session="+ts.cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/integrations: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/integrations: %d", res.StatusCode)
	}
	var body struct {
		Integrations []struct {
			Slug string `json:"slug"`
		} `json:"integrations"`
		Unmanaged []unmanagedJSON `json:"unmanaged"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	managed := make([]string, 0, len(body.Integrations))
	for _, it := range body.Integrations {
		managed = append(managed, it.Slug)
	}
	return managed, body.Unmanaged
}

func findUnmanaged(items []unmanagedJSON, slug string) (unmanagedJSON, bool) {
	for _, it := range items {
		if it.Slug == slug {
			return it, true
		}
	}
	return unmanagedJSON{}, false
}

func hasUnmanaged(items []unmanagedJSON, slug string) bool {
	_, ok := findUnmanaged(items, slug)
	return ok
}

// TestIntegrationsUnmanagedListsExternalPushers: un pusher instalado a mano
// empuja y no tiene fila en router_integrations. La página de Integraciones
// debe decirlo en vez de salir vacía (#1743).
func TestIntegrationsUnmanagedListsExternalPushers(t *testing.T) {
	ts := makeAgentTestServer(t)
	status, token, _ := createAgentToken(t, ts, "crs305")
	if status != http.StatusCreated {
		t.Fatalf("create token: %d", status)
	}
	res := ingest(t, ts, token, "203.0.113.9", externalPayload("crs305", "0.1.1+lldp", 30, time.Now().Unix()))
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("ingest: %d", res.StatusCode)
	}
	res.Body.Close()

	managed, unmanaged := getIntegrations(t, ts)
	if len(managed) != 0 {
		t.Fatalf("integrations = %v, quería vacío", managed)
	}
	item, ok := findUnmanaged(unmanaged, "crs305")
	if !ok {
		t.Fatalf("crs305 no aparece en unmanaged: %+v", unmanaged)
	}
	if item.Kind != "external" || item.Version != "0.1.1+lldp" || item.IntervalS != 30 {
		t.Fatalf("item inesperado: %+v", item)
	}
	if !item.Fresh {
		t.Fatalf("un push recién ingerido debe salir fresh: %+v", item)
	}
	if item.LastPushMs == 0 {
		t.Fatalf("lastPushMs vacío: %+v", item)
	}
}

// TestIntegrationsUnmanagedSkipsManagedSlug: en cuanto la app posee la fila de
// integración, el slug sale de la lista de no gestionadas.
func TestIntegrationsUnmanagedSkipsManagedSlug(t *testing.T) {
	ts := makeAgentTestServer(t)
	status, token, _ := createAgentToken(t, ts, "fn-switch-8")
	if status != http.StatusCreated {
		t.Fatalf("create token: %d", status)
	}
	res := ingest(t, ts, token, "203.0.113.10", externalPayload("fn-switch-8", "0.1.0fn3", 30, time.Now().Unix()))
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("ingest: %d", res.StatusCode)
	}
	res.Body.Close()

	if _, unmanaged := getIntegrations(t, ts); !hasUnmanaged(unmanaged, "fn-switch-8") {
		t.Fatalf("fn-switch-8 debería aparecer sin fila de integración: %+v", unmanaged)
	}

	err := rintegration.NewStore(ts.db.DB).Upsert(rintegration.Row{
		Slug: "fn-switch-8", Vendor: "fn", Mode: "local", Host: "127.0.0.1",
		IntervalS: 30, Enabled: true, State: "running",
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	managed, unmanaged := getIntegrations(t, ts)
	if _, ok := findUnmanaged(unmanaged, "fn-switch-8"); ok {
		t.Fatalf("fn-switch-8 sigue en unmanaged con fila: %+v", unmanaged)
	}
	if len(managed) != 1 || managed[0] != "fn-switch-8" {
		t.Fatalf("integrations = %v", managed)
	}
}

// TestIntegrationsUnmanagedIgnoresNativeAgents: un agente nativo (sin kind) no
// es una integración externa y no debe aparecer.
func TestIntegrationsUnmanagedIgnoresNativeAgents(t *testing.T) {
	ts := makeAgentTestServer(t)
	status, token, _ := createAgentToken(t, ts, "patio")
	if status != http.StatusCreated {
		t.Fatalf("create token: %d", status)
	}
	res := ingest(t, ts, token, "203.0.113.11", validPayload())
	if res.StatusCode != http.StatusAccepted {
		t.Fatalf("ingest: %d", res.StatusCode)
	}
	res.Body.Close()

	if _, unmanaged := getIntegrations(t, ts); len(unmanaged) != 0 {
		t.Fatalf("unmanaged = %+v, quería vacío", unmanaged)
	}
}
