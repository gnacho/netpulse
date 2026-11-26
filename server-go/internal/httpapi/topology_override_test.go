package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
)

// TestTopologyOverrideCreateNormalizesAndValidatesMAC comprueba que el alta de
// overrides pasa MAC y parent con ':' a la forma canónica en mayúsculas,
// conserva referencias routerId:puerto e ids de router (también los que
// parecen una MAC con guiones) y rechaza un valor vacío con 400.
func TestTopologyOverrideCreateNormalizesAndValidatesMAC(t *testing.T) {
	s := newOverrideTestServer(t)

	post := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/topology/overrides", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.handleTopologyOverrideCreate(rec, req)
		return rec
	}

	// MAC y parent con ':' en minúsculas → forma canónica en mayúsculas.
	rec := post(`{"mac":"aa:bb:cc:dd:ee:01","kind":"attach","parent":" c8:ff:bf:08:6f:ba "}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("esperaba 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Override adapters.TopologyOverride `json:"override"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decodificar respuesta: %v", err)
	}
	if resp.Override.ID != "AA:BB:CC:DD:EE:01" || resp.Override.MAC != "AA:BB:CC:DD:EE:01" {
		t.Errorf("esperaba id/mac en forma canónica AA:BB:CC:DD:EE:01, got id=%q mac=%q", resp.Override.ID, resp.Override.MAC)
	}
	if resp.Override.Parent != "C8:FF:BF:08:6F:BA" {
		t.Errorf("esperaba parent en forma canónica C8:FF:BF:08:6F:BA, got %q", resp.Override.Parent)
	}

	// Parent routerId:puerto → se conserva tal cual.
	rec = post(`{"mac":"aa:bb:cc:dd:ee:02","kind":"attach","parent":"sw1:two-gigabitEthernet 1/0/21"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("esperaba 201 con parent router:puerto, got %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decodificar respuesta router:puerto: %v", err)
	}
	if resp.Override.Parent != "sw1:two-gigabitEthernet 1/0/21" {
		t.Errorf("esperaba parent router:puerto intacto, got %q", resp.Override.Parent)
	}

	// Un id de router que parece una MAC con guiones sigue siendo un id de
	// router, como target y como parent.
	rec = post(`{"mac":"aa-bb-cc-dd-ee-03","kind":"attach","parent":"ab-cd-ef-01-02-03"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("esperaba 201 con ids de router con guiones, got %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decodificar respuesta ids con guiones: %v", err)
	}
	if resp.Override.MAC != "aa-bb-cc-dd-ee-03" || resp.Override.Parent != "ab-cd-ef-01-02-03" {
		t.Errorf("esperaba los ids de router intactos, got mac=%q parent=%q", resp.Override.MAC, resp.Override.Parent)
	}

	// Un id de router (#1485) no es una MAC: se guarda en minúsculas, con sus
	// guiones intactos.
	rec = post(`{"mac":" Mi-Router ","kind":"switch"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("esperaba 201 con id de router, got %d: %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decodificar respuesta id de router: %v", err)
	}
	if resp.Override.MAC != "mi-router" {
		t.Errorf("esperaba id de router mi-router, got %q", resp.Override.MAC)
	}

	// Vacío → 400 invalid_mac.
	if rec = post(`{"mac":"  ","kind":"hypervisor"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("MAC vacía: esperaba 400, got %d", rec.Code)
	}
}
