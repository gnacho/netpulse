// alertlang_test.go - traducción server-side de pushes (#888/#889) contra los
// catálogos reales de la app (mismo enfoque que alerts_i18n_test.go).
package alertlang

import (
	"os"
	"strings"
	"path/filepath"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

type mapKV map[string]string

func (m mapKV) Get(key string) (string, bool) { v, ok := m[key]; return v, ok }

func localesFromApp(t *testing.T) {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	SetLocalesFS(os.DirFS(filepath.Join(root, "app", "dist", "locales")))
	if len(Supported()) == 0 {
		t.Fatal("no se detectaron idiomas (¿falta npm run build en app/?)")
	}
}

func agentDownEv() alerts.AlertEvent {
	return alerts.AlertEvent{
		ID:          "alert-agent-down-gateway",
		Category:    alerts.CatRouter,
		Type:        "agent-down",
		Title:       "Agente caído en gateway",
		Description: "Sin datos del agente de gateway desde hace más de 5m — usando datos cacheados",
		Vars:        map[string]string{"router": "gateway", "confirm": "5m"},
	}
}

func TestLocalizeEnglish(t *testing.T) {
	localesFromApp(t)
	out := Localize(agentDownEv(), mapKV{KeyLang: "en"})
	if out.Title != "Agent down on gateway" {
		t.Fatalf("title = %q", out.Title)
	}
	if out.Description == "" || containsES(out.Description) {
		t.Fatalf("description parece sin traducir: %q", out.Description)
	}
}

func containsES(s string) bool {
	return strings.Contains(s, "Agente") || strings.Contains(s, "caído") || strings.Contains(s, "Sin datos")
}

func TestLocalizeSpanishExplicit(t *testing.T) {
	localesFromApp(t)
	out := Localize(agentDownEv(), mapKV{KeyLang: "es"})
	if out.Title != "Agente caído en gateway" {
		t.Fatalf("title = %q", out.Title)
	}
}

func TestLocalizeFallbacks(t *testing.T) {
	localesFromApp(t)
	// Sin type → sin tocar.
	ev := agentDownEv()
	ev.Type = ""
	if out := Localize(ev, mapKV{KeyLang: "en"}); out.Title != ev.Title {
		t.Fatal("un evento sin type no debe cambiar")
	}
	// Type desconocido → sin tocar.
	ev = agentDownEv()
	ev.Type = "no-existe-este-slug"
	if out := Localize(ev, mapKV{KeyLang: "en"}); out.Title != ev.Title {
		t.Fatal("un type sin clave i18n no debe cambiar")
	}
	// Idioma no configurado → default en.
	if got := Lang(mapKV{}); got != DefaultLang {
		t.Fatalf("Lang default = %q", got)
	}
	// Idioma inválido en kv → default en.
	if got := Lang(mapKV{KeyLang: "xx"}); got != DefaultLang {
		t.Fatalf("Lang inválido = %q", got)
	}
}

func TestNotifierWrapsNext(t *testing.T) {
	localesFromApp(t)
	var got alerts.AlertEvent
	n := Notifier(mapKV{KeyLang: "en"}, notifierFunc(func(ev alerts.AlertEvent) { got = ev }))
	n.Notify(agentDownEv())
	if got.Title != "Agent down on gateway" {
		t.Fatalf("el notifier no tradujo: %q", got.Title)
	}
	// next nil no debe explotar.
	Notifier(mapKV{}, nil).Notify(agentDownEv())
}

type notifierFunc func(alerts.AlertEvent)

func (f notifierFunc) Notify(ev alerts.AlertEvent) { f(ev) }
