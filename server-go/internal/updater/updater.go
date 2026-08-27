// Package updater — actualizador (paridad src/updater.js, SPEC §9): chequea
// el último commit de main en la API de GitHub al arrancar y cada 24 h, y
// aplica la actualización lanzando deploy/update.sh (copiado antes a /tmp:
// el propio update hace git reset --hard y reescribiría el script). El
// binario Go se actualiza por el mismo mecanismo de script externo.
package updater

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	checkInterval = 24 * time.Hour
	httpTimeout   = 8 * time.Second
	logTail       = 4000
	statusLogTail = 800
)

// APIBase es la base de la API de GitHub (inyectable en tests).
var APIBase = "https://api.github.com"

// BuildCommit se fija en compilación con -ldflags "-X ...". Si está definido,
// el updater lo usa en vez de ejecutar git rev-parse (permite builds fuera de un
// repo git, p.ej. despliegues manuales sin working tree).
var BuildCommit string

type progress struct {
	Step     string `json:"step"`
	Progress int    `json:"progress"` // 0-100
}

// stepWeight: porcentaje mostrado MIENTRAS el paso está en ejecución
// (issue #280). Pasos del update.sh actual + los legados (binary,
// server-deps, frontend-build) para compatibilidad con scripts viejos.
var stepWeight = map[string]int{
	"start":          3,
	"fetch":          12,
	"binary":         40, // legado: download+install juntos
	"download":       40,
	"verify":         68,
	"install":        84,
	"restart":        94,
	"done":           100,
	"server-deps":    30, // legado flujo Node
	"frontend-build": 60, // legado flujo Node
}

const stepDefaultWeight = 50

// Status es la respuesta de /api/update/status (extiende el shape histórico
// updater.js): updating es false | {step, progress 0-100}; lastLog son los
// últimos 800 chars.
type Status struct {
	Current         string  `json:"current"`
	Latest          *string `json:"latest"`
	LatestMsg       *string `json:"latestMsg"`
	UpdateAvailable bool    `json:"updateAvailable"`
	CanApply        bool    `json:"canApply"`
	Mode            string  `json:"mode"`
	LastCheck       *int64  `json:"lastCheck"`
	Updating        any     `json:"updating"` // false | {"step": ...}
	Error           *string `json:"error"`
	LastLog         *string `json:"lastLog"`
	Repo            string  `json:"repo"`
	HasToken        bool    `json:"hasToken"`
	// Readiness: pre-flight checks del apply (issue #160). Null en layout
	// estable (sin auto-apply) o hasta el primer Status().
	Readiness *Readiness `json:"readiness,omitempty"`
	// PendingApply: confirmación de que el último update aplicó y el servicio
	// arrancó con el commit nuevo (issue #161). Null sin confirmación.
	PendingApply *PendingApply `json:"pendingApply,omitempty"`
}

