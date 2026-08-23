// agent.go — Fase 3 piloto (SPEC-AGENTE-PILOTO §1):
//
//	POST /api/ingest/agent  — ingesta de agentes nativos. NO va detrás del
//	                          middleware de sesión: auth propia Bearer con
//	                          token por equipo (guardado sha256 en kv
//	                          agent.token.<slug>). Rate limit por IP 30/min,
//	                          body cap 2 MB.
//	POST   /api/agents      — crear token para un slug (sesión). El token se
//	                          muestra UNA sola vez, con el one-liner de
//	                          instalación. Nunca se vuelve a servir.
//	GET    /api/agents      — slugs con agente + last_seen + versión.
//	DELETE /api/agents/{slug} — revocar (borra el token; el agente recibe 401).
package httpapi

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/agentbin"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/rearmer"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
)

const (
	// ingestBodyCap: body máximo del push del agente (2 MB, SPEC §1).
	ingestBodyCap = 2 << 20
	// ingestRateLimit: pushes por IP y ventana (30/min, SPEC §1).
	ingestRateLimit  = 30
	ingestRateWindow = time.Minute
	// agentTokenKeyPrefix: kv agent.token.<slug> = sha256 hex del token.
	agentTokenKeyPrefix = "agent.token."
	// maxTsDrift: ventana de frescura del `ts` del agente (anti-replay,
	// auditoría de seguridad #2). Payloads con ts fuera de |±maxTsDrift|
	// respecto al reloj del servidor se rechazan con 401 (evita reinyectar
	// pushes viejos capturados). Configurable vía AGENT_MAX_TS_DRIFT_S.
	maxTsDriftDefault = 5 * time.Minute
)

// agentSlugRe: slugs de equipo válidos (mismo alfabeto que routerstore).
var agentSlugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// SSHRunner ejecuta comandos en los routers (adapters.SSHPool en
// producción; fake en tests). Fase 5: rearme de agentes.
type SSHRunner interface {
	Run(host, cmd string, timeout time.Duration) (string, error)
}

// ---------------------------------------------------------------------------
// Rate limit por IP (ventana fija de 1 min; purga oportunista)
// ---------------------------------------------------------------------------

type ipRateLimit struct {
	mu     sync.Mutex
	hits   map[string]*ipHit
	limit  int
	window time.Duration
	now    func() time.Time
}

type ipHit struct {
	count int
	reset time.Time
}

func newIPRateLimit(limit int, window time.Duration) *ipRateLimit {
	return &ipRateLimit{hits: map[string]*ipHit{}, limit: limit, window: window, now: time.Now}
}

// allow registra un intento de ip; false → retryAfterSec hasta el reset.
func (l *ipRateLimit) allow(ip string) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Purga oportunista para no crecer sin límite en redes con muchos clientes
	if len(l.hits) > 1024 {
		now := l.now()
		for k, h := range l.hits {
			if now.After(h.reset) {
				delete(l.hits, k)
			}
		}
	}
	h, ok := l.hits[ip]
	if !ok || l.now().After(h.reset) {
		l.hits[ip] = &ipHit{count: 1, reset: l.now().Add(l.window)}
		return true, 0
	}
	if h.count >= l.limit {
		return false, int(time.Until(h.reset).Seconds()) + 1
	}
	h.count++
	return true, 0
}

// ---------------------------------------------------------------------------
// Tokens de agente (kv, sha256 — el token en claro solo vive en la respuesta
// de creación y en el env del router)
// ---------------------------------------------------------------------------

func agentTokenKey(slug string) string { return agentTokenKeyPrefix + slug }

