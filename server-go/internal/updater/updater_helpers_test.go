// updater_helpers_test.go — helpers compartidos de los tests de operaciones
// nuevas del updater (readiness #160, historial #159, pendingApply #161).
package updater

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

// openDB abre una BD real temporal (schema completo) para los tests.
func openDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d.DB
}

// writeSlowScript crea deploy/update.sh que duerme 1 s antes de salir 0.
func writeSlowScript(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/bash\nsleep 1\ntrue\n"
	if err := os.WriteFile(filepath.Join(root, "deploy", "update.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

// waitDone espera a que el update acabe (updating false o step done).
func waitDone(t *testing.T, u *Updater) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := u.Status()
		if m, ok := st.Updating.(progress); ok && m.Step == "done" {
			return
		}
		if st.Updating == false {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout esperando fin del apply: %+v", st)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// gitRootCreaSegundoCommit añade un segundo commit al repo de `root` y
// devuelve su short SHA. Útil para simular que un update movió el HEAD.
func gitRootCreaSegundoCommit(t *testing.T, root string) string {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "f2"), []byte(fmt.Sprintf("y-%d", time.Now().UnixNano())), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-qm", "second")
	out, err := exec.Command("git", "-C", root, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}
