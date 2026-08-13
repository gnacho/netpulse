// update_helpers_test.go — helpers de los tests del módulo updater
// (issues #159, #160, #161): servidor con updater rolling (repo git +
// deploy/update.sh) y BD real.
package httpapi_test

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/httpapi"
	"github.com/gnacho/netpulse/server-go/internal/sse"
	"github.com/gnacho/netpulse/server-go/internal/updater"
)

func gitAvailable() bool { _, err := exec.LookPath("git"); return err == nil }

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

// writeDeployScript crea deploy/update.sh en root (layout rolling).
func writeDeployScript(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, "deploy"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "deploy", "update.sh"), []byte("#!/bin/bash\ntrue\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

// gitCreaSegundoCommit añade un segundo commit al repo de `root` y devuelve
// su short SHA (simula que un update movió el HEAD).
func gitCreaSegundoCommit(t *testing.T, root string) string {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "f2"), []byte("y"), 0o644); err != nil {
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

// makeTestServerWithUpdater: app real con updater (repoRoot; si tiene
// deploy/update.sh el updater es rolling) y BD real.
func makeTestServerWithUpdater(t *testing.T, repoRoot string) *testServer {
	t.Helper()
	auth.SetTrustProxy(true)
	t.Cleanup(func() { auth.SetTrustProxy(false) })
	dataDir := t.TempDir()
	cfg, err := config.Load(map[string]string{
		"AUTH_USER": "admin", "AUTH_PASS": "test1234",
		"DEMO_MODE": "0", "DATA_DIR": dataDir, "NODE_ENV": "test",
	}, dataDir)
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	d, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	secret, err := auth.EnsureSessionSecret(d, cfg)
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	if err := auth.EnsureUsers(d, cfg); err != nil {
		t.Fatalf("users: %v", err)
	}
	adapter := adapters.NewDemo()
	hub := sse.NewHub(d, cfg.MaxSSEClients, func() any { return nil })
	up := updater.New(repoRoot, "owner/netpulse", "", "2.0.0", d.DB)
	handler := httpapi.NewHandler(httpapi.Deps{
		Config: cfg, DB: d, Adapter: adapter, Hub: hub, Secret: secret,
		Started: time.Now(), Updater: up,
	})
	srv := httptest.NewServer(handler)
	ts := &testServer{Server: srv, db: d, secret: secret}
	t.Cleanup(func() {
		srv.Close()
		d.Close()
	})
	return ts
}

func adminCookie(t *testing.T, ts *testServer) string {
	t.Helper()
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test1234")
	if cookie == "" {
		t.Fatal("login admin falló")
	}
	return cookie
}
