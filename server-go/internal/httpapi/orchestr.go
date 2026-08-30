// orchestr.go — Fase 10: endpoints de plan/apply/audit (solo admin).
//
//	POST   /api/plans              — crear plan (routerId + resource + diff)
//	GET    /api/plans/{id}         — ver plan + diff + estado + resultado
//	POST   /api/plans/{id}/apply   — aplicar (envía Ops al agente vía SSE)
//	POST   /api/agents/{slug}/apply-result — el agente reporta el resultado
//	GET    /api/audit              — log de auditoría (últimos N)
package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gnacho/netpulse/agent/executor"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/orchestr"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
)

// registerOrchestrRoutes registra las rutas de orquestación (solo admin).
func (s *server) registerOrchestrRoutes(mux *http.ServeMux, mgr *orchestr.Manager) {
	if mgr == nil {
		return
	}

	mux.Handle("POST /api/plans", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			RouterID string          `json:"routerId"`
			Resource string          `json:"resource"`
			Diff     []executor.Op   `json:"diff"`
			Desired  json.RawMessage `json:"desired"`
		}
		if st := readJSONBody(w, r, &body); st != 0 {
			writeBodyError(w, st, "invalid_body",
				`Se esperaba { "routerId": "...", "resource": "adguard", "desired": {...} }`)
			return
		}
		if body.RouterID == "" || body.Resource == "" {
			writeError(w, http.StatusBadRequest, "invalid_body",
				`Se esperaba { "routerId": "...", "resource": "adguard", "desired": {...} }`)
			return
		}
		// Si no hay diff explícito, calcularlo desde desired vía el módulo.
		// El módulo AdGuard ejecuta un probe SSH (Fase 17.1) y aborta con
		// managed_by_firmware si el router trae un fork de fabricante.
		diff := body.Diff
		var method string
		if len(diff) == 0 && len(body.Desired) > 0 {
			computed, m, err := s.computeModuleDiff(body.Resource, body.RouterID, body.Desired)
			if err != nil {
				s.writeModuleErr(w, err)
				return
			}
			diff = computed
			method = m
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
		plan.Method = method // metadato no persistido (escenario detectado)
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
		// Intentar delegar en NetGrip si el router tiene executor token.
		if ok, err := s.applyViaNetGrip(plan.RouterID, plan.ID, plan.Diff); err != nil {
			writeError(w, http.StatusBadGateway, "netgrip_error", err.Error())
			return
		} else if ok {
			mgr.SetApplying(plan.ID)
			if err := mgr.SetResult(plan.ID, netgripApplyResult()); err != nil {
				log.Printf("[netpulse] error marcando plan aplicado vía NetGrip: %v", err)
			}
			writeJSON(w, http.StatusAccepted, map[string]string{"status": "applying", "planId": id})
			return
		}
		if s.agentHub == nil {
			writeError(w, http.StatusServiceUnavailable, "no_agent_hub")
			return
		}
		// Fallback: enviar Ops al agente vía SSE.
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

	// POST /api/plans/{id}/rollback — revertir un plan ya aplicado.
	//
	// Calcula el "desired inverso" del módulo (p. ej. AdGuard: si el plan era
	// enabled=true, el inverso es enabled=false), vuelve a sondear el router
	// (para detectar el escenario actual) y envía las Ops inversas al agente
	// vía SSE. El agente las ejecuta con su snapshot+healthcheck+rollback
	// automático. El resultado llega por POST /api/agents/{slug}/apply-result.
	mux.Handle("POST /api/plans/{id}/rollback", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		plan, err := mgr.GetPlan(id)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		if plan.Status != "applied" {
			writeError(w, http.StatusConflict, "plan_not_applied",
				"Solo se puede revertir un plan aplicado (estado actual: "+plan.Status+")")
			return
		}
		inverseDesired, err := invertDesired(plan.Resource, plan.Desired)
		if err != nil {
			writeError(w, http.StatusBadRequest, "rollback_unsupported", err.Error())
			return
		}
		// Recalcular ops inversas (vuelve a sondear el escenario del router).
		diff, _, err := s.computeModuleDiff(plan.Resource, plan.RouterID, inverseDesired)
		if err != nil {
			s.writeModuleErr(w, err)
			return
		}
		if s.agentHub == nil {
			writeError(w, http.StatusServiceUnavailable, "no_agent_hub")
			return
		}
		applyData, _ := json.Marshal(map[string]any{"plan_id": id, "ops": diff})
		sent := s.agentHub.Send(plan.RouterID, "rollback", json.RawMessage(applyData))
		if !sent {
			writeError(w, http.StatusServiceUnavailable, "agent_not_connected",
				"El agente no está conectado vía SSE")
			return
		}
		user := auth.UserFromContext(r.Context())
		actor := ""
		if user != nil {
			actor = user.Username
		}
		mgr.SetRollingBack(id, actor)
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "rolling_back", "planId": id})
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
			PlanID string               `json:"planId"`
			Result executor.ApplyResult `json:"result"`
		}
		if st := readJSONBody(w, r, &body); st != 0 {
			writeBodyError(w, st, "invalid_body", "")
			return
		}
		if body.PlanID == "" {
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

// Errores sentinelas del cálculo de diff (mapeados a códigos HTTP por
// writeModuleErr).
var (
	errRouterNotFound          = errors.New("router_not_found")
	errUnknownModule           = errors.New("unknown_module")
	errProbeFailed             = errors.New("probe_failed")
	errInvalidDesired          = errors.New("invalid_desired")
	errAdGuardGatewayOnly      = errors.New("adguard_gateway_only")
	errAdGuardAlreadyOnGateway = errors.New("adguard_already_on_gateway")
	errAdGuardInsufficientRAM  = errors.New("adguard_insufficient_ram")
	errGatewayOnly             = errors.New("gateway_only")
)

// computeModuleDiff despacha al módulo correcto, ejecutando el probe SSH si
// el módulo lo requiere (AdGuard). Devuelve las Ops, el método detectado
// (apk|opkg|none|binary, para mostrarlo en el plan) y errores sentinelas
// para que el handler los mapee a códigos HTTP adecuados (422
// managed_by_firmware, etc.).
//
// s.pool es httpapi.SSHRunner; como orchestr.CommandRunner tiene la misma
// firma (Run(host, cmd, timeout)), Go lo acepta por satisfacción estructural.
func (s *server) computeModuleDiff(resource, routerID string, desired json.RawMessage) ([]executor.Op, string, error) {
	switch resource {
	case "adguard":
		var d orchestr.AdGuardDesired
		if err := json.Unmarshal(desired, &d); err != nil {
			return nil, "", fmt.Errorf("%w: %v", errInvalidDesired, err)
		}
		host := s.hostOfRouter(routerID)
		if host == "" {
			return nil, "", errRouterNotFound
		}
		// AdGuard filtra el DNS de la red → por defecto solo el gateway
		// (#120). El resto de checks aplican a un target no-gateway.
		isGateway, gwHost := s.gatewayInfo(routerID)
		if !isGateway && !d.AllowNonGateway {
			return nil, "", errAdGuardGatewayOnly
		}
		sc, err := orchestr.DetectAdGuard(s.pool, host)
		if err != nil {
			return nil, "", fmt.Errorf("%w: %v", errProbeFailed, err)
		}
		// Solo se despliega (enabled) en no-gateway con RAM suficiente.
		if !isGateway && d.Enabled && sc.AvailableRAM > 0 && sc.AvailableRAM < 150 {
			return nil, "", errAdGuardInsufficientRAM
		}
		// Si el gateway ya tiene AdGuard (fork de fabricante o binario
		// nuestro), desplegar en otro router es redundante.
		if !isGateway && gwHost != "" && gwHost != host {
			if gwSc, err := orchestr.DetectAdGuard(s.pool, gwHost); err == nil &&
				(gwSc.ManagedByFirmware || gwSc.BinaryPresent) {
				return nil, "", errAdGuardAlreadyOnGateway
			}
		}
		ops, err := orchestr.AdGuardOps(d, sc)
		if err != nil {
			return nil, "", err
		}
		return ops, sc.InstallMethod(), nil
	case "guestwifi":
		var d orchestr.GuestWiFiDesired
		if err := json.Unmarshal(desired, &d); err != nil {
			return nil, "", fmt.Errorf("%w: %v", errInvalidDesired, err)
		}
		host := s.hostOfRouter(routerID)
		if host == "" {
			return nil, "", errRouterNotFound
		}
		// La guest red solo tiene sentido en el gateway (distribuye la red)
		// o en un AP de distribución. Por defecto gateway-only, como AdGuard.
		isGateway, _ := s.gatewayInfo(routerID)
		if !isGateway && !d.AllowNonGateway {
			return nil, "", errGatewayOnly
		}
		sc, err := orchestr.DetectGuestWiFi(s.pool, host)
		if err != nil {
			return nil, "", fmt.Errorf("%w: %v", errProbeFailed, err)
		}
		ops := orchestr.GuestWiFiOps(d, sc)
		return ops, guestWiFiMethod(d.Enabled), nil
	case "ddns":
		var d orchestr.DdnsDesired
		if err := json.Unmarshal(desired, &d); err != nil {
			return nil, "", fmt.Errorf("%w: %v", errInvalidDesired, err)
		}
		host := s.hostOfRouter(routerID)
		if host == "" {
			return nil, "", errRouterNotFound
		}
		// Gateway-only por defecto (patrón #120/#17.2).
		isGateway, _ := s.gatewayInfo(routerID)
		if !isGateway && !d.AllowNonGateway {
			return nil, "", errGatewayOnly
		}
		sc, err := orchestr.DetectDdns(s.pool, host)
		if err != nil {
			return nil, "", fmt.Errorf("%w: %v", errProbeFailed, err)
		}
		ops := orchestr.DdnsOps(d, sc)
		return ops, guestWiFiMethod(d.Enabled), nil
	case "sqm":
		var d orchestr.SqmDesired
		if err := json.Unmarshal(desired, &d); err != nil {
			return nil, "", fmt.Errorf("%w: %v", errInvalidDesired, err)
		}
		host := s.hostOfRouter(routerID)
		if host == "" {
			return nil, "", errRouterNotFound
		}
		// Gateway-only por defecto (patrón #120/#17.2).
		isGateway, _ := s.gatewayInfo(routerID)
		if !isGateway && !d.AllowNonGateway {
			return nil, "", errGatewayOnly
		}
		sc, err := orchestr.DetectSqm(s.pool, host)
		if err != nil {
			return nil, "", fmt.Errorf("%w: %v", errProbeFailed, err)
		}
		ops := orchestr.SqmOps(d, sc)
		return ops, guestWiFiMethod(d.Enabled), nil
	case "wireguard":
		var d orchestr.WireGuardDesired
		if err := json.Unmarshal(desired, &d); err != nil {
			return nil, "", fmt.Errorf("%w: %v", errInvalidDesired, err)
		}
		host := s.hostOfRouter(routerID)
		if host == "" {
			return nil, "", errRouterNotFound
		}
		// Los peers viven en cualquier router con un túnel WireGuard (no hay
		// gate de gateway: el túnel puede existir en un sitio remoto).
		sc, err := orchestr.DetectWireGuard(s.pool, host)
		if err != nil {
			return nil, "", fmt.Errorf("%w: %v", errProbeFailed, err)
		}
		ops := orchestr.WireGuardOps(d, sc)
		return ops, wireGuardMethod(sc, d.Interface), nil
	default:
		return nil, "", fmt.Errorf("%w: %s", errUnknownModule, resource)
	}
}

// wireGuardMethod es la etiqueta del método (para la UI): el túnel está
// activo en el kernel (active) o no (inactive).
func wireGuardMethod(sc orchestr.WireGuardScenario, iface string) string {
	if iface == "" {
		iface = "wg0"
	}
	for _, a := range sc.ActiveIfaces {
		if a == iface {
			return "active"
		}
	}
	return "inactive"
}

// guestWiFiMethod es la etiqueta del método (para la UI): enabled/disabled.
func guestWiFiMethod(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

// gatewayInfo devuelve (esGateway, gwHost). esGateway: si routerID ES el
// gateway. gwHost: host del gateway (vacío si no hay gateway configurado).
func (s *server) gatewayInfo(routerID string) (bool, string) {
	gwHost := ""
	for _, r := range routerstore.ListRouters(s.db.DB) {
		if r.IsGateway {
			gwHost = r.Host
			if r.ID == routerID {
				return true, r.Host
			}
		}
	}
	return false, gwHost
}

// hostOfRouter busca el host SSH de un router por ID (scan de ListRouters).
// Vacío si no existe o es agent-only (sin SSH).
func (s *server) hostOfRouter(routerID string) string {
	for _, r := range routerstore.ListRouters(s.db.DB) {
		if r.ID == routerID && !r.AgentOnly && r.Host != "" {
			return r.Host
		}
	}
	return ""
}

// writeModuleErr mapea errores de computeModuleDiff a respuestas HTTP.
func (s *server) writeModuleErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, orchestr.ErrManagedByFirmware):
		writeError(w, http.StatusUnprocessableEntity, "managed_by_firmware",
			"El router trae un AdGuard Home del fabricante (GL.iNet). Configúralo desde su propia UI; NetPulse no lo gestiona.")
	case errors.Is(err, errRouterNotFound):
		writeError(w, http.StatusNotFound, "router_not_found",
			"Router no encontrado o sin SSH (agent-only).")
	case errors.Is(err, errAdGuardGatewayOnly):
		writeError(w, http.StatusUnprocessableEntity, "adguard_gateway_only",
			"AdGuard filtra el DNS de la red: por defecto solo se despliega en el gateway. Activa 'permitir en otros routers' en el módulo AdGuard para usar un no-gateway.")
	case errors.Is(err, errAdGuardAlreadyOnGateway):
		writeError(w, http.StatusUnprocessableEntity, "adguard_already_on_gateway",
			"El gateway ya tiene AdGuard Home activo (fork de fabricante o binario instalado). Desplegarlo en otro router es redundante.")
	case errors.Is(err, errAdGuardInsufficientRAM):
		writeError(w, http.StatusUnprocessableEntity, "adguard_insufficient_ram",
			"El router no tiene suficiente RAM libre (<150 MB) para AdGuard Home y sus listas de filtros.")
	case errors.Is(err, errGatewayOnly):
		writeError(w, http.StatusUnprocessableEntity, "gateway_only",
			"Este módulo por defecto solo se despliega en el gateway. Activa 'permitir en otros routers' para usar un no-gateway.")
	case errors.Is(err, errUnknownModule):
		writeError(w, http.StatusBadRequest, "unknown_module", err.Error())
	case errors.Is(err, errInvalidDesired):
		writeError(w, http.StatusBadRequest, "invalid_desired", err.Error())
	case errors.Is(err, errProbeFailed):
		writeError(w, http.StatusServiceUnavailable, "probe_failed",
			"No se pudo sondear el router por SSH para detectar el escenario: "+err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "module_error", err.Error())
	}
}

