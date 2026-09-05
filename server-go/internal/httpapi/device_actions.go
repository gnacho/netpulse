// device_actions.go — acciones directas sobre routers para un dispositivo
// (issue #439): reserva DHCP estática en el gateway y bloqueo MAC en el router
// de atache. Escriben /etc/config/dhcp y /etc/config/firewall vía SSH
// (SSHRunner, fakeable en tests) usando uci, con `uci commit` como límite
// atómico y rollback si el reinicio del servicio falla (sin config a medias).
package httpapi

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/routerstore"
)

// Techos para los comandos uci sobre SSH (show = lectura, apply = escritura
// + commit + reload). Los fija el skill de ejecución; no son configurables.
const (
	uciShowTimeout  = 10 * time.Second
	uciApplyTimeout = 20 * time.Second
)

var reUciLine = regexp.MustCompile(`^([a-z0-9_]+)\.([^=]+)=(.*)$`)
var reUciQuoted = regexp.MustCompile(`^'(.*)'$`)

// uciValue devuelve el valor sin comillas de una línea `cfg.sect.opt='val'`.
func uciValue(raw string) string {
	if m := reUciQuoted.FindStringSubmatch(raw); m != nil {
		return m[1]
	}
	return raw
}

// gatewayHost devuelve el host SSH del gateway (router con is_gateway), o ""
// si no hay gateway configurado o es agent-only. El gateway es quien sirve
// DHCP (dnsmasq), así que es el target por defecto de las reservas de lease.
func (s *server) gatewayHost() string {
	for _, r := range routerstore.ListRouters(s.db.DB) {
		if r.IsGateway && !r.AgentOnly && r.Host != "" {
			return r.Host
		}
	}
	return ""
}

// leaseRouterResolver lo implementa el adapter live para decir qué router
// reportó la MAC en su tabla de leases DHCP (issue #537). La demo no lo
// implementa → la reserva cae al gateway, como antes.
type leaseRouterResolver interface {
	RouterServingDHCP(mac string) string
}

// reservationTargetHost resuelve DÓNDE vive la reserva DHCP de un dispositivo
// (issue #537): no siempre en el gateway global. Un dispositivo cuya IP la
// concede un router con su propio dnsmasq (p. ej. una LAN separada tras un AP
// con DHCP propio) debe reservarse en ESE router, que es quien reportó su
// lease. Orden:
//  1. Router que reportó la MAC en leases (RouterServingDHCP), si el adapter
//     lo soporta y el router es alcanzable por SSH.
//  2. Fallback: el gateway (red gestionada clásica con un solo servidor DHCP).
func (s *server) reservationTargetHost(mac string) string {
	if lr, ok := s.adapter.(leaseRouterResolver); ok {
		if routerID := lr.RouterServingDHCP(mac); routerID != "" {
			if host := s.hostOfRouter(routerID); host != "" {
				return host
			}
		}
	}
	return s.gatewayHost()
}

// uciShow ejecuta `uci show <config>` en un host y devuelve líneas parseables.
func (s *server) uciShow(host, config string) ([]string, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("no hay pool SSH")
	}
	out, err := s.pool.Run(host, fmt.Sprintf("uci show %s", config), uciShowTimeout)
	if err != nil {
		return nil, err
	}
	return strings.Split(out, "\n"), nil
}

// runUCICommands encadena comandos uci + `uci commit <config>` en UNA sesión
// SSH. `uci commit` es el límite atómico: si un `uci set/delete` falla antes,
// el `&&` corta y /etc/config queda intacto (staging descartado).
func (s *server) runUCICommands(host, config string, commands []string) error {
	if s.pool == nil {
		return fmt.Errorf("no hay pool SSH")
	}
	script := strings.Join(append(commands, "uci commit "+config), " && ")
	_, err := s.pool.Run(host, script, uciApplyTimeout)
	return err
}

// reloadService reinicia el servicio procd que aplica la config commitada.
func (s *server) reloadService(host, service string) error {
	if s.pool == nil {
		return fmt.Errorf("no hay pool SSH")
	}
	_, err := s.pool.Run(host, "/etc/init.d/"+service+" restart", uciApplyTimeout)
	return err
}

// ---------------------------------------------------------------------------
// Reserva DHCP (host sections de /etc/config/dhcp)
// ---------------------------------------------------------------------------

