package httpapi

// FORK: Settings > HTTPS - turning HTTPS on, choosing what plain HTTP may
// still do, and handing out the private CA's root. See internal/tlsmode.
//
//	GET  /api/settings/https          (admin) state, and the agents still on HTTP
//	POST /api/settings/https          (admin, password) {enabled?, mode?, force?}
//	POST /api/settings/https/confirm  (admin, over HTTPS) {code}
//	GET  /netpulse-ca.crt | .pem      (anyone) the root, for devices to install

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/security"
	"github.com/gnacho/netpulse/server-go/internal/tlsmode"
)

type agentTransport struct {
	TLS   bool
	At    time.Time
	saved time.Time // when it was last written to kv
}

const (
	agentTransportPrefix = "agent.transport."
	// An agent silent for longer than this no longer counts as "still on
	// HTTP": it is gone, or will show up again when it next reports.
	agentTransportWindow = 24 * time.Hour
	// Written to kv when the transport changes, and at most this often
	// otherwise - not on every push.
	agentTransportSaveEvery = time.Hour
)

// noteAgentTransport records how an agent's push arrived. It is kept in kv
// too: the check before refusing plain HTTP to agents must not forget them
// because the server restarted.
func (s *server) noteAgentTransport(slug string, overTLS bool) {
	now := time.Now()
	t := agentTransport{TLS: overTLS, At: now}
	if v, ok := s.agentTransport.Load(slug); ok {
		prev := v.(agentTransport)
		t.saved = prev.saved
		if prev.TLS != overTLS {
			t.saved = time.Time{}
		}
	}
	if s.db != nil && now.Sub(t.saved) >= agentTransportSaveEvery {
		v := "http"
		if overTLS {
			v = "tls"
		}
		if _, err := s.db.Exec(
			"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value",
			agentTransportPrefix+slug, v+" "+strconv.FormatInt(now.Unix(), 10)); err == nil {
			t.saved = now
		}
	}
	s.agentTransport.Store(slug, t)
}

// agentOnHTTP is an agent and the last time it reported.
type agentOnHTTP struct {
	Slug     string    `json:"slug"`
	LastSeen time.Time `json:"lastSeen"`
}

// agentsByTransport lists the agents whose last push came over TLS (tls) or
// over plain HTTP (!tls): recent ones, that still exist.
func (s *server) agentsByTransport(tls bool) []agentOnHTTP {
	last := map[string]agentTransport{}
	if s.db != nil {
		rows, err := s.db.Query("SELECT key, value FROM kv WHERE key LIKE ?", agentTransportPrefix+"%")
		if err == nil {
			for rows.Next() {
				var k, v string
				if rows.Scan(&k, &v) != nil {
					continue
				}
				kind, ts, _ := strings.Cut(v, " ")
				sec, err := strconv.ParseInt(ts, 10, 64)
				if err != nil {
					continue
				}
				last[strings.TrimPrefix(k, agentTransportPrefix)] = agentTransport{TLS: kind == "tls", At: time.Unix(sec, 0)}
			}
			rows.Close()
		}
	}
	s.agentTransport.Range(func(k, v any) bool {
		last[k.(string)] = v.(agentTransport)
		return true
	})
	// Never nil: the page reads the list's length, and a JSON null there
	// took the whole Settings page down.
	out := []agentOnHTTP{}
	for slug, t := range last {
		if t.TLS != tls || time.Since(t.At) > agentTransportWindow || !s.agentExists(slug) {
			continue
		}
		out = append(out, agentOnHTTP{Slug: slug, LastSeen: t.At})
	}
	slices.SortFunc(out, func(a, b agentOnHTTP) int { return strings.Compare(a.Slug, b.Slug) })
	return out
}

func (s *server) agentsOnHTTP() []agentOnHTTP { return s.agentsByTransport(false) }

