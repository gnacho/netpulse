// backup_compress_internal_test.go — #818: los backups automáticos se guardan
// comprimidos (.db.gz) y la purga por retención reconoce .db y .db.gz.
package httpapi

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func gunzipBytes(t *testing.T, path string) []byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("abrir %s: %v", path, err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer zr.Close()
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("descomprimir: %v", err)
	}
	return raw
}

func TestRunBackupWritesCompressedDB(t *testing.T) {
	s, _ := newBackupTestServer(t)

	// Datos comprimibles + checkpoint para que el fichero principal crezca
	// (con WAL lo insertado vive en el -wal hasta el checkpoint).
	if _, err := s.db.Exec("CREATE TABLE IF NOT EXISTS compress_test (a TEXT)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	blob := strings.Repeat("netpulse", 40)
	for i := 0; i < 4000; i++ {
		if _, err := tx.Exec("INSERT INTO compress_test (a) VALUES (?)", blob); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if _, err := s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatalf("checkpoint: %v", err)
	}
	dst, err := s.runBackup()
	if err != nil {
		t.Fatalf("runBackup: %v", err)
	}
	if !strings.HasSuffix(dst, ".db.gz") {
		t.Fatalf("esperaba un nombre .db.gz, obtuve %s", dst)
	}

	raw := gunzipBytes(t, dst)
	if !bytes.HasPrefix(raw, []byte("SQLite format 3\x00")) {
		t.Fatalf("lo descomprimido no es una DB SQLite válida")
	}
	dbInfo, err := os.Stat(s.db.Path)
	if err != nil {
		t.Fatalf("stat db: %v", err)
	}
	if int64(len(raw)) != dbInfo.Size() {
		t.Fatalf("el backup descomprimido (%d) no coincide con la BD (%d)", len(raw), dbInfo.Size())
	}
	gzInfo, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat backup: %v", err)
	}
	if gzInfo.Size() >= dbInfo.Size() {
		t.Fatalf("el backup comprimido (%d B) no es menor que la BD (%d B)", gzInfo.Size(), dbInfo.Size())
	}
}

func TestPurgeOldBackupsHandlesBothFormats(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().AddDate(0, 0, -10)
	recent := time.Now()

	write := func(name string, mod time.Time) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		if err := os.Chtimes(p, mod, mod); err != nil {
			t.Fatalf("chtimes %s: %v", name, err)
		}
	}
	write("netpulse-old.db.gz", old)
	write("netpulse-old.db", old)
	write("netpulse-new.db.gz", recent)
	write("netpulse-new.db", recent)
	write("notes.txt", old)

	purgeOldBackups(dir, 3)

	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	if exists("netpulse-old.db.gz") {
		t.Fatal("el backup .db.gz viejo debería haberse purgado")
	}
	if exists("netpulse-old.db") {
		t.Fatal("el backup .db viejo (legacy) debería haberse purgado")
	}
	if !exists("netpulse-new.db.gz") || !exists("netpulse-new.db") {
		t.Fatal("los backups recientes no deberían purgarse")
	}
	if !exists("notes.txt") {
		t.Fatal("un fichero que no es backup no debería purgarse")
	}
}
