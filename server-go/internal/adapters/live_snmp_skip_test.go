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

// #1214 (ex-#1147): el gateway no se lista en el survey cuando hay más
// unidades (la pestaña es de roaming, solo APs). Pool nil como tripwire: si
// el gateway no se saltara, pool.Run paniquea. Un fleet mono-router mantiene
// el gateway (sin otros que pisen, no se salta).
func TestGetSurveySkipsGatewayWhenOthersExist(t *testing.T) {
	l := NewLive(nil, nil, nil, nil)
	l.SetRouters([]RouterConfig{
		{ID: "gw", Name: "Gateway", Host: "192.168.1.1", IsGateway: true},
		{ID: "ap1", Name: "AP 1", Host: "192.168.1.2", AgentOnly: true},
	})

	s, err := l.GetSurvey(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Gateway excluido y ap1 agent-only → ninguna unidad con wifi → (nil, nil).
	if s != nil {
		t.Fatalf("expected nil SurveyOverview, got %+v", s)
	}
}
