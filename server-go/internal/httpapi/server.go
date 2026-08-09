// Package httpapi — handlers de la API (paridad con server/src/routes/* +
// app.js + health.js). Ensamblado equivalente a createApp:
//
//	security headers (global) → requireAuth (global) → rutas API →
//	404 JSON /api/* → estáticos + SPA fallback → onError 500.
//
// El middleware noStore (Cache-Control: no-store) se aplica a /api/* salvo
// /api/health, /health y /api/auth/* (paridad con el orden de registro de
// Hono, SPEC §1); /api/stream lo sobrescribe con no-cache.
package httpapi

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/rearmer"
	"github.com/gnacho/netpulse/server-go/internal/security"
	"github.com/gnacho/netpulse/server-go/internal/sse"
	"github.com/gnacho/netpulse/server-go/internal/orchestr"
	"github.com/gnacho/netpulse/server-go/internal/staticspa"
	"github.com/gnacho/netpulse/server-go/internal/updater"
)

// Version es la versión del backend (app.js:18).
const Version = "2.9.0"

// Deps son las dependencias del servidor API (como createApp de app.js).
type Deps struct {
	Config  *config.Config
	DB      *db.DB
	Adapter adapters.Snapshotter
	Hub     *sse.Hub
	Secret  string
	Static  *staticspa.Handler
	Updater *updater.Updater // nil → sin rutas /api/update/*
	// Agents: registry de agentes nativos (ingesta POST /api/ingest/agent y
	// last_seen/versión de GET /api/agents). nil → ingesta 503.
	Agents *adapters.AgentRegistry
	// Pool: ejecutor de comandos SSH del sondeo. nil (demo / sin clave) → el
	// rearme de agentes responde 503 (POST /api/agents/{slug}/rearm, Fase 5).
	// Es una interfaz para poder inyectar un fake en los tests.
	Pool SSHRunner
	// RearmPollWait: cuánto esperar el push de vuelta tras rearmar (default
	// 30 s; los tests lo bajan). <= 0 → rearmPollWait.
	RearmPollWait time.Duration
	// Rearmer: instancia compartida (la usa también el supervisor de
	// auto-rearme). nil → se construye una interna (tests).
	Rearmer *rearmer.Rearmer
	// LastOverview devuelve el último overview del poller (nil si aún no hay).
	LastOverview func() *adapters.Overview
	// PollNow dispara un ciclo de sondeo inmediato (POST /api/refresh);
	// nil → el refresh manual es no-op (tests sin poller).
	PollNow func()
	Started time.Time
	// AgentHub: SSE bidireccional para agentes (Fase 7.3). nil → endpoint
	// de stream devuelve 503.
	AgentHub *sse.AgentHub
	// ServerFP: fingerprint SPKI del servidor (hex). Vacío si no es on-box.
	// Se devuelve en /api/agents/pair para que el agentepine el TLS.
	ServerFP string
	// Orchestr: motor de plan/apply (Fase 10). nil → sin rutas /api/plans.
	Orchestr *orchestr.Manager
}

type server struct {
	cfg     *config.Config
	db      *db.DB
	adapter adapters.Snapshotter
	hub     *sse.Hub
	secret  string
	agents  *adapters.AgentRegistry
	pool    SSHRunner
	rearmer *rearmer.Rearmer
	lastOv  func() *adapters.Overview
	pollNow func()
	started time.Time

	// SSE bidireccional para agentes (Fase 7.3).
	agentHub *sse.AgentHub

	// Fingerprint SPKI del servidor (vacío si no es on-box).
	serverFP string

	// Anti-martilleo de POST /api/refresh (global, min 5 s entre sondeos).
	refreshMu   sync.Mutex
	lastRefresh time.Time

	// Rate limit por IP de POST /api/ingest/agent (30/min, SPEC-AGENTE §1).
	ingestLimit *ipRateLimit

	// Ventana de frescura del `ts` del agente (anti-replay, auditoría #2).
	maxTsDrift time.Duration
}

