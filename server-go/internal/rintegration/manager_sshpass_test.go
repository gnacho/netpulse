package rintegration

import (
	"os"
	"path/filepath"
	"testing"
)

// TestSSHPasswordFilePlumbing (#1708): el fichero compañero <slug>.sshpass
// vive junto al env del pusher, va a 0600, se lee sin salto final y su
// borrado es idempotente. Sin fichero la lectura devuelve "" sin error: la
// llave sigue siendo el método de autenticación por defecto.
func TestSSHPasswordFilePlumbing(t *testing.T) {
	mgr := NewManager(t.TempDir())

	if pass, err := mgr.ReadSSHPassword("lab"); err != nil || pass != "" {
		t.Fatalf("sin fichero: pass=%q err=%v, quiero \"\" y nil", pass, err)
	}

	if err := mgr.WriteSSHPassword("lab", "s3cret"); err != nil {
		t.Fatalf("write: %v", err)
	}
	info, err := os.Stat(mgr.SSHPasswordPath("lab"))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("modo del fichero = %o, quiero 600", got)
	}
	if filepath.Base(mgr.SSHPasswordPath("lab")) != "lab.sshpass" {
		t.Fatalf("nombre del fichero = %s, quiero lab.sshpass", filepath.Base(mgr.SSHPasswordPath("lab")))
	}
	if pass, err := mgr.ReadSSHPassword("lab"); err != nil || pass != "s3cret" {
		t.Fatalf("read: pass=%q err=%v", pass, err)
	}

	// Un salto final de un fichero escrito a mano no entra en la contraseña.
	if err := os.WriteFile(mgr.SSHPasswordPath("lab"), []byte("s3cret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if pass, _ := mgr.ReadSSHPassword("lab"); pass != "s3cret" {
		t.Fatalf("read con salto final: pass=%q, quiero sin salto", pass)
	}

	// Reescribir cambia el valor sin dejar el viejo.
	if err := mgr.WriteSSHPassword("lab", "nueva"); err != nil {
		t.Fatal(err)
	}
	if pass, _ := mgr.ReadSSHPassword("lab"); pass != "nueva" {
		t.Fatalf("read tras reescribir: pass=%q", pass)
	}

	if err := mgr.RemoveSSHPassword("lab"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(mgr.SSHPasswordPath("lab")); !os.IsNotExist(err) {
		t.Fatalf("el fichero sigue ahí: %v", err)
	}
	if err := mgr.RemoveSSHPassword("lab"); err != nil {
		t.Fatalf("remove idempotente: %v", err)
	}
}
