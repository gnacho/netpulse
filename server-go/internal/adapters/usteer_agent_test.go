package adapters

import (
	"context"
	"testing"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
)

// fixtureUsteerPayload: router agent-only con un SSID, 2 APs locales
// (2.4/5 GHz) y 1 remoto del mesh, 6 clientes con distintos grados de
// información (banda conocida, sin estación, sin señal, banda desconocida).
func fixtureUsteerPayload() *probe.Payload {
	return &probe.Payload{
		Router: "rt2",
		Data: probe.PayloadData{
			Usteer: &probe.UsteerData{
				SSIDs: map[string]probe.UsteerSSID{
					"domatix": {
						APs: []probe.UsteerAP{
							{BSSID: "cc:d8:43:b1:e5:96", Freq: 2412, Load: 30, Clients: 3, Local: true},
							{BSSID: "cc:d8:43:b1:e5:97", Freq: 5180, Load: 8, Clients: 1, Local: true},
							{BSSID: "be:dc:e3:b2:d3:db", Freq: 2437, Load: 12, Clients: 5, Local: false},
						},
						Clients: map[string]probe.UsteerClient{
							"aa:aa:aa:aa:aa:01": {Signal: -45},
							"aa:aa:aa:aa:aa:02": {Signal: -70},
							"aa:aa:aa:aa:aa:03": {Signal: -60},
							"aa:aa:aa:aa:aa:04": {Signal: 0},   // sin señal: se ignora
							"aa:aa:aa:aa:aa:05": {Signal: -50}, // banda desconocida con 2 APs: no se adivina
							"aa:aa:aa:aa:aa:06": {Signal: -75}, // estación sin señal: manda la de usteer
						},
					},
				},
			},
			Wireless: &probe.WirelessData{
				OwnBssids: []probe.OwnBSSID{
					{Iface: "phy0-ap0", BSSID: "CC:D8:43:B1:E5:96", SSID: "domatix"},
					{Iface: "phy1-ap0", BSSID: "CC:D8:43:B1:E5:97", SSID: "domatix"},
				},
				Clients: map[string]probe.WirelessClient{
					"aa:aa:aa:aa:aa:01": {SignalDbm: -52, Band: "2.4 GHz"},
					"aa:aa:aa:aa:aa:02": {SignalDbm: -68, Band: "2.4 GHz"},
					"aa:aa:aa:aa:aa:03": {SignalDbm: -61, Band: "5 GHz"},
					"aa:aa:aa:aa:aa:06": {SignalDbm: 0, Band: "2.4 GHz"},
				},
			},
		},
	}
}

func TestUsteerAPsFromPayload(t *testing.T) {
	aps, seen := usteerAPsFromPayload("rt2", fixtureUsteerPayload())
	if seen != 2 || len(aps) != 2 {
		t.Fatalf("esperaba 2 APs locales, got %d (seen=%d)", len(aps), seen)
	}
	// Orden determinista: 2.4 GHz primero (BSSID ...:96 < ...:97).
	ap24, ap5 := aps[0], aps[1]
	if ap24.BSSID != "CC:D8:43:B1:E5:96" || ap24.Iface != "phy0-ap0" {
		t.Errorf("ap 2.4 = %+v", ap24)
	}
	if ap24.Band != "2.4 GHz" || ap24.Freq != 2412 || ap24.UtilizationPct != 30 || ap24.ClientCount != 3 {
		t.Errorf("ap 2.4 band/freq/load/count = %q/%d/%d/%d", ap24.Band, ap24.Freq, ap24.UtilizationPct, ap24.ClientCount)
	}
	if !ap24.Local || ap24.Hostname != "rt2" || ap24.SSID != "domatix" {
		t.Errorf("ap 2.4 local/hostname/ssid = %v/%q/%q", ap24.Local, ap24.Hostname, ap24.SSID)
	}
	// Clientes del 2.4: 01 (-52 de la estación, no -45 de usteer), 02 (-68)
	// y 06 (-75 de usteer, la estación no trae señal). 04 sin señal fuera,
	// 05 con banda desconocida y 2 APs candidatos fuera (no se adivina).
	want24 := []UsteerClient{
		{MAC: "AA:AA:AA:AA:AA:01", Signal: -52},
		{MAC: "AA:AA:AA:AA:AA:02", Signal: -68},
		{MAC: "AA:AA:AA:AA:AA:06", Signal: -75},
	}
	if len(ap24.Clients) != len(want24) {
		t.Fatalf("clientes 2.4 = %+v, want %+v", ap24.Clients, want24)
	}
	for i, c := range want24 {
		if ap24.Clients[i] != c {
			t.Errorf("cliente 2.4[%d] = %+v, want %+v", i, ap24.Clients[i], c)
		}
	}
	// Cliente del 5 GHz.
	if len(ap5.Clients) != 1 || ap5.Clients[0] != (UsteerClient{MAC: "AA:AA:AA:AA:AA:03", Signal: -61}) {
		t.Errorf("clientes 5 = %+v", ap5.Clients)
	}
	if ap5.Iface != "phy1-ap0" || ap5.Band != "5 GHz" {
		t.Errorf("ap 5 = %+v", ap5)
	}
}