// hashAgentToken: sha256 hex del token (lo único que se persiste).
func hashAgentToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// newAgentToken genera un token aleatorio de 32 bytes en hex (64 chars).
func newAgentToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// checkAgentToken: ¿el Bearer es válido para el slug? (comparación en
// tiempo constante sobre los hashes).
func (s *server) checkAgentToken(slug, token string) bool {
	if token == "" || s.db == nil {
		return false
	}
	var stored string
	if err := s.db.QueryRow("SELECT value FROM kv WHERE key = ?", agentTokenKey(slug)).Scan(&stored); err != nil {
		return false
	}
	got := hashAgentToken(token)
	return subtle.ConstantTimeCompare([]byte(got), []byte(stored)) == 1
}

// checkHMAC verifica que X-Agent-Signature coincida con HMAC-SHA256(token, body).
// Devuelve nil si la firma es correcta; error si falta o no coincide.
func checkHMAC(token string, body []byte, sig string) error {
	if sig == "" {
		return errors.New("falta X-Agent-Signature")
	}
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	if subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) != 1 {
		return errors.New("HMAC no coincide")
	}
	return nil
}

// ---------------------------------------------------------------------------
// POST /api/ingest/agent (auth Bearer propia; exenta del middleware de sesión)
// ---------------------------------------------------------------------------

func (s *server) handleIngestAgent(w http.ResponseWriter, r *http.Request) {
	// 429: ráfaga por IP (antes de leer el body, como el login)
	if ok, retry := s.ingestLimit.allow(auth.ClientIP(r)); !ok {
		w.Header().Set("Retry-After", fmt.Sprintf("%d", retry))
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error":         "rate_limited",
			"retryAfterSec": retry,
		})
		return
	}
	// 413: body cap 2 MB
	body, err := io.ReadAll(io.LimitReader(r.Body, ingestBodyCap+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload")
		return
	}
	if len(body) > ingestBodyCap {
		writeError(w, http.StatusRequestEntityTooLarge, "payload_too_large")
		return
	}
	// 400: JSON + campos mínimos del contrato
	var p probe.Payload
	if err := json.Unmarshal(body, &p); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", "JSON inválido")
		return
	}
	if !agentSlugRe.MatchString(p.Router) || p.Ts <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_payload",
			`Se esperaba { "router": "<slug>", "ts": <unix>, "data": {...} }`)
		return
	}
	// 401: Bearer inválido para ese slug
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !s.checkAgentToken(p.Router, token) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	// 401: HMAC-SHA256 del payload no coincide (R4: firma obligatoria)
	sig := r.Header.Get("X-Agent-Signature")
	if err := checkHMAC(token, body, sig); err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_signature", err.Error())
		return
	}
	// 401: anti-replay (auditoría #2) — el `ts` debe estar dentro de la
	// ventana de frescura respecto al reloj del servidor. Rechaza pushes
	// viejos capturados/reinyectados y saltos de reloj grandes del agente.
	if drift := time.Since(time.Unix(p.Ts, 0)); drift < -s.maxTsDrift || drift > s.maxTsDrift {
		writeError(w, http.StatusUnauthorized, "stale_payload",
			"ts fuera de la ventana de frescura (revisa el reloj del router)")
		return
	}
	if s.agents == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable",
			"el servidor no tiene registry de agentes")
		return
	}
	// En modo demo los scrapers/colectores externos siguen empujando (issue
	// #168): se acepta como no-op (202) sin tocar el registry ni la BD. La
	// demo sirve un dataset canónico en memoria; los datos reales no existen.
	if s.cfg != nil && s.cfg.DemoMode {
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "demo": true})
		return
	}
	s.agents.Ingest(&p)
	// Fase 8.2 (R8): persistir el último push en kv (agent.state.<slug>) para
	// que lastSeen/versión/payload sobrevivan a un reinicio del servidor.
	s.persistAgentState(p.Router, s.agents.Snapshot(p.Router))
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

// ---------------------------------------------------------------------------
// GET /api/agents/{slug}/binary?arch=... — Fase 6.2: servir el binario del
// agente desde el propio servidor (embebido vía go:embed en agentbin/).
// Elimina la dependencia de GitHub para instalar/reinstalar agentes.
// Auth por token de agente (Bearer), igual que la ingesta — el one-liner de
// instalación incluye el token y se ejecuta en el router.
// ---------------------------------------------------------------------------

