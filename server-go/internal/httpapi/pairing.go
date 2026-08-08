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
	"fmt"
	"net/http"

	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/db"
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
	writeJSON(w, http.StatusOK, map[string]string{"token": tok, "server_fp": s.serverFP})
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
	Slug    string `json:"slug"`
	Token   string `json:"token"`
	ServerFP string `json:"server_fp"`
}

// handleAgentPair (POST /api/agents/pair): valida el pairing token y crea un
// agente nuevo. No requiere sesión (el pairing token es la auth). Rate limited.
func (s *server) handleAgentPair(w http.ResponseWriter, r *http.Request) {
	ip := auth.ClientIP(r)
	if ok, _ := s.ingestLimit.allow(ip); !ok {
		writeError(w, http.StatusTooManyRequests, "rate_limited")
		return
	}

	var body pairRequest
	if !readJSONBody(r, &body) || !agentSlugRe.MatchString(body.Slug) || body.PairingToken == "" {
		writeError(w, http.StatusBadRequest, "invalid_body",
			`Se esperaba { "pairing_token": "...", "slug": "<equipo>" }`)
		return
	}

	// Validar pairing token contra el almacenado.
	stored, err := s.getPairingToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "pairing_error")
		return
	}
	if stored == "" || subtle.ConstantTimeCompare([]byte(body.PairingToken), []byte(stored)) != 1 {
		writeError(w, http.StatusUnauthorized, "invalid_pairing_token")
		return
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
		ServerFP: s.serverFP,
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
