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
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/apitoken"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/baselines"
	"github.com/gnacho/netpulse/server-go/internal/channelplan"
	"github.com/gnacho/netpulse/server-go/internal/collectorreader"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/configbackup"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/firmware"
	"github.com/gnacho/netpulse/server-go/internal/internethealth"
	"github.com/gnacho/netpulse/server-go/internal/orchestr"
	"github.com/gnacho/netpulse/server-go/internal/pathanalysis"
	"github.com/gnacho/netpulse/server-go/internal/presence"
	"github.com/gnacho/netpulse/server-go/internal/rearmer"
	"github.com/gnacho/netpulse/server-go/internal/security"
	"github.com/gnacho/netpulse/server-go/internal/sse"
	"github.com/gnacho/netpulse/server-go/internal/staticspa"
	"github.com/gnacho/netpulse/server-go/internal/updater"
	"github.com/gnacho/netpulse/server-go/internal/wifisle"
)

// Version es la versión del backend (app.js:18). Es una var (no const) para
// que goreleaser la inyecte con -X httpapi.Version={{.Version}} y el health
// reporte la versión del tag; los builds locales caen al fallback.
var Version = "2.24.0"

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
	// TokenStore: bearer tokens de API con scopes (#330). nil → sin tokens.
	TokenStore *apitoken.Store
	// CollectorReader: lector read-only de metrics.db del sidecar (#328).
	CollectorReader *collectorreader.Reader
	// Baselines: EWMA+sigma por franja horaria (#333). nil → sin baselines.
	Baselines *baselines.Store
	// InternetHealth: sonda multi-target + outage log (#335). nil → sin sondas.
	InternetHealth *internethealth.Store
	// Presence: personas y presencia (#336). nil → sin presencia.
	Presence *presence.Store
	// WiFiSLE: WiFi Service Level Expectations (#342). nil → sin SLEs.
	WiFiSLE *wifisle.Store
	// ChannelPlan: scans pasivos y recomendaciones de canal (#452).
	ChannelPlan *channelplan.Store
	// PathAnalysis: mtr path analysis (#343). nil → sin path data.
	PathAnalysis *pathanalysis.Store
	// ConfigBackup: snapshots UCI de NetGrip (#34). nil → sin backup.
	ConfigBackup *configbackup.Store
	// Firmware: targets y upgrades de firmware (#453).
	Firmware *firmware.Store
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

	// Progreso en vivo de los self-updates de agentes (#284): último paso
	// por slug en memoria, expuesto en GET /api/agents.
	upgrades *upgradeTracker

	// Beacon UDP de pushers embebidos (#291): socket, último seq por slug y
	// candidatos de descubrimiento (anuncios broadcast sin parar).
	beaconConn   net.PacketConn
	beaconSeqMu  sync.Mutex
	beaconSeq    map[string]uint32
	beaconCandMu sync.Mutex
	beaconCand   map[string]beaconCandidate

	// Ventana de frescura del `ts` del agente (anti-replay, auditoría #2).
	maxTsDrift time.Duration

	// TokenStore: bearer tokens de API (#330). nil = sin tokens.
	tokenStore *apitoken.Store

	// CollectorReader: lector read-only de metrics.db del sidecar (#328).
	collectorReader *collectorreader.Reader

	// Baselines EWMA+sigma por franja horaria (#333). nil = sin baselines.
	baselines *baselines.Store

	// Internet Health: sonda multi-target + outage log (#335). nil = sin sondas.
	internetHealth *internethealth.Store

	// Presence: personas y presencia (#336). nil = sin presencia.
	presence *presence.Store

	// WiFiSLE: WiFi Service Level Expectations (#342). nil = sin SLEs.
	wifiSLE *wifisle.Store
	// ChannelPlan: scans pasivos y recomendaciones de canal (#452).
	channelPlan *channelplan.Store
	// PathAnalysis: mtr path analysis (#343). nil = sin path data.
	pathAnalysis *pathanalysis.Store

	// ConfigBackup: snapshots UCI de NetGrip (#34). nil = sin backup.
	configBackup *configbackup.Store

	// Firmware: targets y upgrades de firmware (#453).
	firmware *firmware.Store
}