// invertDesired devuelve el estado deseado opuesto para un módulo, de forma
// que computeModuleDiff(inverseDesired) genere las Ops que deshacen el plan
// original. Solo los módulos que saben invertirse lo soportan (hoy AdGuard:
// toggle Enabled; el rollback de enabled=true es disable).
func invertDesired(resource string, desired json.RawMessage) (json.RawMessage, error) {
	switch resource {
	case "adguard":
		var d orchestr.AdGuardDesired
		if err := json.Unmarshal(desired, &d); err != nil {
			return nil, fmt.Errorf("desired inválido: %w", err)
		}
		d.Enabled = !d.Enabled
		return json.Marshal(d)
	case "guestwifi":
		var d orchestr.GuestWiFiDesired
		if err := json.Unmarshal(desired, &d); err != nil {
			return nil, fmt.Errorf("desired inválido: %w", err)
		}
		d.Enabled = !d.Enabled
		return json.Marshal(d)
	case "ddns":
		var d orchestr.DdnsDesired
		if err := json.Unmarshal(desired, &d); err != nil {
			return nil, fmt.Errorf("desired inválido: %w", err)
		}
		d.Enabled = !d.Enabled
		return json.Marshal(d)
	case "sqm":
		var d orchestr.SqmDesired
		if err := json.Unmarshal(desired, &d); err != nil {
			return nil, fmt.Errorf("desired inválido: %w", err)
		}
		d.Enabled = !d.Enabled
		return json.Marshal(d)
	default:
		return nil, fmt.Errorf("rollback no soportado para el módulo %q", resource)
	}
}

