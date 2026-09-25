// pairing.go — Fase 9 R3: pairing/adopción cero-fricción de agentes.
//
// El servidor on-box genera un pairing token (UUID) en el primer arranque,
// visible en la UI de Ajustes > Adopción y en el log. Al instalar un agente
// nuevo, el admin proporciona el pairing token + el server_fp (ambos visibles
// en la UI). El agente contacta POST /api/agents/pair con ambos; el servidor
// valida el pairing token, crea el agente (slug + token) y devuelve el token
// real del agente + el server_fp para que lo pinee.
//
// POST /api/agents/pair NO requiere sesión (el pairing token ES la auth) pero
// lleva rate limit por IP (mismo que la ingesta, 30/min).
//
// GET  /api/pairing/token   (admin) — ver el pairing token actual.
// POST /api/pairing/rotate  (admin) — rotar el pairing token (invalida el viejo).
package httpapi

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/gnacho/netpulse/agent/pairproof"

	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/tlsmode"
)

const pairingTokenKey = "pairing.token"

// newPairingToken genera un UUID v4 como string (crypto/rand).
func newPairingToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

// getPairingToken lee el pairing token actual del kv. Si no existe, lo genera.
func (s *server) getPairingToken() (string, error) {
	var existing string
	err := s.db.QueryRow("SELECT value FROM kv WHERE key = ?", pairingTokenKey).Scan(&existing)
	if err == nil && existing != "" {
		return existing, nil
	}
	// Generar uno nuevo si no existe (primer arranque o migración).
	tok, err := newPairingToken()
	if err != nil {
		return "", err
	}
	if _, err := s.db.Exec(
		"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO NOTHING",
		pairingTokenKey, tok); err != nil {
		return "", err
	}
	return tok, nil
}

// handlePairingToken (GET /api/pairing/token): devuelve el pairing token
// actual (admin only — el token permite adoptar agentes).
func (s *server) handlePairingToken(w http.ResponseWriter, _ *http.Request) {
	tok, err := s.getPairingToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "pairing_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": tok, "server_fp": s.fingerprint()})
}

// handlePairingRotate (POST /api/pairing/rotate): genera un pairing token
// nuevo, reemplazando el anterior. Los agentes ya paired siguen funcionando
// (su token de agente es independiente).
func (s *server) handlePairingRotate(w http.ResponseWriter, _ *http.Request) {
	tok, err := newPairingToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "token_error")
		return
	}
	if _, err := s.db.Exec(
		"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value",
		pairingTokenKey, tok); err != nil {
		writeError(w, http.StatusInternalServerError, "token_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": tok})
}

// pairRequest es el body de POST /api/agents/pair.
type pairRequest struct {
	PairingToken string `json:"pairing_token"`
	Slug         string `json:"slug"`
}

// pairResponse es lo que recibe el agente tras un pairing exitoso.
type pairResponse struct {
	Slug     string `json:"slug"`
	Token    string `json:"token"`
	ServerFP string `json:"server_fp"`
}