func (s *server) handleAgentBinary(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !agentSlugRe.MatchString(slug) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	// Auth por token de agente (no sesión admin)
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !s.checkAgentToken(slug, token) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	arch := r.URL.Query().Get("arch")
	if arch == "" {
		arch = "arm64"
	}
	f, err := agentbin.Open(arch)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found",
			"binario no disponible para "+arch+" (compila el servidor con los binarios de agente embebidos)")
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="netpulse-agent-%s"`, arch))
	http.ServeContent(w, r, st.Name(), st.ModTime(), rs)
}

// ---------------------------------------------------------------------------
// Gestión de tokens (sesión): POST/GET /api/agents, DELETE /api/agents/{slug}
// ---------------------------------------------------------------------------

// agentCreateResponse es la ÚNICA respuesta que incluye el token en claro.
type agentCreateResponse struct {
	Slug    string `json:"slug"`
	Token   string `json:"token"`
	Install string `json:"install"`
}

// handleAgentsCreate: genera (o rota) el token de un slug. 201 con el token
// en claro UNA vez + el one-liner de instalación.
func (s *server) handleAgentsCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Slug string `json:"slug"`
	}
	if !readJSONBody(r, &body) || !agentSlugRe.MatchString(body.Slug) {
		writeError(w, http.StatusBadRequest, "invalid_body",
			`Se esperaba { "slug": "<equipo>" } (a-z, 0-9, guiones)`)
		return
	}
	token, err := newAgentToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token_error")
		return
	}
	if _, err := s.db.Exec(
		"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value",
		agentTokenKey(body.Slug), hashAgentToken(token)); err != nil {
		writeError(w, http.StatusInternalServerError, "token_error")
		return
	}
	writeJSON(w, http.StatusCreated, agentCreateResponse{
		Slug:    body.Slug,
		Token:   token,
		Install: s.agentInstallLine(r, body.Slug, token),
	})
}

// agentInstallLine: one-liner de instalación vía SSH desde esta máquina.
// El host del router se resuelve de la tabla routers si el slug existe.
// Fase 6.2: el binario se descarga del propio servidor con el token del agente
// en vez de GitHub, eliminando la dependencia de internet y el token en argv.
func (s *server) agentInstallLine(r *http.Request, slug, token string) string {
	host := "<ip-del-router>"
	if s.db != nil {
		for _, rc := range routerstore.ListRouters(s.db.DB) {
			if rc.ID == slug {
				host = rc.Host
				break
			}
		}
	}
	scheme := "http"
	if auth.IsSecureRequest(r) {
		scheme = "https"
	}
	server := scheme + "://" + r.Host
	return fmt.Sprintf(
		"curl -fsSL -H 'Authorization: Bearer %s' %s/api/agents/%s/binary -o /tmp/netpulse-agent && curl -fsSL https://raw.githubusercontent.com/gnacho/netpulse/main/install-agent.sh | sh -s -- --binary /tmp/netpulse-agent --host %s --server %s --slug %s --token %s",
		token, server, slug, host, server, slug, token)
}

// agentListItem: lo que ve la UI — NUNCA el token ni su hash.
type agentListItem struct {
	Slug     string `json:"slug"`
	LastSeen *int64 `json:"lastSeen"` // unix SEGUNDOS; null si nunca empujó
	Version  string `json:"version,omitempty"`
	Fresh    bool   `json:"fresh"`
	// UpdateAvailable: true si el agente reportó una versión distinta de la
	// del binario embebido (agentbin.EmbeddedAgentVersion) → hay upgrade
	// disponible vía POST /api/agents/{slug}/upgrade (Fase 6.3, issue #243).
	// SOLO se marca para routers OpenWrt (type openwrt/glinet): los de tipo
	// managed-switch/external son scrapers que no usan el agente nativo.
	UpdateAvailable bool `json:"updateAvailable"`
}

// agentUpgradeable: ¿un agente de un router con este type es actualizable?
// Un router openwrt/glinet (o sin type) usa el binario netpulse-agent y es
// actualizable; los de type managed-switch/external NO (son scrapers que no
// usan el agente nativo y no entienden el comando "upgrade").
func agentUpgradeable(typ string) bool {
	return typ == "" || typ == "openwrt" || typ == "glinet"
}

// routerUpgradeable: variante por slug que consulta la tabla routers. NO usar
// dentro de un bucle con rows abiertos (SetMaxOpenConns(1) → deadlock);
// para listados precarga los types (ver handleAgentsList).
func (s *server) routerUpgradeable(slug string) bool {
	if s.db == nil {
		return true
	}
	var typ string
	if err := s.db.QueryRow("SELECT type FROM routers WHERE id = ?", slug).Scan(&typ); err != nil {
		return true
	}
	return agentUpgradeable(typ)
}

// handleAgentsList: slugs con token + last_seen + versión.
func (s *server) handleAgentsList(w http.ResponseWriter, _ *http.Request) {
	out := []agentListItem{}
	// Types de routers por slug, precargados en UNA consulta: con
	// SetMaxOpenConns(1) un QueryRow dentro del bucle de rows de abajo
	// esperaría la conexión que el rows activo mantiene (deadlock).
	routerTypes := map[string]string{}
	if s.db != nil {
		if rows, err := s.db.Query("SELECT id, type FROM routers"); err == nil {
			for rows.Next() {
				var id, typ string
				if rows.Scan(&id, &typ) == nil {
					routerTypes[id] = typ
				}
			}
			rows.Close()
		}
	}
	if s.db != nil {
		rows, err := s.db.Query("SELECT key FROM kv WHERE key LIKE ? ORDER BY key", agentTokenKeyPrefix+"%")
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var key string
				if rows.Scan(&key) != nil {
					continue
				}
				slug := strings.TrimPrefix(key, agentTokenKeyPrefix)
				item := agentListItem{Slug: slug}
				if s.agents != nil {
					if seen, version, ok := s.agents.Info(slug); ok {
						ts := seen.Unix()
						item.LastSeen = &ts
						item.Version = version
						// Fase 6.3 (issue #243): upgrade disponible si el agente
						// reporta una versión distinta del binario embebido y es
						// un agente nativo OpenWrt (no un scraper).
						item.UpdateAvailable = agentUpgradeable(routerTypes[slug]) && version != "" && version != agentbin.EmbeddedAgentVersion
					}
					_, item.Fresh = s.agents.Fresh(slug)
				}
				out = append(out, item)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": out})
}

// handleAgentsDelete: revoca el token del slug (el agente recibirá 401 en su
// próximo push) y olvida su estado. 404 si no existía.
func (s *server) handleAgentsDelete(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !agentSlugRe.MatchString(slug) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	res, err := s.db.Exec("DELETE FROM kv WHERE key = ?", agentTokenKey(slug))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if s.agents != nil {
		s.agents.Forget(slug)
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------------------
// POST /api/agents/{slug}/rearm — Fase 5 (Plan B): reiniciar el servicio
// procd del agente en el router vía SSH (el mismo canal del sondeo). La
// lógica completa vive en el paquete rearmer (compartida con el supervisor
// de auto-rearme); este handler solo mapea errores tipificados a status.
// ---------------------------------------------------------------------------

type rearmResponse struct {
	Slug      string `json:"slug"`
	Restarted bool   `json:"restarted"` // el comando SSH se ejecutó
	Recovered bool   `json:"recovered"` // llegó un push nuevo tras el reinicio
	Message   string `json:"message,omitempty"`
}

func (s *server) handleAgentRearm(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !agentSlugRe.MatchString(slug) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if s.rearmer == nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}

	res, err := s.rearmer.Rearm(slug)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, rearmResponse{
			Slug: res.Slug, Restarted: res.Restarted, Recovered: res.Recovered, Message: res.Message,
		})
	case errors.Is(err, rearmer.ErrNoDB):
		writeError(w, http.StatusInternalServerError, "db_error")
	case errors.Is(err, rearmer.ErrNoToken):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, rearmer.ErrNoRouter):
		writeError(w, http.StatusConflict, "router_unknown", err.Error())
	case errors.Is(err, rearmer.ErrNoSSH):
		writeError(w, http.StatusServiceUnavailable, "ssh_unavailable", err.Error())
	default:
		var cd rearmer.ErrCooldown
		if errors.As(err, &cd) {
			writeError(w, http.StatusTooManyRequests, "cooldown", cd.Error())
			return
		}
		writeError(w, http.StatusBadGateway, "ssh_failed", err.Error())
	}
}

// ---------------------------------------------------------------------------
// POST /api/agents/{slug}/reinstall — #246: reinstalar el agente en el router
// vía SSH (recupera un agente borrado por update de firmware/desinstalación).
// Replica el flujo de install-agent.sh sin depender del PC del admin:
//   1. Verifica slug + router en la tabla routers.
//   2. Rota el token (genera uno nuevo en claro y persiste su hash en kv).
//   3. Ejecuta por SSH: detecta arch, descarga el binario del propio server
//      (GET /api/agents/{slug}/binary con el token nuevo), lo instala,
//      escribe /etc/netpulse-agent.env, instala el init procd y lo arranca.
//   4. Espera a que el agente vuelva a empujar (misma lógica que el rearm).
// ---------------------------------------------------------------------------

type reinstallResponse struct {
	Slug      string `json:"slug"`
	Token     string `json:"token"` // en claro UNA vez, para reutilizar en el router
	Installed bool   `json:"installed"`
	Recovered bool   `json:"recovered"` // llegó un push nuevo tras la instalación
	Message   string `json:"message,omitempty"`
}

func (s *server) handleAgentReinstall(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !agentSlugRe.MatchString(slug) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if s.db == nil || s.pool == nil {
		writeError(w, http.StatusServiceUnavailable, "ssh_unavailable", "el servidor no tiene pool SSH")
		return
	}

	// Router del slug (tabla routers; mismo host que usa el rearm).
	host := ""
	for _, rc := range routerstore.ListRouters(s.db.DB) {
		if rc.ID == slug {
			host = rc.Host
			break
		}
	}
	if host == "" {
		writeError(w, http.StatusConflict, "router_unknown", "no hay router con ese slug en la tabla routers")
		return
	}
	// Solo agentes nativos OpenWrt: un dispositivo managed-switch/external es
	// un scraper que no usa el binario netpulse-agent → no se reinstala así.
	if !s.routerUpgradeable(slug) {
		writeError(w, http.StatusConflict, "not_openwrt",
			"este dispositivo no usa el agente nativo OpenWrt (scraper); reinstálalo con su propio mecanismo")
		return
	}

	// Rotar el token (el env del router se reescribe con el nuevo).
	token, err := newAgentToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token_error")
		return
	}
	if _, err := s.db.Exec(
		"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value",
		agentTokenKey(slug), hashAgentToken(token)); err != nil {
		writeError(w, http.StatusInternalServerError, "token_error")
		return
	}

	// URL base del server (para que el router descargue el binario). Mismo
	// esquema que agentInstallLine: http en LAN, https si la request es segura.
	scheme := "http"
	if auth.IsSecureRequest(r) {
		scheme = "https"
	}
	serverURL := scheme + "://" + r.Host

	// Script de instalación (replica install-agent.sh vía SSH, sin scp).
	script := reinstallScript(host, slug, token, serverURL)

	before := time.Now()
	var prevSeen time.Time
	if s.agents != nil {
		if seen, _, ok := s.agents.Info(slug); ok {
			prevSeen = seen
		}
	}

	if _, err := s.pool.Run(host, script, 60*time.Second); err != nil {
		writeError(w, http.StatusBadGateway, "ssh_failed", err.Error())
		return
	}

	// Esperar a que el agente vuelva a empujar (poll 2s hasta 40s).
	recovered := false
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		if s.agents == nil {
			break
		}
		if seen, _, ok := s.agents.Info(slug); ok && seen.After(prevSeen) && seen.After(before.Add(-5*time.Second)) {
			recovered = true
			break
		}
	}

	writeJSON(w, http.StatusOK, reinstallResponse{
		Slug: slug, Token: token, Installed: true, Recovered: recovered,
		Message: map[bool]string{true: "agente instalado y empujando", false: "agente instalado; aún no ha empujado en 40 s"}[recovered],
	})
}

// reinstallScript construye el script POSIX sh que se ejecuta en el router.
func reinstallScript(host, slug, token, serverURL string) string {
	return `#!/bin/sh
