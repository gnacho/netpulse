// Package updater — actualizador (paridad src/updater.js, SPEC §9): chequea
// el último commit de main en la API de GitHub al arrancar y cada 6 h, y
// aplica la actualización lanzando deploy/update.sh (copiado antes a /tmp:
// el propio update hace git reset --hard y reescribiría el script). El
// binario Go se actualiza por el mismo mecanismo de script externo.
package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	checkInterval = 6 * time.Hour
	httpTimeout   = 8 * time.Second
	logTail       = 4000
	statusLogTail = 800
)

// APIBase es la base de la API de GitHub (inyectable en tests).
var APIBase = "https://api.github.com"

type progress struct {
	Step string `json:"step"`
}

// Status es la respuesta de /api/update/status (shape literal de updater.js:
// updating es false | {step}; lastLog son los últimos 800 chars).
type Status struct {
	Current         string  `json:"current"`
	Latest          *string `json:"latest"`
	LatestMsg       *string `json:"latestMsg"`
	UpdateAvailable bool    `json:"updateAvailable"`
	LastCheck       *int64  `json:"lastCheck"`
	Updating        any     `json:"updating"` // false | {"step": ...}
	Error           *string `json:"error"`
	LastLog         *string `json:"lastLog"`
	Repo            string  `json:"repo"`
	HasToken        bool    `json:"hasToken"`
}

