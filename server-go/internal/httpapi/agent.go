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
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/agentbin"
	"github.com/gnacho/netpulse/server-go/internal/reinstall"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/rearmer"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
	"github.com/gnacho/netpulse/server-go/internal/vercmp"
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
	// Fase 18 (#452): persistir scans pasivos de vecinos si el payload trae
	// datos wireless. Se guardan incluso si el payload no es "fresco" para no
	// perder la muestra recién recibida.
	if s.channelPlan != nil && p.Data.Wireless != nil && len(p.Data.Wireless.Scans) > 0 {
		if err := s.channelPlan.SaveScan(p.Router, p.Ts, p.Data.Wireless.Scans); err != nil {
			log.Printf("[netpulse] error guardando scans de %s: %v", p.Router, err)
		}
	}
	// #401: si hay un upgrade en marcha y este push ya reporta la versión
	// objetivo, el ciclo se cierra con el paso terminal "done" en vez de
	// dejar el estado en "restarting" hasta el TTL.
	s.upgrades.finishIfTarget(p.Router, p.Version)
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
	// #284: sha256 del binario embebido para que el agente verifique la
	// integridad de la descarga antes de hacer el swap.
	if d := agentbin.Digest(arch); d != "" {
		w.Header().Set("X-Checksum-Sha256", d)
	}
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
	if st := readJSONBody(w, r, &body); st != 0 {
		writeBodyError(w, st, "invalid_body", `Se esperaba { "slug": "<equipo>" } (a-z, 0-9, guiones)`)
		return
	}
	if !agentSlugRe.MatchString(body.Slug) {
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
	RouterID string `json:"routerId,omitempty"` // id del router asociado (#282)
	// Hostname del board del último payload: los routers legacy del overview
	// se llaman por hostname (flint2) y su id de tabla es otro (gateway);
	// con esta tercera clave la UI puede emparejar fila↔router sin duplicar
	// (#483).
	Hostname string `json:"hostname,omitempty"`
	LastSeen *int64 `json:"lastSeen"`           // unix SEGUNDOS; null si nunca empujó
	Version  string `json:"version,omitempty"`
	Fresh    bool   `json:"fresh"`
	// UpdateAvailable: true si el agente reportó una versión distinta de la
	// del binario embebido (agentbin.EmbeddedAgentVersion) → hay upgrade
	// disponible vía POST /api/agents/{slug}/upgrade (Fase 6.3, issue #243).
	// SOLO se marca para routers OpenWrt (type openwrt/glinet): los de tipo
	// managed-switch/external son scrapers que no usan el agente nativo.
	UpdateAvailable bool `json:"updateAvailable"`
	// Upgrade: último paso del self-update en marcha (requested/downloading/
	// swapping/restarting/failed) con su marca de tiempo. null si no hay
	// actividad reciente (#284: progreso en vivo para la UI).
	Upgrade *agentUpgradeInfo `json:"upgrade,omitempty"`
	// Kind: "native" (agente netpulse-agent) o "external" (pusher externo
	// tipo scraper de switch gestionado, #285/#288). Default "native".
	Kind string `json:"kind"`
	// Interval: cadencia de push declarada en segundos (solo externos, #288).
	Interval int `json:"interval,omitempty"`
}

// agentUpgradeStep es un paso de la historia (timeline de la UI, #284).
type agentUpgradeStep struct {
	Step string `json:"step"`
	Pct  int    `json:"pct,omitempty"`
	Ts   int64  `json:"ts"` // unix segundos
}