// applyViaNetGrip intenta ejecutar un plan en el NetGrip del router.
// Devuelve (true, nil) si NetGrip aceptó y ejecutó las ops; (false, nil) si
// no hay NetGrip configurado o no responde (fallback a SSE); (false, error)
// si NetGrip devolvió error explícito.
func (s *server) applyViaNetGrip(routerID string, planID string, ops []executor.Op) (bool, error) {
	host := s.hostOfRouter(routerID)
	if host == "" {
		return false, nil
	}
	execToken := ""
	if s.db != nil {
		_ = s.db.QueryRow(
			"SELECT value FROM kv WHERE key = ?", "netgrip.executor_token."+routerID,
		).Scan(&execToken)
	}
	if execToken == "" {
		return false, nil
	}

	addr := host
	if !strings.Contains(addr, ":") {
		addr = addr + ":8080"
	}
	u := url.URL{Scheme: "http", Host: addr, Path: "/api/executor/apply"}
	body, _ := json.Marshal(map[string]any{"ops": ops})
	req, err := http.NewRequest("POST", u.String(), bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+execToken)
	req.Header.Set("X-Plan-ID", planID)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[netpulse] NetGrip no responde en %s: %v", u.Host, err)
		return false, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		log.Printf("[netpulse] NetGrip respondió %d: %s", resp.StatusCode, string(b))
		return false, nil
	}
	var res struct {
		Ok    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&res); err != nil {
		log.Printf("[netpulse] NetGrip respondió JSON inválido: %v", err)
		return false, nil
	}
	if !res.Ok {
		return false, fmt.Errorf("netgrip: %s", res.Error)
	}
	return true, nil
}

// netgripApplyResult simula un resultado de apply para marcar un plan como
// aplicado cuando NetGrip ejecutó las ops.
func netgripApplyResult() executor.ApplyResult {
	return executor.ApplyResult{
		Status: "applied",
	}
}
