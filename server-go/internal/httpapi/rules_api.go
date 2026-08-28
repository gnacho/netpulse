package httpapi

import (
	"net/http"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

func (s *server) handleAlertRulesList(w http.ResponseWriter, _ *http.Request) {
	rules, err := alerts.ListRules(s.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

func (s *server) handleAlertRulesGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rule, err := alerts.GetRule(s.db, id)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (s *server) handleAlertRulesCreate(w http.ResponseWriter, r *http.Request) {
	var rule alerts.Rule
	if st := readJSONBody(w, r, &rule); st != 0 {
		writeBodyError(w, st, "invalid_body", "body JSON inválido")
		return
	}
	if err := validateRule(rule); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_rule", err.Error())
		return
	}
	if err := alerts.CreateRule(s.db, rule); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"ok": true})
}

func (s *server) handleAlertRulesUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var rule alerts.Rule
	if st := readJSONBody(w, r, &rule); st != 0 {
		writeBodyError(w, st, "invalid_body", "body JSON inválido")
		return
	}
	rule.ID = id
	if err := validateRule(rule); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_rule", err.Error())
		return
	}
	if err := alerts.UpdateRule(s.db, rule); err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) handleAlertRulesDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := alerts.DeleteRule(s.db, id); err != nil {
		writeError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func validateRule(r alerts.Rule) error {
	if r.Name == "" {
		return &validationError{"nombre requerido"}
	}
	if !alerts.IsCategory(r.Category) {
		return &validationError{"categoría inválida: " + r.Category}
	}
	validOps := map[string]bool{"gt": true, "gte": true, "lt": true, "lte": true, "eq": true}
	if !validOps[r.Condition.Operator] {
		return &validationError{"operador inválido: " + r.Condition.Operator}
	}
	if r.Condition.Metric == "" {
		return &validationError{"métrica requerida"}
	}
	if r.Condition.Duration <= 0 {
		return &validationError{"duración debe ser > 0"}
	}
	validScopes := map[string]bool{"global": true, "router": true}
	if !validScopes[r.Scope.Type] {
		return &validationError{"scope inválido: " + r.Scope.Type}
	}
	if r.Scope.Type == "router" && len(r.Scope.RouterIDs) == 0 {
		return &validationError{"scope=router requiere al menos un routerId"}
	}
	validSeverities := map[string]bool{"info": true, "warn": true, "critical": true}
	if !validSeverities[r.Severity] {
		return &validationError{"severidad inválida: " + r.Severity}
	}
	return nil
}

type validationError struct{ msg string }

func (e *validationError) Error() string { return e.msg }