// NewHandler ensambla el handler HTTP completo (API + estáticos + SPA).
func NewHandler(d Deps) http.Handler {
	s := &server{
		cfg: d.Config, db: d.DB, adapter: d.Adapter, hub: d.Hub,
		secret: d.Secret, agents: d.Agents, pool: d.Pool,
		lastOv: d.LastOverview, pollNow: d.PollNow, started: d.Started,
		agentHub:    d.AgentHub,
		serverFP:    d.ServerFP,
		ingestLimit: newIPRateLimit(ingestRateLimit, ingestRateWindow),
	}
	// Rearmer compartido entre el endpoint manual y el supervisor de
	// auto-rearme (cmd/netpulse lo construye y lo pasa para que ambos
	// compartan cooldowns; nil → se crea uno interno, p. ej. en tests).
	if d.Rearmer != nil {
		s.rearmer = d.Rearmer
	} else {
		// El motor de alertas del adapter alimenta las alertas de rearme.
		var rearmEngine rearmer.AlertsEngine
		if d.Adapter != nil {
			rearmEngine = d.Adapter.AlertsEngine()
		}
		var dbHandle *sql.DB
		if d.DB != nil {
			dbHandle = d.DB.DB
		}
		s.rearmer = rearmer.New(dbHandle, d.Agents, d.Pool, rearmEngine, d.RearmPollWait)
	}
	// Ventana de frescura del ts del agente (anti-replay, auditoría #2).
	s.maxTsDrift = maxTsDriftDefault
	if d.Config != nil && d.Config.MaxTsDriftSec > 0 {
		s.maxTsDrift = time.Duration(d.Config.MaxTsDriftSec) * time.Second
	}
	mode := d.Adapter.Mode()

	mux := http.NewServeMux()

	// --- Health (público; SIN no-store) ---
	mux.HandleFunc("GET /api/health", s.handleAPIHealth(mode))
	mux.HandleFunc("GET /health", s.handleHealth)

	// --- Auth (público login; SIN no-store) ---
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)
	mux.HandleFunc("GET /api/auth/me", s.handleMe(mode))

	// --- Datos (sesión; con no-store) ---
	mux.HandleFunc("GET /api/overview", s.handleOverview)
	mux.HandleFunc("GET /api/routers", s.handleRouters)
	mux.HandleFunc("GET /api/routers/{id}", s.handleRouterDetail)
	mux.HandleFunc("GET /api/devices", s.handleDevices)
	mux.HandleFunc("GET /api/alerts", s.handleAlerts)
	mux.HandleFunc("GET /api/alerts/config", s.handleAlertsConfigGet)
	mux.HandleFunc("PUT /api/alerts/config", s.handleAlertsConfigPut)
	mux.HandleFunc("POST /api/alerts/read", s.handleAlertsRead)
	mux.HandleFunc("POST /api/alerts/read-all", s.handleAlertsReadAll)
	mux.HandleFunc("GET /api/topology", s.handleTopology)
	mux.HandleFunc("GET /api/dawn", s.handleDawn)
	mux.HandleFunc("GET /api/dot11r", s.handleDot11r)
	mux.HandleFunc("GET /api/survey", s.handleSurvey)
	mux.HandleFunc("GET /api/roam-events", s.handleRoamEvents)
	mux.HandleFunc("GET /api/adguard/clients", s.handleAdguardClients)
	mux.HandleFunc("GET /api/system/info", s.handleSystemInfo)
	mux.HandleFunc("GET /api/reports/weekly", s.handleWeeklyReport)
	mux.HandleFunc("GET /api/reports/availability", s.handleAvailabilityReport)

	// --- Sondeo manual (botón "Refrescar" de Topología; 202 + SSE empuja) ---
	mux.HandleFunc("POST /api/refresh", s.handleRefresh)

	// --- Agentes nativos (Fase 3) ---
	// Ingesta: SIN sesión (auth Bearer propia; exenta en RequireAuth).
	mux.HandleFunc("POST /api/ingest/agent", s.handleIngestAgent)
	// Gestión de tokens: tras sesión como el resto del API; las mutaciones
	// (crear/revocar/rearmar) exigen rol admin — ejecutan acciones sobre los
	// routers o exponen credenciales (auditoría v2.4.0 §2, issue #7). La
	// lista es de lectura y se deja tras RequireAuth.
	mux.Handle("POST /api/agents", auth.RequireAdmin(http.HandlerFunc(s.handleAgentsCreate)))
	mux.HandleFunc("GET /api/agents", s.handleAgentsList)
	mux.Handle("DELETE /api/agents/{slug}", auth.RequireAdmin(http.HandlerFunc(s.handleAgentsDelete)))
	// Fase 5 (Plan B): rearme del servicio procd del agente vía SSH.
	mux.Handle("POST /api/agents/{slug}/rearm", auth.RequireAdmin(http.HandlerFunc(s.handleAgentRearm)))
	// Fase 6.2: servir binario del agente desde el propio servidor (sin GitHub).
	// Auth por token de agente (Bearer), igual que la ingesta — el one-liner de
	// instalación incluye el token y se ejecuta en el router, sin sesión admin.
	mux.HandleFunc("GET /api/agents/{slug}/binary", s.handleAgentBinary)
	// Fase 7.3: SSE bidireccional agente↔servidor. El agente mantiene una
	// conexión SSE abierta; el servidor envía comandos (refresh, etc.).
	// Auth por token de agente (Bearer), igual que ingesta y binary.
	if s.agentHub != nil {
		mux.HandleFunc("GET /api/agents/{slug}/stream", s.agentHub.HandleStream)
		// Forzar refresh del agente vía SSE (admin; útil para depuración y futuro UI)
		mux.Handle("POST /api/agents/{slug}/refresh", auth.RequireAdmin(http.HandlerFunc(s.handleAgentRefresh)))
	}

	// --- Fase 9 R3: Pairing / adopción de agentes ---
	// POST /api/agents/pair: sin sesión (el pairing token ES la auth), rate limited.
	mux.HandleFunc("POST /api/agents/pair", s.handleAgentPair)
	// Gestión del pairing token (admin).
	mux.Handle("GET /api/pairing/token", auth.RequireAdmin(http.HandlerFunc(s.handlePairingToken)))
	mux.Handle("POST /api/pairing/rotate", auth.RequireAdmin(http.HandlerFunc(s.handlePairingRotate)))

	// --- Web Push (Fase 3 Bloque C; tras sesión como el resto del API) ---
	mux.HandleFunc("GET /api/push/vapid-key", s.handlePushVapidKey)
	mux.HandleFunc("POST /api/push/subscribe", s.handlePushSubscribe)
	mux.HandleFunc("POST /api/push/unsubscribe", s.handlePushUnsubscribe)

	// --- Users ---
	// Idioma y nombre propios: FUERA del gate admin (se registran antes en Node).
	mux.HandleFunc("PUT /api/users/me/language", s.handleMyLanguage)
	mux.HandleFunc("PUT /api/users/me/display-name", s.handleMyDisplayName)
	// Resto: solo admin.
	mux.Handle("GET /api/users", auth.RequireAdmin(http.HandlerFunc(s.handleListUsers)))
	mux.Handle("POST /api/users", auth.RequireAdmin(http.HandlerFunc(s.handleCreateUser)))
	mux.Handle("PUT /api/users/{id}/password", auth.RequireAdmin(http.HandlerFunc(s.handleSetPassword)))
	mux.Handle("PUT /api/users/{id}/role", auth.RequireAdmin(http.HandlerFunc(s.handleSetRole)))
	mux.Handle("DELETE /api/users/{id}", auth.RequireAdmin(http.HandlerFunc(s.handleDeleteUser)))
	mux.Handle("GET /api/webhook/dlq", auth.RequireAdmin(http.HandlerFunc(s.handleWebhookDLQ)))

	// --- Config (sesión; /api/config/adguard solo admin) ---
	s.registerConfigRoutes(mux)

	// --- Demo (issue #4): activar modo demo desde la UI; solo admin ---
	s.registerDemoRoutes(mux)

	// --- Update (solo admin; ausente si no hay updater, p.ej. en tests) ---
	if d.Updater != nil {
		s.registerUpdateRoutes(mux, d.Updater)
	}

	// --- Orquestación (Fase 10; solo admin) ---
	s.registerOrchestrRoutes(mux, d.Orchestr)

	// --- Ajustes globales en kv (issue #121: orchestration opt-in) ---
	s.registerSettingsRoutes(mux)

	// --- Copias de seguridad (issue #158) ---
	s.registerBackupRoutes(mux)

	// --- Overrides manuales de topología (issue #142; solo admin) ---
	s.registerTopologyOverrideRoutes(mux)

	// --- SSE ---
	mux.HandleFunc("GET /api/stream", s.hub.HandleStream)

	// --- 404 JSON para cualquier /api/* no registrado (cualquier método) ---
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found")
	})

	// --- Estáticos + SPA fallback ---
	static := d.Static
	if static != nil {
		mux.Handle("/", static)
	}

	return requestID(security.Middleware(auth.RequireAuth(s.db, s.secret, s.demoReadOnly(noStoreMux(mux)))))
}

