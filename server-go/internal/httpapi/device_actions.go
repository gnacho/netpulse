// device_actions.go — acciones directas sobre routers para un dispositivo:
// reserva DHCP estática en gateway y bloqueo MAC en el router de atache.
// (issue #439)
package httpapi

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
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

// routerHost devuelve la IP/host de gestión SSH de un router por ID.
func (s *server) routerHost(ctx context.Context, routerID string) string {
	for _, r := range s.adapter.GetRouters(ctx) {
		if r.ID == routerID {
			return r.IP
		}
	}
	return ""
}

// gatewayRouterID devuelve el router marcado como gateway, o "gateway" como fallback.
func (s *server) gatewayRouterID(ctx context.Context) string {
	for _, r := range s.adapter.GetRouters(ctx) {
		if r.RoleBadge == "Principal" || strings.Contains(strings.ToLower(r.Role), "gateway") {
			return r.ID
		}
	}
	return "gateway"
}

// uciShow ejecuta `uci show <config>` en un host y devuelve líneas parseables.
func (s *server) uciShow(host, config string) ([]string, error) {
	if s.pool == nil {
		return nil, fmt.Errorf("no hay pool SSH")
	}
	out, err := s.pool.Run(host, fmt.Sprintf("uci show %s", config), 10*time.Second)
	if err != nil {
		return nil, err
	}
	return strings.Split(out, "\n"), nil
}

// runUCI ejecuta una lista de comandos UCI + reinicio de servicio en un host.
func (s *server) runUCI(host string, commands []string, serviceName string) error {
	if s.pool == nil {
		return fmt.Errorf("no hay pool SSH")
	}
	script := strings.Join(commands, " && ")
	if serviceName != "" {
		script += fmt.Sprintf(" && /etc/init.d/%s restart", serviceName)
	}
	_, err := s.pool.Run(host, script, 20*time.Second)
	return err
}

// dhcpHost representa una sección host de dhcp UCI.
type dhcpHost struct {
	Section  string
	Name     string
	Hostname string
	MAC      string
	IP       string
}

// parseDhcpHosts parsea `uci show dhcp` y devuelve todas las secciones host.
func parseDhcpHosts(lines []string) []*dhcpHost {
	var hosts []*dhcpHost
	current := map[string]*dhcpHost{}
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		m := reUciLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		config, rest, value := m[1], m[2], uciValue(m[3])
		if config != "dhcp" {
			continue
		}
		// rest puede ser nombre de sección o @host[idx]
		dot := strings.Index(rest, ".")
		if dot < 0 {
			continue
		}
		section, option := rest[:dot], rest[dot+1:]
		if !strings.HasPrefix(value, "host") && option == "" {
			// dhcp.hosts=host
			continue
		}
		if _, ok := current[section]; !ok {
			h := &dhcpHost{Section: section}
			current[section] = h
			hosts = append(hosts, h)
		}
		switch option {
		case "name":
			current[section].Name = value
		case "hostname":
			current[section].Hostname = value
		case "mac":
			current[section].MAC = value
		case "ip":
			current[section].IP = value
		}
	}
	return hosts
}

// findDhcpHost busca una sección host cuya MAC coincida (sin importar formato).
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

// handleDeviceReservationGet devuelve la reserva DHCP estática de un dispositivo.
func (s *server) handleDeviceReservationGet(w http.ResponseWriter, r *http.Request) {
	mac := normalizeMAC(r.PathValue("mac"))
	if len(mac) != 17 {
		writeError(w, http.StatusBadRequest, "invalid_mac")
		return
	}
	gwID := s.gatewayRouterID(r.Context())
	host := s.routerHost(r.Context(), gwID)
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
			"hostname": h.Hostname,
			"section":  h.Section,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reserved": false})
}

