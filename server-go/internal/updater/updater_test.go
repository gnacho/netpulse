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
	// repoRoot con git válido (commit distinto) para updateAvailable.
	// deploy/update.sh → modo rolling (compara contra main HEAD).
	root := t.TempDir()
	writeDeployScript(t, root)
	writeGitHead(t, root)
	u := New(root, "owner/netpulse", "", "2.0.0", nil)
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
	if !st.CanApply || st.Mode != "rolling" {
		t.Fatalf("debería ser rolling/canApply: canApply=%v mode=%s", st.CanApply, st.Mode)
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
	// Sin deploy/update.sh → modo estable. El 404 llega tanto a /releases/latest
	// como a /commits/main; el error es el mismo.
	u := New(t.TempDir(), "owner/netpulse", "", "2.0.0", nil)
	st := u.Check(context.Background())
	if st.Error == nil || *st.Error != "no_token" {
		t.Fatalf("error: %v", st.Error)
	}
	if st.CanApply || st.Mode != "stable" {
		t.Fatalf("debería ser stable/!canApply: canApply=%v mode=%s", st.CanApply, st.Mode)
	}
}

func TestCheckMismaVersion(t *testing.T) {
	root := t.TempDir()
	writeDeployScript(t, root) // modo rolling
	short := writeGitHead(t, root)
	withAPI(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"sha":"%sffffffffff","commit":{"message":"x"}}`, short)
	})
	u := New(root, "owner/netpulse", "", "2.0.0", nil)
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

// writeDeployScript crea deploy/update.sh en root (marca el layout como
// git/rolling: el updater detecta que puede auto-aplicar).
func writeDeployScript(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "deploy", "update.sh"), []byte("#!/bin/bash\ntrue\n"), 0o755); err != nil {
		t.Fatal(err)
	}
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
	u := New(root, "owner/netpulse", "", "2.0.0", nil)
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

func TestStableModeUpdateAvailable(t *testing.T) {
	withAPI(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/owner/netpulse/releases/latest" {
			t.Errorf("path: %s (modo estable debe consultar releases/latest)", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tag_name":"v2.7.3","name":"v2.7.3"}`)
	})
	// Sin deploy/update.sh → modo estable. Versión embebida 2.7.2 < 2.7.3.
	u := New(t.TempDir(), "owner/netpulse", "", "2.7.2", nil)
	st := u.Check(context.Background())
	if st.Error != nil {
		t.Fatalf("error: %v", *st.Error)
	}
	if !st.UpdateAvailable {
		t.Fatal("debería haber update (2.7.2 < 2.7.3)")
	}
	if st.Latest == nil || *st.Latest != "v2.7.3" {
		t.Fatalf("latest: %v", st.Latest)
	}
	if st.Current != "2.7.2" {
		t.Fatalf("current: %s", st.Current)
	}
	if st.CanApply || st.Mode != "stable" {
		t.Fatalf("debería ser stable/!canApply: canApply=%v mode=%s", st.CanApply, st.Mode)
	}
}

func TestStableModeNoUpdateWhenSameVersion(t *testing.T) {
	withAPI(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v2.7.2","name":"v2.7.2"}`)
	})
	u := New(t.TempDir(), "owner/netpulse", "", "2.7.2", nil)
	st := u.Check(context.Background())
	if st.UpdateAvailable {
		t.Fatal("no debería haber update (misma versión)")
	}
}

func TestStableModeNoUpdateWhenOlder(t *testing.T) {
	withAPI(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name":"v2.7.0","name":"v2.7.0"}`)
	})
	u := New(t.TempDir(), "owner/netpulse", "", "2.7.2", nil)
	st := u.Check(context.Background())
	if st.UpdateAvailable {
		t.Fatal("no debería haber update (2.7.0 < 2.7.2)")
	}
}

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2.7.2", "2.7.2", 0},
		{"v2.7.2", "2.7.2", 0},
		{"v2.7.3", "v2.7.2", 1},
		{"2.8.0", "2.7.9", 1},
		{"3.0.0", "2.9.9", 1},
		{"2.7.1", "2.7.2", -1},
		{"2.6.9", "2.7.0", -1},
		{"v2.7.2-rc1", "v2.7.2", 0}, // pre-release suffix descartado
		{"1.0.0", "1.0", 0},         // minor faltante = 0
	}
	for _, c := range cases {
		got := compareSemver(c.a, c.b)
		// normalizar a -1/0/1 para comparar
		if (c.want == 0 && got != 0) || (c.want > 0 && got <= 0) || (c.want < 0 && got >= 0) {
			t.Errorf("compareSemver(%q, %q) = %d, want sign %d", c.a, c.b, got, c.want)
		}
	}
}

func TestCanApplyDetection(t *testing.T) {
	// Sin deploy/update.sh → stable, !canApply
	u1 := New(t.TempDir(), "owner/netpulse", "", "1.0.0", nil)
	if u1.CanApply() || u1.mode != "stable" {
		t.Fatal("sin deploy/update.sh debería ser stable/!canApply")
	}
	// Con deploy/update.sh → rolling, canApply
	root := t.TempDir()
	writeDeployScript(t, root)
	u2 := New(root, "owner/netpulse", "", "1.0.0", nil)
	if !u2.CanApply() || u2.mode != "rolling" {
		t.Fatal("con deploy/update.sh debería ser rolling/canApply")
	}
}