// TestUsteerAPsFromPayloadOldAgent: agentes anteriores a #417 no llevan el
// flag Local en los APs; se asume todo local (no se descarta nada).
func TestUsteerAPsFromPayloadOldAgent(t *testing.T) {
	p := fixtureUsteerPayload()
	for ssid, sd := range p.Data.Usteer.SSIDs {
		for i := range sd.APs {
			sd.APs[i].Local = false
		}
		p.Data.Usteer.SSIDs[ssid] = sd
	}
	aps, seen := usteerAPsFromPayload("rt2", p)
	if seen != 3 || len(aps) != 3 {
		t.Fatalf("agente viejo: esperaba 3 APs (todo local), got %d (seen=%d)", len(aps), seen)
	}
}

func TestUsteerAPsFromPayloadNoUsteer(t *testing.T) {
	p := &probe.Payload{Router: "rt2", Data: probe.PayloadData{}}
	if aps, seen := usteerAPsFromPayload("rt2", p); aps != nil || seen != 0 {
		t.Fatalf("sin sección usteer = %+v/%d, want nil/0", aps, seen)
	}
	if aps, seen := usteerAPsFromPayload("rt2", nil); aps != nil || seen != 0 {
		t.Fatalf("payload nil = %+v/%d, want nil/0", aps, seen)
	}
}

// TestGetUsteerAgentOnlyFromPayload (#1431): un router agent-only con el
// agente empujando alimenta /api/usteer sin sondeo SSH. Pool nil como
// tripwire: si la guarda agent-only se rompe y toca SSH, paniquea.
func TestGetUsteerAgentOnlyFromPayload(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "rt2", Name: "rt2", Host: "192.168.1.2", AgentOnly: true},
	}, nil)
	reg := NewAgentRegistry(time.Hour)
	reg.Ingest(fixtureUsteerPayload())
	l.SetAgents(reg)

	u, err := l.GetUsteer(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u == nil || len(u.APs) != 2 {
		t.Fatalf("esperaba 2 APs, got %+v", u)
	}
	if len(u.Mesh) != 1 || !u.Mesh[0].Usteer || u.Mesh[0].ApsSeen != 2 || u.Mesh[0].RouterID != "rt2" {
		t.Errorf("mesh = %+v", u.Mesh)
	}
}

// TestGetUsteerAgentOnlyNoAgent: agent-only SIN agente asociado sigue sin
// aportar APs (503 global si es el único router), sin paniquear por pool nil.
func TestGetUsteerAgentOnlyNoAgent(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "rt2", Name: "rt2", Host: "192.168.1.2", AgentOnly: true},
	}, nil)
	l.SetAgents(NewAgentRegistry(time.Hour))

	u, err := l.GetUsteer(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u != nil {
		t.Fatalf("expected nil Usteer sin agente, got %+v", u)
	}
}
