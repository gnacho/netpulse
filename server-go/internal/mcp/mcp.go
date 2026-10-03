// Package mcp expone un servidor MCP (Model Context Protocol) embebido en
// NetPulse (#1114). Corre en el mismo binario y proceso, montado en /mcp
// con transporte streamable-HTTP de mcp-go. Auth: SOLO API token Bearer
// (internal/apitoken), nunca cookie de sesión. Activación: NETPULSE_MCP_ENABLED=1
// (config.MCPEnabled); desactivado por defecto.
//
// Primera tanda de tools: SOLO LECTURA sobre el último snapshot en memoria del
// poller (nunca sondean routers). Todos declaran readOnlyHint desde el día uno;
// los writes futuros (fase 3.4 del spec) deberán declarar destructiveHint.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

// Deps son las fuentes de datos de los tools. Todo lectura, todo memoria:
// el último snapshot del poller (LastOverview) y, opcionalmente, el motor de
// alertas para el read-state server truth (igual que handleOverview).
type Deps struct {
	// LastOverview devuelve el último snapshot del poller (nil si aún no hay).
	LastOverview func() *adapters.Overview
	// Engine: motor de alertas (read-state server truth). nil → el tool de
	// alertas cae al snapshot (sin read-state).
	Engine *alerts.Engine
	// Version del servidor (se expone en fleet_status).
	Version string
	// RatePerMin: límite de peticiones /mcp por IP y minuto (default 60).
	RatePerMin int
}

// Server envuelve el MCPServer de mcp-go con nuestras fuentes y auditoría.
type Server struct {
	lastOverview func() *adapters.Overview
	engine       *alerts.Engine
	version      string
	http         *server.StreamableHTTPServer
	ratePerMin   int
}

// New construye el servidor MCP con la primera tanda de tools (read-only).
func New(d Deps) *Server {
	s := &Server{
		lastOverview: d.LastOverview,
		engine:       d.Engine,
		version:      d.Version,
		ratePerMin:   d.RatePerMin,
	}
	if s.ratePerMin <= 0 {
		s.ratePerMin = 60
	}
	mcpServer := server.NewMCPServer(
		"netpulse",
		d.Version,
		server.WithToolCapabilities(false),
	)
	s.addTools(mcpServer)
	s.http = server.NewStreamableHTTPServer(mcpServer, server.WithStateLess(true))
	return s
}

// readOnly es la anotación común de TODA la primera tanda: los clientes AI
// pueden ejecutarlas libremente sin pedir confirmación al usuario. mcp-go
// defaulta destructiveHint/openWorldHint a TRUE (asume tools genéricos), así
// que hay que apagarlos explícitamente en los tools de solo lectura.
var readOnly = []mcp.ToolOption{
	mcp.WithReadOnlyHintAnnotation(true),
	mcp.WithDestructiveHintAnnotation(false),
	mcp.WithOpenWorldHintAnnotation(false),
}

// tool arma un tool con descripción + opciones propias + las anotaciones
// read-only comunes. Helper porque Go no permite mezclar argumentos sueltos y
// spread (s...) en una misma llamada variádica.
func tool(name, description string, opts ...mcp.ToolOption) mcp.Tool {
	all := make([]mcp.ToolOption, 0, len(opts)+len(readOnly)+1)
	all = append(all, mcp.WithDescription(description))
	all = append(all, opts...)
	all = append(all, readOnly...)
	return mcp.NewTool(name, all...)
}

