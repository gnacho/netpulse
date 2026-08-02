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
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/security"
	"github.com/gnacho/netpulse/server-go/internal/sse"
	"github.com/gnacho/netpulse/server-go/internal/staticspa"
	"github.com/gnacho/netpulse/server-go/internal/updater"
)

// Version es la versión del backend (app.js:18).
const Version = "2.1.0"

// Deps son las dependencias del servidor API (como createApp de app.js).
type Deps struct {
	Config  *config.Config
	DB      *db.DB
	Adapter adapters.Snapshotter
	Hub     *sse.Hub
	Secret  string
	Static  *staticspa.Handler
	Updater *updater.Updater // nil → sin rutas /api/update/*
	// LastOverview devuelve el último overview del poller (nil si aún no hay).
	LastOverview func() *adapters.Overview
	Started      time.Time
}

type server struct {
	cfg     *config.Config
	db      *db.DB
	adapter adapters.Snapshotter
	hub     *sse.Hub
	secret  string
	lastOv  func() *adapters.Overview
	started time.Time
}

// NewHandler ensambla el handler HTTP completo (API + estáticos + SPA).
func NewHandler(d Deps) http.Handler {
	s := &server{
		cfg: d.Config, db: d.DB, adapter: d.Adapter, hub: d.Hub,
		secret: d.Secret, lastOv: d.LastOverview, started: d.Started,
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
	mux.HandleFunc("GET /api/topology", s.handleTopology)
	mux.HandleFunc("GET /api/dawn", s.handleDawn)
	mux.HandleFunc("GET /api/adguard/clients", s.handleAdguardClients)

	// --- Users ---
	// Idioma propio: FUERA del gate admin (se registra antes en Node).
	mux.HandleFunc("PUT /api/users/me/language", s.handleMyLanguage)
	// Resto: solo admin.
	mux.Handle("GET /api/users", auth.RequireAdmin(http.HandlerFunc(s.handleListUsers)))
	mux.Handle("POST /api/users", auth.RequireAdmin(http.HandlerFunc(s.handleCreateUser)))
	mux.Handle("PUT /api/users/{id}/password", auth.RequireAdmin(http.HandlerFunc(s.handleSetPassword)))
	mux.Handle("PUT /api/users/{id}/role", auth.RequireAdmin(http.HandlerFunc(s.handleSetRole)))
	mux.Handle("DELETE /api/users/{id}", auth.RequireAdmin(http.HandlerFunc(s.handleDeleteUser)))

	// --- Config (sesión; /api/config/adguard solo admin) ---
	s.registerConfigRoutes(mux)

	// --- Update (solo admin; ausente si no hay updater, p.ej. en tests) ---
	if d.Updater != nil {
		s.registerUpdateRoutes(mux, d.Updater)
	}

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

	return security.Middleware(auth.RequireAuth(s.db, s.secret, noStoreMux(mux)))
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

// readJSONBody parsea el body JSON; devuelve false si no es JSON válido
// (equivalente a c.req.json().catch(() => null)).
func readJSONBody(r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		return false
	}
	return true
}
