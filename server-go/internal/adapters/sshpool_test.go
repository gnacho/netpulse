package adapters

import (
	"errors"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
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