set -e
INIT=/etc/init.d/netpulse-agent
BIN=/usr/sbin/netpulse-agent
ENV_FILE=/etc/netpulse-agent.env
SERVER="` + serverURL + `"
SLUG="` + slug + `"
TOKEN="` + token + `"

# Detectar arquitectura del router
ARCH=$(uname -m)
case "$ARCH" in
	aarch64|arm64) GOARCH=arm64 ;;
	armv7l|armv7)  GOARCH=armv7 ;;
	*) echo "arch no soportado: $ARCH"; exit 20 ;;
esac

# Parar servicio previo si existe (el proceso vivo mantiene el binario abierto)
[ -f "$INIT" ] && $INIT stop >/dev/null 2>&1 || true

# Descargar el binario del propio server (auth por token)
if command -v curl >/dev/null 2>&1; then
	curl -fsSL --connect-timeout 10 -H "Authorization: Bearer $TOKEN" "$SERVER/api/agents/$SLUG/binary?arch=$GOARCH" -o /tmp/netpulse-agent.new
else
	wget -q -O /tmp/netpulse-agent.new --header="Authorization: Bearer $TOKEN" "$SERVER/api/agents/$SLUG/binary?arch=$GOARCH"
fi
chmod 0755 /tmp/netpulse-agent.new
mv /tmp/netpulse-agent.new "$BIN"
chmod 0755 "$BIN"

