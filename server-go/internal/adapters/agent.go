// agent.go — Adapter live-agent (SPEC-AGENTE-PILOTO §1): el último payload
// empujado por el agente nativo a POST /api/ingest/agent ES el estado actual
// del router. Si el agente deja de empujar (> AgentTTL, ~3 intervalos), el
// router degrada a Tier 0 (SSH) con alerta category system urgent=false
// ("Agente caído en <nombre> — volviendo a SSH"); al volver el agente se
// retoma solo y se emite la alerta ok de recuperación.
//
// El registry guarda los payloads TAL CUAL llegan (shapes de agent/probe) y
// Live los convierte a routerPolled con el mismo anti-parpadeo del pipeline
// SSH (snapshot → SSE → persistencia sin cambios).
package adapters

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

// AgentTTLDefault: 3 intervalos de push de 15 s (default del agente) + margen.
const AgentTTLDefault = 90 * time.Second

// externalGraceFactor: los pushers externos declaran su cadencia en el
// payload (Interval, segundos); su ventana efectiva es 3x esa cadencia
// (#288). Un scraper que empuja cada 5 min declara 300 y queda fresco
// hasta 15 min, en vez de expirar a los 90 s del default nativo.
const externalGraceFactor = 3

// AgentState es el último push conocido de un agente.
type AgentState struct {
	Payload  *probe.Payload
	LastSeen time.Time
	Version  string
	Kind     string // "" | "native" | "external" (#288)
	Interval int    // cadencia declarada en segundos; 0 = default nativo
}

// effectiveTTL devuelve la ventana de frescura del estado: el TTL base, o
// 3x la cadencia declarada si es mayor (pushers externos lentos, #288).
func (r *AgentRegistry) effectiveTTL(st *AgentState) time.Duration {
	if st != nil && st.Interval > 0 {
		if d := time.Duration(st.Interval*externalGraceFactor) * time.Second; d > r.ttl {
			return d
		}
	}
	return r.ttl
}

// AgentRegistry guarda el último payload por slug (thread-safe: lo escribe el
// handler de ingesta y lo lee el sondeo live).
type AgentRegistry struct {
	mu     sync.Mutex
	ttl    time.Duration
	now    func() time.Time
	states map[string]*AgentState
}

// NewAgentRegistry crea el registry con el TTL de expiración dado
// (<= 0 → AgentTTLDefault).
func NewAgentRegistry(ttl time.Duration) *AgentRegistry {
	if ttl <= 0 {
		ttl = AgentTTLDefault
	}
	return &AgentRegistry{ttl: ttl, now: time.Now, states: map[string]*AgentState{}}
}

// SetClock sustituye el reloj (solo tests).
func (r *AgentRegistry) SetClock(f func() time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.now = f
}

// Ingest registra un push (último payload = estado actual del router).
func (r *AgentRegistry) Ingest(p *probe.Payload) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states[p.Router] = &AgentState{
		Payload: p, LastSeen: r.now(), Version: p.Version,
		Kind: p.Kind, Interval: p.Interval,
	}
}

// Info devuelve last_seen + versión + kind/interval para GET /api/agents
// (sin exponer el payload).
func (r *AgentRegistry) Info(slug string) (lastSeen time.Time, version, kind string, interval int, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, found := r.states[slug]
	if !found {
		return time.Time{}, "", "", 0, false
	}
	return st.LastSeen, st.Version, st.Kind, st.Interval, true
}

// ExternalDownConfirm escala la ventana de confirmación de caída para un
// pusher externo: max(confirm base, 3x su cadencia declarada) (#288). Con
// la base a secas, un scraper de 5 min dispararía "agente caído" tras un
// único push perdido.
func (r *AgentRegistry) ExternalDownConfirm(slug string, base time.Duration) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.states[slug]
	if !ok || st.Interval <= 0 {
		return base
	}
	if d := time.Duration(st.Interval*externalGraceFactor) * time.Second; d > base {
		return d
	}
	return base
}

