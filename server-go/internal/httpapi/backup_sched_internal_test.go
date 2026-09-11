// backup_sched_internal_test.go — #741: el bucle de backups automáticos
// dispara cuando el last_run persistido está vencido. Test interno del
// paquete para poder construir el server con BD temporal y un tick corto.
package httpapi

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func TestBackupLoopRunsWhenDue(t *testing.T) {
	dataDir := t.TempDir()
	d, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()
	s := &server{db: d}

	upsert := "INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value"
	if _, err := d.Exec(upsert, kvBackupEnabled, "1"); err != nil {
		t.Fatalf("kv enabled: %v", err)
	}
	if _, err := d.Exec(upsert, kvBackupFrequencyH, "24"); err != nil {
		t.Fatalf("kv frequency: %v", err)
	}
	oldLastRun := time.Now().Add(-25 * time.Hour).UTC().Format(time.RFC3339)
	if _, err := d.Exec(upsert, kvBackupLastRun, oldLastRun); err != nil {
		t.Fatalf("kv last_run: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.backupLoop(ctx, 20*time.Millisecond)

	backupDir := filepath.Join(dataDir, "backups")
	deadline := time.Now().Add(5 * time.Second)
	for {
		entries, err := os.ReadDir(backupDir)
		if err == nil && len(entries) >= 1 {
			var last string
			_ = d.QueryRow("SELECT value FROM kv WHERE key = ?", kvBackupLastRun).Scan(&last)
			if last == oldLastRun {
				t.Fatal("el backup se creó pero last_run no se actualizó")
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("el scheduler no disparó un backup vencido (25h con frecuencia 24h)")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestBackupLoopSkipsWhenFresh(t *testing.T) {
	dataDir := t.TempDir()
	d, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()
	s := &server{db: d}

	upsert := "INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value"
	d.Exec(upsert, kvBackupEnabled, "1")
	d.Exec(upsert, kvBackupFrequencyH, "24")
	// Último run hace 1h: no vence (24h) y nunca ha habido backup antes.
	d.Exec(upsert, kvBackupLastRun, time.Now().Add(-time.Hour).UTC().Format(time.RFC3339))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.backupLoop(ctx, 20*time.Millisecond)

	backupDir := filepath.Join(dataDir, "backups")
	time.Sleep(300 * time.Millisecond)
	cancel()
	if entries, err := os.ReadDir(backupDir); err == nil && len(entries) > 0 {
		t.Fatalf("no debía disparar un backup fresco, pero hay %d ficheros", len(entries))
	}
}