// dhcpHost representa una sección host de dhcp UCI.
type dhcpHost struct {
	Section string
	Name    string
	MAC     string
	IP      string
}

// uciTypeSections devuelve el conjunto de secciones de un config cuya línea de
// tipo (`cfg.<section>=<type>`) coincide con el tipo pedido.
func uciTypeSections(lines []string, config, typ string) map[string]bool {
	sections := map[string]bool{}
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		m := reUciLine.FindStringSubmatch(line)
		if m == nil || m[1] != config {
			continue
		}
		if strings.Index(m[2], ".") >= 0 {
			continue // es una opción, no la línea de tipo
		}
		if uciValue(m[3]) == typ {
			sections[m[2]] = true
		}
	}
	return sections
}

// parseDhcpHosts parsea `uci show dhcp` y devuelve SOLO las secciones de tipo
// host (named `dhcp.np_host_x=host` y anónimas `dhcp.@host[0]=host`), filtrando
// el ruido de @dnsmasq[0], lan, wan, etc.
func parseDhcpHosts(lines []string) []*dhcpHost {
	hostSections := uciTypeSections(lines, "dhcp", "host")

	var hosts []*dhcpHost
	bySection := map[string]*dhcpHost{}
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		m := reUciLine.FindStringSubmatch(line)
		if m == nil || m[1] != "dhcp" {
			continue
		}
		dot := strings.Index(m[2], ".")
		if dot < 0 {
			continue
		}
		section, option := m[2][:dot], m[2][dot+1:]
		if !hostSections[section] {
			continue
		}
		h := bySection[section]
		if h == nil {
			h = &dhcpHost{Section: section}
			bySection[section] = h
			hosts = append(hosts, h)
		}
		switch option {
		case "name":
			h.Name = uciValue(m[3])
		case "mac":
			h.MAC = uciValue(m[3])
		case "ip":
			h.IP = uciValue(m[3])
		}
	}
	return hosts
}

// findDhcpHost busca una sección host cuya MAC coincida (sin importar el
// formato; admite varias MACs separadas por coma/espacio en una misma sección).
func findDhcpHost(hosts []*dhcpHost, mac string) *dhcpHost {
	macNorm := normalizeMAC(mac)
	for _, h := range hosts {
		for _, m := range strings.FieldsFunc(h.MAC, func(r rune) bool { return r == ',' || r == ' ' }) {
			if normalizeMAC(m) == macNorm {
				return h
			}
		}
	}
	return nil
}

// dhcpHostSection es el nombre de sección determinista para la reserva de una
// MAC (uci set dhcp.np_host_<mac12>=host).
func dhcpHostSection(mac string) string {
	return "np_host_" + strings.ReplaceAll(mac, ":", "")
}

// handleDeviceReservationGet devuelve la reserva DHCP estática de un dispositivo.
func (s *server) handleDeviceReservationGet(w http.ResponseWriter, r *http.Request) {
	mac := normalizeMAC(r.PathValue("mac"))
	if len(mac) != 17 {
		writeError(w, http.StatusBadRequest, "invalid_mac")
		return
	}
	host := s.reservationTargetHost(mac)
	if host == "" {
		writeError(w, http.StatusBadRequest, "no_gateway")
		return
	}
	lines, err := s.uciShow(host, "dhcp")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ssh_error", err.Error())
		return
	}
	hosts := parseDhcpHosts(lines)
	if h := findDhcpHost(hosts, mac); h != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"reserved": true,
			"ip":       h.IP,
			"name":     h.Name,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reserved": false})
}

