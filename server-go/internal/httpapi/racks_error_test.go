package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/rack"
)

// TestWriteRackStoreErrorInvalidMAC comprueba que una MAC inválida en el layout
// es un error del cliente (400 invalid_mac) y no un fallo de base de datos.
func TestWriteRackStoreErrorInvalidMAC(t *testing.T) {
	rec := httptest.NewRecorder()
	writeRackStoreError(rec, rack.ErrInvalidMAC)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("esperaba 400, obtuve %d: %s", rec.Code, rec.Body.String())
	}
}
