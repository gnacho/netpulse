package alerts

import (
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func TestRulesCRUD(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	rules, err := ListRules(d)
	if err != nil {
		t.Fatalf("list empty: %v", err)
	}
	if len(rules) != 0 {
		t.Fatalf("expected 0 rules, got %d", len(rules))
	}

	r := Rule{
		Name:     "CPU alto",
		Category: CatRouter,
		Enabled:  true,
		Condition: RuleCondition{
			Metric:    "cpu",
			Operator:  "gt",
			Threshold: 90,
			Duration:  10 * time.Minute,
		},
		Scope:    RuleScope{Type: "global"},
		Severity: "warn",
	}
	if err := CreateRule(d, r); err != nil {
		t.Fatalf("create: %v", err)
	}

	rules, err = ListRules(d)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].Name != "CPU alto" || rules[0].ID == "" {
		t.Fatalf("unexpected rule: %+v", rules[0])
	}
	if rules[0].CreatedAt.IsZero() || rules[0].UpdatedAt.IsZero() {
		t.Fatalf("timestamps not set: %+v", rules[0])
	}

	rules[0].Enabled = false
	if err := UpdateRule(d, rules[0]); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := GetRule(d, rules[0].ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Enabled {
		t.Fatal("expected disabled after update")
	}

	if err := DeleteRule(d, rules[0].ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	rules, _ = ListRules(d)
	if len(rules) != 0 {
		t.Fatalf("expected 0 rules after delete, got %d", len(rules))
	}
}

func TestRulesPersistence(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	r := Rule{
		Name:     "Latencia alta",
		Category: CatInternet,
		Enabled:  true,
		Condition: RuleCondition{
			Metric:    "latency_ms",
			Operator:  "gt",
			Threshold: 200,
			Duration:  5 * time.Minute,
		},
		Scope:    RuleScope{Type: "global"},
		Severity: "critical",
	}
	if err := CreateRule(d, r); err != nil {
		t.Fatalf("create: %v", err)
	}

	rules, err := ListRules(d)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if rules[0].Name != "Latencia alta" {
		t.Fatalf("unexpected: %+v", rules[0])
	}
}

func TestRuleNotFound(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	if _, err := GetRule(d, "no-existe"); err == nil {
		t.Fatal("expected error for missing rule")
	}
	if err := UpdateRule(d, Rule{ID: "no-existe"}); err == nil {
		t.Fatal("expected error for missing rule")
	}
	if err := DeleteRule(d, "no-existe"); err == nil {
		t.Fatal("expected error for missing rule")
	}
}

func TestRuleScopeRouter(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	r := Rule{
		Name:     "Temperatura patio",
		Category: CatSystem,
		Enabled:  true,
		Condition: RuleCondition{
			Metric:    "temp",
			Operator:  "gt",
			Threshold: 80,
			Duration:  3 * time.Minute,
		},
		Scope:    RuleScope{Type: "router", RouterIDs: []string{"patio"}},
		Severity: "warn",
	}
	if err := CreateRule(d, r); err != nil {
		t.Fatalf("create: %v", err)
	}
	rules, err := ListRules(d)
	if err != nil || len(rules) != 1 {
		t.Fatalf("list: err=%v, len=%d", err, len(rules))
	}
	got := &rules[0]
	if got.Scope.Type != "router" || len(got.Scope.RouterIDs) != 1 || got.Scope.RouterIDs[0] != "patio" {
		t.Fatalf("scope not preserved: %+v", got.Scope)
	}
}
