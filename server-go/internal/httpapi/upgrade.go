// upgrade.go — Fase 6.3 (issue #243): self-update del agente usando el binario
// que ya sirve el servidor (GET /api/agents/{slug}/binary).
//
//	POST /api/agents/{slug}/upgrade        — admin: envía el comando "upgrade"
//	                                         al agente vía SSE para que descargue
//	                                         el binario embebido, lo intercambie
//	                                         y reinicie su servicio procd.
//	POST /api/agents/{slug}/upgrade-result — el agente reporta el resultado
//	                                         (auth por token de agente).
//	POST /api/agents/upgrade-all           — admin (#251): envía "upgrade" a
//	                                         todos los agentes con update
//	                                         disponible y devuelve el resultado
//	                                         por slug.
package httpapi

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gnacho/netpulse/server-go/internal/agentbin"
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

// upgradeAllItem es el resultado por slug de POST /api/agents/upgrade-all.
type upgradeAllItem struct {
	Slug     string `json:"slug"`
	Status   string `json:"status"` // "sent" | "not_connected" | "up_to_date"
	Version  string `json:"version,omitempty"`
	Upgraded bool   `json:"upgraded"`
}

// handleAgentsUpgradeAll (#251): envía "upgrade" a todos los agentes que
// reportan una versión distinta de la embebida (updateAvailable) y están
// conectados por SSE. Devuelve el resultado por slug para que la UI muestre
// cuáles quedaron pendientes.
func (s *server) handleAgentsUpgradeAll(w http.ResponseWriter, r *http.Request) {
	if s.agentHub == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "SSE agentHub no configurado")
		return
	}
	slugs, err := s.registeredAgentSlugs()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	out := []upgradeAllItem{}
	sent := 0
	for _, slug := range slugs {
		item := upgradeAllItem{Slug: slug}
		if s.agents != nil {
			if _, version, ok := s.agents.Info(slug); ok {
				item.Version = version
				// Solo agentes nativos OpenWrt: los de type managed-switch/
				// external son scrapers que no entienden el comando "upgrade".
				if !s.agentUpgradeable(slug) {
					item.Status = "not_openwrt"
				} else if version != "" && version != agentbin.EmbeddedAgentVersion {
					if s.agentHub.Send(slug, "upgrade", map[string]any{}) {
						item.Status = "sent"
						item.Upgraded = true
						sent++
					} else {
						item.Status = "not_connected"
					}
				} else {
					item.Status = "up_to_date"
				}
			} else {
				item.Status = "not_connected"
			}
		} else {
			item.Status = "not_connected"
		}
		out = append(out, item)
	}
	log.Printf("[netpulse] upgrade-all: %d/%d agentes con upgrade enviado", sent, len(out))
	writeJSON(w, http.StatusOK, map[string]any{
		"agents":  out,
		"sent":    sent,
		"total":   len(out),
		"message": fmt.Sprintf("upgrade enviado a %d de %d agentes", sent, len(out)),
	})
}

// registeredAgentSlugs devuelve los slugs con token registrado en kv
// (misma fuente que handleAgentsList).
func (s *server) registeredAgentSlugs() ([]string, error) {
	if s.db == nil {
		return nil, nil
	}
	rows, err := s.db.Query("SELECT key FROM kv WHERE key LIKE ? ORDER BY key", agentTokenKeyPrefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var key string
		if rows.Scan(&key) != nil {
			continue
		}
		out = append(out, strings.TrimPrefix(key, agentTokenKeyPrefix))
	}
	return out, nil
}
