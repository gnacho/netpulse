// role_badge_1892_test.go - el badge de rol sale de la evidencia del payload,
// no del flag que autoriza los claims de la FDB (#1892). Tabla tomada de la
// flota real del reporter.
package adapters

import "testing"

func wirelessClients(n int) map[string]WirelessClient {
	m := make(map[string]WirelessClient, n)
	for i := 0; i < n; i++ {
		m[string(rune('a'+i))] = WirelessClient{SignalDbm: -60, Band: "5 GHz"}
	}
	return m
}

func portsOf(n int) []EthPort {
	out := make([]EthPort, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, EthPort{ID: string(rune('a' + i)), Up: true})
	}
	return out
}

// portsOfFamilies arma puertos con la familia que el agente les asigna. Vacio
// es boca fisica, el resto (virtual, tunnel, bridge, vlan) solo lo expone un
// host.
func portsOfFamilies(families ...string) []EthPort {
	out := make([]EthPort, 0, len(families))
	for i, f := range families {
		out = append(out, EthPort{ID: string(rune('a' + i)), Up: true, Family: f})
	}
	return out
}

func TestDisplayRoleReporterFleet(t *testing.T) {
	cases := []struct {
		name      string
		cfg       RouterConfig
		p         *routerPolled
		isGw      bool
		wantRole  string
		wantBadge string
	}{
		{
			name: "gateway", cfg: RouterConfig{ID: "istoreos-main", Type: "openwrt", IsGateway: true},
			p: &routerPolled{wireless: wirelessClients(1)}, isGw: true,
			wantRole: "Gateway principal", wantBadge: "Principal",
		},
		{
			name:     "wifi 7 AP with 13 clients and no WAN",
			cfg:      RouterConfig{ID: "gt-be98-pro", Type: "external", AgentOnly: true},
			p:        &routerPolled{wireless: wirelessClients(13)},
			wantRole: "Punto de acceso", wantBadge: "AP",
		},
		{
			name: "rt-ax92u 79d0", cfg: RouterConfig{ID: "rt-ax92u-79d0", Type: "external", AgentOnly: true},
			p:        &routerPolled{wireless: wirelessClients(5)},
			wantRole: "Punto de acceso", wantBadge: "AP",
		},
		{
			name: "rt-ax92u 7600", cfg: RouterConfig{ID: "rt-ax92u-7600", Type: "external", AgentOnly: true},
			p:        &routerPolled{wireless: wirelessClients(5)},
			wantRole: "Punto de acceso", wantBadge: "AP",
		},
		{
			name: "NAS with one wired client is not a switch",
			cfg:  RouterConfig{ID: "ds1821plus", Type: "external", AgentOnly: true},
			p:    &routerPolled{ports: portsOf(1)},
			// El fixture original contaba un puerto; el agente real anuncia
			// diez y el badge salia SW (#1892, comentario del reportero).
			wantRole: "External", wantBadge: "EXT",
		},
		{
			name: "NAS with Docker: ten interfaces, four of them not physical",
			cfg:  RouterConfig{ID: "ds1821plus", Type: "external", AgentOnly: true},
			// wan, eth0, eth1, eth2, eth3 y eth5 fisicas; los puentes de
			// Docker y el tunel sit son de host.
			p:        &routerPolled{ports: portsOfFamilies("", "", "", "", "", "", "virtual", "virtual", "virtual", "tunnel")},
			wantRole: "External", wantBadge: "EXT",
		},
		{
			name:     "host with four NICs and one docker bridge is not a switch",
			cfg:      RouterConfig{ID: "media-box", Type: "external", AgentOnly: true},
			p:        &routerPolled{ports: portsOfFamilies("", "", "", "", "bridge")},
			wantRole: "External", wantBadge: "EXT",
		},
		{
			name:     "RouterOS switch with eight ports",
			cfg:      RouterConfig{ID: "crs305", Type: "external", AgentOnly: true},
			p:        &routerPolled{ports: portsOf(8)},
			wantRole: "Switch", wantBadge: "SW",
		},
		{
			name: "SNMP switch xs708t", cfg: RouterConfig{ID: "xs708t", Type: "external", SnmpEnabled: true},
			p:        &routerPolled{ports: portsOf(8)},
			wantRole: "Switch", wantBadge: "SW",
		},
		{
			name: "SNMP switch s3300", cfg: RouterConfig{ID: "s3300-28x-poe", Type: "external", SnmpEnabled: true},
			p:        &routerPolled{ports: portsOf(28)},
			wantRole: "Switch", wantBadge: "SW",
		},
		{
			name: "SNMP switch fn-switch-8", cfg: RouterConfig{ID: "fn-switch-8", Type: "external", SnmpEnabled: true},
			p:        &routerPolled{ports: portsOf(8)},
			wantRole: "Switch", wantBadge: "SW",
		},
		{
			name: "OpenWrt unit polled over SSH", cfg: RouterConfig{ID: "rt2", Type: "openwrt"},
			p:        &routerPolled{wireless: wirelessClients(4), ports: portsOf(4)},
			wantRole: "Punto de acceso", wantBadge: "AP",
		},
		{
			name:     "offline external unit without data",
			cfg:      RouterConfig{ID: "ap-offline", Type: "external", AgentOnly: true},
			p:        nil,
			wantRole: "External", wantBadge: "EXT",
		},
		{
			name: "offline SNMP switch", cfg: RouterConfig{ID: "sw-offline", Type: "external", SnmpEnabled: true},
			p:        nil,
			wantRole: "Switch", wantBadge: "SW",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			role, badge := displayRole(c.p, c.cfg, c.isGw)
			if role != c.wantRole || badge != c.wantBadge {
				t.Fatalf("displayRole = (%q,%q), esperaba (%q,%q)", role, badge, c.wantRole, c.wantBadge)
			}
		})
	}
}

// La separacion es el punto del issue: el claim de la FDB sigue mirando la
// configuracion, aunque el badge ya no la use.
func TestClaimAuthorityStaysOnConfig(t *testing.T) {
	cfg := RouterConfig{ID: "gt-be98-pro", Type: "external", AgentOnly: true}
	if !cfgIsSwitch(cfg) {
		t.Fatal("cfgIsSwitch debe seguir autorizando el claim de un pusher external")
	}
	p := &routerPolled{wireless: wirelessClients(13)}
	if _, badge := displayRole(p, cfg, false); badge == "SW" {
		t.Fatal("un AP con clientes inalambricos no debe pintarse como switch")
	}
}
