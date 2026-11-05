// updater_freshcheck_test.go — #1881: un apply con el chequeo viejo refresca
// el objetivo antes de descargar (si no, instala una release ya superada y
// necesita un segundo reinicio); con el chequeo fresco no gasta otra consulta.
package updater

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// setLastCheck fija el instante del último chequeo (edad respecto a ahora).
func setLastCheck(u *Updater, age time.Duration) {
	ts := time.Now().Add(-age).UnixMilli()
	u.mu.Lock()
	u.lastCheck = &ts
	u.mu.Unlock()
}

// applyTarget devuelve el objetivo que ApplyBy usaría ahora mismo.
func applyTarget(u *Updater) string {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.latest == nil {
		return ""
	}
	return *u.latest
}

func TestApplyRefreshesStaleTarget(t *testing.T) {
	var latest atomic.Value
	latest.Store("v2.9.0")
	withAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/netpulse/releases/latest" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"tag_name":%q,"name":%q}`, latest.Load(), latest.Load())
	})
	u := New(t.TempDir(), "owner/netpulse", "", "2.0.0", nil)

	if st := u.Check(context.Background()); !st.UpdateAvailable {
		t.Fatalf("esperaba novedad: %+v", st)
	}
	// Escenario real: el último chequeo es de hace 24 h (checkInterval) y
	// desde entonces salió una release nueva.
	setLastCheck(u, 24*time.Hour)
	latest.Store("v2.10.0")

	if !u.ApplyBy("admin") {
		t.Fatal("apply rechazado")
	}
	if got := applyTarget(u); got != "v2.10.0" {
		t.Fatalf("objetivo = %q, esperaba v2.10.0 (apply con chequeo viejo)", got)
	}
}

func TestApplyKeepsTargetWhenCheckIsFresh(t *testing.T) {
	var hits int32
	withAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/netpulse/releases/latest" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tag_name":"v2.9.0","name":"v2.9.0"}`)
	})
	u := New(t.TempDir(), "owner/netpulse", "", "2.0.0", nil)
	u.Check(context.Background())

	if !u.ApplyBy("admin") {
		t.Fatal("apply rechazado")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("consultas a GitHub = %d, esperaba 1 (sin re-chequeo)", got)
	}
	if got := applyTarget(u); got != "v2.9.0" {
		t.Fatalf("objetivo = %q", got)
	}
}
