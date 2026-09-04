// store_test.go — contrato del store de firmware (issue #519: descarte del
// último intento fallido).
package firmware

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func open(t *testing.T) *Store {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return NewStore(d.DB)
}

func TestDismissLatestFailed(t *testing.T) {
	s := open(t)
	id, err := s.BeginUpgrade("rt1", "v2", "http://x", "abc")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := s.SetStatus(id, "failed", "agent not connected", ""); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	up, _ := s.LatestUpgrade("rt1")
	if up == nil || up.Status != "failed" {
		t.Fatalf("esperaba upgrade failed, got %+v", up)
	}
	if err := s.DismissLatest("rt1"); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	up, _ = s.LatestUpgrade("rt1")
	if up != nil {
		t.Fatalf("tras dismiss no debería haber upgrade: %+v", up)
	}
}

func TestDismissLatestKeepsRunning(t *testing.T) {
	s := open(t)
	id, err := s.BeginUpgrade("rt1", "v2", "http://x", "abc")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	// Estado en curso (requested): el descarte no debe tocarlo.
	if err := s.DismissLatest("rt1"); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	up, _ := s.LatestUpgrade("rt1")
	if up == nil || up.ID != id {
		t.Fatalf("un upgrade en curso debe sobrevivir al dismiss: %+v", up)
	}
	// Router sin upgrades: no debe fallar.
	if err := s.DismissLatest("no-existe"); err != nil {
		t.Fatalf("dismiss sin upgrades: %v", err)
	}
}
