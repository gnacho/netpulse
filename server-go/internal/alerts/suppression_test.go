package alerts

import (
	"testing"
)

func TestSuppressionGraphBasic(t *testing.T) {
	g := NewSuppressionGraph()
	g.SetTopology(map[string]string{
		"ap-sala":  "gateway",
		"ap-patio": "gateway",
	})

	if g.SuppressedBy("ap-sala") != "" {
		t.Fatal("no parent should suppress when no one is down")
	}

	g.MarkDown("gateway")
	if got := g.SuppressedBy("ap-sala"); got != "gateway" {
		t.Fatalf("expected gateway, got %q", got)
	}
	if got := g.SuppressedBy("ap-patio"); got != "gateway" {
		t.Fatalf("expected gateway, got %q", got)
	}
	if got := g.SuppressedBy("gateway"); got != "" {
		t.Fatalf("gateway itself should not be suppressed, got %q", got)
	}

	g.MarkUp("gateway")
	if g.SuppressedBy("ap-sala") != "" {
		t.Fatal("after MarkUp, no suppression")
	}
}

func TestSuppressionGraphChain(t *testing.T) {
	g := NewSuppressionGraph()
	g.SetTopology(map[string]string{
		"switch-1": "gateway",
		"ap-sala":  "switch-1",
	})

	g.MarkDown("gateway")
	if got := g.SuppressedBy("ap-sala"); got != "gateway" {
		t.Fatalf("chain: expected gateway, got %q", got)
	}

	g.MarkUp("gateway")
	g.MarkDown("switch-1")
	if got := g.SuppressedBy("ap-sala"); got != "switch-1" {
		t.Fatalf("chain: expected switch-1, got %q", got)
	}
}

func TestSuppressionGraphCycleSafe(t *testing.T) {
	g := NewSuppressionGraph()
	g.SetTopology(map[string]string{
		"a": "b",
		"b": "a",
	})
	g.MarkDown("a")
	if got := g.SuppressedBy("b"); got != "a" {
		t.Fatalf("cycle: expected a, got %q", got)
	}
	if got := g.SuppressedBy("c"); got != "" {
		t.Fatalf("unknown node: expected empty, got %q", got)
	}
}

func TestSuppressionInEmit(t *testing.T) {
	g := NewSuppressionGraph()
	g.SetTopology(map[string]string{"ap": "gw"})
	g.MarkDown("gw")

	spy := &spyNotifier{}
	e := New(nil, spy)
	e.SetSuppression(g)

	ev := AlertEvent{
		ID: "alert-offline-ap", Category: CatRouter, Urgent: true,
		Severity: "critical", Title: "AP offline", RouterID: "ap",
	}
	if !e.Emit(ev) {
		t.Fatal("emit should pass (router:urgent + urgent=true)")
	}

	list := e.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 event, got %d", len(list))
	}
	if list[0].SuppressedBy != "gw" {
		t.Fatalf("expected SuppressedBy=gw, got %q", list[0].SuppressedBy)
	}
	if len(spy.got) != 0 {
		t.Fatal("suppressed alert should NOT notify")
	}
}

func TestSuppressionNilGraph(t *testing.T) {
	spy := &spyNotifier{}
	e := New(nil, spy)
	e.Emit(AlertEvent{
		ID: "a1", Category: CatSystem, Urgent: true,
		Severity: "warn", Title: "x", RouterID: "r",
	})
	if len(spy.got) != 1 {
		t.Fatal("nil suppression should not block notification")
	}
}
