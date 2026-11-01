// mqtt_settings.go - ajustes del publisher MQTT (#838): GET (sin password),
// PUT (password vacío = conservar), test de conexión y propagación a los
// routers NetGrip de la flota.
package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gnacho/netpulse/agent/executor"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/mqttpub"
	"github.com/gnacho/netpulse/server-go/internal/orchestr"
	"github.com/gnacho/netpulse/server-go/internal/sse"
	"github.com/gnacho/netpulse/server-go/internal/vercmp"
)

// mqttRedactedPass sustituye la contraseña del broker en la copia del plan que
// se persiste: el secreto viaja al agente por el canal autenticado (SSE), pero
// no se duplica en orchestr_plans ni en el historial de auditoría.
const mqttRedactedPass = "<redacted>"

// netgripMQTTTLSMinVersion is the first NetGrip release that reads the tls and
// tlsInsecure arguments of mqtt.configure. Older agents keep only the original
// seven args, so a TLS config is not propagated to them at all.
const netgripMQTTTLSMinVersion = "0.78.0"

// agentSupportsMQTTTLS reports whether an agent version understands the TLS
// arguments. An empty or unparsable version is treated as unsupported: better
// to ask for an update than to silently configure the router without TLS.
func agentSupportsMQTTTLS(version string) bool {
	if _, ok := vercmp.Parse3(version); !ok {
		return false
	}
	return vercmp.Cmp(version, netgripMQTTTLSMinVersion) >= 0
}