// agentExists: a deleted agent's token is gone.
func (s *server) agentExists(slug string) bool {
	if s.db == nil {
		return true
	}
	var v string
	return s.db.QueryRow("SELECT value FROM kv WHERE key = ?", agentTokenKey(slug)).Scan(&v) == nil
}

// hsts decides the Strict-Transport-Security header (see security.Middleware:
// it is per host, not per port).
func (s *server) hsts(r *http.Request) string {
	secure := auth.IsSecureRequest(r)
	// Once the private CA exists the manager decides - with HTTPS off too,
	// when the plain port's TLS side answers max-age=0.
	if s.tlsMgr != nil && s.tlsMgr.Available() && s.tlsMgr.CA() != nil {
		return s.tlsMgr.HSTS(secure)
	}
	switch {
	case r.TLS != nil && s.cfg != nil && s.cfg.Onbox:
		// On-box, PORT itself is TLS: no plain port is left to break.
		return security.HSTS
	case r.TLS == nil && secure:
		// Behind a TLS-terminating proxy the server trusts, which serves
		// every port of the name.
		return security.HSTS
	default:
		// The extra TLS listener while PORT still serves plain HTTP: a
		// policy from it would make browsers rewrite http://name:PORT to
		// https://name:PORT, where nothing speaks TLS.
		return ""
	}
}

func (s *server) registerHTTPS(mux *http.ServeMux) {
	if s.tlsMgr == nil {
		return
	}
	mux.Handle("GET /api/settings/https", auth.RequireAdmin(http.HandlerFunc(s.handleHTTPSStatus)))
	mux.Handle("POST /api/settings/https", auth.RequireAdmin(http.HandlerFunc(s.handleHTTPSChange)))
	mux.Handle("POST /api/settings/https/confirm", auth.RequireAdmin(http.HandlerFunc(s.handleHTTPSConfirm)))
	mux.HandleFunc("GET /netpulse-ca.crt", s.handleCARoot(false))
	mux.HandleFunc("GET /netpulse-ca.pem", s.handleCARoot(true))
}

type httpsStatus struct {
	tlsmode.Status
	AgentsOnHTTP []agentOnHTTP `json:"agentsOnHttp"`
}

func (s *server) handleHTTPSStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, httpsStatus{Status: s.tlsMgr.Status(), AgentsOnHTTP: s.agentsOnHTTP()})
}

// handleHTTPSChange turns HTTPS on or off and requests a mode. Every change
// asks for the admin's password again: transport security is not something
// a stolen session cookie alone should be able to change, in either
// direction.
func (s *server) handleHTTPSChange(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFromContext(r.Context())
	var body struct {
		Enabled  *bool   `json:"enabled"`
		Mode     *string `json:"mode"`
		Force    bool    `json:"force"`
		Password string  `json:"password"`
	}
	if st := readJSONBody(w, r, &body); st != 0 {
		writeBodyError(w, st, "invalid_input", `expected { "enabled"?, "mode"?, "password" }`)
		return
	}
	// Counted as sign-in attempts: a stolen session must not be a way to
	// guess the password without the sign-in's lockout.
	if limited, retry := auth.LoginRateLimited(s.db, r); limited {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "rate_limited", "retryAfterSec": retry})
		return
	}
	if me == nil || !s.checkOwnPassword(me.ID, body.Password) {
		auth.RegisterLoginFail(s.db, r)
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "the password is not correct")
		return
	}
	by := me.Username + " from " + auth.ClientIP(r)
	if body.Enabled != nil && !*body.Enabled && !body.Force {
		// Agents already moved to HTTPS pin its key and have no way back to
		// plain HTTP by themselves: turning it off cuts them off.
		if moved := s.agentsByTransport(true); len(moved) > 0 && s.tlsMgr.Enabled() {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":   "agents_on_https",
				"message": "these agents report over HTTPS and would stop reporting",
				"agents":  moved,
			})
			return
		}
	}
	if body.Enabled != nil {
		if err := s.tlsMgr.SetEnabled(*body.Enabled, by); err != nil {
			writeHTTPSError(w, err)
			return
		}
	}
	resp := map[string]any{}
	if body.Mode != nil {
		mode, err := tlsmode.ParseMode(*body.Mode)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_input", err.Error())
			return
		}
		if mode == tlsmode.Redirect && !body.Force {
			if left := s.agentsOnHTTP(); len(left) > 0 {
				writeJSON(w, http.StatusConflict, map[string]any{
					"error":   "agents_on_http",
					"message": "these agents still report over plain HTTP and would stop reporting",
					"agents":  left,
				})
				return
			}
		}
		code, expires, err := s.tlsMgr.RequestMode(mode, by)
		if err != nil {
			writeHTTPSError(w, err)
			return
		}
		if code != "" {
			resp["pending"] = mode
			resp["expires"] = expires
			resp["confirmUrl"] = s.confirmURL(r, code)
		}
	}
	resp["status"] = s.tlsMgr.Status()
	writeJSON(w, http.StatusOK, resp)
}

