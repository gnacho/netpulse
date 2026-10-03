// racks.go — API REST del rack canvas bajo /api/racks (fase 1.5).
//
// Contrato wire en snake_case; el dominio (internal/rack) usa camelCase y el
// mapper de este fichero es la frontera explícita entre ambos. Lecturas para
// cualquier sesión (RequireAuth global); escrituras solo admin.
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/rack"
)

func (s *server) registerRackRoutes(mux *http.ServeMux) {
	if s.db == nil {
		return
	}
	mux.HandleFunc("GET /api/racks", s.handleRacksGet)
	mux.Handle("POST /api/racks", auth.RequireAdmin(http.HandlerFunc(s.handleRackCreate)))
	mux.Handle("PUT /api/racks/{id}", auth.RequireAdmin(http.HandlerFunc(s.handleRackUpdate)))
	mux.Handle("DELETE /api/racks/{id}", auth.RequireAdmin(http.HandlerFunc(s.handleRackDelete)))
	mux.Handle("PUT /api/racks/layout", auth.RequireAdmin(http.HandlerFunc(s.handleRackLayoutPut)))
	mux.Handle("POST /api/racks/cables", auth.RequireAdmin(http.HandlerFunc(s.handleRackCableAdd)))
	mux.Handle("DELETE /api/racks/cables/{id}", auth.RequireAdmin(http.HandlerFunc(s.handleRackCableDelete)))
	mux.Handle("PUT /api/racks/profiles/{mac}", auth.RequireAdmin(http.HandlerFunc(s.handleRackProfilePut)))
	mux.Handle("POST /api/racks/import-cables", auth.RequireAdmin(http.HandlerFunc(s.handleRackImportCables)))
}

// buildRackHints deriva candidatos de cable desde la DETECCIÓN (FDB): cada
// dispositivo cableado cuya MAC y la de su router están montados genera un
// hint con el puerto físico donde el bridge aprende su MAC. La fuente es el
// descubrimiento, no el dibujo.
func (s *server) buildRackHints(ctx context.Context, mounts []rack.MountRow) []rack.CableHint {
	mounted := map[string]bool{}
	for _, m := range mounts {
		if m.DeviceMAC != "" {
			mounted[strings.ToLower(m.DeviceMAC)] = true
		}
	}
	routerMAC := map[string]string{}
	if ov := s.lastOv(); ov != nil {
		for _, r := range ov.Routers {
			if r.MAC != "" {
				routerMAC[r.ID] = strings.ToLower(r.MAC)
			}
		}
	}
	// Honestidad de la inferencia: un puerto con VARIAS MACs aprendidas es un
	// agregado (switch/bridge/hipervisor detrás); cablearlo a un dispositivo
	// concreto sería inventar infraestructura. Solo los puertos de MAC única
	// generan hint.
	perPort := map[string]int{}
	for _, d := range s.adapter.GetDevices(ctx) {
		if d.Band != "cable" || d.MAC == "" || d.Port == "" {
			continue
		}
		perPort[d.RouterID+"|"+d.Port]++
	}
	hints := []rack.CableHint{}
	seen := map[string]bool{}
	for _, d := range s.adapter.GetDevices(ctx) {
		if d.Band != "cable" || d.MAC == "" || d.Port == "" {
			continue
		}
		if perPort[d.RouterID+"|"+d.Port] != 1 {
			continue
		}
		from, ok := routerMAC[d.RouterID]
		if !ok || from == "" {
			continue
		}
		to := strings.ToLower(d.MAC)
		if !mounted[from] || !mounted[to] {
			continue
		}
		if seen[from+"|"+to] {
			continue
		}
		seen[from+"|"+to] = true
		hints = append(hints, rack.CableHint{
			FromDeviceID: from,
			ToDeviceID:   to,
			FromPortHint: d.Port,
			Source:       "fdb",
		})
	}
	return hints
}

// --- DTOs (snake_case en el wire) ---

type rackDTO struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	UHeight       int     `json:"u_height"`
	WidthStandard string  `json:"width_standard"`
	Numbering     string  `json:"numbering"`
	Style         string  `json:"style"`
	Location      string  `json:"location,omitempty"`
	PositionX     float64 `json:"position_x"`
	PositionY     float64 `json:"position_y"`
}

type mountDTO struct {
	ID             string `json:"id"`
	RackID         string `json:"rack_id"`
	DeviceMAC      string `json:"device_mac,omitempty"`
	FaceplateID    string `json:"faceplate_id"`
	UStart         int    `json:"u_start"`
	UHeight        int    `json:"u_height"`
	ColStart       int    `json:"col_start"`
	ColSpan        int    `json:"col_span"`
	Label          string `json:"label,omitempty"`
	StatusPin      string `json:"status_pin"`
	PortVisibility string `json:"port_visibility"`
}

