// updater_restart_test.go — #444: cuando el reinicio diferido mata a
// update.sh después del swap, el marcador pre-reinicio convierte la muerte
// por señal en el camino de éxito (historial success + pendingApply vivo).
package updater

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestApplyKilledAfterMarkerIsSuccess(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git no disponible")
	}
	root := t.TempDir()
	writeGitHead(t, root)
	if err := os.MkdirAll(filepath.Join(root, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Simula el final real de update.sh: escribe el marcador con el SHA
	// objetivo y muere por señal (lo que hace el restart del systemd.path).
	script := "#!/bin/bash\necho STEP:install\necho abc1234 > '" + root + "/.update-applied'\nkill -9 $$\n"
	if err := os.WriteFile(filepath.Join(root, "deploy", "update.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	withAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"sha":"abc1234deadbeef","commit":{"message":"feat: algo"}}`)
	})
	dbh := openDB(t)
	u := New(root, "owner/netpulse", "", "2.0.0", dbh)
	u.Check(context.Background()) // fija latest = abc1234
	if !u.Apply() {
		t.Fatal("apply debería arrancar")
	}
	waitDone(t, u)

	st := u.Status()
	if st.Error != nil {
		t.Fatalf("no debería haber error (fue el reinicio esperado): %v", *st.Error)
	}
	// El marcador en BD debe seguir vivo para la confirmación post-reinicio
	// (#161); PendingApply en memoria solo se expone tras el arranque.
	if _, ok := readPendingApply(dbh); !ok {
		t.Fatal("el marcador pendingApply debe persistirse para el post-reinicio")
	}
	hist, err := ListHistory(dbh, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) == 0 || hist[0].Status != "success" {
		t.Fatalf("historial: esperaba success, got %+v", hist)
	}
}

func TestApplyKilledWithoutMarkerIsFailure(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git no disponible")
	}
	root := t.TempDir()
	writeGitHead(t, root)
	if err := os.MkdirAll(filepath.Join(root, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Muere por señal SIN marcador: fallo real (crash del script).
	script := "#!/bin/bash\nkill -9 $$\n"
	if err := os.WriteFile(filepath.Join(root, "deploy", "update.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	withAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"sha":"abc1234deadbeef","commit":{"message":"feat: algo"}}`)
	})
	dbh := openDB(t)
	u := New(root, "owner/netpulse", "", "2.0.0", dbh)
	u.Check(context.Background())
	if !u.Apply() {
		t.Fatal("apply debería arrancar")
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		if u.Status().Error != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timeout esperando el fallo")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if u.Status().PendingApply != nil {
		t.Fatal("pendingApply debe limpiarse en un fallo real (#161)")
	}
	hist, err := ListHistory(dbh, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hist) == 0 || hist[0].Status != "failed" {
		t.Fatalf("historial: esperaba failed, got %+v", hist)
	}
}
