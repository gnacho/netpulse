package sshkey

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureKeypairRestoresFromBackup(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")

	// Generar par inicial.
	if err := EnsureKeypair(keyPath); err != nil {
		t.Fatalf("EnsureKeypair inicial falló: %v", err)
	}
	origPub, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatalf("no se pudo leer clave pública: %v", err)
	}

	// Simular rotación: RotateKeypair mueve el par actual a .bak.<epoch> y
	// genera uno nuevo. EnsureKeypair no debe restaurar ese backup porque es
	// una rotación intencionada.
	if _, err := RotateKeypair(keyPath); err != nil {
		t.Fatalf("RotateKeypair falló: %v", err)
	}
	if err := EnsureKeypair(keyPath); err != nil {
		t.Fatalf("EnsureKeypair tras rotación falló: %v", err)
	}
	newPub, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatalf("no se pudo leer nueva clave pública: %v", err)
	}
	if string(newPub) == string(origPub) {
		t.Fatalf("se esperaba un par nuevo tras rotación, pero coinciden")
	}

	// Simular pérdida: borrar par actual. EnsureKeypair debería restaurar el backup.
	if err := os.Remove(keyPath); err != nil {
		t.Fatalf("remove key failed: %v", err)
	}
	if err := os.Remove(keyPath + ".pub"); err != nil {
		t.Fatalf("remove pub failed: %v", err)
	}
	if err := EnsureKeypair(keyPath); err != nil {
		t.Fatalf("EnsureKeypair tras pérdida falló: %v", err)
	}
	restoredPub, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		t.Fatalf("no se pudo leer clave pública restaurada: %v", err)
	}
	if string(restoredPub) != string(origPub) {
		t.Fatalf("la clave restaurada no coincide con el backup; got=%s want=%s", restoredPub, origPub)
	}
}

func TestRestoreLatestBackupIgnoresIncomplete(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "id_ed25519")

	// Backup sin .pub: no debería restaurar; se genera uno nuevo.
	bakKey := keyPath + ".bak.1234567890"
	if err := os.WriteFile(bakKey, []byte("incomplete"), 0o600); err != nil {
		t.Fatalf("write incomplete backup failed: %v", err)
	}
	if err := EnsureKeypair(keyPath); err != nil {
		t.Fatalf("EnsureKeypair falló: %v", err)
	}
	if _, err := os.Stat(keyPath + ".pub"); err != nil {
		t.Fatalf("no se generó el par nuevo: %v", err)
	}
}