type cableDTO struct {
	ID         string `json:"id"`
	FromMount  string `json:"from_mount"`
	FromPort   string `json:"from_port"`
	ToMount    string `json:"to_mount"`
	ToPort     string `json:"to_port"`
	Type       string `json:"type"`
	Label      string `json:"label,omitempty"`
	Properties string `json:"properties,omitempty"`
	Origin     string `json:"origin"`
	CreatedAt  int64  `json:"created_at"`
}

type profileDTO struct {
	MAC         string      `json:"mac"`
	FaceplateID string      `json:"faceplate_id"`
	UHeight     int         `json:"u_height"`
	ColSpan     int         `json:"col_span"`
	Color       string      `json:"color"`
	Ports       []rack.Port `json:"ports"`
}

func rackToDTO(r rack.Rack) rackDTO {
	return rackDTO{
		ID: r.ID, Name: r.Name, UHeight: r.UHeight, WidthStandard: r.WidthStandard,
		Numbering: r.Numbering, Style: r.StyleJSON, Location: r.Location,
		PositionX: r.PositionX, PositionY: r.PositionY,
	}
}

func dtoToRack(d rackDTO) rack.Rack {
	return rack.Rack{
		ID: d.ID, Name: d.Name, UHeight: d.UHeight, WidthStandard: d.WidthStandard,
		Numbering: d.Numbering, StyleJSON: d.Style, Location: d.Location,
		PositionX: d.PositionX, PositionY: d.PositionY,
	}
}

func mountRowToDTO(m rack.MountRow) mountDTO {
	return mountDTO{
		ID: m.ID, RackID: m.RackID, DeviceMAC: m.DeviceMAC, FaceplateID: m.FaceplateID,
		UStart: m.UStart, UHeight: m.UHeight, ColStart: m.ColStart, ColSpan: m.ColSpan,
		Label: m.Label, StatusPin: m.StatusPin, PortVisibility: m.PortVisibility,
	}
}

func dtoToMount(d mountDTO) rack.Mount {
	return rack.Mount{
		ID: d.ID, RackID: d.RackID, DeviceMAC: d.DeviceMAC, FaceplateID: d.FaceplateID,
		UStart: d.UStart, ColStart: d.ColStart,
		Label: d.Label, StatusPin: d.StatusPin, PortVisibility: d.PortVisibility,
	}
}

func cableToDTO(c rack.Cable) cableDTO {
	return cableDTO{
		ID: c.ID, FromMount: c.FromMount, FromPort: c.FromPort, ToMount: c.ToMount,
		ToPort: c.ToPort, Type: c.Type, Label: c.Label, Properties: c.PropertiesJSON,
		Origin: c.Origin, CreatedAt: c.CreatedAt,
	}
}

func dtoToCable(d cableDTO) rack.Cable {
	return rack.Cable{
		ID: d.ID, FromMount: d.FromMount, FromPort: d.FromPort, ToMount: d.ToMount,
		ToPort: d.ToPort, Type: d.Type, Label: d.Label, PropertiesJSON: d.Properties,
		Origin: d.Origin, CreatedAt: d.CreatedAt,
	}
}

func profileToDTO(p rack.DeviceProfile) profileDTO {
	return profileDTO{
		MAC: p.MAC, FaceplateID: p.FaceplateID, UHeight: p.UHeight,
		ColSpan: p.ColSpan, Color: p.Color, Ports: p.Ports,
	}
}

func dtoToProfile(d profileDTO) rack.DeviceProfile {
	return rack.DeviceProfile{
		MAC: d.MAC, FaceplateID: d.FaceplateID, UHeight: d.UHeight,
		ColSpan: d.ColSpan, Color: d.Color, Ports: d.Ports,
	}
}

// --- Handlers ---

