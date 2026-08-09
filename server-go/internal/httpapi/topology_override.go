// topology_override.go — API admin de overrides manuales de topología
// (issue #142, Fase A): CRUD sobre la tabla topology_overrides.
package httpapi

import (
	"net/http"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/topooverride"
)

// registerTopologyOverrideRoutes registra las rutas admin de overrides:
//   - GET    /api/topology/overrides          → lista
//   - POST   /api/topology/overrides          → crear (upsert por MAC)
//   - PUT    /api/topology/overrides/{id}     → actualizar (id = MAC)
//   - DELETE /api/topology/overrides/{id}     → borrar
func (s *server) registerTopologyOverrideRoutes(mux *http.ServeMux) {
	mux.Handle("GET /api/topology/overrides", auth.RequireAdmin(http.HandlerFunc(s.handleTopologyOverrideList)))
	mux.Handle("POST /api/topology/overrides", auth.RequireAdmin(http.HandlerFunc(s.handleTopologyOverrideCreate)))
	mux.Handle("PUT /api/topology/overrides/{id}", auth.RequireAdmin(http.HandlerFunc(s.handleTopologyOverrideUpdate)))
	mux.Handle("DELETE /api/topology/overrides/{id}", auth.RequireAdmin(http.HandlerFunc(s.handleTopologyOverrideDelete)))
}

func (s *server) handleTopologyOverrideList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"overrides": topooverride.List(s.db.DB)})
}

func (s *server) handleTopologyOverrideCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MAC    string `json:"mac"`
		Kind   string `json:"kind"`
		Name   string `json:"name"`
		Parent string `json:"parent"`
	}
	if !readJSONBody(r, &body) {
		writeError(w, http.StatusBadRequest, "invalid_body")
		return
	}
	body.MAC = adapters.NormalizeMAC(body.MAC)
	if body.MAC == "" {
		writeError(w, http.StatusBadRequest, "invalid_mac")
		return
	}
	if body.Kind != "hypervisor" && body.Kind != "switch" && body.Kind != "attach" {
		writeError(w, http.StatusBadRequest, "invalid_kind")
		return
	}
	if body.Kind == "attach" && adapters.NormalizeMAC(body.Parent) == "" {
		writeError(w, http.StatusBadRequest, "attach_requires_parent")
		return
	}
	if body.Kind != "attach" {
		body.Parent = ""
	}
	o, err := topooverride.Add(s.db.DB, topooverride.AddInput{
		MAC: body.MAC, Kind: body.Kind, Name: body.Name, Parent: body.Parent,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"override": o})
}

func (s *server) handleTopologyOverrideUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Kind    *string `json:"kind"`
		Name    *string `json:"name"`
		Parent  *string `json:"parent"`
		Enabled *bool   `json:"enabled"`
	}
	if !readJSONBody(r, &body) {
		writeError(w, http.StatusBadRequest, "invalid_body")
		return
	}
	if body.Kind != nil && *body.Kind != "hypervisor" && *body.Kind != "switch" && *body.Kind != "attach" {
		writeError(w, http.StatusBadRequest, "invalid_kind")
		return
	}
	o, ok := topooverride.Update(s.db.DB, id, topooverride.UpdateInput{
		Kind: body.Kind, Name: body.Name, Parent: body.Parent, Enabled: body.Enabled,
	})
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"override": o})
}

func (s *server) handleTopologyOverrideDelete(w http.ResponseWriter, r *http.Request) {
	if !topooverride.Remove(s.db.DB, r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
