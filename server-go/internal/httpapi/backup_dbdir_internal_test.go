// backup_dbdir_internal_test.go — DB_DIR: con la BD fuera de DATA_DIR,
// el backup se guarda en DATA_DIR/backups y sigue llevando .ssh y tls, que
// viven en DATA_DIR y no junto a la BD.
package httpapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

func TestRunBackupWithDBOutsideDataDir(t *testing.T) {
	dataDir := t.TempDir()
	dbDir := t.TempDir()
	d, err := db.Open(dbDir)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	s := &server{db: d, cfg: &config.Config{DataDir: dataDir}}

	sshDir := filepath.Join(dataDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("mkdir .ssh: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "id_ed25519"), []byte("KEY"), 0o600); err != nil {
		t.Fatalf("escribir clave: %v", err)
	}

	dst, err := s.runBackup()
	if err != nil {
		t.Fatalf("runBackup: %v", err)
	}
	if !strings.HasPrefix(dst, filepath.Join(dataDir, "backups")+string(os.PathSeparator)) {
		t.Fatalf("backup en %s, se esperaba bajo %s/backups", dst, dataDir)
	}
	if _, err := os.Stat(filepath.Join(dbDir, "backups")); !os.IsNotExist(err) {
		t.Fatalf("no debe crearse backups junto a la BD (err=%v)", err)
	}
	bodies, _ := readTarEntries(t, dst)
	if _, ok := bodies["netpulse.db"]; !ok {
		t.Fatalf("el archivo no lleva netpulse.db: %v", keys(bodies))
	}
	if string(bodies[".ssh/id_ed25519"]) != "KEY" {
		t.Fatalf("el archivo no lleva .ssh/id_ed25519 de DATA_DIR: %v", keys(bodies))
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