# Config (chmod 600)
cat > "$ENV_FILE" <<EOF
# netpulse-agent — config (generado por reinstall)
NETPULSE_SERVER=$SERVER
NETPULSE_SLUG=$SLUG
NETPULSE_TOKEN=$TOKEN
EOF
chmod 600 "$ENV_FILE"

# Init script procd (respawn)
cat > "$INIT" <<'INITEOF'
#!/bin/sh /etc/rc.common
# netpulse-agent — agente nativo NetPulse para OpenWrt
START=99
STOP=10
USE_PROCD=1

BIN=/usr/sbin/netpulse-agent
[ -x "$BIN" ] || BIN=/tmp/netpulse-agent

start_service() {
    procd_open_instance netpulse-agent
    procd_set_param command "$BIN"
    procd_set_param respawn "${respawn_threshold:-3600}" "${respawn_timeout:-5}" "${respawn_retry:-5}"
    procd_set_param stdout 1
    procd_set_param stderr 1
    procd_set_param file /etc/netpulse-agent.env
    procd_close_instance
}
INITEOF
chmod 0755 "$INIT"
$INIT enable
$INIT restart
`
}

// ---------------------------------------------------------------------------
// POST /api/agents/{slug}/refresh — Fase 7.3: enviar comando refresh al
// agente vía SSE para que haga un sondeo inmediato. Admin solo.
// ---------------------------------------------------------------------------

func (s *server) handleAgentRefresh(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !agentSlugRe.MatchString(slug) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if s.agentHub == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "SSE agentHub no configurado")
		return
	}
	if !s.agentHub.Send(slug, "refresh", map[string]any{}) {
		writeError(w, http.StatusNotFound, "not_found", "el agente "+slug+" no está conectado por SSE")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}