// NewHandler ensambla el handler HTTP completo (API + estáticos + SPA).
func NewHandler(d Deps) http.Handler {
	s := &server{
		cfg: d.Config, db: d.DB, adapter: d.Adapter, hub: d.Hub,
		secret: d.Secret, agents: d.Agents, pool: d.Pool,
		lastOv: d.LastOverview, pollNow: d.PollNow, started: d.Started,
		agentHub:        d.AgentHub,
		serverFP:        d.ServerFP,
		ingestLimit:     newIPRateLimit(ingestRateLimit, ingestRateWindow),
		upgrades:        newUpgradeTracker(),
		tokenStore:      d.TokenStore,
		collectorReader: d.CollectorReader,
		baselines:       d.Baselines,
		internetHealth:  d.InternetHealth,
		presence:        d.Presence,
		wifiSLE:         d.WiFiSLE,
		channelPlan:     d.ChannelPlan,
		pathAnalysis:    d.PathAnalysis,
		configBackup:    d.ConfigBackup,
		firmware:        d.Firmware,
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
	// #284: upgrades encolados se envían cuando el agente (re)conecta su
	// stream SSE (flush on-connect del hub).
	if s.agentHub != nil {
		s.agentHub.SetOnConnect(s.FlushQueuedUpgrade)
	}
	// #291: listener UDP de beacons (NETPULSE_BEACON_LISTEN; vacío = off).
	if d.Config != nil && d.Config.BeaconListen != "" {
		if la, err := s.startBeaconListener(d.Config.BeaconListen); err != nil {
			log.Printf("[netpulse] aviso: beacon UDP no arrancó en %s (%v)", d.Config.BeaconListen, err)
		} else {
			log.Printf("[netpulse] beacon UDP escuchando en %s", la)
		}
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
	mux.Handle("PUT /api/devices/{mac}/override", auth.RequireAdmin(http.HandlerFunc(s.handleDeviceOverridePut)))
	mux.Handle("GET /api/devices/{mac}/override", auth.RequireAdmin(http.HandlerFunc(s.handleDeviceOverrideGet)))
	mux.Handle("PUT /api/devices/{mac}/ban", auth.RequireAdmin(http.HandlerFunc(s.handleDeviceBanPut)))
	// Reserva DHCP y bloqueo de dispositivo (issue #439).
	mux.Handle("GET /api/devices/{mac}/reservation", auth.RequireAdmin(http.HandlerFunc(s.handleDeviceReservationGet)))
	mux.Handle("PUT /api/devices/{mac}/reservation", auth.RequireAdmin(http.HandlerFunc(s.handleDeviceReservationPut)))
	mux.Handle("DELETE /api/devices/{mac}/reservation", auth.RequireAdmin(http.HandlerFunc(s.handleDeviceReservationDelete)))
	mux.Handle("GET /api/devices/{mac}/block", auth.RequireAdmin(http.HandlerFunc(s.handleDeviceBlockGet)))
	mux.Handle("PUT /api/devices/{mac}/block", auth.RequireAdmin(http.HandlerFunc(s.handleDeviceBlockPut)))
	mux.Handle("DELETE /api/devices/{mac}/block", auth.RequireAdmin(http.HandlerFunc(s.handleDeviceBlockDelete)))
	mux.HandleFunc("GET /api/alerts", s.handleAlerts)
	mux.HandleFunc("GET /api/alerts/config", s.handleAlertsConfigGet)
	mux.HandleFunc("PUT /api/alerts/config", s.handleAlertsConfigPut)
	mux.HandleFunc("POST /api/alerts/read", s.handleAlertsRead)
	mux.HandleFunc("POST /api/alerts/read-all", s.handleAlertsReadAll)
	mux.HandleFunc("POST /api/alerts/silence", s.handleAlertsSilence)
	mux.HandleFunc("POST /api/alerts/unsilence", s.handleAlertsUnsilence)
	mux.HandleFunc("GET /api/alerts/silenced", s.handleAlertsSilenced)
	mux.HandleFunc("GET /api/alert-rules", s.handleAlertRulesList)
	mux.HandleFunc("GET /api/alert-rules/{id}", s.handleAlertRulesGet)
	mux.HandleFunc("POST /api/alert-rules", s.handleAlertRulesCreate)
	mux.HandleFunc("PUT /api/alert-rules/{id}", s.handleAlertRulesUpdate)
	mux.HandleFunc("DELETE /api/alert-rules/{id}", s.handleAlertRulesDelete)
	mux.HandleFunc("GET /api/baselines", s.handleBaselines)
	mux.HandleFunc("GET /api/internet-health", s.handleInternetHealth)
	mux.HandleFunc("GET /api/presence", s.handlePresenceStatus)
	mux.HandleFunc("GET /api/presence/people", s.handlePresencePeople)
	mux.HandleFunc("GET /api/wifi-sles", s.handleWiFiSLESummary)
	mux.HandleFunc("GET /api/wifi-sles/series", s.handleWiFiSLESeries)
	s.registerChannelPlanRoutes(mux)
	s.registerFirmwareRoutes(mux)
	mux.HandleFunc("GET /api/path/summaries", s.handlePathSummaries)
	mux.HandleFunc("GET /api/path/latest", s.handlePathLatest)
	mux.HandleFunc("GET /api/path/history", s.handlePathHistory)
	mux.HandleFunc("GET /api/path/destinations", s.handlePathDestinations)
	mux.HandleFunc("GET /api/metrics", s.handleMetrics)
	mux.HandleFunc("GET /api/topology", s.handleTopology)
	mux.HandleFunc("GET /api/usteer", s.handleUsteer)
	mux.HandleFunc("POST /api/usteer/{mac}/kick", s.handleUsteerKick)
	s.registerReanchorRoutes(mux)
	mux.HandleFunc("GET /api/dot11r", s.handleDot11r)
	mux.HandleFunc("GET /api/survey", s.handleSurvey)
	mux.HandleFunc("GET /api/roam-events", s.handleRoamEvents)
	mux.HandleFunc("GET /api/device-events", s.handleDeviceEvents)
	mux.HandleFunc("GET /api/adguard/clients", s.handleAdguardClients)
	mux.HandleFunc("GET /api/routers/{id}/ports/{portId}/series", s.handlePortSeries)
	mux.HandleFunc("GET /api/system/info", s.handleSystemInfo)
	mux.HandleFunc("GET /api/reports/weekly", s.handleWeeklyReport)
	mux.HandleFunc("GET /api/reports/availability", s.handleAvailabilityReport)
	mux.Handle("GET /api/config-backup", auth.RequireAdmin(http.HandlerFunc(s.handleConfigBackupList)))
	mux.HandleFunc("POST /api/config-backup", s.handleConfigBackupUpload)
	mux.Handle("GET /api/config-backup/{id}", auth.RequireAdmin(http.HandlerFunc(s.handleConfigBackupDownload)))
	mux.Handle("DELETE /api/config-backup/{id}", auth.RequireAdmin(http.HandlerFunc(s.handleConfigBackupDelete)))

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
	// #291: switches embebidos anunciándose por broadcast sin parar.
	mux.Handle("GET /api/agents/discovered", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"discovered": s.beaconCandidates()})
	})))
	mux.Handle("DELETE /api/agents/{slug}", auth.RequireAdmin(http.HandlerFunc(s.handleAgentsDelete)))
	// Fase 5 (Plan B): rearme del servicio procd del agente vía SSH.
	mux.Handle("POST /api/agents/{slug}/rearm", auth.RequireAdmin(http.HandlerFunc(s.handleAgentRearm)))
	// #246: reinstalación completa del agente en el router (binario, env,
	// servicio procd) vía SSH — recupera un agente borrado por una
	// actualización de firmware o una desinstalación manual.
	mux.Handle("POST /api/agents/{slug}/reinstall", auth.RequireAdmin(http.HandlerFunc(s.handleAgentReinstall)))
	// Fase 6.2: servir binario del agente desde el propio servidor (sin GitHub).
	// Auth por token de agente (Bearer), igual que la ingesta — el one-liner de
	// instalación incluye el token y se ejecuta en el router, sin sesión admin.
	mux.HandleFunc("GET /api/agents/{slug}/binary", s.handleAgentBinary)
	// Fase 6.3 (issue #243): el agente reporta el resultado del self-upgrade.
	// Auth por token de agente (Bearer), igual que binary/apply-result.
	mux.HandleFunc("POST /api/agents/{slug}/upgrade-result", s.handleAgentUpgradeResult)
	// #284: pasos intermedios del self-update (auth por token de agente).
	mux.HandleFunc("POST /api/agents/{slug}/upgrade-progress", s.handleAgentUpgradeProgress)
	// #251: upgrade masivo de todos los agentes con versión desactualizada.
	mux.Handle("POST /api/agents/upgrade-all", auth.RequireAdmin(http.HandlerFunc(s.handleAgentsUpgradeAll)))
	// Fase 7.3: SSE bidireccional agente↔servidor. El agente mantiene una
	// conexión SSE abierta; el servidor envía comandos (refresh, etc.).
	// Auth por token de agente (Bearer), igual que ingesta y binary.
	if s.agentHub != nil {
		mux.HandleFunc("GET /api/agents/{slug}/stream", s.agentHub.HandleStream)
		// Forzar refresh del agente vía SSE (admin; útil para depuración y futuro UI)
		mux.Handle("POST /api/agents/{slug}/refresh", auth.RequireAdmin(http.HandlerFunc(s.handleAgentRefresh)))
		// Self-update del agente vía SSE (admin; Fase 6.3, issue #243).
		mux.Handle("POST /api/agents/{slug}/upgrade", auth.RequireAdmin(http.HandlerFunc(s.handleAgentUpgrade)))
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

	// --- API tokens (#330): CRUD de bearer tokens con scopes ---
	mux.HandleFunc("GET /api/tokens", s.handleTokensList)
	mux.HandleFunc("POST /api/tokens", s.handleTokensCreate)
	mux.HandleFunc("DELETE /api/tokens/{id}", s.handleTokensDelete)

	// --- Collector sidecar (#328): series de latencia/disponibilidad ---
	mux.HandleFunc("GET /api/collector/metrics", s.handleCollectorMetrics)
	mux.HandleFunc("GET /api/collector/series", s.handleCollectorSeries)

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

	var tv auth.TokenValidator
	if s.tokenStore != nil {
		tv = s.tokenStore
	}
	return requestID(security.Middleware(auth.RequireSameOrigin(auth.RequireAuth(s.db, s.secret, tv, s.demoReadOnly(noStoreMux(mux))))))
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

// readJSONBody parsea el body JSON. Devuelve 0 si OK o, si falla, el status
// HTTP a responder: 400 (JSON inválido) o 413 (body > maxBodyBytes, issue
// #215). Con el writer real, http.MaxBytesReader además marca la conexión
// para cerrarla tras el 413 (el nil anterior devolvía 400 invalid_body).
func readJSONBody(w http.ResponseWriter, r *http.Request, dst any) int {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return http.StatusRequestEntityTooLarge
		}
		return http.StatusBadRequest
	}
	return 0
}

