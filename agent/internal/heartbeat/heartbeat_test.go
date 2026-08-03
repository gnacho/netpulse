package heartbeat

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTouchAndAge(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hb")
	now := time.Unix(1_000_000, 0)

	if err := Touch(path, now); err != nil {
		t.Fatalf("Touch: %v", err)
	}
	age, ok := Age(path, now.Add(17*time.Second))
	if !ok || age != 17*time.Second {
		t.Fatalf("Age = %v, %v; quiero 17s, true", age, ok)
	}
}

func TestAgeSinFichero(t *testing.T) {
	age, ok := Age(filepath.Join(t.TempDir(), "noexiste"), time.Now())
	if ok || age != 0 {
		t.Fatalf("Age(sin fichero) = %v, %v; quiero 0, false", age, ok)
	}
}

func TestAgeFicheroCorrupto(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hb")
	if err := os.WriteFile(path, []byte("no-es-un-ts\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := Age(path, time.Now()); ok {
		t.Fatal("Age con contenido no numérico debe devolver ok=false")
	}
}

func TestTouchCreaDirectorio(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "dir", "hb")
	if err := Touch(path, time.Unix(42, 0)); err != nil {
		t.Fatalf("Touch con dirs inexistentes: %v", err)
	}
}
