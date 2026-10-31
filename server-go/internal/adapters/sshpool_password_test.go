package adapters

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// startPasswordOnlySSHServer levanta un servidor SSH real en memoria que
// solo acepta la contraseña dada para el usuario dado (dropbear de un
// asuswrt de fábrica: sin llaves). Devuelve la dirección escuchando.
func startPasswordOnlySSHServer(t *testing.T, user, acceptPassword string, seenUser *atomic.Value) net.Addr {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	config := &ssh.ServerConfig{
		PasswordCallback: func(conn ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if seenUser != nil {
				seenUser.Store(conn.User())
			}
			if conn.User() == user && string(password) == acceptPassword {
				return &ssh.Permissions{}, nil
			}
			return nil, fmt.Errorf("password rejected for %q", conn.User())
		},
	}
	config.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// El pool solo necesita el handshake: la conexión se cierra al
			// terminar el test sin llegar a abrir sesiones.
			go func() {
				_, _, reqs, _ := ssh.NewServerConn(conn, config)
				go ssh.DiscardRequests(reqs)
			}()
		}
	}()
	return ln.Addr()
}

// TestSSHPoolPasswordAuthRealHandshake (#1708): con credencial registrada el
// dial completa un handshake SSH real contra un servidor solo-contraseña
// (dropbear de fábrica); con la contraseña equivocada el fallo sale como
// SSHAuthError, que la API convierte en ssh_auth_failed.
func TestSSHPoolPasswordAuthRealHandshake(t *testing.T) {
	var seenUser atomic.Value
	addr := startPasswordOnlySSHServer(t, "admin", "s3cret", &seenUser)
	host := addr.String()

	pool := newTestPool(t)
	pool.SetPassword(host, "admin", "s3cret")
	client, err := pool.dial(host)
	if err != nil {
		t.Fatalf("dial con la contraseña correcta: %v", err)
	}
	if client == nil {
		t.Fatal("dial sin cliente")
	}
	if got := seenUser.Load(); got != "admin" {
		t.Fatalf("el servidor vio al usuario %q, quiero admin", got)
	}
	_ = pool.closeSSHClient(client)

	// Contraseña equivocada: rechazo de autenticación tipado.
	pool.SetPassword(host, "admin", "mala")
	_, err = pool.dial(host)
	var ae *SSHAuthError
	if err == nil || !errors.As(err, &ae) {
		t.Fatalf("la contraseña equivocada debe dar SSHAuthError, obtuve %T: %v", err, err)
	}
}

// TestSSHPoolDialUsesRegisteredPasswordUser (#1708): con credencial el dial
// usa el usuario registrado (no root); sin credencial vuelve a root y a la
// llave del servidor. Con fakes de dialTCP (sin handshake real).
func TestSSHPoolDialUsesRegisteredPasswordUser(t *testing.T) {
	pool := newTestPool(t)
	var lastUser atomic.Value
	lastUser.Store("")

	pool.dialTCP = func(_ string, _ string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
		lastUser.Store(cfg.User)
		return &ssh.Client{}, nil
	}

	pool.SetPassword("10.0.0.9", "admin", "pw")
	if _, err := pool.dial("10.0.0.9"); err != nil {
		t.Fatalf("dial con credencial: %v", err)
	}
	if got := lastUser.Load().(string); got != "admin" {
		t.Fatalf("usuario con credencial = %q, quiero admin", got)
	}

	// Same-value SetPassword no reinicia nada (la conexión cacheada queda).
	if e := pool.conns["10.0.0.9"]; e == nil || e.client == nil {
		t.Fatal("SetPassword con el mismo valor no debe tirar la conexión cacheada")
	}
}

