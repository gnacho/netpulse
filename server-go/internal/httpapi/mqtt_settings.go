// mqtt_settings.go - ajustes del publisher MQTT (#838): GET (sin password),
// PUT (password vacío = conservar), test de conexión y propagación a los
// routers NetGrip de la flota.
package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gnacho/netpulse/agent/executor"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/mqttpub"
)

type mqttConfigResponse struct {
	Enabled     bool   `json:"enabled"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	User        string `json:"user"`
	PassSet     bool   `json:"passSet"`
	Instance    string `json:"instance"`
	IntervalSec int    `json:"intervalSec"`
	Running     bool   `json:"running"`
}

func (s *server) registerMQTTRoutes(mux *http.ServeMux, mgr *mqttpub.Manager) {
	adapter := &dbKVAdapter{db: s.db.DB}

	mux.Handle("GET /api/settings/mqtt", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mgr == nil {
			writeError(w, http.StatusServiceUnavailable, "mqtt_unavailable")
			return
		}
		cfg := mgr.Config()
		writeJSON(w, http.StatusOK, mqttConfigResponse{
			Enabled: cfg.Enabled, Host: cfg.Host, Port: cfg.Port, User: cfg.User,
			PassSet: cfg.Pass != "", Instance: cfg.Instance,
			IntervalSec: int(cfg.Interval.Seconds()), Running: mgr.Running(),
		})
	})))

	mux.Handle("PUT /api/settings/mqtt", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mgr == nil {
			writeError(w, http.StatusServiceUnavailable, "mqtt_unavailable")
			return
		}
		var body struct {
			Enabled     bool   `json:"enabled"`
			Host        string `json:"host"`
			Port        int    `json:"port"`
			User        string `json:"user"`
			Pass        string `json:"pass"`
			Instance    string `json:"instance"`
			IntervalSec int    `json:"intervalSec"`
		}
		if st := readJSONBody(w, r, &body); st != 0 {
			return
		}
		cfg := mgr.Config()
		cfg.Enabled = body.Enabled
		if body.Host != "" {
			cfg.Host = body.Host
		}
		if body.Port > 0 {
			cfg.Port = body.Port
		}
		if body.User != "" {
			cfg.User = body.User
		}
		if body.Pass != "" {
			cfg.Pass = body.Pass
		}
		if body.Instance != "" {
			cfg.Instance = body.Instance
		}
		if body.IntervalSec > 0 {
			cfg.Interval = time.Duration(body.IntervalSec) * time.Second
		}
		if err := mqttpub.SaveConfig(adapter, cfg); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_input", err.Error())
			return
		}
		mgr.Apply(cfg)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})))

	mux.Handle("POST /api/settings/mqtt/test", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mgr == nil {
			writeError(w, http.StatusServiceUnavailable, "mqtt_unavailable")
			return
		}
		var body struct {
			Host string `json:"host"`
			Port int    `json:"port"`
			User string `json:"user"`
			Pass string `json:"pass"`
		}
		if st := readJSONBody(w, r, &body); st != 0 {
			return
		}
		cfg := mgr.Config()
		if body.Host != "" {
			cfg.Host = body.Host
		}
		if body.Port > 0 {
			cfg.Port = body.Port
		}
		if body.User != "" {
			cfg.User = body.User
		}
		if body.Pass != "" {
			cfg.Pass = body.Pass
		}
		if err := mqttpub.Probe(r.Context(), cfg); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})))

	// propagate envía la configuración del broker a cada router NetGrip de la
	// flota (op mqtt.configure vía su executor), para no configurarlos a mano.
	mux.Handle("POST /api/settings/mqtt/propagate", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mgr == nil {
			writeError(w, http.StatusServiceUnavailable, "mqtt_unavailable")
			return
		}
		cfg := mgr.Config()
		if !cfg.Enabled {
			writeError(w, http.StatusBadRequest, "mqtt_disabled", "MQTT está desactivado en este servidor")
			return
		}
		ov := s.lastOv()
		if ov == nil {
			writeError(w, http.StatusServiceUnavailable, "no_overview")
			return
		}
		op := executor.Op{
			Kind: "mqtt.configure",
			Args: map[string]string{
				"enabled":  "true",
				"host":     cfg.Host,
				"port":     strconv.Itoa(cfg.Port),
				"user":     cfg.User,
				"pass":     cfg.Pass,
				"interval": strconv.Itoa(int(cfg.Interval.Seconds())),
			},
			Desc: "MQTT: aplicar el broker de NetPulse",
		}
		planID := "mqtt-propagate-" + strconv.FormatInt(time.Now().Unix(), 10)
		results := []map[string]any{}
		for _, rt := range ov.Routers {
			if s.agentKindOf(rt.ID) != "netgrip" {
				continue
			}
			ok, err := s.applyViaNetGrip(rt.ID, planID, []executor.Op{op})
			res := map[string]any{"id": rt.ID, "name": rt.Name, "delegated": ok}
			if err != nil {
				res["error"] = err.Error()
			}
			results = append(results, res)
		}
		writeJSON(w, http.StatusOK, map[string]any{"results": results})
	})))
}
