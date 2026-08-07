// sshpool.go — Pool de conexiones SSH persistentes (equivalente Go del
// ControlMaster/ControlPersist del JS, SPEC §7.2): UNA conexión por router,
// reutilizada entre ticks del poller, con reconexión automática y backoff
// exponencial (30 s base, máx 5 min) para no martillear un router caído.
//
// known_hosts con semántica accept-new junto a la clave (como el JS):
// host desconocido → se acepta y se anota; host conocido con clave distinta
// → se rechaza (posible MITM).
package adapters

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/sshkey"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const (
	sshDialTimeout    = 4 * time.Second // ConnectTimeout=4 del JS
	sshDefaultTimeout = 5 * time.Second // SSH_TIMEOUT_MS del JS
	sshBackoffBase    = 30 * time.Second
	sshBackoffMax     = 5 * time.Minute
)

// SSHPool gestiona conexiones persistentes a los routers.
type SSHPool struct {
	keyPath string
	signer  ssh.Signer
	khPath  string

	mu     sync.Mutex
	conns  map[string]*sshConn
	closed bool

	dialTCP func(network, addr string, config *ssh.ClientConfig) (*ssh.Client, error)
}

type sshConn struct {
	client   *ssh.Client
	dialing  chan struct{}
	failures int
	notUntil time.Time
}

// NewSSHPool crea el pool cargando la clave privada (ed25519 sin passphrase).
func NewSSHPool(keyPath string) (*SSHPool, error) {
	pem, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("leer clave SSH: %w", err)
	}
	signer, err := ssh.ParsePrivateKey(pem)
	if err != nil {
		return nil, fmt.Errorf("parsear clave SSH: %w", err)
	}
	return &SSHPool{
		keyPath: keyPath,
		signer:  signer,
		khPath:  sshkey.KnownHostsPath(keyPath),
		conns:   map[string]*sshConn{},
		dialTCP: ssh.Dial,
	}, nil
}

// hostKeyCallback implementa accept-new sobre el known_hosts propio.
func (p *SSHPool) hostKeyCallback() (ssh.HostKeyCallback, error) {
	if _, err := os.Stat(p.khPath); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(p.khPath), 0o700); err == nil {
			_ = os.WriteFile(p.khPath, nil, 0o600)
		}
	}
	checker, err := knownhosts.New(p.khPath)
	if err != nil {
		return nil, err
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		err := checker(hostname, remote, key)
		if err == nil {
			return nil
		}
		var keyErr *knownhosts.KeyError
		if errors.As(err, &keyErr) && len(keyErr.Want) > 0 {
			return err // clave CAMBIADA → rechazar
		}
		// Host desconocido → accept-new
		line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
		f, ferr := os.OpenFile(p.khPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if ferr != nil {
			return ferr
		}
		defer f.Close()
		_, ferr = f.WriteString(line + "\n")
		return ferr
	}, nil
}

// dial abre (o reabre) la conexión a un host respetando el backoff.
// Single-flight por host: dos llamantes concurrentes comparten el mismo dial.
func (p *SSHPool) dial(host string) (*ssh.Client, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, errors.New("ssh pool closed")
	}
	entry := p.conns[host]
	if entry == nil {
		entry = &sshConn{}
		p.conns[host] = entry
	}
	if entry.client != nil {
		c := entry.client
		p.mu.Unlock()
		return c, nil
	}
	if entry.dialing != nil {
		ch := entry.dialing
		p.mu.Unlock()
		<-ch
		p.mu.Lock()
		if entry.client != nil {
			c := entry.client
			p.mu.Unlock()
			return c, nil
		}
		p.mu.Unlock()
		return nil, errors.New("ssh dial failed")
	}
	if time.Now().Before(entry.notUntil) {
		p.mu.Unlock()
		return nil, fmt.Errorf("ssh %s: en backoff tras %d fallos", host, entry.failures)
	}
	entry.dialing = make(chan struct{})
	p.mu.Unlock()

	cb, err := p.hostKeyCallback()
	if err != nil {
		p.mu.Lock()
		close(entry.dialing)
		entry.dialing = nil
		p.mu.Unlock()
		return nil, err
	}
	cfg := &ssh.ClientConfig{
		User:            "root",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(p.signer)},
		HostKeyCallback: cb,
		Timeout:         sshDialTimeout,
	}
	client, err := p.dialTCP("tcp", net.JoinHostPort(host, "22"), cfg)

	p.mu.Lock()
	close(entry.dialing)
	entry.dialing = nil
	if err != nil {
		entry.failures++
		backoff := sshBackoffBase << min(entry.failures-1, 4)
		if backoff > sshBackoffMax {
			backoff = sshBackoffMax
		}
		entry.notUntil = time.Now().Add(backoff)
		p.mu.Unlock()
		return nil, fmt.Errorf("ssh %s: %w", host, err)
	}
	entry.client = client
	entry.failures = 0
	entry.notUntil = time.Time{}
	p.mu.Unlock()
	return client, nil
}

// drop invalida la conexión de un host (la próxima llamada rediala).
func (p *SSHPool) drop(host string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if e := p.conns[host]; e != nil && e.client != nil {
		_ = e.client.Close()
		e.client = nil
	}
}

// Run ejecuta cmd en host con timeout (por defecto 5 s, como el JS).
// Una sesión fallida por canal roto invalida la conexión (reconexión).
func (p *SSHPool) Run(host, cmd string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = sshDefaultTimeout
	}
	client, err := p.dial(host)
	if err != nil {
		return "", err
	}
	session, err := client.NewSession()
	if err != nil {
		p.drop(host)
		return "", fmt.Errorf("ssh %s: session: %w", host, err)
	}
	defer session.Close()

	type result struct {
		out []byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		out, err := session.Output(cmd)
		ch <- result{out, err}
	}()
	select {
	case res := <-ch:
		if res.err != nil {
			var ee *ssh.ExitError
			if !errors.As(res.err, &ee) {
				p.drop(host) // canal roto, no exit status
			}
			return "", fmt.Errorf("ssh %s: %w", host, res.err)
		}
		return string(res.out), nil
	case <-time.After(timeout):
		_ = session.Close()
		p.drop(host)
		return "", fmt.Errorf("ssh %s: timeout (%s)", host, timeout)
	}
}

// RunCtx es Run con cancelación por contexto.
func (p *SSHPool) RunCtx(ctx context.Context, host, cmd string, timeout time.Duration) (string, error) {
	type res struct {
		out string
		err error
	}
	ch := make(chan res, 1)
	go func() {
		out, err := p.Run(host, cmd, timeout)
		ch <- res{out, err}
	}()
	select {
	case r := <-ch:
		return r.out, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// Close cierra todas las conexiones del pool.
func (p *SSHPool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	for _, e := range p.conns {
		if e.client != nil {
			_ = e.client.Close()
			e.client = nil
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
