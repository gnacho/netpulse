package adapters

import (
	"context"
	"testing"
)

// issue #1026: las sondas SSH de features (usteer, 802.11r, survey) deben
// saltar las unidades sondeadas por SNMP, igual que las agent-only: esas
// unidades no tienen SSH en absoluto y cada intento genera ruido de backoff
// en el pool. Los tests usan pool nil como tripwire: si la guarda no salta,
// l.pool.Run paniquea por nil pointer y el test falla.

func TestGetUsteerSkipsSnmpRouters(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "sw1", Name: "Switch 1", Host: "192.168.1.10", SnmpEnabled: true},
	}, nil)

	u, err := l.GetUsteer(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Sin APs (la unidad SNMP se salta) la función devuelve (nil, nil).
	if u != nil {
		t.Fatalf("expected nil Usteer, got %+v", u)
	}
}

func TestGetDot11rSkipsSnmpRouters(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "sw1", Name: "Switch 1", Host: "192.168.1.10", SnmpEnabled: true},
	}, nil)

	d, err := l.GetDot11r(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Ningún router con wifi disponible -> (nil, nil).
	if d != nil {
		t.Fatalf("expected nil Dot11rOverview, got %+v", d)
	}
}

func TestGetSurveySkipsSnmpRouters(t *testing.T) {
	l := NewLive(nil, nil, []RouterConfig{
		{ID: "sw1", Name: "Switch 1", Host: "192.168.1.10", SnmpEnabled: true},
	}, nil)

	s, err := l.GetSurvey(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Ningún router respondió -> (nil, nil).
	if s != nil {
		t.Fatalf("expected nil SurveyOverview, got %+v", s)
	}
}

// #1308: el gateway YA NO se excluye del survey cuando hay más unidades (la
// exclusión era de la época de la pestaña de roaming, #1214; la lente vive en
// Canales desde #1229 y ahí el gateway es un AP más). Pool nil como tripwire
// para las unidades SSH; con el gateway como agent-only y survey empujado
// debe aparecer en el overview con sus radios.
func TestGetSurveyIncludesGateway(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	l.SetRouters([]RouterConfig{
		{ID: "gw", Name: "Gateway", AgentOnly: true, IsGateway: true},
		{ID: "ap1", Name: "AP 1", Host: "192.168.1.2", AgentOnly: true},
	})
	l.StoreAgentSurvey("gw", `Survey data from wlan0
	frequency:			2412 MHz [in use]
	noise:				-90 dBm
	channel active time:		1000 ms
	channel busy time:		400 ms
`)

	s, err := l.GetSurvey(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == nil || !s.Available {
		t.Fatalf("expected available overview, got %+v", s)
	}
	var gw *SurveyRouter
	for i := range s.Routers {
		if s.Routers[i].RouterID == "gw" {
			gw = &s.Routers[i]
		}
	}
	if gw == nil {
		t.Fatalf("gateway missing from survey routers: %+v", s.Routers)
	}
	if !gw.Available || len(gw.Radios) == 0 {
		t.Fatalf("expected gateway available with radios, got %+v", gw)
	}
}