// handleAgentPair (POST /api/agents/pair): valida el pairing token y crea un
// agente nuevo. No requiere sesión (el pairing token es la auth). Rate limited.
//
// #367: además del pairing token de admin (crea o ROTA el agente del slug),
// acepta el token de alta de red del autoenroll cuando AGENT_AUTOENROLL=1.
// El token de red SOLO puede crear agentes para slugs que no existan: si el
// slug ya tiene token, responde 409 slug_taken (un router descubierto nunca
// puede suplantar ni rotar el agente de otro).
func (s *server) handleAgentPair(w http.ResponseWriter, r *http.Request) {
	ip := auth.ClientIP(r)
	if ok, _ := s.ingestLimit.allow(ip); !ok {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}

	var body pairRequest
	if st := readJSONBody(w, r, &body); st != 0 {
		writeBodyError(w, st, "invalid_body",
			`Se esperaba { "pairing_token": "...", "slug": "<equipo>" }`)
		return
	}
	if !agentSlugRe.MatchString(body.Slug) || body.PairingToken == "" {
		writeError(w, http.StatusBadRequest, "invalid_body",
			`Se esperaba { "pairing_token": "...", "slug": "<equipo>" }`)
		return
	}

	// Validar el token: primero el pairing token de admin (semántica
	// completa: crear o rotar), después el token de red del autoenroll
	// (solo creación de slugs nuevos).
	stored, err := s.getPairingToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "pairing_error")
		return
	}
	adminToken := stored != "" && subtle.ConstantTimeCompare([]byte(body.PairingToken), []byte(stored)) == 1
	// FORK: once plain HTTP is being retired, the admin pairing token - which
	// can create or rotate any agent - is not taken over it: pair over https,
	// where the agent proves the server's key first (pair/hello). Refusing
	// cannot un-send it, but keeps anything from relying on it.
	if adminToken && s.tlsMgr != nil && s.tlsMgr.Enabled() && s.tlsMgr.Mode() != tlsmode.Full && !auth.IsSecureRequest(r) {
		writeError(w, http.StatusForbidden, "use_https",
			"pair over https: this server no longer takes its pairing token over plain HTTP")
		return
	}
	if !adminToken {
		if !s.checkAutoenrollToken(body.PairingToken) {
			writeError(w, http.StatusUnauthorized, "invalid_pairing_token")
			return
		}
		var existing string
		_ = s.db.QueryRow(
			"SELECT COALESCE(value,'') FROM kv WHERE key = ?", agentTokenKey(body.Slug)).Scan(&existing)
		if existing != "" {
			writeError(w, http.StatusConflict, "slug_taken",
				"el slug ya tiene un agente; el autoenroll no puede rotarlo (renombra el equipo o usa el pairing token de admin)")
			return
		}
	}

	// Crear el agente (mismo flujo que POST /api/agents).
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

	writeJSON(w, http.StatusCreated, pairResponse{
		Slug:     body.Slug,
		Token:    token,
		ServerFP: s.fingerprint(),
	})
}

// handleAgentPairHello (POST /api/agents/pair/hello): FORK. Proves to an
// agent that the key this server serves is the real server's, so an agent
// given a pairing token but no pin can learn the pin without trusting
// whoever answers first. The reply is pairproof.MAC(pairing token, the
// agent's nonce, our fingerprint); the agent checks it against the chain it
// was shown (runtime.ProveServerKey). The pairing token itself is never sent
// to us here, and the reply is useless to anyone who does not hold it.
//
// Only the admin pairing token proves anything. The autoenroll token is
// handed out over UDP to whoever asks, so a MAC keyed with it would prove
// nothing about who answered.
func (s *server) handleAgentPairHello(w http.ResponseWriter, r *http.Request) {
	ip := auth.ClientIP(r)
	if ok, _ := s.ingestLimit.allow(ip); !ok {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}
	if r.TLS == nil {
		// Also the answer behind a TLS-terminating proxy: the key the agent
		// sees is the proxy's, which this server cannot vouch for.
		writeError(w, http.StatusBadRequest, "use_https",
			"the server's key can only be proven over a direct https connection to it; "+
				"behind a TLS-terminating proxy, give the agent NETPULSE_SERVER_FP instead")
		return
	}
	fp := s.fingerprint()
	if fp == "" {
		writeError(w, http.StatusConflict, "no_fingerprint",
			"this server has no key for agents to pin")
		return
	}
	var body struct {
		Nonce string `json:"nonce"`
	}
	if st := readJSONBody(w, r, &body); st != 0 {
		writeBodyError(w, st, "invalid_body", `expected { "nonce": "<hex>" }`)
		return
	}
	if b, err := hex.DecodeString(body.Nonce); err != nil || len(b) < 16 || len(b) > 64 {
		writeError(w, http.StatusBadRequest, "invalid_body", "nonce must be 16 to 64 random bytes in hex")
		return
	}
	tok, err := s.getPairingToken()
	if err != nil || tok == "" {
		writeError(w, http.StatusInternalServerError, "pairing_error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"server_fp": fp,
		"mac":       pairproof.MAC(tok, body.Nonce, fp),
	})
}

// EnsurePairingToken es llamado desde main.go en el primer arranque on-box
// para generar y loguear el pairing token.
func EnsurePairingToken(database *db.DB) (string, error) {
	var existing string
	err := database.QueryRow("SELECT value FROM kv WHERE key = ?", pairingTokenKey).Scan(&existing)
	if err == nil && existing != "" {
		return existing, nil
	}
	tok, err := newPairingToken()
	if err != nil {
		return "", err
	}
	if _, err := database.Exec(
		"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO NOTHING",
		pairingTokenKey, tok); err != nil {
		return "", err
	}
	return tok, nil
}
