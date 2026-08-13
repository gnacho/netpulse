// updater_readiness_test.go — pre-flight checks del apply (issue #160).
package updater

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestReadinessAllOK(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git no disponible")
	}
	root := t.TempDir()
	writeDeployScript(t, root)
	writeGitHead(t, root)
	withAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"sha":"abc1234deadbeef","commit":{"message":"feat: algo"}}`)
	})
	u := New(root, "owner/netpulse", "", "2.0.0", nil)
	r := u.Readiness()
	if r == nil {
		t.Fatal("readiness nil en layout rolling")
	}
	if !r.Disk.OK {
		t.Errorf("disk: %+v", r.Disk)
	}
	if !r.Git.OK {
		t.Errorf("git: %+v", r.Git)
	}
	if !r.Network.OK {
		t.Errorf("network: %+v", r.Network)
	}
	if !r.Concurrent.OK {
		t.Errorf("concurrent: %+v", r.Concurrent)
	}
	if !r.Ready {
		t.Errorf("ready false: %+v", r)
	}
}

func TestReadinessGitDirty(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git no disponible")
	}
	root := t.TempDir()
	writeDeployScript(t, root)
	writeGitHead(t, root)
	// Un archivo untracked NO debe bloquear: los artefactos runtime del
	// servidor (.env, data/, binarios, backups) viven fuera de git y un
	// `git reset --hard` del update.sh no los toca. De lo contrario el
	// apply quedaría bloqueado para siempre en un layout real.
	if err := os.WriteFile(filepath.Join(root, "untracked.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	withAPI(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	u := New(root, "owner/netpulse", "", "2.0.0", nil)
	r := u.Readiness()
	if !r.Git.OK {
		t.Errorf("git NO debería fallar con solo untracked: %+v", r.Git)
	}
	if !r.Ready {
		t.Error("ready debería ser true con solo untracked")
	}
}

func TestReadinessGitTrackedModificado(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git no disponible")
	}
	root := t.TempDir()
	writeDeployScript(t, root)
	writeGitHead(t, root)
	// Un cambio en un archivo tracked SÍ se perdería con git reset --hard.
	if err := os.WriteFile(filepath.Join(root, "f"), []byte("modificado"), 0o644); err != nil {
		t.Fatal(err)
	}
	withAPI(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	u := New(root, "owner/netpulse", "", "2.0.0", nil)
	r := u.Readiness()
	if r.Git.OK {
		t.Errorf("git debería fallar con un tracked modificado: %+v", r.Git)
	}
	if r.Ready {
		t.Error("ready debería ser false con tracked modificado")
	}
}

func TestReadinessConcurrent(t *testing.T) {
	root := t.TempDir()
	writeSlowScript(t, root)
	u := New(root, "owner/netpulse", "", "2.0.0", nil)
	if !u.Apply() {
		t.Fatal("apply debería arrancar")
	}
	defer waitDone(t, u)
	r := u.Readiness()
	if r.Concurrent.OK {
		t.Errorf("concurrent debería fallar durante el apply: %+v", r.Concurrent)
	}
	if r.Ready {
		t.Error("ready debería ser false durante el apply")
	}
}

func TestReadinessDiskInsufficient(t *testing.T) {
	root := t.TempDir()
	writeDeployScript(t, root)
	old := minDiskFreeBytes
	minDiskFreeBytes = 1 << 62 // umbral imposible de cumplir
	t.Cleanup(func() { minDiskFreeBytes = old })
	u := New(root, "owner/netpulse", "", "2.0.0", nil)
	r := u.Readiness()
	if r.Disk.OK {
		t.Errorf("disk debería fallar con umbral imposible: %+v", r.Disk)
	}
}

func TestReadinessNetworkDown(t *testing.T) {
	root := t.TempDir()
	writeDeployScript(t, root)
	old := APIBase
	APIBase = "http://127.0.0.1:1" // puerto cerrado → conexión rechazada
	t.Cleanup(func() { APIBase = old })
	u := New(root, "owner/netpulse", "", "2.0.0", nil)
	r := u.Readiness()
	if r.Network.OK {
		t.Errorf("network debería fallar con endpoint muerto: %+v", r.Network)
	}
}

// Status incluye el readiness (issue #160).
func TestStatusExponeReadiness(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git no disponible")
	}
	root := t.TempDir()
	writeDeployScript(t, root)
	writeGitHead(t, root)
	withAPI(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	u := New(root, "owner/netpulse", "", "2.0.0", nil)
	st := u.Status()
	if st.Readiness == nil {
		t.Fatal("status debería exponer readiness")
	}
	if !st.Readiness.Ready {
		t.Errorf("ready esperado: %+v", st.Readiness)
	}
}
