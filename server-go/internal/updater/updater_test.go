// updater_test.go — check contra API GitHub simulada (httptest) y shape del
// estado; apply con script sintético (STEP:*/exit).
package updater

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func gitAvailable() bool { _, err := exec.LookPath("git"); return err == nil }

func withAPI(t *testing.T, h http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	old := APIBase
	APIBase = srv.URL
	t.Cleanup(func() { APIBase = old })
}

func TestCheckUpdateAvailable(t *testing.T) {
	withAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/netpulse/commits/main" {
			t.Errorf("path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"sha":"abc1234deadbeef","commit":{"message":"feat: algo\n\nbody"}}`)
	})
	// repoRoot con git válido (commit distinto) para updateAvailable
	root := t.TempDir()
	writeGitHead(t, root)
	u := New(root, "owner/netpulse", "")
	st := u.Check(context.Background())
	if st.Error != nil {
		t.Fatalf("error: %v", *st.Error)
	}
	if st.Latest == nil || *st.Latest != "abc1234" {
		t.Fatalf("latest: %v", st.Latest)
	}
	if st.LatestMsg == nil || *st.LatestMsg != "feat: algo" {
		t.Fatalf("latestMsg: %v", st.LatestMsg)
	}
	if !st.UpdateAvailable {
		t.Fatalf("debería haber update: %+v", st)
	}
	if st.Updating != false || st.HasToken || st.Repo != "owner/netpulse" {
		t.Fatalf("shape: %+v", st)
	}
	if st.LastCheck == nil {
		t.Fatal("lastCheck nil")
	}
}

func TestCheckNoToken(t *testing.T) {
	withAPI(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	u := New(t.TempDir(), "owner/netpulse", "")
	st := u.Check(context.Background())
	if st.Error == nil || *st.Error != "no_token" {
		t.Fatalf("error: %v", st.Error)
	}
}

func TestCheckMismaVersion(t *testing.T) {
	root := t.TempDir()
	short := writeGitHead(t, root)
	withAPI(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"sha":"%sffffffffff","commit":{"message":"x"}}`, short)
	})
	u := New(root, "owner/netpulse", "")
	st := u.Check(context.Background())
	if st.UpdateAvailable {
		t.Fatal("no debería haber update")
	}
}

// writeGitHead crea un repo git mínimo con un commit y devuelve su short SHA.
func writeGitHead(t *testing.T, root string) string {
	t.Helper()
	if !gitAvailable() {
		t.Skip("git no disponible")
	}
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@t")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(root, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-qm", "init")
	out, err := exec.Command("git", "-C", root, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestApplyYaEnCursoYScript(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git no disponible")
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/bash\necho STEP:backend\nsleep 0.1\necho STEP:done-steps\ntrue\n"
	if err := os.WriteFile(filepath.Join(root, "deploy", "update.sh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	u := New(root, "owner/netpulse", "")
	if !u.Apply() {
		t.Fatal("apply debería arrancar")
	}
	// Segundo apply concurrente → 409 already_updating
	if u.Apply() {
		t.Fatal("apply duplicado debería ser false")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := u.Status()
		if m, ok := st.Updating.(progress); ok && m.Step == "done" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("timeout esperando done: %+v", st)
		}
		time.Sleep(50 * time.Millisecond)
	}
	st := u.Status()
	if st.LastLog == nil || *st.LastLog == "" {
		t.Fatalf("lastLog: %+v", st)
	}
}
