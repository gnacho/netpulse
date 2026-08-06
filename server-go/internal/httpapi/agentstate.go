// agentstate.go — Persistencia del registry de agentes (Fase 8.2, R8).
//
// El registry vive en RAM; sin persistir, lastSeen/versión/payload de cada
// agente se pierden al reiniciar el servidor. Aquí se vuelca el último push
// a la tabla kv (clave "agent.state.<slug>") en cada ingesta y se restaura
// al arrancar. La BD (kv) es la única fuente de verdad del estado previo;
// el siguiente push del agente la refresca igualmente (self-healing).
package httpapi

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

// agentStateKeyPrefix: kv agent.state.<slug> = último push serializado.
const agentStateKeyPrefix = "agent.state."

// persistedAgentState es el formato en kv: el payload + cuándo llegó.
type persistedAgentState struct {
	Payload  *probe.Payload `json:"payload"`
	LastSeen int64          `json:"lastSeen"` // unix segundos
	Version  string         `json:"version"`
}

func agentStateKey(slug string) string { return agentStateKeyPrefix + slug }

// persistAgentState vuelca el último push del slug a kv. Best-effort: si la
// escritura falla se loguea y se sigue (el agente reintentará su push).
func (s *server) persistAgentState(slug string, st *adapters.AgentState) {
	if st == nil || st.Payload == nil {
		return
	}
	raw, err := json.Marshal(persistedAgentState{
		Payload:  st.Payload,
		LastSeen: st.LastSeen.Unix(),
		Version:  st.Version,
	})
	if err != nil {
		log.Printf("[netpulse] aviso: no se pudo serializar el estado del agente %s: %v", slug, err)
		return
	}
	_, err = s.db.Exec(
		"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value",
		agentStateKey(slug), string(raw),
	)
	if err != nil {
		log.Printf("[netpulse] aviso: no se pudo persistir el estado del agente %s: %v", slug, err)
	}
}

// NewStateRestorer devuelve una función que restaura en el registry los
// estados persistidos de kv (agent.state.<slug>). Se invoca en el arranque
// del servidor, antes de construir el handler (no hay server todavía).
func NewStateRestorer(d *db.DB) func(*adapters.AgentRegistry) {
	return func(reg *adapters.AgentRegistry) {
		rows, err := d.Query("SELECT key, value FROM kv WHERE key LIKE '" + agentStateKeyPrefix + "%'")
		if err != nil {
			log.Printf("[netpulse] aviso: no se pudieron restaurar los estados de agentes: %v", err)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var key, raw string
			if err := rows.Scan(&key, &raw); err != nil {
				continue
			}
			slug := strings.TrimPrefix(key, agentStateKeyPrefix)
			if slug == "" {
				continue
			}
			var ps persistedAgentState
			if err := json.Unmarshal([]byte(raw), &ps); err != nil || ps.Payload == nil {
				continue
			}
			reg.Restore(slug, &adapters.AgentState{
				Payload:  ps.Payload,
				LastSeen: time.Unix(ps.LastSeen, 0),
				Version:  ps.Version,
			})
		}
	}
}