// handleDeviceReservationPut crea o actualiza una reserva DHCP estática.
// Idempotente (misma MAC+IP → no-op). Rechaza con 409 si OTRA MAC ya tiene
// reservada la IP pedida.
func (s *server) handleDeviceReservationPut(w http.ResponseWriter, r *http.Request) {
	mac := normalizeMAC(r.PathValue("mac"))
	if len(mac) != 17 {
		writeError(w, http.StatusBadRequest, "invalid_mac")
		return
	}
	var body struct {
		IP       string `json:"ip"`
		Hostname string `json:"hostname"`
	}
	if st := readJSONBody(w, r, &body); st != 0 {
		writeBodyError(w, st, "invalid_body", "body JSON inválido")
		return
	}
	if net.ParseIP(body.IP) == nil {
		writeError(w, http.StatusBadRequest, "invalid_ip")
		return
	}
	host := s.reservationTargetHost(mac)
	if host == "" {
		writeError(w, http.StatusBadRequest, "no_gateway")
		return
	}
	lines, err := s.uciShow(host, "dhcp")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ssh_error", err.Error())
		return
	}
	hosts := parseDhcpHosts(lines)

	// Conflicto: otra MAC (distinta de ésta) ya tiene esa IP reservada.
	for _, h := range hosts {
		if h.IP != body.IP {
			continue
		}
		if findDhcpHost([]*dhcpHost{h}, mac) != nil {
			continue // es la sección de esta misma MAC (update idempotente)
		}
		writeError(w, http.StatusConflict, "ip_conflict",
			fmt.Sprintf("la IP %s ya está reservada para %s", body.IP, h.MAC))
		return
	}

	existing := findDhcpHost(hosts, mac)
	var apply, rollback []string
	if existing != nil {
		oldIP := existing.IP
		apply = append(apply, fmt.Sprintf("uci set dhcp.%s.ip='%s'", existing.Section, body.IP))
		if body.Hostname != "" && body.Hostname != existing.Name {
			apply = append(apply, fmt.Sprintf("uci set dhcp.%s.name='%s'", existing.Section, body.Hostname))
		}
		rollback = append(rollback, fmt.Sprintf("uci set dhcp.%s.ip='%s'", existing.Section, oldIP))
	} else {
		section := dhcpHostSection(mac)
		name := body.Hostname
		if name == "" {
			name = mac
		}
		apply = append(apply,
			fmt.Sprintf("uci set dhcp.%s=host", section),
			fmt.Sprintf("uci set dhcp.%s.name='%s'", section, name),
			fmt.Sprintf("uci set dhcp.%s.mac='%s'", section, mac),
			fmt.Sprintf("uci set dhcp.%s.ip='%s'", section, body.IP),
		)
		rollback = append(rollback, fmt.Sprintf("uci delete dhcp.%s", section))
	}

	if err := s.runUCICommands(host, "dhcp", apply); err != nil {
		writeError(w, http.StatusInternalServerError, "apply_error", err.Error())
		return
	}
	if err := s.reloadService(host, "dnsmasq"); err != nil {
		if rbErr := s.runUCICommands(host, "dhcp", rollback); rbErr != nil {
			log.Printf("[netpulse] rollback reserva %s falló: %v", mac, rbErr)
		}
		writeError(w, http.StatusInternalServerError, "apply_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mac": mac, "ip": body.IP})
}

// handleDeviceReservationDelete elimina la reserva DHCP estática.
func (s *server) handleDeviceReservationDelete(w http.ResponseWriter, r *http.Request) {
	mac := normalizeMAC(r.PathValue("mac"))
	if len(mac) != 17 {
		writeError(w, http.StatusBadRequest, "invalid_mac")
		return
	}
	host := s.reservationTargetHost(mac)
	if host == "" {
		writeError(w, http.StatusBadRequest, "no_gateway")
		return
	}
	lines, err := s.uciShow(host, "dhcp")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ssh_error", err.Error())
		return
	}
	hosts := parseDhcpHosts(lines)
	h := findDhcpHost(hosts, mac)
	if h == nil {
		writeError(w, http.StatusNotFound, "not_reserved")
		return
	}

	// Rollback = re-crear la sección tal cual estaba (si el reload falla).
	var rollback []string
	rollback = append(rollback, fmt.Sprintf("uci set dhcp.%s=host", h.Section))
	if h.Name != "" {
		rollback = append(rollback, fmt.Sprintf("uci set dhcp.%s.name='%s'", h.Section, h.Name))
	}
	if h.MAC != "" {
		rollback = append(rollback, fmt.Sprintf("uci set dhcp.%s.mac='%s'", h.Section, h.MAC))
	}
	if h.IP != "" {
		rollback = append(rollback, fmt.Sprintf("uci set dhcp.%s.ip='%s'", h.Section, h.IP))
	}

	apply := []string{fmt.Sprintf("uci delete dhcp.%s", h.Section)}
	if err := s.runUCICommands(host, "dhcp", apply); err != nil {
		writeError(w, http.StatusInternalServerError, "apply_error", err.Error())
		return
	}
	if err := s.reloadService(host, "dnsmasq"); err != nil {
		if rbErr := s.runUCICommands(host, "dhcp", rollback); rbErr != nil {
			log.Printf("[netpulse] rollback borrado reserva %s falló: %v", mac, rbErr)
		}
		writeError(w, http.StatusInternalServerError, "apply_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mac": mac})
}

