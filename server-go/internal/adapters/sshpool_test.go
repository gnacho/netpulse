package adapters

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func newTestPool(t *testing.T) *SSHPool {
	t.Helper()
	kh, err := os.CreateTemp(t.TempDir(), "known_hosts")
	if err != nil {
		t.Fatal(err)
	}
	kh.Close()
	return &SSHPool{
		khPath:  kh.Name(),
		conns:   map[string]*sshConn{},
		dialTCP: ssh.Dial,
	}
}

func TestSSHPoolClosedRejectsDial(t *testing.T) {
	pool := newTestPool(t)
	pool.Close()
	_, err := pool.dial("1.2.3.4")
	if err == nil || err.Error() != "ssh pool closed" {
		t.Fatalf("esperaba 'ssh pool closed', obtuve: %v", err)
	}
}

func TestSSHPoolSingleFlight_LeaderSuccess(t *testing.T) {
	pool := newTestPool(t)
	var dials atomic.Int32
	ready := make(chan struct{})

	pool.dialTCP = func(network, addr string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
		dials.Add(1)
		<-ready
		return &ssh.Client{}, nil
	}

	const N = 2
	results := make([]*ssh.Client, N)
	errs := make([]error, N)
	var wg sync.WaitGroup

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			time.Sleep(30 * time.Millisecond)
			results[i], errs[i] = pool.dial("10.0.0.1")
		}(i)
	}

	time.Sleep(100 * time.Millisecond)
	close(ready)
	wg.Wait()

	if n := dials.Load(); n != 1 {
		t.Fatalf("dialTCP llamado %d veces, esperaba 1 (single-flight)", n)
	}
	for i := 0; i < N; i++ {
		if errs[i] != nil {
			t.Errorf("goroutine %d: %v", i, errs[i])
		}
		if results[i] == nil {
			t.Errorf("goroutine %d: result nil", i)
		}
	}
}

func TestSSHPoolSingleFlight_LeaderFails(t *testing.T) {
	pool := newTestPool(t)
	var dials atomic.Int32
	ready := make(chan struct{})

	pool.dialTCP = func(network, addr string, cfg *ssh.ClientConfig) (*ssh.Client, error) {
		dials.Add(1)
		<-ready
		return nil, &net.OpError{Op: "dial", Err: errors.New("timeout")}
	}

	const N = 2
	var wg sync.WaitGroup
	errCh := make(chan error, N)

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(30 * time.Millisecond)
			_, err := pool.dial("10.0.0.2")
			errCh <- err
		}()
	}

	time.Sleep(100 * time.Millisecond)
	close(ready)
	wg.Wait()
	close(errCh)

	if n := dials.Load(); n != 1 {
		t.Fatalf("dialTCP llamado %d veces, esperaba 1", n)
	}

	failures := 0
	for err := range errCh {
		if err == nil {
			t.Error("esperaba error del follower")
			continue
		}
		if err.Error() == "ssh dial failed" || err.Error()[:3] == "ssh" {
			failures++
		}
	}
	if failures != N {
		t.Errorf("%d errores, esperaba %d", failures, N)
	}
}

func genTestKey(t *testing.T) (ssh.Signer, ssh.PublicKey) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := ssh.NewPublicKey(priv.Public())
	if err != nil {
		t.Fatal(err)
	}
	return signer, pub
}

// TestRefreshHostKeyReplacesEntry: el refresh reemplaza la línea del host por la
// clave nueva, conserva los demás hosts y deja UNA sola entrada (#567).
func TestRefreshHostKeyReplacesEntry(t *testing.T) {
	pool := newTestPool(t)
	host := "host.example"
	other := "other.example"

	_, oldPub := genTestKey(t)
	_, otherPub := genTestKey(t)
	oldLine := knownhosts.Line([]string{knownhosts.Normalize(host)}, oldPub) + "\n"
	otherLine := knownhosts.Line([]string{knownhosts.Normalize(other)}, otherPub) + "\n"
	if err := os.WriteFile(pool.khPath, []byte(oldLine+otherLine), 0o600); err != nil {
		t.Fatal(err)
	}

	_, newPub := genTestKey(t)
	if err := pool.refreshHostKey(host, newPub); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(pool.khPath)
	s := string(data)
	if strings.Contains(s, oldLine) {
		t.Fatal("la línea antigua del host no se eliminó")
	}
	if !strings.Contains(s, otherLine) {
		t.Fatal("se perdió un host no relacionado")
	}
	if n := strings.Count(s, knownhosts.Normalize(host)); n != 1 {
		t.Fatalf("esperaba 1 entrada para %s, hay %d", host, n)
	}
}

// TestHostKeyCallbackReOnboardsOnChangedKey: una clave conocida pero distinta
// ya NO se rechaza; el callback re-onboarda (nil) y actualiza la entrada (#567).
func TestHostKeyCallbackReOnboardsOnChangedKey(t *testing.T) {
	pool := newTestPool(t)
	host := "host.example"
	_, oldPub := genTestKey(t)
	oldLine := knownhosts.Line([]string{knownhosts.Normalize(host)}, oldPub) + "\n"
	if err := os.WriteFile(pool.khPath, []byte(oldLine), 0o600); err != nil {
		t.Fatal(err)
	}

	cb, err := pool.hostKeyCallback()
	if err != nil {
		t.Fatal(err)
	}
	_, newPub := genTestKey(t)
	if err := cb(host, &net.IPAddr{IP: net.ParseIP("1.2.3.4")}, newPub); err != nil {
		t.Fatalf("callback devolvió error en clave cambiada: %v", err)
	}
	data, _ := os.ReadFile(pool.khPath)
	if !strings.Contains(string(data), knownhosts.Normalize(host)) {
		t.Fatal("no se re-onboardó el host")
	}
}
