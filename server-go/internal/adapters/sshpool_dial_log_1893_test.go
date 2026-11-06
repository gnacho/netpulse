package adapters

// #1893: una unidad solo-SSH mostraba en dropbear ráfagas de "Exit before
// auth" y el server no registraba nada con lo que cruzarlas. Estos tests fijan
// que el pool deja en el log del sistema cada dial nuevo, cada descarte y
// cada fallo con su motivo, y que cierra el episodio al recuperar.

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func captureSSHPoolLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	flags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prev)
		log.SetFlags(flags)
	})
	return &buf
}

func TestSSHPoolDialLogsFailureAndRecovery(t *testing.T) {
	buf := captureSSHPoolLog(t)
	pool := newTestPool(t)
	const host = "192.168.1.50"

	pool.dialTCP = func(_ string, _ string, _ *ssh.ClientConfig) (*ssh.Client, error) {
		return nil, errors.New("i/o timeout")
	}
	if _, err := pool.dial(host); err == nil {
		t.Fatal("el dial debía fallar")
	}
	out := buf.String()
	if !strings.Contains(out, host) || !strings.Contains(out, "i/o timeout") {
		t.Fatalf("el fallo de dial no quedó registrado con host y motivo: %q", out)
	}
	if !strings.Contains(out, "fallo 1") {
		t.Fatalf("el log debería numerar el fallo: %q", out)
	}

	// Recuperación: se limpia el backoff a mano para no esperar al enfriamiento.
	pool.conns[host].notUntil = time.Time{}
	pool.dialTCP = func(_ string, _ string, _ *ssh.ClientConfig) (*ssh.Client, error) {
		return &ssh.Client{}, nil
	}
	if _, err := pool.dial(host); err != nil {
		t.Fatalf("redial: %v", err)
	}
	out = buf.String()
	if !strings.Contains(out, "nueva conexión establecida") {
		t.Fatalf("la conexión nueva no quedó registrada: %q", out)
	}
	if !strings.Contains(out, "recuperada tras 1 fallos") {
		t.Fatalf("la recuperación no cerró el episodio: %q", out)
	}

}

func TestSSHPoolDropLogsDiscard(t *testing.T) {
	buf := captureSSHPoolLog(t)
	var seenUser atomic.Value
	addr := startPasswordOnlySSHServer(t, "admin", "s3cret", &seenUser)
	host := addr.String()

	pool := newTestPool(t)
	pool.SetPassword(host, "admin", "s3cret")
	if _, err := pool.dial(host); err != nil {
		t.Fatalf("dial real: %v", err)
	}
	pool.drop(host)
	if !strings.Contains(buf.String(), "conexión descartada") {
		t.Fatalf("el descarte no quedó registrado: %q", buf.String())
	}
}

func TestSSHPoolDialFailureLogIsRateLimited(t *testing.T) {
	buf := captureSSHPoolLog(t)
	pool := newTestPool(t)
	const host = "192.168.1.51"

	pool.dialTCP = func(_ string, _ string, _ *ssh.ClientConfig) (*ssh.Client, error) {
		return nil, errors.New("connection refused")
	}
	for i := 0; i < 3; i++ {
		if e := pool.conns[host]; e != nil {
			e.notUntil = time.Time{}
		}
		_, _ = pool.dial(host)
	}
	// Tres fallos seguidos: solo el primero se registra (luego cada diez), para
	// que una unidad apagada no escriba en el journal cada ciclo de sondeo.
	if got := strings.Count(buf.String(), "connection refused"); got != 1 {
		t.Fatalf("líneas de fallo = %d, quiero 1 (rate limit): %q", got, buf.String())
	}
}
