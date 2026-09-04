// firmware.go — endpoints de firmware upgrades de routers OpenWrt (#453).
package httpapi

import (
	"errors"
	"net/http"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/firmware"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
)

// firmwareStatusResponse es la vista completa de un router para upgrades.
type firmwareStatusResponse struct {
	RouterID       string            `json:"routerId"`
	Name           string            `json:"name"`
	Model          string            `json:"model"`
	CurrentVersion string            `json:"currentVersion"`
	TargetVersion  string            `json:"targetVersion"`
	TargetURL      string            `json:"targetUrl"`
	Checksum       string            `json:"checksum"`
	// Detección #477 P2: último board info reportado por el agente/SSH.
	// detectedBoard es el board_name (perfil ASU) y detectedTarget el
	// DISTRIB_TARGET; base para el prefill del formulario y el lookup ASU.
	DetectedModel   string            `json:"detectedModel,omitempty"`
	DetectedBoard   string            `json:"detectedBoard,omitempty"`
	DetectedVersion string            `json:"detectedVersion,omitempty"`
	DetectedTarget  string            `json:"detectedTarget,omitempty"`
	Upgrade         *firmware.Upgrade `json:"upgrade,omitempty"`
}

// fillDetectedBoard copia el board info al response (nil-safe).
func (item *firmwareStatusResponse) fillDetectedBoard(bi *adapters.BoardInfo) {
	if bi == nil {
		return
	}
	item.DetectedModel = bi.Model
	item.DetectedBoard = bi.BoardName
	item.DetectedVersion = bi.Release.Version
	item.DetectedTarget = bi.Release.Target
}