// addTools registra los siete tools de solo lectura (spec fase 3.3, #1114).
func (s *Server) addTools(mcpServer *server.MCPServer) {
	mcpServer.AddTool(tool("fleet_status",
		"Fleet-wide status from the latest poller snapshot: health score, WAN summary, per-router status, device totals and alert counts. Read-only.",
	), s.run("fleet_status", s.toolFleetStatus))

	mcpServer.AddTool(tool("router_health",
		"Health of one monitored router: online status, health score, CPU/RAM/temperature vitals, uptime and client count from the latest snapshot. Read-only.",
		mcp.WithString("router_id",
			mcp.Required(),
			mcp.Description("Router id as listed by fleet_status"),
		),
	), s.run("router_health", s.toolRouterHealth))

	mcpServer.AddTool(tool("list_devices",
		"Known devices (clients) from the latest snapshot, optionally filtered by router. Read-only.",
		mcp.WithString("router_id",
			mcp.Description("Optional router id to filter by"),
		),
	), s.run("list_devices", s.toolListDevices))

	mcpServer.AddTool(tool("wireguard_peers",
		"WireGuard peers with tunnel IPs, active state and last handshake, as polled on the gateway. Read-only.",
	), s.run("wireguard_peers", s.toolWireGuardPeers))

	mcpServer.AddTool(tool("adguard_stats",
		"AdGuard Home stats: DNS queries and blocking rates over the last 24h, latency, top blocked domains. Read-only.",
	), s.run("adguard_stats", s.toolAdGuardStats))

	mcpServer.AddTool(tool("topology",
		"Current network topology graph derived from FDB/LLDP/SNMP: links, per-router rings and inferred distribution nodes (switches, hypervisors). Read-only.",
	), s.run("topology", s.toolTopology))

	mcpServer.AddTool(tool("alerts",
		"Active alerts from the alert engine (read-state aware). Pass active_only=true to list only unread alerts. Read-only.",
		mcp.WithBoolean("active_only",
			mcp.Description("Only return unread alerts (default false = all active alerts)"),
		),
	), s.run("alerts", s.toolAlerts))
}

// toolFunc produce el resultado de un tool a partir del snapshot actual y los
// argumentos de la petición.
type toolFunc func(ctx context.Context, req mcp.CallToolRequest, ov *adapters.Overview) (any, error)

// run envuelve cada tool con snapshot-nil check y auditoría (qué AI tocó qué).
// Los errores de negocio se devuelven como ToolResultError (isError), no como
// error Go: el protocolo lo trata como resultado válido con error dentro.
func (s *Server) run(name string, fn toolFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		start := time.Now()
		v, err := fn(ctx, req, s.lastOverview())
		audit(name, req, err, time.Since(start))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultJSON(v)
	}
}

// audit registra cada invocación de tool: nombre, argumentos, éxito y duración.
// Es la traza de auditoría del spec (fase 3.5): qué asistente tocó qué y cuándo.
func audit(name string, req mcp.CallToolRequest, err error, dur time.Duration) {
	args, _ := json.Marshal(req.GetArguments())
	if err != nil {
		log.Printf("[netpulse] mcp: tool=%s args=%s error=%q dur=%s", name, args, err.Error(), dur)
		return
	}
	log.Printf("[netpulse] mcp: tool=%s args=%s ok dur=%s", name, args, dur)
}

// overviewOrErr devuelve el snapshot actual o el error de "aún no hay datos".
func overviewOrErr(ov *adapters.Overview) (*adapters.Overview, error) {
	if ov == nil {
		return nil, fmt.Errorf("no poller snapshot available yet (the first poll has not finished)")
	}
	return ov, nil
}

// routerBrief es la forma resumida de un router para fleet_status (el tipo
// completo Router se devuelve entero en router_health).
type routerBrief struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Model   string `json:"model"`
	Role    string `json:"role"`
	Status  string `json:"status"`
	Health  int    `json:"health"`
	Clients int    `json:"clients"`
}

type fleetStatus struct {
	Version         string                `json:"version"`
	Ts              int64                 `json:"ts"`
	Health          adapters.HealthScore  `json:"health"`
	WAN             adapters.WAN          `json:"wan"`
	Routers         []routerBrief         `json:"routers"`
	DeviceTotals    adapters.DeviceTotals `json:"deviceTotals"`
	AlertCount      int                   `json:"alertCount"`
	UnreadAlerts    int                   `json:"unreadAlerts"`
	ServerUptimeSec int64                 `json:"serverUptimeSec"`
}

func (s *Server) toolFleetStatus(_ context.Context, _ mcp.CallToolRequest, ov *adapters.Overview) (any, error) {
	ov, err := overviewOrErr(ov)
	if err != nil {
		return nil, err
	}
	out := fleetStatus{
		Version:         s.version,
		Ts:              ov.Ts,
		Health:          ov.Health,
		WAN:             ov.WAN,
		DeviceTotals:    ov.DeviceTotals,
		AlertCount:      len(ov.Alerts),
		UnreadAlerts:    ov.UnreadAlerts,
		ServerUptimeSec: ov.ServerUptimeSec,
	}
	out.Routers = make([]routerBrief, 0, len(ov.Routers))
	for _, r := range ov.Routers {
		out.Routers = append(out.Routers, routerBrief{
			ID:      r.ID,
			Name:    r.Name,
			Model:   r.Model,
			Role:    r.Role,
			Status:  r.Status,
			Health:  r.Health,
			Clients: r.Clients,
		})
	}
	return out, nil
}

