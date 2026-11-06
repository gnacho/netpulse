package runtime

// #1897: el backup standalone de config dejaba el tar.gz en el dispositivo
// tras subirlo. En un router /tmp es tmpfs, o sea RAM, así que cada backup
// manual o programado restaba memoria hasta el reboot. Estos tests fijan que
// el fichero local desaparece tanto si la subida va bien como si falla.

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// stubConfigBackup apunta el tmp del firmware a un dir temporal y sustituye
// sysupgrade por un script que crea el tar.gz que el handler espera.
func stubConfigBackup(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	oldTmp := firmwareTmpDir
	firmwareTmpDir = dir
	t.Cleanup(func() { firmwareTmpDir = oldTmp })

	fake := filepath.Join(dir, "sysupgrade")
	// sysupgrade -b <dest>: el segundo argumento es el fichero a crear.
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nprintf 'tar' > \"$2\"\n"), 0o755); err != nil {
		t.Fatalf("WriteFile sysupgrade fake: %v", err)
	}
	oldBin := sysupgradeBin
	sysupgradeBin = fake
	t.Cleanup(func() { sysupgradeBin = oldBin })
	return dir
}

func leftoverConfigBackups(t *testing.T, dir string) []string {
	t.Helper()
	left, err := filepath.Glob(filepath.Join(dir, "netpulse-config-backup-*.tar.gz"))
	if err != nil {
		t.Fatalf("Glob: %v", err)
	}
	return left
}

func TestConfigBackupDeletesTheLocalTarball(t *testing.T) {
	dir := stubConfigBackup(t)

	var mu sync.Mutex
	uploads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/agents/patio/firmware-backup" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		mu.Lock()
		uploads++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	handleConfigBackup(Options{Server: srv.URL, Slug: "patio", Token: "tok"}, nil)

	mu.Lock()
	if uploads != 1 {
		t.Fatalf("subidas = %d, quiero 1", uploads)
	}
	mu.Unlock()
	if left := leftoverConfigBackups(t, dir); len(left) != 0 {
		t.Fatalf("quedan backups en el dispositivo tras subir: %v", left)
	}
}

func TestConfigBackupDeletesTheLocalTarballWhenUploadFails(t *testing.T) {
	dir := stubConfigBackup(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	handleConfigBackup(Options{Server: srv.URL, Slug: "patio", Token: "tok"}, nil)

	// Sin reintento posterior, un fichero huérfano solo sirve para gastar RAM.
	if left := leftoverConfigBackups(t, dir); len(left) != 0 {
		t.Fatalf("quedan backups en el dispositivo tras una subida fallida: %v", left)
	}
}
