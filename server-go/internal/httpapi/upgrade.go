// upgrade.go — Fase 6.3 (issue #243): self-update del agente usando el binario
// que ya sirve el servidor (GET /api/agents/{slug}/binary).
//
//	POST /api/agents/{slug}/upgrade        — admin: envía el comando "upgrade"
//	                                         al agente vía SSE para que descargue
//	                                         el binario embebido, lo intercambie
//	                                         y reinicie su servicio procd.
//	POST /api/agents/{slug}/upgrade-result — el agente reporta el resultado
//	                                         (auth por token de agente).
package httpapi

import (
	"log"
	"net/http"
)

// upgradeResult es el cuerpo que el agente envía a upgrade-result tras
// intentar el auto-upgrade.
type upgradeResult struct {
	Slug  string `json:"slug"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// handleAgentUpgrade (admin): ordena a un agente conectado que se actualice
// descargando el binario embebido del propio servidor.
//
//	Códigos: 404 slug desconocido (sin token), 409 agente no conectado por SSE,
//	         202 comando enviado, 503 sin AgentHub.
func (s *server) handleAgentUpgrade(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !agentSlugRe.MatchString(slug) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if s.agentHub == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "SSE agentHub no configurado")
		return
	}
	// 404 si el slug no tiene token registrado (agente desconocido).
	if !s.agentTokenExists(slug) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	// El agente conoce su propia arquitectura (runtime.GOARCH); no hace falta
	// pasársela. Data vacía {}.
	if !s.agentHub.Send(slug, "upgrade", map[string]any{}) {
		writeError(w, http.StatusConflict, "agent_not_connected",
			"el agente "+slug+" no está conectado por SSE")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

// handleAgentUpgradeResult: el agente reporta el resultado del auto-upgrade.
// Auth por token de agente (Bearer), igual que binary/stream/apply-result.
func (s *server) handleAgentUpgradeResult(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	token := bearerToken(r)
	if !s.checkAgentToken(slug, token) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body upgradeResult
	if !readJSONBody(r, &body) {
		writeError(w, http.StatusBadRequest, "invalid_body")
		return
	}
	if body.OK {
		log.Printf("[netpulse] upgrade: agente %s actualizado correctamente", slug)
	} else {
		log.Printf("[netpulse] upgrade: agente %s falló: %s", slug, body.Error)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// agentTokenExists informa de si hay un token registrado para el slug
// (independientemente de si el agente ha empujado o no).
func (s *server) agentTokenExists(slug string) bool {
	if s.db == nil {
		return false
	}
	var value string
	err := s.db.QueryRow("SELECT value FROM kv WHERE key = ?", agentTokenKey(slug)).Scan(&value)
	return err == nil
}
