// backup_snapshot_1866_test.go — #1866: el backup copiaba el fichero de la BD
// vivo, así que en producción fallaba siempre con "archive/tar: write too
// long" (el fichero crecía entre la cabecera y la copia) y, aunque hubiera
// pasado, el .db podía venir sin el WAL. Ahora se archiva un snapshot hecho
// con VACUUM INTO y addTarFile corta la copia en el tamaño de la cabecera.
package httpapi

import (
	"archive/tar"
	"bytes"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// TestRunBackupSurvivesAConcurrentWriter reproduce el caso real: la BD sigue
// recibiendo escrituras mientras corre el backup. Con la copia directa del
// fichero esto abortaba; con el snapshot tiene que salir y ser íntegro.
func TestRunBackupSurvivesAConcurrentWriter(t *testing.T) {
	s, dataDir := newBackupTestServer(t)
	backupDir := filepath.Join(dataDir, "backups")
	if err := ensureBackupDir(backupDir); err != nil {
		t.Fatalf("ensureBackupDir: %v", err)
	}
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS writer_probe (id INTEGER PRIMARY KEY, v TEXT)`); err != nil {
		t.Fatalf("crear tabla: %v", err)
	}
	for i := 0; i < 200; i++ {
		if _, err := s.db.Exec(`INSERT INTO writer_probe (v) VALUES (?)`, "seed"); err != nil {
			t.Fatalf("sembrar: %v", err)
		}
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		payload := string(bytes.Repeat([]byte("x"), 4096))
		for {
			select {
			case <-stop:
				return
			default:
			}
			// Escrituras reales: crecen el WAL y el fichero mientras se archiva.
			if _, err := s.db.Exec(`INSERT INTO writer_probe (v) VALUES (?)`, payload); err != nil {
				return
			}
		}
	}()

	dst, err := s.runBackup()
	close(stop)
	wg.Wait()
	if err != nil {
		t.Fatalf("runBackup con escritor concurrente: %v", err)
	}

	bodies, _ := readTarEntries(t, dst)
	dbBytes, ok := bodies["netpulse.db"]
	if !ok {
		t.Fatalf("el archivo no lleva netpulse.db, entradas: %v", keysOf(bodies))
	}

	// El snapshot tiene que ser una base válida por sí sola: sin el WAL del
	// servidor, abierta en un directorio limpio.
	restore := filepath.Join(t.TempDir(), "netpulse.db")
	if err := os.WriteFile(restore, dbBytes, 0o600); err != nil {
		t.Fatalf("escribir snapshot: %v", err)
	}
	conn, err := sql.Open("sqlite", "file:"+restore+"?mode=ro")
	if err != nil {
		t.Fatalf("abrir snapshot: %v", err)
	}
	defer conn.Close()
	var integrity string
	if err := conn.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatalf("integrity_check: %v", err)
	}
	if integrity != "ok" {
		t.Fatalf("integrity_check = %q", integrity)
	}
	var rows int
	if err := conn.QueryRow(`SELECT count(*) FROM writer_probe`).Scan(&rows); err != nil {
		t.Fatalf("contar filas: %v", err)
	}
	if rows < 200 {
		t.Fatalf("el snapshot perdió filas: %d", rows)
	}
}

// TestAddTarFileDoesNotOutgrowTheHeader fija la otra mitad del arreglo: un
// fichero que crece a mitad de copia ya no rompe el tar, porque la copia se
// corta en el tamaño declarado.
func TestAddTarFileDoesNotOutgrowTheHeader(t *testing.T) {
	src := filepath.Join(t.TempDir(), "growing.bin")
	if err := os.WriteFile(src, bytes.Repeat([]byte("a"), 1<<20), 0o600); err != nil {
		t.Fatalf("crear origen: %v", err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		f, err := os.OpenFile(src, os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return
		}
		defer f.Close()
		chunk := bytes.Repeat([]byte("b"), 32<<10)
		for {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := f.Write(chunk); err != nil {
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	err := addTarFile(tw, src, "growing.bin")
	close(stop)
	wg.Wait()
	if err != nil {
		t.Fatalf("addTarFile con fichero creciendo: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("cerrar tar: %v", err)
	}

	tr := tar.NewReader(&buf)
	hdr, err := tr.Next()
	if err != nil {
		t.Fatalf("leer cabecera: %v", err)
	}
	body, err := io.ReadAll(tr)
	if err != nil {
		t.Fatalf("leer cuerpo: %v", err)
	}
	if int64(len(body)) != hdr.Size {
		t.Fatalf("cuerpo %d bytes, cabecera %d", len(body), hdr.Size)
	}
}