// ---------------------------------------------------------------------------
// Bloqueo de dispositivo (rule de /etc/config/firewall, fw4)
// ---------------------------------------------------------------------------

// firewallRule representa una sección rule de firewall UCI.
type firewallRule struct {
	Section string
	Name    string
	SrcMAC  string
	Target  string
}

// parseFirewallRules parsea `uci show firewall` y devuelve las reglas (named
// `firewall.np_block_x=rule` y anónimas `firewall.@rule[0]=rule`).
func parseFirewallRules(lines []string) []*firewallRule {
	ruleSections := uciTypeSections(lines, "firewall", "rule")

	var rules []*firewallRule
	bySection := map[string]*firewallRule{}
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		m := reUciLine.FindStringSubmatch(line)
		if m == nil || m[1] != "firewall" {
			continue
		}
		dot := strings.Index(m[2], ".")
		if dot < 0 {
			continue
		}
		section, option := m[2][:dot], m[2][dot+1:]
		if !ruleSections[section] {
			continue
		}
		r := bySection[section]
		if r == nil {
			r = &firewallRule{Section: section}
			bySection[section] = r
			rules = append(rules, r)
		}
		switch option {
		case "name":
			r.Name = uciValue(m[3])
		case "src_mac":
			r.SrcMAC = uciValue(m[3])
		case "target":
			r.Target = uciValue(m[3])
		}
	}
	return rules
}

// blockRuleSection es el nombre de sección determinista para el bloqueo de una
// MAC (uci set firewall.np_block_<mac12>=rule).
func blockRuleSection(mac string) string {
	return "np_block_" + strings.ReplaceAll(mac, ":", "")
}

// blockRuleName es el nombre legible de la regla (mostrado en LuCI).
func blockRuleName(mac string) string {
	return "np-block-" + strings.ReplaceAll(mac, ":", "")
}

// findBlockRule busca la regla de bloqueo de una MAC: por sección determinista
// o por nombre legado (reglas anónimas `@rule[N]` con name='np-block-<mac>').
func findBlockRule(rules []*firewallRule, mac string) *firewallRule {
	section := blockRuleSection(mac)
	name := blockRuleName(mac)
	for _, r := range rules {
		if r.Section == section || r.Name == name {
			return r
		}
	}
	return nil
}

// blockRuleApply/blockRuleRollback construyen los comandos uci de la regla
// fw4. Forma mínima correcta (drop de tráfico reenviado desde la MAC, sin
// romper su lease DHCP, que es input al router): src=lan, dest=*, target=DROP,
// src_mac=<mac>.
func blockRuleApply(section, mac string) []string {
	return []string{
		fmt.Sprintf("uci set firewall.%s=rule", section),
		fmt.Sprintf("uci set firewall.%s.name='%s'", section, blockRuleName(mac)),
		fmt.Sprintf("uci set firewall.%s.src='lan'", section),
		fmt.Sprintf("uci set firewall.%s.dest='*'", section),
		fmt.Sprintf("uci set firewall.%s.target='DROP'", section),
		fmt.Sprintf("uci set firewall.%s.src_mac='%s'", section, mac),
	}
}

func blockRuleRollback(section string) []string {
	return []string{fmt.Sprintf("uci delete firewall.%s", section)}
}

// handleDeviceBlockGet indica si un dispositivo está bloqueado.
func (s *server) handleDeviceBlockGet(w http.ResponseWriter, r *http.Request) {
	mac := normalizeMAC(r.PathValue("mac"))
	if len(mac) != 17 {
		writeError(w, http.StatusBadRequest, "invalid_mac")
		return
	}
	routerID := r.URL.Query().Get("router")
	if routerID == "" {
		writeError(w, http.StatusBadRequest, "missing_router")
		return
	}
	host := s.hostOfRouter(routerID)
	if host == "" {
		writeError(w, http.StatusBadRequest, "no_router")
		return
	}
	lines, err := s.uciShow(host, "firewall")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ssh_error", err.Error())
		return
	}
	rules := parseFirewallRules(lines)
	blocked := findBlockRule(rules, mac) != nil
	writeJSON(w, http.StatusOK, map[string]any{"blocked": blocked})
}

