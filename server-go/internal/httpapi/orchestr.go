// orchestr.go — Fase 10: endpoints de plan/apply/audit (solo admin).
//
//	POST   /api/plans              — crear plan (routerId + resource + diff)
//	GET    /api/plans/{id}         — ver plan + diff + estado + resultado
//	POST   /api/plans/{id}/apply   — aplicar (envía Ops al agente vía SSE)
//	POST   /api/agents/{slug}/apply-result — el agente reporta el resultado
//	GET    /api/audit              — log de auditoría (últimos N)
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/gnacho/netpulse/agent/executor"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/orchestr"
)

// registerOrchestrRoutes registra las rutas de orquestación (solo admin).
func (s *server) registerOrchestrRoutes(mux *http.ServeMux, mgr *orchestr.Manager) {
	if mgr == nil {
		return
	}

	mux.Handle("POST /api/plans", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			RouterID string             `json:"routerId"`
			Resource string             `json:"resource"`
			Diff     []executor.Op      `json:"diff"`
			Desired  json.RawMessage    `json:"desired"`
		}
		if !readJSONBody(r, &body) || body.RouterID == "" || body.Resource == "" {
			writeError(w, http.StatusBadRequest, "invalid_body",
				`Se esperaba { "routerId": "...", "resource": "adguard", "desired": {...} }`)
			return
		}
		// Si no hay diff explícito, calcularlo desde desired vía el módulo.
		diff := body.Diff
		if len(diff) == 0 && len(body.Desired) > 0 {
			computed, _, err := orchestr.ModuleDiff(body.Resource, body.Desired)
			if err != nil {
				writeError(w, http.StatusBadRequest, "unknown_module", err.Error())
				return
			}
			diff = computed
		}
		user := auth.UserFromContext(r.Context())
		username := ""
		if user != nil {
			username = user.Username
		}
		plan, err := mgr.CreatePlan(body.RouterID, body.Resource, body.Desired, diff, username)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "plan_error")
			return
		}
		writeJSON(w, http.StatusCreated, plan)
	})))

	mux.Handle("GET /api/plans/{id}", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		plan, err := mgr.GetPlan(r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		writeJSON(w, http.StatusOK, plan)
	})))

	mux.Handle("POST /api/plans/{id}/apply", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		plan, err := mgr.GetPlan(id)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		if plan.Status != "pending" {
			writeError(w, http.StatusConflict, "plan_not_pending", "El plan ya fue aplicado o está en curso")
			return
		}
		if s.agentHub == nil {
			writeError(w, http.StatusServiceUnavailable, "no_agent_hub")
			return
		}
		// Enviar Ops al agente vía SSE.
		applyData, _ := json.Marshal(map[string]any{"plan_id": id, "ops": plan.Diff})
		sent := s.agentHub.Send(plan.RouterID, "apply", json.RawMessage(applyData))
		if !sent {
			writeError(w, http.StatusServiceUnavailable, "agent_not_connected",
				"El agente no está conectado vía SSE")
			return
		}
		mgr.SetApplying(id)
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "applying", "planId": id})
	})))

	// El agente reporta el resultado del apply. Auth por token de agente
	// (Bearer, mismo que ingesta — ya validado por RequireAuth bypass).
	mux.HandleFunc("POST /api/agents/{slug}/apply-result", func(w http.ResponseWriter, r *http.Request) {
		// Auth: Bearer token del agente (validado por checkAgentToken en el
		// middleware o aquí). El middleware RequireAuth ya deja pasar
		// /api/agents/{slug}/apply-result si lo añadimos al bypass, pero
		// como el agente ya tiene token, usamos el mismo patrón que binary.
		slug := r.PathValue("slug")
		token := bearerToken(r)
		if !s.checkAgentToken(slug, token) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var body struct {
			PlanID string                 `json:"planId"`
			Result executor.ApplyResult  `json:"result"`
		}
		if !readJSONBody(r, &body) || body.PlanID == "" {
			writeError(w, http.StatusBadRequest, "invalid_body")
			return
		}
		if err := mgr.SetResult(body.PlanID, body.Result); err != nil {
			writeError(w, http.StatusInternalServerError, "update_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	})

	mux.Handle("GET /api/audit", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entries, err := mgr.RecentAudit(50)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "audit_error")
			return
		}
		if entries == nil {
			entries = []orchestr.AuditEntry{}
		}
		writeJSON(w, http.StatusOK, entries)
	})))
}

func bearerToken(r *http.Request) string {
	const prefix = "Bearer "
	t := r.Header.Get("Authorization")
	if len(t) > len(prefix) {
		return t[len(prefix):]
	}
	return ""
}