type mqttConfigResponse struct {
	Enabled     bool   `json:"enabled"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	User        string `json:"user"`
	PassSet     bool   `json:"passSet"`
	Instance    string `json:"instance"`
	IntervalSec int    `json:"intervalSec"`
	TLS         bool   `json:"tls"`
	TLSInsecure bool   `json:"tlsInsecure"`
	Running     bool   `json:"running"`
}

func (s *server) registerMQTTRoutes(mux *http.ServeMux, mgr *mqttpub.Manager, orchMgr *orchestr.Manager) {
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
			IntervalSec: int(cfg.Interval.Seconds()), TLS: cfg.TLS, TLSInsecure: cfg.TLSInsecure,
			Running: mgr.Running(),
		})
	})))

	mux.Handle("PUT /api/settings/mqtt", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mgr == nil {
			writeError(w, http.StatusServiceUnavailable, "mqtt_unavailable")
			return
		}
		var body struct {
			// Enabled es puntero (#968): un PUT parcial que solo toca otro
			// campo (p.ej. host) NO debe apagar el publisher por accidente.
			Enabled     *bool  `json:"enabled"`
			Host        string `json:"host"`
			Port        int    `json:"port"`
			User        string `json:"user"`
			Pass        string `json:"pass"`
			Instance    string `json:"instance"`
			IntervalSec int    `json:"intervalSec"`
			TLS         *bool  `json:"tls"`
			TLSInsecure *bool  `json:"tlsInsecure"`
		}
		if st := readJSONBody(w, r, &body); st != 0 {
			return
		}
		cfg := mgr.Config()
		if body.Enabled != nil {
			cfg.Enabled = *body.Enabled
		}
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
		if body.TLS != nil {
			cfg.TLS = *body.TLS
		}
		if body.TLSInsecure != nil {
			cfg.TLSInsecure = *body.TLSInsecure
		}
		if !cfg.TLS {
			cfg.TLSInsecure = false
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
			Host        string `json:"host"`
			Port        int    `json:"port"`
			User        string `json:"user"`
			Pass        string `json:"pass"`
			TLS         *bool  `json:"tls"`
			TLSInsecure *bool  `json:"tlsInsecure"`
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
		if body.TLS != nil {
			cfg.TLS = *body.TLS
		}
		if body.TLSInsecure != nil {
			cfg.TLSInsecure = *body.TLSInsecure
		}
		if !cfg.TLS {
			cfg.TLSInsecure = false
		}
		if err := mqttpub.Probe(r.Context(), cfg); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})))

	// propagate envía la configuración del broker a cada router NetGrip de la
	// flota (op mqtt.configure) por SSE, al agente embebido en su NetGrip.
	//
	// Cada router recibe un plan REAL (fila en orchestr_plans, marcado
	// applying antes del envío) y se identifica en la respuesta con su planId.
	// La respuesta solo afirma que el comando quedó ENCOLADO en el agente: el
	// resultado real llega después por POST /api/agents/{slug}/apply-result.
	mux.Handle("POST /api/settings/mqtt/propagate", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mgr == nil {
			writeError(w, http.StatusServiceUnavailable, "mqtt_unavailable")
			return
		}
		if orchMgr == nil {
			writeError(w, http.StatusServiceUnavailable, "orchestr_unavailable")
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
		if s.agentHub == nil {
			writeError(w, http.StatusServiceUnavailable, "no_agent_hub")
			return
		}
		actor := ""
		if user := auth.UserFromContext(r.Context()); user != nil {
			actor = user.Username
		}
		intervalSec := int(cfg.Interval.Seconds())
		op := executor.Op{
			Kind: "mqtt.configure",
			Args: map[string]string{
				"enabled":  "true",
				"host":     cfg.Host,
				"port":     strconv.Itoa(cfg.Port),
				"user":     cfg.User,
				"pass":     cfg.Pass,
				"interval": strconv.Itoa(intervalSec),
			},
			Desc: "MQTT: aplicar el broker de NetPulse",
		}
		if cfg.TLS {
			// Added last on purpose: an old NetGrip decodes only the original
			// seven keys and would drop TLS without saying so, so propagation
			// is refused below unless the agent reports a version that knows
			// them.
			op.Args["tls"] = "1"
			op.Args["tlsInsecure"] = strconv.FormatBool(cfg.TLSInsecure)
		}
		// Copia persistida del op: el secreto se redacta (el agente recibe el
		// valor real por el stream autenticado).
		opPersisted := op
		opPersisted.Args = map[string]string{}
		for k, v := range op.Args {
			opPersisted.Args[k] = v
		}
		if cfg.Pass != "" {
			opPersisted.Args["pass"] = mqttRedactedPass
		}
		// Estado deseado persistido: la configuración del broker sin secreto.
		desiredJSON, _ := json.Marshal(map[string]any{
			"enabled":     true,
			"host":        cfg.Host,
			"port":        cfg.Port,
			"user":        cfg.User,
			"instance":    cfg.Instance,
			"intervalSec": intervalSec,
			"tls":         cfg.TLS,
			"tlsInsecure": cfg.TLSInsecure,
			"passSet":     cfg.Pass != "",
		})

		results := []map[string]any{}
		for _, rt := range ov.Routers {
			if s.agentKindOf(rt.ID) != "netgrip" {
				continue
			}
			res := map[string]any{
				"id": rt.ID, "name": rt.Name,
				"queued": false, "delegated": false, "status": "not_queued",
			}
			if cfg.TLS {
				_, ver, _, _, _ := s.agents.Info(rt.ID)
				if !agentSupportsMQTTTLS(ver) {
					res["error"] = "netgrip_update_required"
					results = append(results, res)
					continue
				}
			}
			plan, err := orchMgr.CreatePlan(rt.ID, "mqtt", desiredJSON, []executor.Op{opPersisted}, actor)
			if err != nil {
				log.Printf("[netpulse] mqtt propagate: no se pudo crear el plan de %s: %v", rt.ID, err)
				res["error"] = "plan_error"
				results = append(results, res)
				continue
			}
			res["planId"] = plan.ID
			// El estado se fija ANTES de enviar: si el agente responde al
			// instante, su resultado encuentra el plan en applying.
			if err := orchMgr.SetApplying(plan.ID, actor); err != nil {
				log.Printf("[netpulse] mqtt propagate: no se pudo marcar applying el plan %s: %v", plan.ID, err)
				res["error"] = "plan_error"
				results = append(results, res)
				continue
			}
			data, _ := json.Marshal(map[string]any{"plan_id": plan.ID, "ops": []executor.Op{op}})
			if err := s.agentHub.SendPrivileged(rt.ID, "apply", json.RawMessage(data)); err != nil {
				// No se entregó: el plan vuelve a pending (no se simula éxito).
				s.restorePlan(orchMgr, plan.ID, "applying", "pending", actor, "sse delivery failed")
				if errors.Is(err, sse.ErrInsecureAgentTransport) {
					res["error"] = "insecure_agent_transport"
				} else {
					res["error"] = "agent_not_connected"
				}
				results = append(results, res)
				continue
			}
			res["queued"] = true
			// delegated: alias histórico de queued (compatibilidad de campo).
			res["delegated"] = true
			res["status"] = "queued"
			results = append(results, res)
		}
		writeJSON(w, http.StatusOK, map[string]any{"results": results})
	})))
}