// Updater mantiene el estado y los timers (como createUpdater).
type Updater struct {
	repoRoot string
	repo     string
	token    string
	version  string // semver embebido (httpapi.Version) para comparar en estable
	canApply bool   // true si existe deploy/update.sh (layout git)
	mode     string // "rolling" (git layout) | "stable" (install.sh)
	// db: SQLite para historial (#159) y marcador pendingApply (#161). nil →
	// persistencia deshabilitada (tests sin BD).
	db *sql.DB

	mu           sync.Mutex
	current      string
	latest       *string
	latestMsg    *string
	updateAvail  bool
	lastCheck    *int64
	updatingStep *string // nil = no actualizando
	updatingPct  int     // 0-100 (issue #280); peso del paso o PROGRESS explícito
	updatingLog  string
	lastLog      *string
	err          *string

	// Suscriptores del stream /api/update/stream (issue #280). Notificados
	// en cada cambio de paso/porcentaje o fin de actualización.
	subs map[chan Status]struct{}

	// readiness cacheado (issue #160): el network check toca red y no debe
	// bloquear el polling de status.
	readiness   *Readiness
	readinessAt time.Time

	// pendingApply en memoria (issue #161): confirmación post-update.
	pendingApply *PendingApply

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// New crea el updater. version es el semver embebido (httpapi.Version) usado
// para comparar contra el último release tag en modo estable. La detección de
// layout es automática: si existe deploy/update.sh junto a repoRoot, el modo
// es "rolling" (compara contra main HEAD y puede auto-aplicar); si no, es
// "stable" (compara contra release tags y NO puede auto-aplicar — el usuario
// debe re-ejecutar install.sh). db puede ser nil (persistencia deshabilitada).
func New(repoRoot, repo, token, version string, db *sql.DB) *Updater {
	canApply := fileExists(filepath.Join(repoRoot, "deploy", "update.sh"))
	mode := "stable"
	if canApply {
		mode = "rolling"
	}
	u := &Updater{
		repoRoot: repoRoot,
		repo:     repo,
		token:    token,
		version:  version,
		canApply: canApply,
		mode:     mode,
		db:       db,
		current:  "desconocido",
		subs:     make(map[chan Status]struct{}),
		stopCh:   make(chan struct{}),
	}
	// Issue #161: procesar el marcador pendiente (confirmación post-update)
	// y finalizar historial interrumpido en cada arranque.
	u.loadPendingApply()
	return u
}

// CanApply indica si el updater puede auto-aplicar en este layout.
func (u *Updater) CanApply() bool { return u.canApply }

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func gitShort(repoRoot string) string {
	if BuildCommit != "" {
		return BuildCommit
	}
	out, err := exec.Command("git", "-C", repoRoot, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// fetchLatestCommit consulta el último commit de main (modo rolling).
// Errores → {error: ...}.
func (u *Updater) fetchLatestCommit(ctx context.Context) (sha, msg string, errCode string) {
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

// fetchLatestRelease consulta el último release tag (modo stable). Devuelve
// tag_name (ej. "v2.7.3") y el nombre del release. Errores → {error: ...}.
func (u *Updater) fetchLatestRelease(ctx context.Context) (tag, name string, errCode string) {
	req, err := http.NewRequestWithContext(ctx, "GET",
		fmt.Sprintf("%s/repos/%s/releases/latest", APIBase, u.repo), nil)
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
		TagName string `json:"tag_name"`
		Name    string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&data); err != nil {
		return "", "", "network"
	}
	tag = data.TagName
	name = data.Name
	if name == "" {
		name = tag
	}
	return tag, name, ""
}

// compareSemver compara dos strings semver (con o sin prefijo "v").
// Devuelve >0 si a > b, 0 si iguales, <0 si a < b. No-parseable → 0.
func compareSemver(a, b string) int {
	av := parseSemver(a)
	bv := parseSemver(b)
	for i := 0; i < 3; i++ {
		if av[i] != bv[i] {
			return av[i] - bv[i]
		}
	}
	return 0
}

func parseSemver(s string) [3]int {
	s = strings.TrimPrefix(s, "v")
	parts := strings.SplitN(s, ".", 3)
	var out [3]int
	for i := 0; i < len(parts) && i < 3; i++ {
		p := parts[i]
		// descartar sufijos pre-release/build (-rc1, +build, etc.)
		if idx := strings.IndexAny(p, "-+"); idx >= 0 {
			p = p[:idx]
		}
		n, _ := strconv.Atoi(p)
		out[i] = n
	}
	return out
}

// Check fuerza un chequeo contra GitHub y devuelve el estado. En modo
// rolling compara contra el último commit de main; en modo estable contra
// el último release tag (semver).
func (u *Updater) Check(ctx context.Context) Status {
	var current, latest, latestMsg, errCode string

	if u.mode == "stable" {
		current = u.version
		if current == "" {
			current = "desconocido"
		}
		latest, latestMsg, errCode = u.fetchLatestRelease(ctx)
	} else {
		current = gitShort(u.repoRoot)
		if current == "" {
			current = "desconocido"
		}
		latest, latestMsg, errCode = u.fetchLatestCommit(ctx)
	}

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
	u.latest = &latest
	u.latestMsg = &latestMsg
	if u.mode == "stable" {
		u.updateAvail = latest != "" && current != "desconocido" && compareSemver(latest, current) > 0
	} else {
		u.updateAvail = latest != "" && current != "desconocido" && latest != current
	}
	if u.updateAvail {
		fmt.Printf("[netpulse] nueva versión disponible: %s → %s (%s)\n", current, latest, latestMsg)
	}
	return u.statusLocked()
}

var (
	stepRe     = regexp.MustCompile(`STEP:(\w+)`)
	progressRe = regexp.MustCompile(`PROGRESS:(\d{1,3})`)
)

// setStepLocked fija el paso activo y su porcentaje base (issue #280).
func (u *Updater) setStepLocked(step string) {
	u.updatingStep = &step
	if w, ok := stepWeight[step]; ok {
		u.updatingPct = w
	} else {
		u.updatingPct = stepDefaultWeight
	}
}

// Subscribe registra un canal para recibir el Status en cada cambio del
// update en curso (issue #280). El cancel devuelto lo retira.
func (u *Updater) Subscribe() (<-chan Status, func()) {
	ch := make(chan Status, 8)
	u.mu.Lock()
	u.subs[ch] = struct{}{}
	u.mu.Unlock()
	cancel := func() {
		u.mu.Lock()
		delete(u.subs, ch)
		u.mu.Unlock()
	}
	return ch, cancel
}

// broadcastLocked emite el estado a todos los suscriptores sin bloquear
// (un cliente lento pierde el evento; el siguiente lo corrige). Llamar con mu.
func (u *Updater) broadcastLocked() {
	st := u.statusLocked()
	for ch := range u.subs {
		select {
		case ch <- st:
		default:
		}
	}
}

// Apply lanza update.sh en segundo plano (iniciado manualmente desde la UI,
// rol admin). Devuelve false si ya hay una actualización en curso (→ 409
// already_updating) o si no se pudo copiar.
func (u *Updater) Apply() bool { return u.ApplyBy("admin") }

// ApplyBy lanza update.sh en segundo plano con el iniciador dado ("admin"
// manual o "auto" para el rolling main). Antes de lanzar registra la entrada
// 'running' del historial (#159) y persiste el marcador pendingApply (#161);
// la goroutine finaliza ambos según el resultado del script.
func (u *Updater) ApplyBy(initiatedBy string) bool {
	u.mu.Lock()
	if u.updatingStep != nil {
		u.mu.Unlock()
		return false
	}
	u.setStepLocked("start")
	u.updatingPct = 0
	u.updatingLog = ""
	u.broadcastLocked()
	u.mu.Unlock()

	// Historial + marcador: version_from es el commit actual, version_to el
	// objetivo (latest de GitHub si ya se consultó).
	from := u.current
	if from == "" || from == "desconocido" {
		from = gitShort(u.repoRoot)
	}
	u.mu.Lock()
	var to *string
	if u.latest != nil {
		c := *u.latest
		to = &c
	}
	u.mu.Unlock()
	historyID := u.recordStart(from, to, initiatedBy)
	u.persistPendingApply(from, to)
	startedAt := time.Now()

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
		u.broadcastLocked()
		u.mu.Unlock()
		u.finishHistory(historyID, "failed", code, time.Since(startedAt))
		fmt.Printf("[netpulse] no se pudo copiar update.sh: %v\n", err)
		return false
	}

	cmd := exec.Command("bash", tmpScript)
	cmd.Dir = u.repoRoot
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		u.mu.Lock()
		u.updatingStep = nil
		u.broadcastLocked()
		u.mu.Unlock()
		u.finishHistory(historyID, "failed", "update_pipe_failed", time.Since(startedAt))
		return false
	}
	cmd.Stderr = &tailWriter{u: u}
	if err := cmd.Start(); err != nil {
		u.mu.Lock()
		u.updatingStep = nil
		u.broadcastLocked()
		u.mu.Unlock()
		u.finishHistory(historyID, "failed", "update_start_failed", time.Since(startedAt))
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
		dur := time.Since(startedAt)
		u.mu.Lock()
		defer u.mu.Unlock()
		if waitErr == nil {
			u.setStepLocked("done")
			log := u.updatingLog
			u.lastLog = &log
			u.updateAvail = false
			u.finishHistory(historyID, "success", "", dur)
		} else {
			code := "update_exit_-1"
			if ee, ok := waitErr.(*exec.ExitError); ok {
				log := u.updatingLog
				u.lastLog = &log
				u.updatingStep = nil
				code = fmt.Sprintf("update_exit_%d", ee.ExitCode())
				u.err = &code
			} else {
				u.updatingStep = nil
				u.err = &code
			}
			u.finishHistory(historyID, "failed", code, dur)
			// Issue #161: el apply falló → sin confirmación post-update.
			u.clearPendingApply()
		}
		u.broadcastLocked()
	}()
	return true
}

// appendLog acumula el log (tail 4000) y extrae STEP:xxx y PROGRESS:nn del
// stdout. Solo hace broadcast cuando el paso o el porcentaje CAMBIAN (no en
// cada línea de log) para no saturar el stream (issue #280).
func (u *Updater) appendLog(text string, parseStep bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.updatingLog = tail(u.updatingLog+text, logTail)
	if parseStep {
		before := ""
		if u.updatingStep != nil {
			before = *u.updatingStep
		}
		beforePct := u.updatingPct
		if m := stepRe.FindStringSubmatch(text); m != nil {
			u.setStepLocked(m[1])
		}
		if m := progressRe.FindStringSubmatch(text); m != nil && u.updatingStep != nil {
			// El progreso explícito no puede bajar del peso del paso activo
			// ni superar el 99 (done se reserva al final del script).
			n, err := strconv.Atoi(m[1])
			if err == nil {
				if n < u.updatingPct {
					n = u.updatingPct
				}
				if n > 99 {
					n = 99
				}
				u.updatingPct = n
			}
		}
		after := ""
		if u.updatingStep != nil {
			after = *u.updatingStep
		}
		if after != before || u.updatingPct != beforePct {
			u.broadcastLocked()
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

// Status devuelve el estado actual (shape de la API), incluyendo readiness
// (issue #160) y la confirmación pendingApply (issue #161).
func (u *Updater) Status() Status {
	u.mu.Lock()
	st := u.statusLocked()
	u.mu.Unlock()
	st.Readiness = u.Readiness()
	return st
}

func (u *Updater) statusLocked() Status {
	var updating any = false
	if u.updatingStep != nil {
		pct := u.updatingPct
		if *u.updatingStep == "done" {
			pct = 100
		}
		updating = progress{Step: *u.updatingStep, Progress: pct}
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
		CanApply:        u.canApply,
		Mode:            u.mode,
		LastCheck:       u.lastCheck,
		Updating:        updating,
		Error:           u.err,
		LastLog:         lastLog,
		Repo:            u.repo,
		HasToken:        u.token != "",
		PendingApply:    u.pendingApply,
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