// handleRacksGet: bundle completo del canvas en una sola petición
// (offline-first: racks + montajes con huella + cables + perfiles).
func (s *server) handleRacksGet(w http.ResponseWriter, r *http.Request) {
	racks, err := rack.ListRacks(s.db.DB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	mounts, err := rack.ListMountRows(s.db.DB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	cables, err := rack.ListCables(s.db.DB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	profiles, err := rack.ListProfiles(s.db.DB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	racksOut := make([]rackDTO, 0, len(racks))
	for _, r := range racks {
		racksOut = append(racksOut, rackToDTO(r))
	}
	mountsOut := make([]mountDTO, 0, len(mounts))
	for _, m := range mounts {
		mountsOut = append(mountsOut, mountRowToDTO(m))
	}
	cablesOut := make([]cableDTO, 0, len(cables))
	for _, c := range cables {
		cablesOut = append(cablesOut, cableToDTO(c))
	}
	profilesOut := make([]profileDTO, 0, len(profiles))
	for _, p := range profiles {
		profilesOut = append(profilesOut, profileToDTO(p))
	}
	// Auditoría dibujado vs detectado: cada cable lleva su estado.
	hints := s.buildRackHints(r.Context(), mounts)
	writeJSON(w, http.StatusOK, map[string]any{
		"racks": racksOut, "mounts": mountsOut, "cables": cablesOut, "profiles": profilesOut,
		"audit": rack.AuditCables(cables, hints, mounts),
	})
}

// handleRackImportCables: siembra cables desde la topología detectada.
// Idempotente por construcción: re-ejecutar solo hace la mitad útil.
func (s *server) handleRackImportCables(w http.ResponseWriter, r *http.Request) {
	mounts, err := rack.ListMountRows(s.db.DB)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
		return
	}
	hints := s.buildRackHints(r.Context(), mounts)
	res, err := rack.ImportCables(s.db.DB, hints)
	if err != nil {
		writeRackStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"hints":      len(hints),
		"created":    len(res.Created),
		"skipped":    res.Skipped,
		"noFreePort": len(res.NoFreePort),
	})
}

func (s *server) handleRackCreate(w http.ResponseWriter, r *http.Request) {
	var d rackDTO
	if st := readJSONBody(w, r, &d); st != 0 {
		writeBodyError(w, st, "invalid_body", "body JSON inválido")
		return
	}
	created, err := rack.CreateRack(s.db.DB, dtoToRack(d))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_rack", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, rackToDTO(created))
}

func (s *server) handleRackUpdate(w http.ResponseWriter, r *http.Request) {
	var d rackDTO
	if st := readJSONBody(w, r, &d); st != 0 {
		writeBodyError(w, st, "invalid_body", "body JSON inválido")
		return
	}
	d.ID = r.PathValue("id")
	if err := rack.UpdateRack(s.db.DB, dtoToRack(d)); err != nil {
		writeRackStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) handleRackDelete(w http.ResponseWriter, r *http.Request) {
	if err := rack.DeleteRack(s.db.DB, r.PathValue("id")); err != nil {
		writeRackStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleRackLayoutPut: guardada atómica de montajes de un rack. Body:
// {"rack_id": "...", "upsert": [mount...], "delete": ["id"...]}.
func (s *server) handleRackLayoutPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		RackID string     `json:"rack_id"`
		Upsert []mountDTO `json:"upsert"`
		Delete []string   `json:"delete"`
	}
	if st := readJSONBody(w, r, &body); st != 0 {
		writeBodyError(w, st, "invalid_body", "body JSON inválido")
		return
	}
	if body.RackID == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "rack_id is required")
		return
	}
	change := rack.LayoutChange{RackID: body.RackID, Delete: body.Delete}
	for _, d := range body.Upsert {
		change.Upsert = append(change.Upsert, dtoToMount(d))
	}
	if err := rack.SaveLayout(s.db.DB, change); err != nil {
		writeRackStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) handleRackCableAdd(w http.ResponseWriter, r *http.Request) {
	var d cableDTO
	if st := readJSONBody(w, r, &d); st != 0 {
		writeBodyError(w, st, "invalid_body", "body JSON inválido")
		return
	}
	created, err := rack.AddCable(s.db.DB, dtoToCable(d))
	if err != nil {
		writeRackStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cableToDTO(created))
}

func (s *server) handleRackCableDelete(w http.ResponseWriter, r *http.Request) {
	if err := rack.DeleteCable(s.db.DB, r.PathValue("id")); err != nil {
		writeRackStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *server) handleRackProfilePut(w http.ResponseWriter, r *http.Request) {
	var d profileDTO
	if st := readJSONBody(w, r, &d); st != 0 {
		writeBodyError(w, st, "invalid_body", "body JSON inválido")
		return
	}
	d.MAC = r.PathValue("mac")
	if err := rack.UpsertProfile(s.db.DB, dtoToProfile(d)); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_profile", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, profileToDTO(dtoToProfile(d)))
}

// writeRackStoreError mapea los errores del store a códigos HTTP estables.
func writeRackStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, rack.ErrOverlap), errors.Is(err, rack.ErrPortBusy):
		writeError(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, rack.ErrUnknownMount), errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, rack.ErrUnknownPort):
		writeError(w, http.StatusBadRequest, "unknown_port", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "db_error", err.Error())
	}
}
