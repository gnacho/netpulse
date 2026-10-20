package adapters

import (
	"context"
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
)

// uciWireless11rFixture: `uci show wireless` de un AP con dos radios y el
// mismo SSID en ambas, con 802.11r activo (el caso real de la flota de
// oficina del issue #1450: ieee80211r=1, mobility_domain=2025, ft_over_ds=1).
const uciWireless11rFixture = `wireless.radio0=wifi-device
wireless.radio0.band='2g'
wireless.radio0.channel='1'
wireless.wifi2g=wifi-iface
wireless.wifi2g.device='radio0'
wireless.wifi2g.network='lan'
wireless.wifi2g.mode='ap'
wireless.wifi2g.ssid='temiscira'
wireless.wifi2g.ieee80211k='1'
wireless.wifi2g.ieee80211r='1'
wireless.wifi2g.mobility_domain='2025'
wireless.wifi2g.ft_over_ds='1'
wireless.wifi2g.ft_psk_generate_local='1'
wireless.wifi2g.ifname='wlan0'
wireless.radio1=wifi-device
wireless.radio1.band='5g'
wireless.radio1.channel='36'
wireless.wifi5g=wifi-iface
wireless.wifi5g.device='radio1'
wireless.wifi5g.network='lan'
wireless.wifi5g.mode='ap'
wireless.wifi5g.ssid='temiscira'
wireless.wifi5g.ieee80211r='1'
wireless.wifi5g.mobility_domain='2025'
wireless.wifi5g.ft_over_ds='1'
wireless.wifi5g.ft_psk_generate_local='1'
wireless.wifi5g.ifname='wlan1'
`

// TestGetDot11rAgentOnlyFromPayload (#1450): en una flota agent-only la vista
// 802.11r se alimenta del payload del agente, sin SSH. Pool nil como
// tripwire: si la guarda agent-only se rompe y se intenta SSH, paniquea.
func TestGetDot11rAgentOnlyFromPayload(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "flint2", Name: "flint2", Host: "192.168.1.1", AgentOnly: true},
		{ID: "rt2", Name: "rt2", Host: "192.168.1.2", AgentOnly: true},
	}, nil)
	reg := NewAgentRegistry(time.Hour)
	for _, slug := range []string{"flint2", "rt2"} {
		reg.Ingest(&probe.Payload{
			Router:  slug,
			Version: "3.0.20",
			Data: probe.PayloadData{Wireless: &probe.WirelessData{
				Clients:        map[string]probe.WirelessClient{},
				UciWirelessRaw: uciWireless11rFixture,
			}},
		})
	}
	l.SetAgents(reg)

	d, err := l.GetDot11r(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == nil || !d.Available {
		t.Fatalf("esperaba overview disponible, got %+v", d)
	}
	if len(d.Routers) != 2 {
		t.Fatalf("esperaba 2 routers, got %d: %+v", len(d.Routers), d.Routers)
	}
	for _, r := range d.Routers {
		if !r.Available || len(r.Ifaces) != 2 {
			t.Fatalf("router %s sin datos del agente: %+v", r.RouterID, r)
		}
	}

	if len(d.SSIDs) != 1 {
		t.Fatalf("esperaba 1 SSID, got %+v", d.SSIDs)
	}
	s := d.SSIDs[0]
	if s.SSID != "temiscira" {
		t.Errorf("SSID = %q, want temiscira", s.SSID)
	}
	if s.EnabledCount != 4 || s.TotalCount != 4 || !s.EnabledEverywhere {
		t.Errorf("cobertura 11r = %d/%d everywhere=%v, want 4/4 true",
			s.EnabledCount, s.TotalCount, s.EnabledEverywhere)
	}
	if s.MobilityDomain != "2025" {
		t.Errorf("mobility domain = %q, want 2025", s.MobilityDomain)
	}
	if !s.FTOverDS {
		t.Errorf("ft_over_ds = false, want true")
	}
	if !s.FTPSKGenerateLocal {
		t.Errorf("ft_psk_generate_local = false, want true")
	}
	if len(s.RouterIDs) != 2 || s.RouterIDs[0] != "flint2" || s.RouterIDs[1] != "rt2" {
		t.Errorf("routerIDs = %v, want [flint2 rt2]", s.RouterIDs)
	}
}

// TestGetDot11rAgentOnlyNoPayload: un router agent-only cuyo agente todavía
// no ha empujado (o que es de un tipo sin sección wireless) queda como antes:
// sin datos y sin error. Igual que el test SNMP, pool nil es el tripwire.
func TestGetDot11rAgentOnlyNoPayload(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "rt2", Name: "rt2", Host: "192.168.1.2", AgentOnly: true},
	}, nil)
	l.SetAgents(NewAgentRegistry(time.Hour))

	d, err := l.GetDot11r(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != nil {
		t.Fatalf("expected nil Dot11rOverview sin payload, got %+v", d)
	}
}

// TestGetDot11rAgentOldWithoutField: un agente anterior a #1450 no manda
// uciWirelessRaw. Debe degradar (sin datos), no romper ni inventar.
func TestGetDot11rAgentOldWithoutField(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "rt2", Name: "rt2", Host: "192.168.1.2", AgentOnly: true},
	}, nil)
	reg := NewAgentRegistry(time.Hour)
	reg.Ingest(&probe.Payload{
		Router:  "rt2",
		Version: "3.0.18",
		Data: probe.PayloadData{Wireless: &probe.WirelessData{
			Clients: map[string]probe.WirelessClient{"AA:BB:CC:DD:EE:FF": {SignalDbm: -55, Band: "2.4 GHz"}},
		}},
	})
	l.SetAgents(reg)

	d, err := l.GetDot11r(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != nil {
		t.Fatalf("agente viejo: esperaba nil, got %+v", d)
	}
}

