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
	// Ningún router respondió. #1742: el overview ya no es nil, se devuelve
	// available=false con el motivo por unidad para que la UI pueda explicar
	// por qué no hay survey en vez de mandar a ejecutar `iw survey dump`.
	if s == nil {
		t.Fatal("expected overview with available=false, got nil")
	}
	if s.Available {
		t.Fatalf("expected available=false, got %+v", s)
	}
	if len(s.Routers) != 1 || s.Routers[0].Reason != SurveyReasonSnmpOnly {
		t.Fatalf("expected one snmp_only router, got %+v", s.Routers)
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

// #1742: una unidad agent-only sin survey empujado (el caso de los AP
// Broadcom/dhd, donde el agente va por `wl` y no hay `iw survey dump`) deja
// available=false con motivo agent_no_survey, en vez de desaparecer del
// overview sin explicación.
func TestGetSurveyAgentOnlyWithoutSurveyReportsReason(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	l.SetRouters([]RouterConfig{
		{ID: "ap1", Name: "AP 1", Host: "192.168.1.2", AgentOnly: true},
	})

	s, err := l.GetSurvey(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s == nil || s.Available {
		t.Fatalf("expected overview with available=false, got %+v", s)
	}
	if len(s.Routers) != 1 || s.Routers[0].Reason != SurveyReasonAgentNoSurvey {
		t.Fatalf("expected one agent_no_survey router, got %+v", s.Routers)
	}
	if s.Routers[0].Available {
		t.Fatalf("expected the unit to stay unavailable, got %+v", s.Routers[0])
	}
}