// registerFirmwareRoutes registra los endpoints de firmware upgrades.
func (s *server) registerFirmwareRoutes(mux *http.ServeMux) {
	if s.firmware == nil {
		return
	}

	mux.Handle("GET /api/firmware-upgrades", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.db == nil {
			writeError(w, http.StatusServiceUnavailable, "no_db")
			return
		}
		routers := routerstore.ListRouters(s.db.DB)
		targets, err := s.firmware.ListTargets()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "firmware_error")
			return
		}
		targetMap := map[string]*firmware.Target{}
		for i := range targets {
			t := targets[i]
			targetMap[t.RouterID] = &t
		}

		out := make([]firmwareStatusResponse, 0, len(routers))
		for _, r := range routers {
			// #477: solo routers con agente nativo OpenWrt/GLiNet; los
			// managed-switches y pushers externos no tienen sysupgrade.
			if !agentUpgradeable(r.Type) {
				continue
			}
			// El firmware target legacy vive en routers.firmware_target.
			model := r.FirmwareTarget
			if t := targetMap[r.ID]; t != nil {
				if t.Model != "" {
					model = t.Model
				}
			}
			item := firmwareStatusResponse{
				RouterID: r.ID,
				Name:     r.Name,
				Model:    model,
			}
			if t := targetMap[r.ID]; t != nil {
				item.CurrentVersion = t.CurrentVersion
				item.TargetVersion = t.TargetVersion
				item.TargetURL = t.TargetURL
				item.Checksum = t.Checksum
			}
			if up, _ := s.firmware.LatestUpgrade(r.ID); up != nil {
				item.Upgrade = up
			}
			if s.adapter != nil {
				item.fillDetectedBoard(s.adapter.BoardInfoFor(r.ID))
			}
			out = append(out, item)
		}
		writeJSON(w, http.StatusOK, out)
	})))

	// DELETE /api/firmware-upgrades/{routerId}/failure: descarta el último
	// intento de upgrade terminado (failed/done) del router (#519). Permite
	// cerrar un aviso de error obsoleto sin poder cancelar un upgrade vivo.
	mux.Handle("DELETE /api/firmware-upgrades/{routerId}/failure", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routerID := r.PathValue("routerId")
		if routerID == "" {
			writeError(w, http.StatusBadRequest, "invalid_input", "routerId requerido")
			return
		}
		if err := s.firmware.DismissLatest(routerID); err != nil {
			writeError(w, http.StatusInternalServerError, "firmware_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})))

	mux.Handle("GET /api/firmware-upgrades/{routerId}", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {		id := r.PathValue("routerId")
		item, err := s.firmwareStatus(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "firmware_error")
			return
		}
		if item == nil {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		writeJSON(w, http.StatusOK, item)
	})))

	mux.Handle("POST /api/firmware-upgrades/{routerId}/target", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("routerId")
		var body struct {
			Model          string `json:"model"`
			CurrentVersion string `json:"currentVersion"`
			TargetVersion  string `json:"targetVersion"`
			TargetURL      string `json:"targetUrl"`
			Checksum       string `json:"checksum"`
		}
		if st := readJSONBody(w, r, &body); st != 0 {
			return
		}
		if body.Model == "" || body.TargetVersion == "" || body.TargetURL == "" {
			writeError(w, http.StatusBadRequest, "invalid_body", "model, targetVersion y targetUrl son requeridos")
			return
		}
		if err := s.firmware.SetTarget(firmware.Target{
			RouterID:       id,
			Model:          body.Model,
			CurrentVersion: body.CurrentVersion,
			TargetVersion:  body.TargetVersion,
			TargetURL:      body.TargetURL,
			Checksum:       body.Checksum,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, "firmware_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	})))

	mux.Handle("POST /api/firmware-upgrades/{routerId}/upgrade", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("routerId")
		// #494: el flujo manual delega en el motor compartido (mismo que el
		// scheduler). El contrato HTTP no cambia: mismos códigos y mensajes.
		if s.agentHub == nil {
			writeError(w, http.StatusServiceUnavailable, "no_agent_hub")
			return
		}
		upgradeID, err := s.firmwareEngine.StartUpgrade(id)
		switch {
		case errors.Is(err, firmware.ErrNoTarget):
			writeError(w, http.StatusBadRequest, "no_target", "Configura el target de firmware antes de actualizar")
		case errors.Is(err, firmware.ErrUpgradeInProgress):
			writeError(w, http.StatusConflict, "upgrade_in_progress", "Ya hay un upgrade en curso")
		case errors.Is(err, firmware.ErrAgentNotConnected):
			writeError(w, http.StatusServiceUnavailable, "agent_not_connected", "El agente no está conectado vía SSE")
		case err != nil:
			writeError(w, http.StatusInternalServerError, "firmware_error")
		default:
			writeJSON(w, http.StatusAccepted, map[string]any{"upgradeId": upgradeID, "status": "requested"})
		}
	})))

	// POST /api/firmware-upgrades/{routerId}/schedule: programa un upgrade
	// desatendido para el target conocido del router (#494).
	mux.Handle("POST /api/firmware-upgrades/{routerId}/schedule", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("routerId")
		var body struct {
			ScheduledFor int64 `json:"scheduledFor"`
		}
		if st := readJSONBody(w, r, &body); st != 0 {
			writeBodyError(w, st, "invalid_body", "")
			return
		}
		if body.ScheduledFor <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_body", "scheduledFor (epoch ms) es requerido")
			return
		}
		target, err := s.firmware.GetTarget(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "firmware_error")
			return
		}
		if target == nil || target.TargetURL == "" {
			writeError(w, http.StatusBadRequest, "no_target", "Configura el target de firmware antes de programar")
			return
		}
		// Rechazar si hay un upgrade activo; una programación ya pendiente se
		// actualiza (UPSERT en el store), no se duplica.
		if up, _ := s.firmware.LatestUpgrade(id); up != nil && up.Status != "done" && up.Status != "failed" && up.Status != "scheduled" {
			writeError(w, http.StatusBadRequest, "upgrade_in_progress", "Ya hay un upgrade en curso")
			return
		}
		upgradeID, err := s.firmware.ScheduleUpgrade(id, target.TargetVersion, target.TargetURL, target.Checksum, body.ScheduledFor)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "firmware_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "upgradeId": upgradeID})
	})))

	// DELETE /api/firmware-upgrades/{routerId}/schedule: cancela una
	// programación aún no iniciada (#494). No-op si no hay ninguna.
	mux.Handle("DELETE /api/firmware-upgrades/{routerId}/schedule", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routerID := r.PathValue("routerId")
		if routerID == "" {
			writeError(w, http.StatusBadRequest, "invalid_input", "routerId requerido")
			return
		}
		if err := s.firmware.CancelScheduled(routerID); err != nil {
			writeError(w, http.StatusInternalServerError, "firmware_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})))

	// El agente reporta progreso del upgrade (Bearer, misma auth que ingesta).
	mux.HandleFunc("POST /api/agents/{slug}/firmware-progress", func(w http.ResponseWriter, r *http.Request) {
		slug := r.PathValue("slug")
		token := bearerToken(r)
		if !s.checkAgentToken(slug, token) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		var body struct {
			UpgradeID int64  `json:"upgradeId"`
			Status    string `json:"status"`
			Percent   int    `json:"percent,omitempty"`
			Message   string `json:"message,omitempty"`
		}
		if st := readJSONBody(w, r, &body); st != 0 {
			return
		}
		if body.UpgradeID == 0 {
			writeError(w, http.StatusBadRequest, "invalid_body")
			return
		}
		// Validar que el upgrade pertenece al slug.
		up, err := s.firmware.GetUpgradeByID(body.UpgradeID)
		if err != nil || up == nil || up.RouterID != slug {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		if err := s.firmware.SetStatus(body.UpgradeID, body.Status, "", ""); err != nil {
			writeError(w, http.StatusInternalServerError, "firmware_error")
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	})

	// El agente reporta el resultado final del upgrade.
	mux.HandleFunc("POST /api/agents/{slug}/firmware-result", func(w http.ResponseWriter, r *http.Request) {
		slug := r.PathValue("slug")
		token := bearerToken(r)
		if !s.checkAgentToken(slug, token) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		var body struct {
			UpgradeID  int64  `json:"upgradeId"`
			OK         bool   `json:"ok"`
			Error      string `json:"error,omitempty"`
			BackupPath string `json:"backupPath,omitempty"`
			NewVersion string `json:"newVersion,omitempty"`
		}
		if st := readJSONBody(w, r, &body); st != 0 {
			return
		}
		if body.UpgradeID == 0 {
			writeError(w, http.StatusBadRequest, "invalid_body")
			return
		}
		up, err := s.firmware.GetUpgradeByID(body.UpgradeID)
		if err != nil || up == nil || up.RouterID != slug {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		status := "done"
		if !body.OK {
			status = "failed"
		}
		if err := s.firmware.SetStatus(body.UpgradeID, status, body.Error, body.BackupPath); err != nil {
			writeError(w, http.StatusInternalServerError, "firmware_error")
			return
		}
		// Actualizar current_version del target si el upgrade terminó bien.
		if body.OK && body.NewVersion != "" {
			if t, _ := s.firmware.GetTarget(slug); t != nil {
				t.CurrentVersion = body.NewVersion
				_ = s.firmware.SetTarget(*t)
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	})
}

// firmwareStatus devuelve el estado de firmware de un router o nil si no existe.
func (s *server) firmwareStatus(id string) (*firmwareStatusResponse, error) {
	routers := routerstore.ListRouters(s.db.DB)
	var r *adapters.RouterConfig
	for i := range routers {
		if routers[i].ID == id {
			r = &routers[i]
			break
		}
	}
	if r == nil {
		return nil, nil
	}
	target, err := s.firmware.GetTarget(id)
	if err != nil {
		return nil, err
	}
	up, err := s.firmware.LatestUpgrade(id)
	if err != nil {
		return nil, err
	}
	item := &firmwareStatusResponse{
		RouterID: id,
		Name:     r.Name,
		Model:    r.FirmwareTarget,
	}
	if target != nil {
		item.Model = target.Model
		item.CurrentVersion = target.CurrentVersion
		item.TargetVersion = target.TargetVersion
		item.TargetURL = target.TargetURL
		item.Checksum = target.Checksum
	}
	if up != nil {
		item.Upgrade = up
	}
	if s.adapter != nil {
		item.fillDetectedBoard(s.adapter.BoardInfoFor(id))
	}
	return item, nil
}
