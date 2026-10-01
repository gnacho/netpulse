// adguard_detect_internal_test.go - POST /api/config/adguard/detect (#964):
// orden de sondeo (gateway primero, luego flota), modos y found=false.
package httpapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
)

// fakeAdguardProbe sustituye el sondeo real y registra el orden de hosts.
func fakeAdguardProbe(t *testing.T, found map[string]bool, probed *[]string) {
	t.Helper()
	old := adguardStatusProbe
	t.Cleanup(func() { adguardStatusProbe = old })
	adguardStatusProbe = func(_ context.Context, host string, _ int) bool {
		*probed = append(*probed, host)
		return found[host]
	}
}

func detectAdguardCall(t *testing.T, s *server) adguardDetectResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/config/adguard/detect", nil)
	s.handleDetectAdguard(rec, req)
	if rec.Code != 200 {
		t.Fatalf("detect: status %d", rec.Code)
	}
	var out adguardDetectResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("detect json: %v", err)
	}
	return out
}

func TestDetectAdguardGatewayFirst(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if _, err := routerstore.AddRouter(d.DB, routerstore.AddInput{Name: "gw", Host: "192.0.2.1", IsGateway: true}); err != nil {
		t.Fatalf("AddRouter gw: %v", err)
	}
	if _, err := routerstore.AddRouter(d.DB, routerstore.AddInput{Name: "sat", Host: "192.0.2.2"}); err != nil {
		t.Fatalf("AddRouter sat: %v", err)
	}

	// AdGuard vive en la flota, no en el gateway: el gateway se sondea
	// PRIMERO (aunque falle) y el hallazgo es mode=standard, source=fleet.
	probed := []string{}
	fakeAdguardProbe(t, map[string]bool{"192.0.2.2": true}, &probed)
	s := &server{db: d}
	out := detectAdguardCall(t, s)
	if !out.Found || out.Mode != "standard" || out.Host != "192.0.2.2" || out.Port != 3000 || out.Source != "fleet" {
		t.Fatalf("respuesta: %+v", out)
	}
	if len(probed) < 2 || probed[0] != "192.0.2.1" {
		t.Fatalf("orden de sondeo: %v (gateway primero)", probed)
	}
}

func TestDetectAdguardGatewayHit(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if _, err := routerstore.AddRouter(d.DB, routerstore.AddInput{Name: "gw", Host: "192.0.2.1", IsGateway: true}); err != nil {
		t.Fatalf("AddRouter gw: %v", err)
	}
	probed := []string{}
	fakeAdguardProbe(t, map[string]bool{"192.0.2.1": true}, &probed)
	s := &server{db: d}
	out := detectAdguardCall(t, s)
	if !out.Found || out.Mode != "glinet" || out.Host != "192.0.2.1" || out.Source != "gateway" {
		t.Fatalf("respuesta: %+v", out)
	}
}

func TestDetectAdguardNotFound(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if _, err := routerstore.AddRouter(d.DB, routerstore.AddInput{Name: "gw", Host: "192.0.2.1", IsGateway: true}); err != nil {
		t.Fatalf("AddRouter gw: %v", err)
	}
	probed := []string{}
	fakeAdguardProbe(t, map[string]bool{}, &probed)
	s := &server{db: d}
	out := detectAdguardCall(t, s)
	if out.Found {
		t.Fatalf("respuesta: %+v, esperado found=false", out)
	}
}
