package iwevents

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestListenNoPanicOnExit regression para #448: el agente no debe hacer panic
// cuando el proceso `iw event` termina y Listen vuelve de cmd.Wait.
// Antes el código hacía una aserción de tipo inválida sobre cmd.Stderr.
func TestListenNoPanicOnExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("iw no corre en Windows")
	}

	// Crear un `iw` falso que salga inmediatamente.
	tmpDir := t.TempDir()
	iwPath := filepath.Join(tmpDir, "iw")
	if err := os.WriteFile(iwPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("escribir iw falso: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t.Setenv("PATH", tmpDir+":"+os.Getenv("PATH"))

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Si vuelve a surgir el panic de #448, esta llamada lo lanzará y
		// matará la goroutine de test en vez de propagarse al proceso.
		if err := Listen(ctx, func(Event) {}); err != nil {
			// Un error por ctx cancelado es aceptable; lo importante es no
			// panicar.
		}
	}()

	select {
	case <-done:
		// OK
	case <-time.After(3 * time.Second):
		t.Fatal("Listen no terminó a tiempo")
	}
}