// requestID lee o genera un x-request-id para cada petición y lo expone en
// la respuesta. Útil para correlar logs y debugging entre front y back.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = r.Header.Get("X-Request-Id")
		}
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r)
	})
}

func newRequestID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// noStoreMux replica el middleware noStore() de routes/data.js: aplica
// Cache-Control: no-store a /api/* EXCEPTO /api/health y /api/auth/*
// (/health no es /api/*). /api/stream sobrescribe con no-cache después.
func noStoreMux(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if strings.HasPrefix(p, "/api/") && p != "/api/health" && !strings.HasPrefix(p, "/api/auth/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

// writeJSON serializa como JSON.stringify: compacto, SIN escapar HTML
// (SetEscapeHTML(false)) y SIN el '\n' final que añade Encoder.Encode
// (consistencia con D5 WriteError / paridad Hono: marshal + write).
func writeJSON(w http.ResponseWriter, status int, v any) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(bytes.TrimSuffix(buf.Bytes(), []byte("\n")))
}

// writeError escribe el envelope {error, message?}.
func writeError(w http.ResponseWriter, status int, code string, message ...string) {
	if len(message) > 0 {
		writeJSON(w, status, map[string]any{"error": code, "message": message[0]})
		return
	}
	writeJSON(w, status, map[string]any{"error": code})
}

// maxBodyBytes: techo defensivo para cualquier body JSON de sesión
// (login, users, config, alerts, push). Todos los cuerpos reales son < 1 KB;
// el cap evita abuso de memoria con bodies gigantes (auditoría #3).
const maxBodyBytes = 64 << 10

// readJSONBody parsea el body JSON; devuelve false si no es JSON válido o
// supera maxBodyBytes (equivalente a c.req.json().catch(() => null)).
func readJSONBody(r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(nil, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		return false
	}
	return true
}
