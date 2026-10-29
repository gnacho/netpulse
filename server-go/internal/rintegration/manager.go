package rintegration

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	// ErrNotRunning is returned by Stop when no process exists for the slug.
	ErrNotRunning = errors.New("integration process not running")
	// ErrAlreadyRunning is returned by Start when a process is already up.
	ErrAlreadyRunning = errors.New("integration process already running")
)

// Manager supervises pusher processes in memory (no systemd: the service runs
// as an unprivileged user). It owns the on-disk directory that holds each
// slug's environment file, log file and extracted binary.
type Manager struct {
	dir    string
	mu     sync.Mutex
	procs  map[string]*proc
	fetch  *Fetcher
	onExit func(slug string, err error)
}

type proc struct {
	cmd      *exec.Cmd
	done     chan struct{}
	stopping bool // guarded by Manager.mu
}

// NewManager builds a Manager rooted at dir.
func NewManager(dir string) *Manager {
	return &Manager{dir: dir, procs: map[string]*proc{}, fetch: NewFetcher()}
}

// Fetch returns the shared release fetcher.
func (m *Manager) Fetch() *Fetcher { return m.fetch }

// Dir returns the on-disk root for environments, logs and binaries.
func (m *Manager) Dir() string { return m.dir }

// Env returns the environment file of a slug as a map. The password and the
// token live only in that file, so editing an integration reads it first and
// rewrites it with the changed keys instead of rebuilding it from scratch.
func (m *Manager) Env(slug string) (map[string]string, error) {
	return readEnvFile(filepath.Join(m.dir, slug+".env"))
}

// WriteEnv replaces the environment file of a slug, keeping mode 0600.
func (m *Manager) WriteEnv(slug string, env map[string]string) error {
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return err
	}
	return writeEnvFile(filepath.Join(m.dir, slug+".env"), env)
}

// SetOnExit registers the callback invoked when a process exits unexpectedly
// (a crash). Intentional stops do not fire it: the caller owns that state.
func (m *Manager) SetOnExit(fn func(slug string, err error)) {
	m.mu.Lock()
	m.onExit = fn
	m.mu.Unlock()
}

// Start launches the pusher binary for slug. When env is non-empty it is
// written to <dir>/<slug>.env (0600); otherwise the existing env file is
// reused, which is how restarts recover the password/token that live only
// there. The process gets the vars in its environment, runs in its own
// process group, and logs stdout/stderr to <dir>/<slug>.log.
func (m *Manager) Start(slug string, env map[string]string, binPath string) error {
	if m.Running(slug) {
		return ErrAlreadyRunning
	}
	if err := os.MkdirAll(m.dir, 0o755); err != nil {
		return err
	}

	envPath := filepath.Join(m.dir, slug+".env")
	if len(env) > 0 {
		if err := writeEnvFile(envPath, env); err != nil {
			return err
		}
	}
	processEnv, err := readEnvFile(envPath)
	if err != nil {
		return fmt.Errorf("integration %s: %w", slug, err)
	}

	logPath := filepath.Join(m.dir, slug+".log")
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}

	cmd := exec.Command(binPath)
	cmd.Dir = m.dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = logf
	cmd.Stderr = logf
	cmd.Env = append(os.Environ(), envMapToList(processEnv)...)
	if err := cmd.Start(); err != nil {
		logf.Close()
		return err
	}

	p := &proc{cmd: cmd, done: make(chan struct{})}
	m.mu.Lock()
	m.procs[slug] = p
	m.mu.Unlock()

	go func() {
		err := cmd.Wait()
		logf.Close()
		m.mu.Lock()
		cur := m.procs[slug]
		stopping := cur != nil && cur.stopping
		delete(m.procs, slug)
		onExit := m.onExit
		m.mu.Unlock()
		close(p.done)
		if !stopping && onExit != nil {
			onExit(slug, err)
		}
	}()

	return nil
}

// Stop sends SIGTERM to the process group, waits up to 10s, then SIGKILL.
func (m *Manager) Stop(slug string) error {
	m.mu.Lock()
	p, ok := m.procs[slug]
	if !ok {
		m.mu.Unlock()
		return ErrNotRunning
	}
	p.stopping = true
	m.mu.Unlock()

	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
	select {
	case <-p.done:
		return nil
	case <-time.After(10 * time.Second):
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
		<-p.done
		return nil
	}
}

// Running reports whether a process is currently tracked for the slug.
func (m *Manager) Running(slug string) bool {
	m.mu.Lock()
	_, ok := m.procs[slug]
	m.mu.Unlock()
	return ok
}

// StopAll stops every tracked process (used on graceful shutdown).
func (m *Manager) StopAll() {
	m.mu.Lock()
	slugs := make([]string, 0, len(m.procs))
	for s := range m.procs {
		slugs = append(slugs, s)
	}
	m.mu.Unlock()
	sort.Strings(slugs)
	for _, s := range slugs {
		_ = m.Stop(s)
	}
}

// Cleanup removes the env file, log file and extracted directory for a slug.
// Errors are collected and returned; it is safe to call when nothing exists.
func (m *Manager) Cleanup(slug string) error {
	var errs []string
	for _, p := range []string{
		filepath.Join(m.dir, slug+".env"),
		filepath.Join(m.dir, slug+".log"),
	} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err.Error())
		}
	}
	if err := os.RemoveAll(filepath.Join(m.dir, slug)); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// EscapeEnvValue escapes a value so it stays on one line of a KEY=value env
// file and can be read back losslessly by UnescapeEnvValue.
func EscapeEnvValue(v string) string {
	var b strings.Builder
	for _, r := range v {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// UnescapeEnvValue reverses EscapeEnvValue.
func UnescapeEnvValue(v string) string {
	runes := []rune(v)
	var b strings.Builder
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\\' && i+1 < len(runes) {
			switch runes[i+1] {
			case '\\':
				b.WriteRune('\\')
			case 'n':
				b.WriteRune('\n')
			case 'r':
				b.WriteRune('\r')
			case 't':
				b.WriteRune('\t')
			default:
				b.WriteRune('\\')
				b.WriteRune(runes[i+1])
			}
			i++
			continue
		}
		b.WriteRune(runes[i])
	}
	return b.String()
}

func envMapToList(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(env))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out
}

func writeEnvFile(path string, env map[string]string) error {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(EscapeEnvValue(env[k]))
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

func readEnvFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseEnv(string(data)), nil
}

// ParseEnv parses a KEY=value env file body into a map, unescaping the values
// written by EscapeEnvValue. It is also used on the output of a remote read.
func ParseEnv(body string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		i := strings.IndexByte(line, '=')
		if i < 0 {
			continue
		}
		out[line[:i]] = UnescapeEnvValue(line[i+1:])
	}
	return out
}
