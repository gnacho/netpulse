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
	"strings"
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

// MatchRouter busca el agente asociado a un router: primero por slug exacto,
// luego por hostname de board y finalmente por bridge MAC (#282). Devuelve
// el slug real del agente, su payload y si está fresco. Permite que un agente
// emparejado con un slug elegido por el usuario alimente un router cuyo id
// autogenerado no coincide con ese slug.
func (r *AgentRegistry) MatchRouter(cfg RouterConfig, macs map[string]string) (string, *probe.Payload, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	norm := func(s string) string {
		return strings.ToLower(strings.TrimSpace(s))
	}
	hostNames := []string{}
	if cfg.Host != "" {
		hostNames = append(hostNames, norm(cfg.Host))
	}
	if cfg.Name != "" && norm(cfg.Name) != norm(cfg.Host) {
		hostNames = append(hostNames, norm(cfg.Name))
	}
	if cfg.ID != "" && norm(cfg.ID) != norm(cfg.Host) && norm(cfg.ID) != norm(cfg.Name) {
		hostNames = append(hostNames, norm(cfg.ID))
	}

	var match *AgentState
	var matchSlug string
	for slug, st := range r.states {
		if slug == cfg.ID {
			match = st
			matchSlug = slug
			break
		}
		if st.Payload == nil || st.Payload.Data.System == nil || st.Payload.Data.System.Board == nil {
			continue
		}
		h := norm(st.Payload.Data.System.Board.Hostname)
		if h != "" {
			for _, candidate := range hostNames {
				if h == candidate {
					match = st
					matchSlug = slug
					break
				}
			}
		}
		if match != nil {
			break
		}
		mac := st.Payload.Data.System.BridgeMAC
		if mac != "" && macs != nil {
			if routerMac, ok := macs[cfg.ID]; ok && strings.EqualFold(mac, routerMac) {
				match = st
				matchSlug = slug
				break
			}
		}
	}
	if match == nil {
		return "", nil, false
	}
	fresh := r.now().Sub(match.LastSeen) <= r.effectiveTTL(match)
	return matchSlug, match.Payload, fresh
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

// pollRouterAgent: si un agente asociado al router está fresco, construye el
// sondeo desde su payload (Tier 2). Si expiró, emite UNA vez la alerta de
// caída y devuelve (false, nil) para que el caller siga por SSH (Tier 0).
// El emparejamiento no exige slug == router.id: también usa hostname/MAC
// (#282). Cuando el agente está vivo pero el SSH falla por clave no
// autorizada, emite una alerta informativa en vez de dejar la tarjeta en
// offline (#281).
func (l *Live) pollRouterAgent(cfg RouterConfig) (bool, *routerPolled) {
	l.mu.Lock()
	reg := l.agents
	macs := make(map[string]string, len(l.routerMacs))
	for k, v := range l.routerMacs {
		macs[k] = v
	}
	l.mu.Unlock()
	if reg == nil {
		return false, nil
	}

	slug, p, fresh := reg.MatchRouter(cfg, macs)
	if fresh {
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
		// issue #281: SSH falla por clave no autorizada pero el agente sigue
		// vivo → avisa una sola vez. Se borra el flag cuando SSH vuelve.
		if l.lastErr[cfg.ID] != nil && isAccessError(l.lastErr[cfg.ID]) {
			if !l.sshAuthFailAlerted[cfg.ID] {
				l.sshAuthFailAlerted[cfg.ID] = true
				name := agentName(cfg)
				l.engine.Emit(AlertEvent{
					ID:       fmt.Sprintf("alert-ssh-auth-%s-%d", cfg.ID, time.Now().UnixMilli()),
					Category: alerts.CatSystem, Urgent: false,
					Severity:    "warn",
					Title:       "Acceso SSH perdido en " + name,
					Description: fmt.Sprintf("%s no acepta la clave SSH, pero su agente sigue enviando datos. Revisa authorized_keys tras un firmware upgrade.", name),
					Time:        "ahora mismo", RouterID: cfg.ID,
				})
			}
		} else if l.sshAuthFailAlerted[cfg.ID] {
			delete(l.sshAuthFailAlerted, cfg.ID)
			name := agentName(cfg)
			l.engine.Emit(AlertEvent{
				ID:       fmt.Sprintf("alert-ssh-auth-ok-%s-%d", cfg.ID, time.Now().UnixMilli()),
				Category: alerts.CatSystem, Urgent: false,
				Severity:    "ok",
				Title:       "Acceso SSH recuperado en " + name,
				Description: fmt.Sprintf("El acceso SSH a %s funciona de nuevo", name),
				Time:        "ahora mismo", RouterID: cfg.ID,
			})
		}
		l.mu.Unlock()
		return true, l.polledFromAgent(cfg, p)
	}

	if slug != "" {
		// Dead Man's Switch (P6): el agente está stale, pero solo disparamos la
		// alerta de caída si lleva más de agentDownConfirm sin empujar. Entre
		// TTL y agentDownConfirm el router degrada a SSH silenciosamente.
		confirm := l.agentDownConfirm
		if confirm <= 0 {
			confirm = 3 * time.Minute
		}
		confirm = reg.ExternalDownConfirm(slug, confirm)
		if reg.StaleFor(slug, confirm) {
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
						Hint:        alerts.HintFor(alerts.HintAgentDown),
						Time:        "ahora mismo", RouterID: cfg.ID,
					})
				} else {
					l.engine.Emit(AlertEvent{
						ID:       fmt.Sprintf("alert-agent-down-%s-%d", cfg.ID, time.Now().UnixMilli()),
						Category: alerts.CatSystem, Urgent: false,
						Severity:    "warn",
						Title:       fmt.Sprintf("Agente caído en %s — volviendo a SSH", name),
						Description: fmt.Sprintf("Sin datos del agente de %s desde hace más de %s — sondeo SSH reanudado", name, confirm),
						Hint:        alerts.HintFor(alerts.HintAgentDown),
						Time:        "ahora mismo", RouterID: cfg.ID,
					})
				}
			}
			l.mu.Unlock()
		}
		if cfg.AgentOnly {
			if p != nil {
				return true, l.polledFromAgent(cfg, p)
			}
		}
	}
	return false, nil
}