// TestGetDot11rMixedFleet: agent-only con payload + unidad SNMP. La SNMP se
// lista sin datos y el resto de la vista sigue disponible.
func TestGetDot11rMixedFleet(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "rt2", Name: "rt2", Host: "192.168.1.2", AgentOnly: true},
		{ID: "sw1", Name: "Switch 1", Host: "192.168.1.10", SnmpEnabled: true},
	}, nil)
	reg := NewAgentRegistry(time.Hour)
	reg.Ingest(&probe.Payload{
		Router:  "rt2",
		Version: "3.0.20",
		Data: probe.PayloadData{Wireless: &probe.WirelessData{
			Clients:        map[string]probe.WirelessClient{},
			UciWirelessRaw: uciWireless11rFixture,
		}},
	})
	l.SetAgents(reg)

	d, err := l.GetDot11r(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == nil || !d.Available {
		t.Fatalf("esperaba overview disponible, got %+v", d)
	}
	var sw *Dot11rRouter
	for i := range d.Routers {
		if d.Routers[i].RouterID == "sw1" {
			sw = &d.Routers[i]
		}
	}
	if sw == nil || sw.Available || len(sw.Ifaces) != 0 {
		t.Fatalf("la unidad SNMP debe seguir sin datos: %+v", sw)
	}
}

// TestGetDot11rAgentOnlyStalePayload: un payload fuera del TTL no pinta la
// config como actual (mismo criterio que /api/usteer, #1431).
func TestGetDot11rAgentOnlyStalePayload(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "rt2", Name: "rt2", Host: "192.168.1.2", AgentOnly: true},
	}, nil)
	reg := NewAgentRegistry(time.Minute)
	now := time.Now()
	reg.SetClock(func() time.Time { return now })
	reg.Ingest(&probe.Payload{
		Router:  "rt2",
		Version: "3.0.20",
		Data: probe.PayloadData{Wireless: &probe.WirelessData{
			Clients:        map[string]probe.WirelessClient{},
			UciWirelessRaw: uciWireless11rFixture,
		}},
	})
	reg.SetClock(func() time.Time { return now.Add(10 * time.Minute) })
	l.SetAgents(reg)

	d, err := l.GetDot11r(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d != nil {
		t.Fatalf("payload caducado: esperaba nil, got %+v", d)
	}
}

// TestGetDot11rNo11rEnabledIsAvailable: si la config se leyó pero ningún SSID
// tiene 802.11r, la vista está disponible con el estado "inactivo" en vez de
// devolver 503 (que la UI mostraba como error de lectura).
func TestGetDot11rNo11rEnabledIsAvailable(t *testing.T) {
	sin11r := `wireless.radio0=wifi-device
wireless.radio0.band='2g'
wireless.radio0.channel='6'
wireless.wifi2g=wifi-iface
wireless.wifi2g.device='radio0'
wireless.wifi2g.network='lan'
wireless.wifi2g.ssid='temiscira'
wireless.wifi2g.ieee80211r='0'
`
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "rt2", Name: "rt2", Host: "192.168.1.2", AgentOnly: true},
	}, nil)
	reg := NewAgentRegistry(time.Hour)
	reg.Ingest(&probe.Payload{
		Router:  "rt2",
		Version: "3.0.20",
		Data: probe.PayloadData{Wireless: &probe.WirelessData{
			Clients:        map[string]probe.WirelessClient{},
			UciWirelessRaw: sin11r,
		}},
	})
	l.SetAgents(reg)

	d, err := l.GetDot11r(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == nil || !d.Available {
		t.Fatalf("esperaba overview disponible (11r inactivo), got %+v", d)
	}
	if len(d.SSIDs) != 1 || d.SSIDs[0].EnabledCount != 0 || d.SSIDs[0].EnabledEverywhere {
		t.Fatalf("SSIDs = %+v, want temiscira con 11r inactivo", d.SSIDs)
	}
}

// TestDot11rIfacesFromPayloadDegenerados: el helper nunca paniquea y devuelve
// nil para payload/sección/campo ausentes.
func TestDot11rIfacesFromPayloadDegenerados(t *testing.T) {
	cases := map[string]*probe.Payload{
		"nil":            nil,
		"sin-wireless":   {Router: "rt2"},
		"sin-campo":      {Router: "rt2", Data: probe.PayloadData{Wireless: &probe.WirelessData{}}},
		"raw-sin-ifaces": {Router: "rt2", Data: probe.PayloadData{Wireless: &probe.WirelessData{UciWirelessRaw: "wireless.radio0=wifi-device\n"}}},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			if got := dot11rIfacesFromPayload(p); len(got) != 0 {
				t.Fatalf("esperaba 0 ifaces, got %+v", got)
			}
		})
	}
	if got := dot11rIfacesFromPayload(&probe.Payload{Data: probe.PayloadData{Wireless: &probe.WirelessData{UciWirelessRaw: uciWireless11rFixture}}}); len(got) != 2 {
		t.Fatalf("esperaba 2 ifaces del fixture, got %d", len(got))
	}
}