// Fresh devuelve el último payload si está dentro de su ventana de
// frescura (TTL base o 3x cadencia declarada, #288).
func (r *AgentRegistry) Fresh(slug string) (*probe.Payload, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.states[slug]
	if !ok || r.now().Sub(st.LastSeen) > r.effectiveTTL(st) {
		return nil, false
	}
	return st.Payload, true
}

// Expired reporta si el slug tiene agente conocido pero su último push
// expiró (candidato a degradar a SSH con aviso).
func (r *AgentRegistry) Expired(slug string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.states[slug]
	return ok && r.now().Sub(st.LastSeen) > r.effectiveTTL(st)
}

// StalePayload devuelve el último payload del slug sin comprobar TTL.
// Sirve para usar datos stale en routers agent-only (sin SSH).
func (r *AgentRegistry) StalePayload(slug string) (*probe.Payload, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.states[slug]
	if !ok || st.Payload == nil {
		return nil, false
	}
	return st.Payload, true
}

// StaleFor reporta si el slug lleva más de threshold sin empujar datos.
// Se usa para el Dead Man's Switch (P6): confirmar que un agente está
// realmente caído antes de disparar la alerta, evitando spam en flapeos.
func (r *AgentRegistry) StaleFor(slug string, threshold time.Duration) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.states[slug]
	if !ok || st.LastSeen.IsZero() {
		return false
	}
	return r.now().Sub(st.LastSeen) > threshold
}

// ActiveCount devuelve cuántos agentes tienen su último push dentro del TTL
// (métricas operativas de /api/health).
func (r *AgentRegistry) ActiveCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, st := range r.states {
		if r.now().Sub(st.LastSeen) <= r.effectiveTTL(st) {
			n++
		}
	}
	return n
}

// Restore repuebla el estado de un slug desde el almacén persistente
// (arranque del servidor). No revalida TTL: la expiración la decide el
// reloj comparando contra el LastSeen restaurado. Sobrescribe cualquier
// estado previo del slug. Los estados persistidos antes de #288 no traen
// Kind/Interval: se recuperan del payload si están ahí.
func (r *AgentRegistry) Restore(slug string, st *AgentState) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if st != nil && st.Interval == 0 && st.Payload != nil {
		st.Kind, st.Interval = st.Payload.Kind, st.Payload.Interval
	}
	r.states[slug] = st
}

// Snapshot devuelve una copia del estado de un slug (o nil si no existe).
// Usado para persistir el último push sin exponer el map interno.
func (r *AgentRegistry) Snapshot(slug string) *AgentState {
	r.mu.Lock()
	defer r.mu.Unlock()
	st, ok := r.states[slug]
	if !ok {
		return nil
	}
	cp := *st
	return &cp
}

// Forget borra el estado del slug (revocación de token).
func (r *AgentRegistry) Forget(slug string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.states, slug)
}

// ---------------------------------------------------------------------------
// Integración con Live
// ---------------------------------------------------------------------------

// SetAgents enchufa el registry de agentes al adapter live (nil = sin
// soporte de agentes; todo va por SSH como antes).
func (l *Live) SetAgents(r *AgentRegistry) {
	l.mu.Lock()
	l.agents = r
	l.mu.Unlock()
}

// SetAgentDownConfirm fija el periodo de confirmación del Dead Man's Switch
// (P6): tiempo sin push tras el cual se confirma la caída de un agente.
func (l *Live) SetAgentDownConfirm(d time.Duration) {
	l.mu.Lock()
	if d > 0 {
		l.agentDownConfirm = d
	}
	l.mu.Unlock()
}

// agentName: nombre para las alertas (cfg.Name o Host como el resto).
func agentName(cfg RouterConfig) string {
	if cfg.Name != "" {
		return cfg.Name
	}
	return cfg.Host
}