// agentIfSample: contadores por iface del último payload del agente (para
// el delta de rates por boca, issue #305).
type agentIfSample struct {
	at     time.Time
	ifaces map[string]probe.IfCounters
}

// injectPortRates copia absolutos + rates de rates (keyed por iface física)
// sobre las bocas: match por Iface si la boca la declara, si no por ID (en
// DSA coinciden; en swconfig el ID es numérico y solo Iface casa).
func injectPortRates(ports []EthPort, rates map[string]probe.IfRate) {
	for i := range ports {
		key := ports[i].Iface
		if key == "" {
			key = ports[i].ID
		}
		r, ok := rates[key]
		if !ok {
			continue
		}
		ports[i].Iface = key
		ports[i].RxBytes = r.Rx
		ports[i].TxBytes = r.Tx
		ports[i].RxErrs = r.RxErr
		ports[i].TxErrs = r.TxErr
		if r.RxBps != nil {
			ports[i].RxBps = *r.RxBps
		}
		if r.TxBps != nil {
			ports[i].TxBps = *r.TxBps
		}
	}
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

	// NetIf (issue #305): rates por boca con el delta entre payloads del
	// agente. El payload trae contadores ABSOLUTOS por iface; el server
	// calcula bps con la muestra anterior (ts del propio payload, no el
	// reloj del server). Sección ausente → conserva los rates cacheados.
	// Corre tras el anti-parpadeo (bocas definitivas) y ANTES de guardar el
	// snapshot para que el cache lleve los rates frescos.
	if p.Data.NetIf != nil {
		now := time.Unix(p.Ts, 0)
		l.mu.Lock()
		prev := l.lastAgentIf[cfg.ID]
		l.lastAgentIf[cfg.ID] = &agentIfSample{at: now, ifaces: p.Data.NetIf}
		l.mu.Unlock()
		if prev != nil && now.Sub(prev.at).Seconds() > 0 && len(out.ports) > 0 {
			rates := probe.IfRates(prev.ifaces, p.Data.NetIf, now.Sub(prev.at).Seconds())
			injectPortRates(out.ports, rates)
		}
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