// agentUpgradeInfo es el paso de progreso expuesto en GET /api/agents.
type agentUpgradeInfo struct {
	Step  string             `json:"step"`
	Pct   int                `json:"pct,omitempty"` // 0-100 en "downloading"
	Error string             `json:"error,omitempty"`
	Ts    int64              `json:"ts"`              // unix segundos del último reporte
	Steps []agentUpgradeStep `json:"steps,omitempty"` // historia de pasos (#284)
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
	// Routers precargados en UNA consulta: con SetMaxOpenConns(1) un QueryRow
	// dentro del bucle de rows de abajo esperaría la conexión que el rows
	// activo mantiene (deadlock). Guardamos id, type, name, host y mac para
	// resolver la asociación agente↔router (#282).
	routerByID := map[string]routerIdentity{}
	if s.db != nil {
		if rows, err := s.db.Query("SELECT id, type, name, host, COALESCE(mac,'') FROM routers"); err == nil {
			for rows.Next() {
				var id, typ, name, host, mac string
				if rows.Scan(&id, &typ, &name, &host, &mac) == nil {
					routerByID[id] = routerIdentity{ID: id, Type: typ, Name: name, Host: host, Mac: mac}
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
				var payload *probe.Payload
				agentKind := ""
				if s.agents != nil {
					if st := s.agents.Snapshot(slug); st != nil {
						payload = st.Payload
					}
					if seen, version, kind, interval, ok := s.agents.Info(slug); ok {
						ts := seen.Unix()
						item.LastSeen = &ts
						item.Version = version
						agentKind = kind
						switch kind {
						case "external", "netgrip":
							item.Kind = kind
						default:
							item.Kind = "native"
						}
						item.Interval = interval
					}
					_, item.Fresh = s.agents.Fresh(slug)
				}
				item.RouterID = resolveAgentRouter(slug, payload, routerByID)
				if payload != nil && payload.Data.System != nil && payload.Data.System.Board != nil {
					item.Hostname = strings.ToLower(strings.TrimSpace(payload.Data.System.Board.Hostname))
				}
				// Fase 6.3 (issue #243): upgrade disponible si el agente
				// reporta una versión distinta del binario embebido y el router
				// asociado es actualizable (nativo OpenWrt/GL.iNet).
				matchedType := ""
				if r, ok := routerByID[item.RouterID]; ok {
					matchedType = r.Type
				}
				// #363: un agente embebido en NetGrip se actualiza vía el
				// evento SSE upgrade (NetGrip corre su propio self-update);
				// "hay novedad" = versión reportada < última release de NetGrip.
				// #447: hay upgrade disponible solo si el agente reporta una
				// versión MÁS VIEJA que la del binario embebido, build incluido
				// (mismo criterio que finishIfTarget y upgrade-all: un agente
				// más nuevo que el embebido no está desactualizado).
				if agentKind == "netgrip" {
					item.UpdateAvailable = item.Version != "" && cmpSemver(netgripLatest(), item.Version) > 0
				} else {
					item.UpdateAvailable = agentUpgradeable(matchedType) && item.Version != "" && vercmp.CmpBuild(item.Version, agentbin.EmbeddedAgentVersion) < 0
				}
				// Progreso en vivo del upgrade (#284), si hay actividad reciente.
				if st, ok := s.upgrades.snapshot(slug); ok {
					ts := st.Ts.Unix()
					info := &agentUpgradeInfo{Step: st.Step, Pct: st.Pct, Error: st.Error, Ts: ts}
					for _, e := range st.History {
						info.Steps = append(info.Steps, agentUpgradeStep{Step: e.Step, Pct: e.Pct, Ts: e.Ts.Unix()})
					}
					item.Upgrade = info
				}
				out = append(out, item)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"agents": out})
}

// routerIdentity es la información mínima de un router para resolver la
// asociación agente↔router sin exponer credenciales.
type routerIdentity struct {
	ID   string
	Type string
	Name string
	Host string
	Mac  string
}

// resolveAgentRouter devuelve el id del router asociado a un agente. Primero
// intenta slug == router.id; si no, empareja por hostname del board del
// payload o por bridge MAC persistida (#282).
func resolveAgentRouter(slug string, payload *probe.Payload, routers map[string]routerIdentity) string {
	if slug != "" {
		if _, ok := routers[slug]; ok {
			return slug
		}
	}
	if payload == nil || payload.Data.System == nil || payload.Data.System.Board == nil {
		return ""
	}
	norm := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	hostname := norm(payload.Data.System.Board.Hostname)
	if hostname != "" {
		for _, r := range routers {
			if hostname == norm(r.Host) || hostname == norm(r.Name) {
				return r.ID
			}
		}
	}
	if payload.Data.System.BridgeMAC != "" {
		for _, r := range routers {
			if strings.EqualFold(payload.Data.System.BridgeMAC, r.Mac) {
				return r.ID
			}
		}
	}
	return ""
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
	case errors.Is(err, rearmer.ErrExternalAgent):
		writeError(w, http.StatusConflict, "not_openwrt", err.Error())
	case errors.Is(err, rearmer.ErrNetgripAgent):
		writeError(w, http.StatusConflict, "netgrip_managed", err.Error())
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
	// #363: jamás desplegar el binario standalone sobre un router cuyo
	// agente es el NetGrip embebido (lo destrozaría: duplicado + token roto).
	if s.agentKindOf(slug) == "netgrip" {
		writeError(w, http.StatusConflict, "netgrip_managed", "agente embebido en NetGrip: actualiza o reinstala el panel NetGrip en el propio router")
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

	// URL base del server (para que el router descargue el binario).
	// NETPULSE_PUBLIC_URL manda si está configurada (#457): sin ella, llamar
	// al endpoint vía localhost haría que el router descargue de localhost.
	scheme := "http"
	if auth.IsSecureRequest(r) {
		scheme = "https"
	}
	serverURL := scheme + "://" + r.Host
	if s.cfg != nil && s.cfg.PublicURL != "" {
		serverURL = s.cfg.PublicURL
	}

	// Script de instalación canónico (compartido con el supervisor, #457).
	script := reinstall.Script(slug, token, serverURL, reinstall.Digests())

	before := time.Now()
	var prevSeen time.Time
	if s.agents != nil {
		if seen, _, _, _, ok := s.agents.Info(slug); ok {
			prevSeen = seen
		}
	}

	// Descarga + verificación + swap: margen generoso para enlaces lentos.
	if _, err := s.pool.Run(host, script, 300*time.Second); err != nil {
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
		if seen, _, _, _, ok := s.agents.Info(slug); ok && seen.After(prevSeen) && seen.After(before.Add(-5*time.Second)) {
			recovered = true
			break
		}
	}

	writeJSON(w, http.StatusOK, reinstallResponse{
		Slug: slug, Token: token, Installed: true, Recovered: recovered,
		Message: map[bool]string{true: "agente instalado y empujando", false: "agente instalado; aún no ha empujado en 40 s"}[recovered],
	})
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

// agentKindOf devuelve el kind del último push del slug ("" si no hay).
func (s *server) agentKindOf(slug string) string {
	if s.agents == nil {
		return ""
	}
	if _, _, kind, _, ok := s.agents.Info(slug); ok {
		return kind
	}
	return ""
}

// ------------------------------------------------------------ netgrip latest --
// netgripLatest resuelve la última release de gnacho/netgrip con cache (TTL):
// los agentes kind=netgrip comparan su versión contra ELLA (el binario
// embebido del server no aplica). Falla de red → cache/"" → sin botón.
// NetgripLatestVersion es la puerta pública para el cableado del check de
// agentes desactualizados (issue #400).
func NetgripLatestVersion() string { return netgripLatest() }

var (
	netgripLatestMu    sync.Mutex
	netgripLatestVer   string
	netgripLatestAt    time.Time
	netgripLatestFetch = fetchNetgripLatestVersion // sustituible en tests
)

const netgripLatestTTL = 5 * time.Minute

func fetchNetgripLatestVersion() string {
	client := &http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequest("GET", "https://api.github.com/repos/gnacho/netgrip/releases/latest", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "netpulse-server")
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		if resp != nil {
			resp.Body.Close()
		}
		return ""
	}
	defer resp.Body.Close()
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&rel); err != nil {
		return ""
	}
	return strings.TrimPrefix(rel.TagName, "v")
}

func netgripLatest() string {
	netgripLatestMu.Lock()
	defer netgripLatestMu.Unlock()
	if netgripLatestVer != "" && time.Since(netgripLatestAt) < netgripLatestTTL {
		return netgripLatestVer
	}
	if v := netgripLatestFetch(); v != "" {
		netgripLatestVer, netgripLatestAt = v, time.Now()
	}
	return netgripLatestVer
}

// cmpSemver compara dos versiones x.y.z (-rN se ignora): -1, 0, 1. Malparse
// → 0 (sin señal de novedad). Implementación compartida en internal/vercmp.
func cmpSemver(a, b string) int { return vercmp.Cmp(a, b) }
