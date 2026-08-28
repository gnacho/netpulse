package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

func TestAlertRulesAPI(t *testing.T) {
	ts := makeTestServer(t)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	res := get(t, ts.URL, "/api/alert-rules", cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("list empty: %d", res.StatusCode)
	}
	res.Body.Close()

	body := `{"name":"CPU alto","category":"router","enabled":true,"condition":{"metric":"cpu","operator":"gt","threshold":90,"duration":600000000000},"scope":{"type":"global"},"severity":"warn"}`
	res = doJSON(t, "POST", ts.URL, "/api/alert-rules", cookie, body)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d", res.StatusCode)
	}
	res.Body.Close()

	rules, err := alerts.ListRules(ts.db)
	if err != nil || len(rules) != 1 {
		t.Fatalf("list after create: err=%v len=%d", err, len(rules))
	}
	ruleID := rules[0].ID

	res = get(t, ts.URL, "/api/alert-rules/"+ruleID, cookie)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("get: %d", res.StatusCode)
	}
	res.Body.Close()

	body = `{"name":"CPU muy alto","category":"router","enabled":false,"condition":{"metric":"cpu","operator":"gt","threshold":95,"duration":300000000000},"scope":{"type":"global"},"severity":"critical"}`
	res = doJSON(t, "PUT", ts.URL, "/api/alert-rules/"+ruleID, cookie, body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("update: %d", res.StatusCode)
	}
	res.Body.Close()

	got, _ := alerts.GetRule(ts.db, ruleID)
	if got.Enabled || got.Name != "CPU muy alto" {
		t.Fatalf("update not applied: %+v", got)
	}

	res = doJSON(t, "DELETE", ts.URL, "/api/alert-rules/"+ruleID, cookie, "")
	if res.StatusCode != http.StatusOK {
		t.Fatalf("delete: %d", res.StatusCode)
	}
	res.Body.Close()

	rules, _ = alerts.ListRules(ts.db)
	if len(rules) != 0 {
		t.Fatalf("expected 0 rules after delete, got %d", len(rules))
	}
}

func TestAlertRulesValidation(t *testing.T) {
	ts := makeTestServer(t)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	cases := []struct {
		name string
		body string
	}{
		{"empty name", `{"name":"","category":"router","enabled":true,"condition":{"metric":"cpu","operator":"gt","threshold":90,"duration":600000000000},"scope":{"type":"global"},"severity":"warn"}`},
		{"bad category", `{"name":"x","category":"wifi","enabled":true,"condition":{"metric":"cpu","operator":"gt","threshold":90,"duration":600000000000},"scope":{"type":"global"},"severity":"warn"}`},
		{"bad operator", `{"name":"x","category":"router","enabled":true,"condition":{"metric":"cpu","operator":"neq","threshold":90,"duration":600000000000},"scope":{"type":"global"},"severity":"warn"}`},
		{"no metric", `{"name":"x","category":"router","enabled":true,"condition":{"metric":"","operator":"gt","threshold":90,"duration":600000000000},"scope":{"type":"global"},"severity":"warn"}`},
		{"zero duration", `{"name":"x","category":"router","enabled":true,"condition":{"metric":"cpu","operator":"gt","threshold":90,"duration":0},"scope":{"type":"global"},"severity":"warn"}`},
		{"bad scope", `{"name":"x","category":"router","enabled":true,"condition":{"metric":"cpu","operator":"gt","threshold":90,"duration":600000000000},"scope":{"type":"magic"},"severity":"warn"}`},
		{"router scope no ids", `{"name":"x","category":"router","enabled":true,"condition":{"metric":"cpu","operator":"gt","threshold":90,"duration":600000000000},"scope":{"type":"router"},"severity":"warn"}`},
		{"bad severity", `{"name":"x","category":"router","enabled":true,"condition":{"metric":"cpu","operator":"gt","threshold":90,"duration":600000000000},"scope":{"type":"global"},"severity":"panic"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := doJSON(t, "POST", ts.URL, "/api/alert-rules", cookie, tc.body)
			if res.StatusCode != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d", res.StatusCode)
			}
			res.Body.Close()
		})
	}
}