// handleDeviceReservationPut crea o actualiza una reserva DHCP estática.
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
	gwID := s.gatewayRouterID(r.Context())
	host := s.routerHost(r.Context(), gwID)
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
	var commands []string
	if h := findDhcpHost(hosts, mac); h != nil {
		commands = append(commands,
			fmt.Sprintf("uci set dhcp.%s.ip='%s'", h.Section, body.IP),
		)
		if body.Hostname != "" {
			commands = append(commands, fmt.Sprintf("uci set dhcp.%s.hostname='%s'", h.Section, body.Hostname))
		}
	} else {
		section := "np_host_" + strings.ReplaceAll(mac, ":", "")
		commands = append(commands,
			fmt.Sprintf("uci set dhcp.%s=host", section),
			fmt.Sprintf("uci set dhcp.%s.name='%s'", section, body.Hostname),
			fmt.Sprintf("uci set dhcp.%s.mac='%s'", section, mac),
			fmt.Sprintf("uci set dhcp.%s.ip='%s'", section, body.IP),
		)
		if body.Hostname != "" {
			commands = append(commands, fmt.Sprintf("uci set dhcp.%s.hostname='%s'", section, body.Hostname))
		}
	}
	commands = append(commands, "uci commit dhcp")
	if err := s.runUCI(host, commands, "dnsmasq"); err != nil {
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
	gwID := s.gatewayRouterID(r.Context())
	host := s.routerHost(r.Context(), gwID)
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
	commands := []string{
		fmt.Sprintf("uci delete dhcp.%s", h.Section),
		"uci commit dhcp",
	}
	if err := s.runUCI(host, commands, "dnsmasq"); err != nil {
		writeError(w, http.StatusInternalServerError, "apply_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mac": mac})
}

// firewallRule representa una sección rule de firewall UCI.
type firewallRule struct {
	Section string
	Name    string
	SrcMAC  string
	Target  string
}

// parseFirewallRules parsea `uci show firewall` y devuelve las rules.
func parseFirewallRules(lines []string) []*firewallRule {
	var rules []*firewallRule
	current := map[string]*firewallRule{}
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		m := reUciLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		config, rest, value := m[1], m[2], uciValue(m[3])
		if config != "firewall" {
			continue
		}
		dot := strings.Index(rest, ".")
		if dot < 0 {
			continue
		}
		section, option := rest[:dot], rest[dot+1:]
		if _, ok := current[section]; !ok {
			if strings.HasPrefix(section, "@rule[") || (strings.HasPrefix(value, "rule") && option == "") {
				r := &firewallRule{Section: section}
				current[section] = r
				rules = append(rules, r)
			}
		}
		r, ok := current[section]
		if !ok {
			continue
		}
		switch option {
		case "name":
			r.Name = value
		case "src_mac":
			r.SrcMAC = value
		case "target":
			r.Target = value
		}
	}
	return rules
}

func blockRuleName(mac string) string {
	return "np-block-" + strings.ReplaceAll(mac, ":", "")
}

// findBlockRule busca la regla de bloqueo de una MAC.
func findBlockRule(rules []*firewallRule, mac string) *firewallRule {
	want := blockRuleName(mac)
	for _, rule := range rules {
		if rule.Name == want {
			return rule
		}
	}
	return nil
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
	host := s.routerHost(r.Context(), routerID)
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
	host := s.routerHost(r.Context(), body.RouterID)
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
	commands := []string{
		"uci add firewall rule",
		"uci set firewall.@rule[-1].name='" + blockRuleName(mac) + "'",
		"uci set firewall.@rule[-1].src='lan'",
		"uci set firewall.@rule[-1].dest='*'",
		"uci set firewall.@rule[-1].target='DROP'",
		"uci set firewall.@rule[-1].src_mac='" + mac + "'",
		"uci commit firewall",
	}
	if err := s.runUCI(host, commands, "firewall"); err != nil {
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
	host := s.routerHost(r.Context(), body.RouterID)
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
	commands := []string{
		fmt.Sprintf("uci delete firewall.%s", br.Section),
		"uci commit firewall",
	}
	if err := s.runUCI(host, commands, "firewall"); err != nil {
		writeError(w, http.StatusInternalServerError, "apply_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mac": mac})
}
