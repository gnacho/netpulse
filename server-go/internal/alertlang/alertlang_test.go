// alertlang_test.go - localización server-side de pushes (#888/#889) contra
// los catálogos reales de la app (mismo enfoque que alerts_i18n_test.go).
// Dirección #1014: los literales del server son INGLÉS canónico; el catálogo
// ES traduce cuando alerts.lang=es y el fallback es siempre el literal EN.
package alertlang

import (
	"os"
	"path/filepath"
	"strings"
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

// agentDownEv: evento tal y como lo compone el motor tras #1014: literales
// en inglés canónico + Type/Vars para la localización por catálogo.
func agentDownEv() alerts.AlertEvent {
	return alerts.AlertEvent{
		ID:          "alert-agent-down-gateway",
		Category:    alerts.CatRouter,
		Type:        "agent-down",
		Title:       "Agent down on gateway",
		Description: "No data from the agent on gateway for more than 5m - using cached data",
		Hint:        alerts.HintFor(alerts.HintAgentDown),
		Vars:        map[string]string{"router": "gateway", "confirm": "5m"},
	}
}

// (a) Default (sin ajuste alerts.lang) = inglés: texto EN nativo del
// catálogo, interpolado con las vars del evento.
func TestLocalizeDefaultIsEnglish(t *testing.T) {
	localesFromApp(t)
	out := Localize(agentDownEv(), mapKV{})
	if out.Title != "Agent down on gateway" {
		t.Fatalf("title = %q", out.Title)
	}
	// La descripción sale del catálogo EN interpolado (no del literal).
	for _, part := range []string{"No data from the agent on gateway for more than 5m", "using cached data"} {
		if !strings.Contains(out.Description, part) {
			t.Fatalf("description sin %q: %q", part, out.Description)
		}
	}
	if strings.Contains(out.Description, "{{") || containsES(out.Description) {
		t.Fatalf("description sin interpolar o en español: %q", out.Description)
	}
}

func containsES(s string) bool {
	return strings.Contains(s, "Agente") || strings.Contains(s, "caído") || strings.Contains(s, "Sin datos")
}

// (b) lang=es produce el castellano del catálogo (título, descripción e
// hint), no el literal EN.
func TestLocalizeSpanishExplicit(t *testing.T) {
	localesFromApp(t)
	out := Localize(agentDownEv(), mapKV{KeyLang: "es"})
	if out.Title != "Agente caído en gateway" {
		t.Fatalf("title = %q", out.Title)
	}
	if !strings.Contains(out.Description, "Sin datos del agente de gateway") {
		t.Fatalf("description no viene del catálogo ES: %q", out.Description)
	}
	if !strings.Contains(out.Hint, "Comprueba la alimentación") {
		t.Fatalf("hint no viene del catálogo ES: %q", out.Hint)
	}
}

// (c) Tipo desconocido (sin clave en el catálogo): cae al literal EN sin
// romperse, incluso con lang=es.
func TestLocalizeUnknownTypeKeepsEnglishLiteral(t *testing.T) {
	localesFromApp(t)
	ev := agentDownEv()
	ev.Type = "no-existe-este-slug"
	for _, lang := range []string{"en", "es"} {
		out := Localize(ev, mapKV{KeyLang: lang})
		if out.Title != ev.Title || out.Description != ev.Description {
			t.Fatalf("lang=%s: un type sin clave i18n debe conservar el literal EN: %+v", lang, out)
		}
	}
	// Sin type: tampoco se toca.
	ev.Type = ""
	if out := Localize(ev, mapKV{KeyLang: "es"}); out.Title != ev.Title {
		t.Fatal("un evento sin type no debe cambiar")
	}
}

// (d) Plantilla con valores interpolados: las vars caen en su sitio en ambos
// idiomas y no queda ningún placeholder {{...}} sin resolver.
func TestLocalizeInterpolatesBothLanguages(t *testing.T) {
	localesFromApp(t)
	ev := alerts.AlertEvent{
		ID:    "alert-agent-outdated-rt1",
		Type:  "agent-outdated",
		Title: "Agent outdated on rt1",
		Vars:  map[string]string{"router": "rt1", "version": "3.0.4", "kind": "native", "latest": "3.0.5"},
	}
	en := Localize(ev, mapKV{KeyLang: "en"})
	if en.Description != "rt1 pushes version 3.0.4; the latest available for the native agent is 3.0.5" {
		t.Fatalf("EN description = %q", en.Description)
	}
	es := Localize(ev, mapKV{KeyLang: "es"})
	for _, out := range []alerts.AlertEvent{en, es} {
		if strings.Contains(out.Title, "{{") || strings.Contains(out.Description, "{{") {
			t.Fatalf("placeholder sin resolver: %+v", out)
		}
		for _, v := range []string{"rt1", "3.0.4", "3.0.5"} {
			if !strings.Contains(out.Description, v) {
				t.Fatalf("description sin el valor %q: %q", v, out.Description)
			}
		}
	}
}

func TestLangDefaults(t *testing.T) {
	localesFromApp(t)
	// Idioma no configurado → default en.
	if got := Lang(mapKV{}); got != DefaultLang {
		t.Fatalf("Lang default = %q", got)
	}
	// Idioma inválido en kv → default en.
	if got := Lang(mapKV{KeyLang: "xx"}); got != DefaultLang {
		t.Fatalf("Lang inválido = %q", got)
	}
}

// El notifier entrega al canal el evento ya localizado: con lang=es el push
// sale en castellano aunque el motor compuso en inglés.
func TestNotifierWrapsNext(t *testing.T) {
	localesFromApp(t)
	var got alerts.AlertEvent
	n := Notifier(mapKV{KeyLang: "es"}, notifierFunc(func(ev alerts.AlertEvent) { got = ev }))
	n.Notify(agentDownEv())
	if got.Title != "Agente caído en gateway" {
		t.Fatalf("el notifier no localizó: %q", got.Title)
	}
	// next nil no debe explotar.
	Notifier(mapKV{}, nil).Notify(agentDownEv())
}

type notifierFunc func(alerts.AlertEvent)

func (f notifierFunc) Notify(ev alerts.AlertEvent) { f(ev) }