// pollRouterAgent: si el agente del router está fresco, construye el sondeo
// desde su payload (Tier 2); si expiró, emite UNA vez la alerta de caída y
// devuelve (nil, nil) para que el caller siga por SSH (Tier 0).
// (fresh, polled) — fresh=false ⇒ el caller hace el sondeo SSH normal.
func (l *Live) pollRouterAgent(cfg RouterConfig) (bool, *routerPolled) {
	l.mu.Lock()
	reg := l.agents
	l.mu.Unlock()
	if reg == nil {
		return false, nil
	}
	if p, ok := reg.Fresh(cfg.ID); ok {
		// Recuperación: estaba caído y volvió a empujar → alerta ok.
		l.mu.Lock()
		if l.agentDown[cfg.ID] {
			l.agentDown[cfg.ID] = false
			name := agentName(cfg)
			l.engine.Emit(AlertEvent{
				ID:       fmt.Sprintf("alert-agent-ok-%s-%d", cfg.ID, time.Now().UnixMilli()),
				Category: alerts.CatSystem, Urgent: false,
				Severity:    "ok",
				Title:       "Agente recuperado en " + name,
				Description: fmt.Sprintf("El agente de %s vuelve a empujar datos — sondeo nativo reanudado", name),
				Time:        "ahora mismo", RouterID: cfg.ID,
			})
		}
		l.mu.Unlock()
		return true, l.polledFromAgent(cfg, p)
	}
	if reg.Expired(cfg.ID) {
		// Dead Man's Switch (P6): el agente está stale (más allá del TTL),
		// pero solo disparamos la alerta de caída si lleva más de
		// agentDownConfirm sin empujar. Entre TTL y agentDownConfirm el
		// router degrada a SSH silenciosamente (sin alertar), evitando
		// spam en flapeos breves de fibra/WiFi.
		confirm := l.agentDownConfirm
		if confirm <= 0 {
			confirm = 3 * time.Minute
		}
		// Pushers externos (#288): escalar la confirmación a su cadencia
		// declarada (3x interval) para no alertar caídas falsas.
		confirm = reg.ExternalDownConfirm(cfg.ID, confirm)
		if reg.StaleFor(cfg.ID, confirm) {
			l.mu.Lock()
			if !l.agentDown[cfg.ID] {
				l.agentDown[cfg.ID] = true
				name := agentName(cfg)
				if cfg.AgentOnly {
					l.engine.Emit(AlertEvent{
						ID:       fmt.Sprintf("alert-agent-down-%s-%d", cfg.ID, time.Now().UnixMilli()),
						Category: alerts.CatSystem, Urgent: false,
						Severity:    "warn",
						Title:       fmt.Sprintf("Agente caído en %s", name),
						Description: fmt.Sprintf("Sin datos del agente de %s desde hace más de %s — usando datos cacheados", name, confirm),
						Time:        "ahora mismo", RouterID: cfg.ID,
					})
				} else {
					l.engine.Emit(AlertEvent{
						ID:       fmt.Sprintf("alert-agent-down-%s-%d", cfg.ID, time.Now().UnixMilli()),
						Category: alerts.CatSystem, Urgent: false,
						Severity:    "warn",
						Title:       fmt.Sprintf("Agente caído en %s — volviendo a SSH", name),
						Description: fmt.Sprintf("Sin datos del agente de %s desde hace más de %s — sondeo SSH reanudado", name, confirm),
						Time:        "ahora mismo", RouterID: cfg.ID,
					})
				}
			}
			l.mu.Unlock()
		}
		if cfg.AgentOnly {
			if p, ok := reg.StalePayload(cfg.ID); ok {
				return true, l.polledFromAgent(cfg, p)
			}
		}
	}
	return false, nil
}