// TestSSHPoolPasswordChangeDropsConnAndBackoff (#1708): cambiar la
// credencial tira la conexión cacheada y limpia el backoff, de modo que un
// fallo de contraseña corregido no hereda el enfriamiento.
func TestSSHPoolPasswordChangeDropsConnAndBackoff(t *testing.T) {
	pool := newTestPool(t)
	// Los clientes del fake son valores cero: el cierre va por el hook.
	pool.closeClient = func(*ssh.Client) error { return nil }
	var dials atomic.Int32
	authErr := errors.New("ssh: handshake failed: ssh: unable to authenticate, attempted methods [none password], no supported methods remain")

	pool.dialTCP = func(_ string, _ string, _ *ssh.ClientConfig) (*ssh.Client, error) {
		dials.Add(1)
		if dials.Load() == 1 {
			return nil, authErr
		}
		return &ssh.Client{}, nil
	}

	pool.SetPassword("10.0.0.9", "admin", "mala")
	if _, err := pool.dial("10.0.0.9"); err == nil {
		t.Fatal("el dial con contraseña mala debía fallar")
	} else {
		var ae *SSHAuthError
		if !errors.As(err, &ae) {
			t.Fatalf("el fallo de autenticación debe salir como SSHAuthError, obtuve %T: %v", err, err)
		}
	}
	if e := pool.conns["10.0.0.9"]; e == nil || !time.Now().Before(e.notUntil) {
		t.Fatal("el fallo debe dejar backoff en el host")
	}

	pool.SetPassword("10.0.0.9", "admin", "buena")
	if e := pool.conns["10.0.0.9"]; e != nil && time.Now().Before(e.notUntil) {
		t.Fatal("el cambio de credencial debe limpiar el backoff")
	}
	if _, err := pool.dial("10.0.0.9"); err != nil {
		t.Fatalf("dial con la credencial corregida: %v", err)
	}
	if dials.Load() != 2 {
		t.Fatalf("dials = %d, quiero 2 (el fallo inicial y la reconexión)", dials.Load())
	}
}

// TestSSHPoolPasswordClearFallsBackToKey (#1708): borrar la credencial
// devuelve el dial a la llave (usuario root) y tira la conexión establecida
// con contraseña.
func TestSSHPoolPasswordClearFallsBackToKey(t *testing.T) {
	pool := newTestPool(t)
	// Los clientes del fake son valores cero: el cierre va por el hook.
	pool.closeClient = func(*ssh.Client) error { return nil }
	var dials atomic.Int32
	var lastUser atomic.Value
	lastUser.Store("")

	pool.dialTCP = func(_ string, _ string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
		dials.Add(1)
		lastUser.Store(cfg.User)
		return &ssh.Client{}, nil
	}

	pool.SetPassword("10.0.0.9", "admin", "pw")
	if _, err := pool.dial("10.0.0.9"); err != nil {
		t.Fatalf("dial: %v", err)
	}
	if lastUser.Load().(string) != "admin" {
		t.Fatal("el primer dial debía ir con el usuario de la credencial")
	}

	pool.SetPassword("10.0.0.9", "", "")
	if _, err := pool.dial("10.0.0.9"); err != nil {
		t.Fatalf("dial tras limpiar: %v", err)
	}
	if dials.Load() != 2 {
		t.Fatalf("dials = %d, quiero 2 (la credencial limpiada tira la conexión)", dials.Load())
	}
	if lastUser.Load().(string) != "root" {
		t.Fatal("tras limpiar la credencial el dial debe volver a root con la llave")
	}
}

// TestSSHPoolAuthErrorNotClassifiedForOtherFailures: un fallo de red no es
// un rechazo de autenticación (código ssh_failed en la API, no
// ssh_auth_failed).
func TestSSHPoolAuthErrorNotClassifiedForOtherFailures(t *testing.T) {
	pool := newTestPool(t)
	pool.dialTCP = func(_ string, _ string, _ *ssh.ClientConfig) (*ssh.Client, error) {
		return nil, &net.OpError{Op: "dial", Err: errors.New("connection refused")}
	}
	_, err := pool.dial("10.0.0.9")
	var ae *SSHAuthError
	if errors.As(err, &ae) {
		t.Fatalf("un fallo de red no debe clasificar como SSHAuthError: %v", err)
	}
}