// Updater mantiene el estado y los timers (como createUpdater).
type Updater struct {
	repoRoot string
	repo     string
	token    string

	mu           sync.Mutex
	current      string
	latest       *string
	latestMsg    *string
	updateAvail  bool
	lastCheck    *int64
	updatingStep *string // nil = no actualizando
	updatingLog  string
	lastLog      *string
	err          *string

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// New crea el updater (repoRoot = padre de serverRoot, como index.js).
func New(repoRoot, repo, token string) *Updater {
	return &Updater{
		repoRoot: repoRoot,
		repo:     repo,
		token:    token,
		current:  "desconocido",
		stopCh:   make(chan struct{}),
	}
}

func gitShort(repoRoot string) string {
	out, err := exec.Command("git", "-C", repoRoot, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// fetchLatest consulta el último commit de main (errores → {error: ...}).
func (u *Updater) fetchLatest(ctx context.Context) (sha, msg string, errCode string) {
	req, err := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/repos/%s/commits/main", APIBase, u.repo), nil)
	if err != nil {
		return "", "", "network"
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "netpulse-updater")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if u.token != "" {
		req.Header.Set("Authorization", "Bearer "+u.token)
	}
	client := &http.Client{Timeout: httpTimeout}
	res, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", "", "timeout"
		}
		return "", "", "network"
	}
	defer res.Body.Close()
	if res.StatusCode == 401 || res.StatusCode == 403 || res.StatusCode == 404 {
		if u.token != "" {
			return "", "", fmt.Sprintf("github_%d", res.StatusCode)
		}
		return "", "", "no_token"
	}
	if res.StatusCode != 200 {
		return "", "", fmt.Sprintf("github_%d", res.StatusCode)
	}
	var data struct {
		SHA    string `json:"sha"`
		Commit struct {
			Message string `json:"message"`
		} `json:"commit"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&data); err != nil {
		return "", "", "network"
	}
	sha = data.SHA
	if len(sha) > 7 {
		sha = sha[:7]
	}
	msg = strings.Split(data.Commit.Message, "\n")[0]
	// JS slice(0, 80) corta por UNIDADES UTF-16 (un astral = 2 unidades),
	// no por bytes: replicar ese cómputo (sin dejar surrogates partidos).
	units, cut := 0, len(msg)
	for i, r := range msg {
		w := 1
		if r > 0xFFFF {
			w = 2
		}
		if units+w > 80 {
			cut = i
			break
		}
		units += w
	}
	msg = msg[:cut]
	return sha, msg, ""
}

// Check fuerza un chequeo contra GitHub y devuelve el estado.
func (u *Updater) Check(ctx context.Context) Status {
	current := gitShort(u.repoRoot)
	if current == "" {
		current = "desconocido"
	}
	sha, msg, errCode := u.fetchLatest(ctx)
	now := time.Now().UnixMilli()

	u.mu.Lock()
	defer u.mu.Unlock()
	u.current = current
	u.lastCheck = &now
	if errCode != "" {
		u.err = &errCode // no_token no es un error visible: el banner no aparece
		return u.statusLocked()
	}
	u.err = nil
	u.latest = &sha
	u.latestMsg = &msg
	u.updateAvail = sha != "" && current != "desconocido" && sha != current
	if u.updateAvail {
		fmt.Printf("[netpulse] nueva versión disponible: %s → %s (%s)\n", current, sha, msg)
	}
	return u.statusLocked()
}

var stepRe = regexp.MustCompile(`STEP:(\w+)`)

// Apply lanza update.sh en segundo plano. Devuelve false si ya hay una
// actualización en curso (→ 409 already_updating) o si no se pudo copiar.
func (u *Updater) Apply() bool {
	u.mu.Lock()
	if u.updatingStep != nil {
		u.mu.Unlock()
		return false
	}
	startStep := "start"
	u.updatingStep = &startStep
	u.updatingLog = ""
	u.mu.Unlock()

	tmpScript := filepath.Join(os.TempDir(), fmt.Sprintf("netpulse-update-%d.sh", time.Now().UnixMilli()))
	src, err := os.ReadFile(filepath.Join(u.repoRoot, "deploy", "update.sh"))
	if err == nil {
		err = os.WriteFile(tmpScript, src, 0o755)
	}
	if err != nil {
		u.mu.Lock()
		u.updatingStep = nil
		code := "update_copy_failed"
		u.err = &code
		u.mu.Unlock()
		fmt.Printf("[netpulse] no se pudo copiar update.sh: %v\n", err)
		return false
	}

	cmd := exec.Command("bash", tmpScript)
	cmd.Dir = u.repoRoot
	cmd.Env = append(os.Environ(), "NODE_OPTIONS=--max-old-space-size=400")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		u.mu.Lock()
		u.updatingStep = nil
		u.mu.Unlock()
		return false
	}
	cmd.Stderr = &tailWriter{u: u}
	if err := cmd.Start(); err != nil {
		u.mu.Lock()
		u.updatingStep = nil
		u.mu.Unlock()
		return false
	}
	u.wg.Add(1)
	go func() {
		defer u.wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				u.appendLog(string(buf[:n]), true)
			}
			if err != nil {
				break
			}
		}
		waitErr := cmd.Wait()
		u.mu.Lock()
		defer u.mu.Unlock()
		if waitErr == nil {
			step := "done"
			u.updatingStep = &step
			log := u.updatingLog
			u.lastLog = &log
			u.updateAvail = false
		} else {
			if ee, ok := waitErr.(*exec.ExitError); ok {
				log := u.updatingLog
				u.lastLog = &log
				u.updatingStep = nil
				code := fmt.Sprintf("update_exit_%d", ee.ExitCode())
				u.err = &code
			} else {
				u.updatingStep = nil
				code := "update_exit_-1"
				u.err = &code
			}
		}
	}()
	return true
}

// appendLog acumula el log (tail 4000) y extrae STEP:xxx del stdout.
func (u *Updater) appendLog(text string, parseStep bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.updatingLog = tail(u.updatingLog+text, logTail)
	if parseStep {
		if m := stepRe.FindStringSubmatch(text); m != nil {
			step := m[1]
			u.updatingStep = &step
		}
		fmt.Printf("[netpulse:update] %s\n", strings.TrimSpace(text))
	}
}

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// tailWriter captura stderr al log.
type tailWriter struct{ u *Updater }

func (w *tailWriter) Write(p []byte) (int, error) {
	w.u.appendLog(string(p), false)
	return len(p), nil
}

// Status devuelve el estado actual (shape de la API).
func (u *Updater) Status() Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.statusLocked()
}

func (u *Updater) statusLocked() Status {
	var updating any = false
	if u.updatingStep != nil {
		updating = progress{Step: *u.updatingStep}
	}
	var lastLog *string
	if u.updatingStep != nil {
		// JS: state.updating?.log?.slice(-800) ?? state.lastLog ?? null
		l := tail(u.updatingLog, statusLogTail)
		lastLog = &l
	} else {
		lastLog = u.lastLog
	}
	return Status{
		Current:         u.current,
		Latest:          u.latest,
		LatestMsg:       u.latestMsg,
		UpdateAvailable: u.updateAvail,
		LastCheck:       u.lastCheck,
		Updating:        updating,
		Error:           u.err,
		LastLog:         lastLog,
		Repo:            u.repo,
		HasToken:        u.token != "",
	}
}

// Start lanza el chequeo inicial y el timer de 6 h (paridad start()).
func (u *Updater) Start() {
	u.wg.Add(1)
	go func() {
		defer u.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), httpTimeout+2*time.Second)
		u.Check(ctx)
		cancel()
		t := time.NewTicker(checkInterval)
		defer t.Stop()
		for {
			select {
			case <-u.stopCh:
				return
			case <-t.C:
				ctx, cancel := context.WithTimeout(context.Background(), httpTimeout+2*time.Second)
				u.Check(ctx)
				cancel()
			}
		}
	}()
}

// Stop para el timer y espera a los procesos en curso (paridad stop()).
func (u *Updater) Stop() {
	select {
	case <-u.stopCh:
	default:
		close(u.stopCh)
	}
	u.wg.Wait()
}
