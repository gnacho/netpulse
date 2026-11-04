// backup_compress_internal_test.go — #818: los backups automáticos se guardan
// comprimidos; #1864: en un único tar.gz con la BD, .ssh y tls, y la purga por
// retención reconoce también los formatos anteriores (.db, .db.gz).
package httpapi

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// readTarEntries vuelca un .tar.gz a nombre -> contenido + cabecera.
func readTarEntries(t *testing.T, path string) (map[string][]byte, map[string]*tar.Header) {
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
	tr := tar.NewReader(zr)
	bodies := map[string][]byte{}
	headers := map[string]*tar.Header{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("leer tar: %v", err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("leer %s: %v", name(hdr), err)
		}
		bodies[name(hdr)] = body
		headers[name(hdr)] = hdr
	}
	return bodies, headers
}

func name(hdr *tar.Header) string {
	return strings.TrimSuffix(hdr.Name, "/")
}

func TestRunBackupWritesCompleteArchive(t *testing.T) {
	s, dataDir := newBackupTestServer(t)

	// El material que faltaba (#1864): la clave con la que el servidor entra
	// en los routers y el certificado TLS.
	sshDir := filepath.Join(dataDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("mkdir .ssh: %v", err)
	}
	keyPath := filepath.Join(sshDir, "id_ed25519")
	if err := os.WriteFile(keyPath, []byte("PRIVATE-KEY-BYTES"), 0o600); err != nil {
		t.Fatalf("escribir clave: %v", err)
	}
	tlsDir := filepath.Join(dataDir, "tls")
	if err := os.MkdirAll(tlsDir, 0o700); err != nil {
		t.Fatalf("mkdir tls: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tlsDir, "cert.pem"), []byte("CERT"), 0o600); err != nil {
		t.Fatalf("escribir cert: %v", err)
	}

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
	if !strings.HasSuffix(dst, ".tar.gz") {
		t.Fatalf("esperaba un nombre .tar.gz, obtuve %s", dst)
	}

	bodies, headers := readTarEntries(t, dst)

	dbBody, ok := bodies["netpulse.db"]
	if !ok {
		t.Fatalf("el archivo no lleva netpulse.db: %v", keysOf(bodies))
	}
	if !strings.HasPrefix(string(dbBody), "SQLite format 3\x00") {
		t.Fatal("netpulse.db del archivo no es una DB SQLite válida")
	}
	dbInfo, err := os.Stat(s.db.Path)
	if err != nil {
		t.Fatalf("stat db: %v", err)
	}
	if int64(len(dbBody)) != dbInfo.Size() {
		t.Fatalf("netpulse.db del archivo (%d) no coincide con la BD (%d)", len(dbBody), dbInfo.Size())
	}

	keyBody, ok := bodies[".ssh/id_ed25519"]
	if !ok {
		t.Fatalf("el archivo no lleva la clave SSH: %v", keysOf(bodies))
	}
	if string(keyBody) != "PRIVATE-KEY-BYTES" {
		t.Fatalf("la clave del archivo no coincide: %q", keyBody)
	}
	if mode := headers[".ssh/id_ed25519"].Mode; mode != 0o600 {
		t.Fatalf("la clave debería viajar en 0600, va en %o", mode)
	}
	if _, ok := bodies["tls/cert.pem"]; !ok {
		t.Fatalf("el archivo no lleva tls: %v", keysOf(bodies))
	}

	arcInfo, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat backup: %v", err)
	}
	if arcInfo.Size() >= dbInfo.Size() {
		t.Fatalf("el archivo (%d B) no es menor que la BD (%d B)", arcInfo.Size(), dbInfo.Size())
	}
}

func keysOf(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestBackupArchiveWithoutSSHOrTLS(t *testing.T) {
	s, _ := newBackupTestServer(t)

	dst, err := s.runBackup()
	if err != nil {
		t.Fatalf("runBackup: %v", err)
	}
	bodies, _ := readTarEntries(t, dst)
	if _, ok := bodies["netpulse.db"]; !ok {
		t.Fatalf("un backup sin .ssh ni tls debe llevar igualmente la BD: %v", keysOf(bodies))
	}
	if len(bodies) != 1 {
		t.Fatalf("esperaba solo netpulse.db, hay %v", keysOf(bodies))
	}
}

func TestPurgeOldBackupsHandlesAllFormats(t *testing.T) {
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
	write("netpulse-old.tar.gz", old)
	write("netpulse-old.db.gz", old)
	write("netpulse-old.db", old)
	write("netpulse-new.tar.gz", recent)
	write("netpulse-new.db.gz", recent)
	write("netpulse-new.db", recent)
	write("notes.txt", old)

	purgeOldBackups(dir, 3)

	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	for _, name := range []string{"netpulse-old.tar.gz", "netpulse-old.db.gz", "netpulse-old.db"} {
		if exists(name) {
			t.Fatalf("el backup viejo %s debería haberse purgado", name)
		}
	}
	for _, name := range []string{"netpulse-new.tar.gz", "netpulse-new.db.gz", "netpulse-new.db", "notes.txt"} {
		if !exists(name) {
			t.Fatalf("%s no debería purgarse", name)
		}
	}
}