// polledFromAgent convierte el payload del agente en el routerPolled del
// pipeline (mismos shapes que el sondeo SSH). Aplica el mismo anti-parpadeo
// que pollRouter: sección ausente en el payload = sonda fallida → conserva
// el último dato bueno cacheado.
func (l *Live) polledFromAgent(cfg RouterConfig, p *probe.Payload) *routerPolled {
	l.mu.Lock()
	cached := l.extrasCache[cfg.ID]
	l.mu.Unlock()

	out := &routerPolled{cfg: cfg, client: l.clients[cfg.ID], net: &NetDevBps{}}
	sysInfo := &SysInfo{}
	ramPct := 0

	if sd := p.Data.System; sd != nil {
		if sd.SysInfo != nil {
			sysInfo = sd.SysInfo
		}
		out.board = sd.Board
		if sd.Board != nil {
			l.mu.Lock()
			l.boardCache[cfg.ID] = sd.Board
			l.mu.Unlock()
		}
		if sd.CPU != nil {
			out.cpu = *sd.CPU
		}
		if sd.Temp != nil {
			out.temp = *sd.Temp
		}
		out.net = &NetDevBps{RxBps: sd.RxBps, TxBps: sd.TxBps}
		out.latencyMs = sd.LatencyMs
		out.lossPct = sd.LossPct
		out.backhaul = sd.Backhaul
		out.brMac = sd.BridgeMAC
	}
	mem := sysInfo.Memory
	if mem.Total > 0 {
		avail := mem.Available
		if avail == 0 {
			avail = mem.Free + mem.Buffered
		}
		ramPct = int(math.Round((mem.Total - avail) / mem.Total * 100))
	}
	out.sysInfo = sysInfo
	out.ram = ramPct
	out.uptimeSec = sysInfo.Uptime

	// Secciones con anti-parpadeo (nil en el payload = sonda fallida).
	if wd := p.Data.Wireless; wd != nil {
		if wd.Clients != nil {
			out.wireless = wd.Clients
		}
		if len(wd.Radios) > 0 {
			out.radios = radiosToAdapter(wd.Radios)
		}
	}
	if dd := p.Data.DHCP; dd != nil {
		out.leases = dd.Leases
		// gl-clients del agente (GL.iNet): enriquece IPs igual que en la
		// ruta SSH (issue #5 bug 1).
		out.glClients = dd.GlClients
	}
	if fd := p.Data.FDB; fd != nil {
		if fd.MACs != nil {
			out.fdb = fd.MACs
		}
		if len(fd.Ports) > 0 {
			out.ports = ethPortsToAdapter(fd.Ports)
			// #291: los pushers externos declaran el puerto del FDB como
			// número ("1") mientras sus bocas usan id "lan1". Normalizar
			// para que el enriquecimiento del detalle (MAC→nombre por boca)
			// y la topología casen ambas claves.
			ids := map[string]bool{}
			for _, ep := range out.ports {
				ids[ep.ID] = true
			}
			if len(ids) > 0 {
				for mac, port := range out.fdb {
					if port == "wan" || ids[port] {
						continue
					}
					if lan := "lan" + port; ids[lan] {
						out.fdb[mac] = lan
					}
				}
			}
		}
	}
	// LuCI (issue #258): etiquetas de puertos/VLANs, fuente de nombres de
	// topología. Best-effort: sección ausente → conserva la última buena.
	if p.Data.LuCI != nil {
		out.luci = p.Data.LuCI
	}

	if cached == nil {
		cached = &extrasSnapshot{ports: []EthPort{}, radios: []Radio{},
			wireless: map[string]WirelessClient{}, fdb: map[string]string{}}
	}
	if out.ports == nil {
		out.ports = cached.ports
	}
	if out.radios == nil {
		out.radios = cached.radios
	}
	if out.wireless == nil {
		out.wireless = cached.wireless
	}
	if out.fdb == nil {
		out.fdb = cached.fdb
	}
	if out.luci == nil {
		out.luci = cached.luci
	}
	l.mu.Lock()
	l.extrasCache[cfg.ID] = &extrasSnapshot{ports: out.ports, radios: out.radios,
		wireless: out.wireless, fdb: out.fdb, luci: out.luci}
	l.mu.Unlock()

	if out.leases == nil {
		out.leases = []DhcpLease{}
	}
	return out
}
