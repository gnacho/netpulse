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
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
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
			// Clave cambiada (#603): NO se auto-acepta. Una clave distinta en
			// un host ya conocido es un evento de seguridad (posible MITM);
			// el operador debe confirmar el re-onboard explícitamente tras
			// verificar fuera de banda (p. ej. tras un flash del router). El
			// error tipado alimenta el estado "host key changed" del router.
			oldFP := ""
			if len(keyErr.Want) > 0 {
				oldFP = ssh.FingerprintSHA256(keyErr.Want[0].Key)
			}
			newFP := ssh.FingerprintSHA256(key)
			log.Printf("ssh %s: host key changed (%s -> %s); re-onboard requerido", hostname, oldFP, newFP)
			return &HostKeyChangedError{Host: hostname, OldFP: oldFP, NewFP: newFP}
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

// HostKeyChangedError: el host presenta una clave distinta a la registrada en
// known_hosts. No se auto-acepta: la conexión se rechaza hasta que el admin
// confirme el re-onboard (RemoveHostKey + siguiente sondeo hace TOFU).
type HostKeyChangedError struct {
	Host  string
	OldFP string
	NewFP string
}

func (e *HostKeyChangedError) Error() string {
	return "ssh host key changed for " + e.Host + " (" + e.OldFP + " -> " + e.NewFP + "); re-onboard required"
}

// RemoveHostKey elimina la entrada known_hosts de un host (confirmación de
// re-onboard #603). Quita las líneas previas de ese host (la lista de hosts
// de una línea puede ser separada por comas); el siguiente sondeo la vuelve a
// registrar con TOFU.
func (p *SSHPool) RemoveHostKey(hostname string) error {
	norm := knownhosts.Normalize(hostname)
	var keep []string
	if data, err := os.ReadFile(p.khPath); err == nil {
		for _, ln := range strings.Split(string(data), "\n") {
			if strings.TrimSpace(ln) == "" {
				continue
			}
			fields := strings.Fields(ln)
			if len(fields) == 0 {
				continue
			}
			drop := false
			for _, h := range strings.Split(fields[0], ",") {
				if knownhosts.Normalize(h) == norm {
					drop = true
					break
				}
			}
			if !drop {
				keep = append(keep, ln)
			}
		}
	}
	return os.WriteFile(p.khPath, []byte(strings.Join(keep, "\n")+"\n"), 0o600)
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
		// Timeout: cerrar solo la sesión. NO dropear el cliente entero: el
		// pool permite sesiones concurrentes sobre el mismo router y un
		// comando lento no debe cortar las demás (issue #205).
		_ = session.Close()
		return "", fmt.Errorf("ssh %s: timeout (%s)", host, timeout)
	}
}

// RunCtx es Run con cancelación por contexto.
func (p *SSHPool) RunCtx(ctx context.Context, host, cmd string, timeout time.Duration) (string, error) {
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
		return "", fmt.Errorf("ssh %s: timeout (%s)", host, timeout)
	case <-ctx.Done():
		// Cancelación del cliente: cerrar la sesión para abortar el comando
		// remoto y liberar la goroutine (issue #204). Sin drop: es una
		// cancelación del llamador, no un fallo de la conexión.
		_ = session.Close()
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