func (s *Server) toolRouterHealth(_ context.Context, req mcp.CallToolRequest, ov *adapters.Overview) (any, error) {
	ov, err := overviewOrErr(ov)
	if err != nil {
		return nil, err
	}
	id, err := req.RequireString("router_id")
	if err != nil {
		return nil, err
	}
	for i := range ov.Routers {
		if ov.Routers[i].ID == id {
			return ov.Routers[i], nil
		}
	}
	known := make([]string, 0, len(ov.Routers))
	for _, r := range ov.Routers {
		known = append(known, r.ID)
	}
	return nil, fmt.Errorf("router %q not found in the latest snapshot; known routers: %v", id, known)
}

func (s *Server) toolListDevices(_ context.Context, req mcp.CallToolRequest, ov *adapters.Overview) (any, error) {
	ov, err := overviewOrErr(ov)
	if err != nil {
		return nil, err
	}
	routerID := req.GetString("router_id", "")
	out := make([]adapters.Device, 0, len(ov.Devices))
	for _, d := range ov.Devices {
		if routerID == "" || d.RouterID == routerID {
			out = append(out, d)
		}
	}
	return map[string]any{"total": len(out), "devices": out}, nil
}

func (s *Server) toolWireGuardPeers(_ context.Context, _ mcp.CallToolRequest, ov *adapters.Overview) (any, error) {
	ov, err := overviewOrErr(ov)
	if err != nil {
		return nil, err
	}
	// Wireguard/Adguard son VALORES en el snapshot (nunca null en live; el
	// poller pone status "inactive" cuando no hay datos). Se reportan tal cual:
	// el propio status comunica la disponibilidad.
	if ov.Wireguard.Interface == "" && len(ov.Wireguard.Peers) == 0 {
		return nil, fmt.Errorf("no WireGuard data in the latest snapshot (no gateway with WireGuard polled)")
	}
	return ov.Wireguard, nil
}

func (s *Server) toolAdGuardStats(_ context.Context, _ mcp.CallToolRequest, ov *adapters.Overview) (any, error) {
	ov, err := overviewOrErr(ov)
	if err != nil {
		return nil, err
	}
	if ov.Adguard.Host == "" {
		return nil, fmt.Errorf("no AdGuard Home data in the latest snapshot (not configured or gateway offline)")
	}
	return ov.Adguard, nil
}

type topologyView struct {
	Links             []adapters.TopoLink         `json:"links"`
	Rings             map[string][]string         `json:"rings"`
	HiddenPeers       map[string]int              `json:"hiddenPeers,omitempty"`
	WanPeer           string                      `json:"wanPeer,omitempty"`
	DistributionNodes []adapters.DistributionNode `json:"distributionNodes"`
}

func (s *Server) toolTopology(_ context.Context, _ mcp.CallToolRequest, ov *adapters.Overview) (any, error) {
	ov, err := overviewOrErr(ov)
	if err != nil {
		return nil, err
	}
	out := topologyView{
		DistributionNodes: ov.DistributionNodes,
	}
	if ov.Topology != nil {
		out.Links = ov.Topology.Links
		out.Rings = ov.Topology.Rings
		out.HiddenPeers = ov.Topology.HiddenPeers
		out.WanPeer = ov.Topology.WanPeer
	}
	if out.Links == nil {
		out.Links = []adapters.TopoLink{}
	}
	if out.Rings == nil {
		out.Rings = map[string][]string{}
	}
	if out.DistributionNodes == nil {
		out.DistributionNodes = []adapters.DistributionNode{}
	}
	return out, nil
}

func (s *Server) toolAlerts(_ context.Context, req mcp.CallToolRequest, ov *adapters.Overview) (any, error) {
	activeOnly := req.GetBool("active_only", false)
	// Fuente canónica: el motor (read-state + dismissed/silence server truth,
	// igual que GET /api/alerts). Fallback al snapshot si no hay motor.
	var list []adapters.AlertEvent
	if s.engine != nil {
		list = s.engine.List()
	} else {
		ov, err := overviewOrErr(ov)
		if err != nil {
			return nil, err
		}
		list = ov.Alerts
	}
	out := make([]adapters.AlertEvent, 0, len(list))
	for _, a := range list {
		if activeOnly && a.Read {
			continue
		}
		out = append(out, a)
	}
	return map[string]any{"total": len(out), "alerts": out}, nil
}