// handleDeviceBlockPut bloquea un dispositivo añadiendo una regla de firewall.
func (s *server) handleDeviceBlockPut(w http.ResponseWriter, r *http.Request) {
	mac := normalizeMAC(r.PathValue("mac"))
	if len(mac) != 17 {
		writeError(w, http.StatusBadRequest, "invalid_mac")
		return
	}
	var body struct {
		RouterID string `json:"router"`
	}
	if st := readJSONBody(w, r, &body); st != 0 {
		writeBodyError(w, st, "invalid_body", "body JSON inválido")
		return
	}
	if body.RouterID == "" {
		writeError(w, http.StatusBadRequest, "missing_router")
		return
	}
	host := s.hostOfRouter(body.RouterID)
	if host == "" {
		writeError(w, http.StatusBadRequest, "no_router")
		return
	}
	lines, err := s.uciShow(host, "firewall")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ssh_error", err.Error())
		return
	}
	rules := parseFirewallRules(lines)
	if findBlockRule(rules, mac) != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mac": mac, "already": true})
		return
	}
	section := blockRuleSection(mac)
	if err := s.runUCICommands(host, "firewall", blockRuleApply(section, mac)); err != nil {
		writeError(w, http.StatusInternalServerError, "apply_error", err.Error())
		return
	}
	if err := s.reloadService(host, "firewall"); err != nil {
		if rbErr := s.runUCICommands(host, "firewall", blockRuleRollback(section)); rbErr != nil {
			log.Printf("[netpulse] rollback bloqueo %s falló: %v", mac, rbErr)
		}
		writeError(w, http.StatusInternalServerError, "apply_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mac": mac})
}

// handleDeviceBlockDelete desbloquea un dispositivo.
func (s *server) handleDeviceBlockDelete(w http.ResponseWriter, r *http.Request) {
	mac := normalizeMAC(r.PathValue("mac"))
	if len(mac) != 17 {
		writeError(w, http.StatusBadRequest, "invalid_mac")
		return
	}
	var body struct {
		RouterID string `json:"router"`
	}
	if st := readJSONBody(w, r, &body); st != 0 {
		writeBodyError(w, st, "invalid_body", "body JSON inválido")
		return
	}
	if body.RouterID == "" {
		writeError(w, http.StatusBadRequest, "missing_router")
		return
	}
	host := s.hostOfRouter(body.RouterID)
	if host == "" {
		writeError(w, http.StatusBadRequest, "no_router")
		return
	}
	lines, err := s.uciShow(host, "firewall")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "ssh_error", err.Error())
		return
	}
	rules := parseFirewallRules(lines)
	br := findBlockRule(rules, mac)
	if br == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mac": mac, "already": true})
		return
	}
	apply := []string{fmt.Sprintf("uci delete firewall.%s", br.Section)}
	rollback := []string{fmt.Sprintf("uci set firewall.%s=rule", br.Section)}
	if br.Name != "" {
		rollback = append(rollback, fmt.Sprintf("uci set firewall.%s.name='%s'", br.Section, br.Name))
	}
	if br.SrcMAC != "" {
		rollback = append(rollback, fmt.Sprintf("uci set firewall.%s.src_mac='%s'", br.Section, br.SrcMAC))
	}
	if br.Target != "" {
		rollback = append(rollback, fmt.Sprintf("uci set firewall.%s.target='%s'", br.Section, br.Target))
	}
	if err := s.runUCICommands(host, "firewall", apply); err != nil {
		writeError(w, http.StatusInternalServerError, "apply_error", err.Error())
		return
	}
	if err := s.reloadService(host, "firewall"); err != nil {
		if rbErr := s.runUCICommands(host, "firewall", rollback); rbErr != nil {
			log.Printf("[netpulse] rollback desbloqueo %s falló: %v", mac, rbErr)
		}
		writeError(w, http.StatusInternalServerError, "apply_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mac": mac})
}