// confirmURL is the HTTPS page the change must be confirmed from.
func (s *server) confirmURL(r *http.Request, code string) string {
	u := &url.URL{Path: "/settings", RawQuery: url.Values{"tab": {"https"}, "confirm": {code}}.Encode()}
	req := r.Clone(r.Context())
	req.URL = u
	if target, ok := s.tlsMgr.RedirectTarget(req); ok {
		return target
	}
	return u.String()
}

func (s *server) handleHTTPSConfirm(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFromContext(r.Context())
	var body struct {
		Code string `json:"code"`
	}
	if st := readJSONBody(w, r, &body); st != 0 {
		writeBodyError(w, st, "invalid_input", `expected { "code": "..." }`)
		return
	}
	by := "unknown"
	if me != nil {
		by = me.Username + " from " + auth.ClientIP(r)
	}
	if _, err := s.tlsMgr.Confirm(body.Code, auth.IsSecureRequest(r), by); err != nil {
		writeHTTPSError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": s.tlsMgr.Status()})
}

func writeHTTPSError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, tlsmode.ErrLocked):
		writeError(w, http.StatusConflict, "locked", err.Error())
	case errors.Is(err, tlsmode.ErrNotFull):
		writeError(w, http.StatusConflict, "not_full", err.Error())
	case errors.Is(err, tlsmode.ErrConfirm):
		writeError(w, http.StatusGone, "expired", err.Error())
	default:
		writeError(w, http.StatusBadRequest, "https_error", err.Error())
	}
}

// checkOwnPassword checks the signed-in user's password, as changing it does.
func (s *server) checkOwnPassword(userID int64, password string) bool {
	if password == "" {
		return false
	}
	var hash string
	if err := s.db.QueryRow("SELECT pass_hash FROM users WHERE id = ?", userID).Scan(&hash); err != nil {
		return false
	}
	return auth.CheckPassword(password, hash)
}

// handleCARoot serves the root certificate. It is public by nature - devices
// need it before they can trust HTTPS - and the page that links it tells the
// admin to compare its fingerprint with the one the server logged.
func (s *server) handleCARoot(asPEM bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		ca := s.tlsMgr.CA()
		if ca == nil || !s.tlsMgr.Enabled() {
			http.NotFound(w, nil)
			return
		}
		if asPEM {
			w.Header().Set("Content-Type", "application/x-pem-file")
			w.Header().Set("Content-Disposition", `attachment; filename="netpulse-ca.pem"`)
			_, _ = w.Write(ca.RootPEM())
			return
		}
		// DER with this type is what Android and iOS offer to install.
		w.Header().Set("Content-Type", "application/x-x509-ca-cert")
		w.Header().Set("Content-Disposition", `attachment; filename="netpulse-ca.crt"`)
		_, _ = w.Write(ca.RootDER())
	}
}
