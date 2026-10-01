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
