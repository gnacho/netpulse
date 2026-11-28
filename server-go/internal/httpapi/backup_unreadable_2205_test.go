// backup_unreadable_2205_test.go — #2205: un fichero no legible dentro de
// .ssh (un .bak-<fecha>-rearm que dejó una sesión como root) hacía fallar el
// backup entero con EACCES, así que no se escribía ningún archivo y el
// siguiente tick volvía a intentarlo cada minuto, una línea de journal por
// minuto. El backup tiene que salir sin ese fichero, diciendo cuál dejó fuera.
package httpapi

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestBackupSkipsUnreadableFileInSSHDir: el archivo se crea y lleva la clave,
// la no legible queda fuera y se reporta por su ruta relativa.
func TestBackupSkipsUnreadableFileInSSHDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("como root todo es legible: el caso no se reproduce")
	}
	s, dataDir := newBackupTestServer(t)
	sshDir := filepath.Join(dataDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatalf("mkdir .ssh: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sshDir, "id_ed25519"), []byte("KEY"), 0o600); err != nil {
		t.Fatalf("escribir clave: %v", err)
	}
	junk := filepath.Join(sshDir, "known_hosts.bak-20261128-rearm")
	if err := os.WriteFile(junk, []byte("stale"), 0o600); err != nil {
		t.Fatalf("escribir .bak: %v", err)
	}
	if err := os.Chmod(junk, 0o000); err != nil {
		t.Fatalf("chmod .bak: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(junk, 0o600) })

	dst := filepath.Join(dataDir, "backups", "netpulse-test.tar.gz")
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		t.Fatalf("mkdir backups: %v", err)
	}

	skipped, err := writeBackupArchive(s.db.DB, dataDir, s.db.Path, dst)
	if err != nil {
		t.Fatalf("el backup no debe abortar por un fichero no legible: %v", err)
	}
	if len(skipped) != 1 || skipped[0] != ".ssh/known_hosts.bak-20261128-rearm" {
		t.Fatalf("debe reportar el fichero dejado fuera, got %v", skipped)
	}

	bodies, _ := readTarEntries(t, dst)
	if _, ok := bodies["netpulse.db"]; !ok {
		t.Fatalf("el archivo no lleva la BD, entradas: %v", keysOf(bodies))
	}
	if _, ok := bodies[".ssh/id_ed25519"]; !ok {
		t.Fatalf("la clave SSH debe seguir en el archivo, entradas: %v", keysOf(bodies))
	}
	if _, ok := bodies[".ssh/known_hosts.bak-20261128-rearm"]; ok {
		t.Fatal("el fichero no legible no puede entrar en el archivo")
	}
}

// TestBackupFailureLogIsRateLimited: el mismo error no se escribe en el
// journal una vez por minuto. Un error distinto se escribe siempre.
func TestBackupFailureLogIsRateLimited(t *testing.T) {
	now := time.Date(2026, 11, 28, 3, 1, 0, 0, time.UTC)
	l := &backupFailureLog{now: func() time.Time { return now }}

	if !l.allow("permission denied") {
		t.Fatal("el primer fallo debe registrarse")
	}
	now = now.Add(time.Minute)
	if l.allow("permission denied") {
		t.Fatal("el mismo fallo un minuto después no debe repetirse")
	}
	now = now.Add(58 * time.Minute)
	if l.allow("permission denied") {
		t.Fatal("el mismo fallo dentro de la hora no debe repetirse")
	}
	now = now.Add(2 * time.Minute)
	if !l.allow("permission denied") {
		t.Fatal("pasada la hora el fallo persistente debe volver a registrarse")
	}
	now = now.Add(time.Minute)
	if !l.allow("no space left on device") {
		t.Fatal("un error distinto se registra siempre")
	}
}
