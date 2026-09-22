// backup_sched_internal_test.go — #741: el bucle de backups automáticos
// dispara cuando el last_run persistido está vencido. Test interno del
// paquete para poder construir el server con BD temporal.
//
// #807: los tests ejercitan backupTick de forma SÍNCRONA en vez de arrancar
// el bucle y sondear el directorio con wall-clock. El sondeo del directorio
// competía con la escritura de last_run (runBackup crea el fichero antes de
// actualizar el kv) y el deadline fijo fallaba de forma intermitente bajo
// carga de CI. La decisión de "toca backup" ya está cubierta por BackupDue.
package httpapi

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

const backupTestUpsert = "INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value"

func newBackupTestServer(t *testing.T) (*server, string) {
	t.Helper()
	dataDir := t.TempDir()
	d, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return &server{db: d}, dataDir
}

// TestBackupTickRunsWhenDue: con last_run de hace 25h y frecuencia 24h, un
// único tick síncrono crea el backup y actualiza last_run. Sin ticker, sin
// goroutine y sin aserciones sobre el reloj.
func TestBackupTickRunsWhenDue(t *testing.T) {
	s, dataDir := newBackupTestServer(t)

	if _, err := s.db.Exec(backupTestUpsert, kvBackupEnabled, "1"); err != nil {
		t.Fatalf("kv enabled: %v", err)
	}
	if _, err := s.db.Exec(backupTestUpsert, kvBackupFrequencyH, "24"); err != nil {
		t.Fatalf("kv frequency: %v", err)
	}
	oldLastRun := time.Now().Add(-25 * time.Hour).UTC().Format(time.RFC3339)
	if _, err := s.db.Exec(backupTestUpsert, kvBackupLastRun, oldLastRun); err != nil {
		t.Fatalf("kv last_run: %v", err)
	}

	s.backupTick()

	entries, err := os.ReadDir(filepath.Join(dataDir, "backups"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("el tick no creó backup vencido (25h con frecuencia 24h): err=%v entries=%d", err, len(entries))
	}
	var last string
	if err := s.db.QueryRow("SELECT value FROM kv WHERE key = ?", kvBackupLastRun).Scan(&last); err != nil {
		t.Fatalf("leer last_run: %v", err)
	}
	if last == oldLastRun {
		t.Fatal("el backup se creó pero last_run no se actualizó")
	}
}

// TestBackupTickSkipsWhenFresh: last_run de hace 1h con frecuencia 24h -> el
// tick no crea nada.
func TestBackupTickSkipsWhenFresh(t *testing.T) {
	s, dataDir := newBackupTestServer(t)

	s.db.Exec(backupTestUpsert, kvBackupEnabled, "1")
	s.db.Exec(backupTestUpsert, kvBackupFrequencyH, "24")
	s.db.Exec(backupTestUpsert, kvBackupLastRun, time.Now().Add(-time.Hour).UTC().Format(time.RFC3339))

	s.backupTick()

	if entries, err := os.ReadDir(filepath.Join(dataDir, "backups")); err == nil && len(entries) > 0 {
		t.Fatalf("no debía disparar un backup fresco, pero hay %d ficheros", len(entries))
	}
}

// TestBackupLoopStopsOnCancel: el bucle respeta la cancelación del contexto.
// El timeout es solo red de seguridad ante un cuelgue, no una aserción de
// cadencia (no depende de que "pase el tiempo" para dar el resultado).
func TestBackupLoopStopsOnCancel(t *testing.T) {
	s, _ := newBackupTestServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.backupLoop(ctx, time.Hour)
		close(done)
	}()
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("backupLoop no terminó tras cancelar el contexto")
	}
}
