// updater743_test.go — #743: cuando el fetch a GitHub falla (rate limit
// anónimo, red), el status debe declarar checkFailed en vez de presentar el
// último estado conocido como "estás en la última versión".
package updater

import (
	"context"
	"fmt"
	"net/http"
	"testing"
)

func TestCheckFailureIsVisibleAndRecovers(t *testing.T) {
	var fail bool
	withAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if fail {
			w.WriteHeader(http.StatusForbidden) // rate limit anónimo de GitHub
			return
		}
		if r.URL.Path != "/repos/owner/netpulse/releases/latest" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tag_name":"v2.1.0","name":"v2.1.0","body":"notas"}`)
	})

	u := New(t.TempDir(), "owner/netpulse", "", "2.0.0", nil).WithDataDir(t.TempDir())

	// Escenario del reporte: el check falla tras publicarse una release
	// nueva. El estado debe declarar el fallo y NO celebrar "última versión".
	fail = true
	st := u.Check(context.Background())
	if !st.CheckFailed {
		t.Fatal("un check fallido debe marcarse como checkFailed")
	}
	if st.CheckErr == "" {
		t.Fatal("checkErr debe llevar el errCode (no_token/github_403)")
	}
	if st.UpdateAvailable {
		t.Fatal("con el fetch caído no puede afirmarse que hay actualización")
	}

	// Recuperación: GitHub responde y hay una release mayor instalada.
	fail = false
	st2 := u.Check(context.Background())
	if st2.CheckFailed {
		t.Fatalf("un check exitoso debe limpiar checkFailed: %+v", st2)
	}
	if !st2.UpdateAvailable || st2.Latest == nil || *st2.Latest != "v2.1.0" {
		t.Fatalf("debe detectar v2.1.0 sobre 2.0.0: %+v", st2)
	}
}

func TestCheckSuccessKeepsCheckFailedClean(t *testing.T) {
	withAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/netpulse/releases/latest" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tag_name":"v2.0.0","name":"v2.0.0","body":"notas"}`)
	})
	u := New(t.TempDir(), "owner/netpulse", "", "2.0.0", nil).WithDataDir(t.TempDir())
	st := u.Check(context.Background())
	if st.CheckFailed || st.CheckErr != "" {
		t.Fatalf("check sano no debe marcar fallo: %+v", st)
	}
	if st.UpdateAvailable {
		t.Fatal("misma versión: no hay actualización")
	}
}