// bodyError mapea el status devuelto por readJSONBody al código del envelope:
// 413 → payload_too_large (paridad con la ingesta); cualquier otro fallo →
// el código que usaba el caller (invalid_body/invalid_json/invalid_input).
func bodyError(status int, fallback string) string {
	if status == http.StatusRequestEntityTooLarge {
		return "payload_too_large"
	}
	return fallback
}

// writeBodyError responde el fallo de readJSONBody: 413 sin mensaje extra
// (el body no se pudo leer), 400 con el código y mensaje del caller.
func writeBodyError(w http.ResponseWriter, status int, fallback, message string) {
	if status == http.StatusRequestEntityTooLarge {
		writeError(w, status, "payload_too_large")
		return
	}
	if message != "" {
		writeError(w, status, fallback, message)
		return
	}
	writeError(w, status, fallback)
}

func (s *server) handleConfigBackupList(w http.ResponseWriter, r *http.Request) {
	if s.configBackup == nil {
		writeError(w, http.StatusNotFound, "config_backup_disabled")
		return
	}
	routerID := r.URL.Query().Get("router")
	var snaps []configbackup.Snapshot
	var err error
	if routerID != "" {
		snaps, err = s.configBackup.List(routerID)
	} else {
		snaps, err = s.configBackup.ListAll()
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if snaps == nil {
		snaps = []configbackup.Snapshot{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"snapshots": snaps})
}

func (s *server) handleConfigBackupUpload(w http.ResponseWriter, r *http.Request) {
	if s.configBackup == nil {
		writeError(w, http.StatusNotFound, "config_backup_disabled")
		return
	}
	routerID := r.Header.Get("X-Router-ID")
	snapshotID := r.Header.Get("X-Snapshot-ID")
	configs := r.Header.Get("X-Configs")
	if routerID == "" || snapshotID == "" {
		writeError(w, http.StatusBadRequest, "missing X-Router-ID or X-Snapshot-ID header")
		return
	}
	token := bearerToken(r)
	if token == "" || !s.checkAgentToken(routerID, token) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "read body: "+err.Error())
		return
	}
	if len(data) == 0 {
		writeError(w, http.StatusBadRequest, "empty body")
		return
	}
	if execToken := r.Header.Get("X-Executor-Token"); execToken != "" {
		if _, err := s.db.Exec(
			`INSERT INTO kv (key, value) VALUES (?, ?)
			 ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
			"netgrip.executor_token."+routerID, execToken); err != nil {
			writeError(w, http.StatusInternalServerError, "store executor token")
			return
		}
	}
	if err := s.configBackup.Save(routerID, snapshotID, configs, data); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "saved"})
}

func (s *server) handleConfigBackupDownload(w http.ResponseWriter, r *http.Request) {
	if s.configBackup == nil {
		writeError(w, http.StatusNotFound, "config_backup_disabled")
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	data, snap, err := s.configBackup.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "snapshot not found")
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="netgrip-snapshot-%s-%s.tar.gz"`, snap.RouterID, snap.SnapshotID))
	w.Write(data)
}

func (s *server) handleConfigBackupDelete(w http.ResponseWriter, r *http.Request) {
	if s.configBackup == nil {
		writeError(w, http.StatusNotFound, "config_backup_disabled")
		return
	}
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.configBackup.Delete(id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
