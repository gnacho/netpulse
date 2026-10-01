// Package alertlang traduce server-side las alertas para los canales de
// notificación externa (#888 / #889 / #1014).
//
// El motor de alertas compone título/descripción/hint con literales en
// INGLÉS (idioma canónico desde #1014) y la web los traduce en render
// (#671). Los canales ntfy/telegram/webhook reciben el evento ya localizado
// por este paquete: con alerts.lang=en (default) se compone contra el
// catálogo EN; con alerts.lang=es se traduce al catálogo ES. En ambos casos
// se reutilizan los MISMOS catálogos de la app embebidos en el binario
// (dist/locales/<lang>/translation.json, claves alerts.types.<slug>.title|
// .description|.results.* y alerts.hints.*), sin duplicar textos, y el
// fallback cuando falta la clave es el literal EN del evento.
//
// El idioma es un ajuste GLOBAL del servidor (kv "alerts.lang", default "en"):
// las alertas no llevan usuario emisor, son server-wide.
package alertlang

import (
	"encoding/json"
	"io/fs"
	"log"
	"strings"
	"sync"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
)

// KV es el mínimo de store que necesita el paquete (mismo contrato que usan
// ntfy/telegram: Get(key) (string, bool)).
type KV interface {
	Get(key string) (string, bool)
}

const (
	// KeyLang: idioma de las notificaciones push del servidor.
	KeyLang = "alerts.lang"
	// DefaultLang: un usuario que no toca nada recibe pushes en inglés (el
	// reporte de #888 pedía exactamente eso; la UI sigue auto-detectando).
	DefaultLang = "en"
)

var (
	localesFS fs.FS
	mu        sync.Mutex
	cache     = map[string]*catalog{} // lang → catálogo parseado
	supported = map[string]bool{}     // idiomas presentes en el dist
)

type typeEntry struct {
	Title           string            `json:"title"`
	Description     string            `json:"description"`
	DescriptionUnit string            `json:"descriptionUnit"`
	Results         map[string]string `json:"results"`
}

type catalog struct {
	Alerts struct {
		Types map[string]typeEntry `json:"types"`
		Hints map[string]string    `json:"hints"`
	} `json:"alerts"`
}

// SetLocalesFS fija el FS de catálogos ("<lang>/translation.json"). Se llama
// una vez al arrancar (staticspa.LocalesFS).
func SetLocalesFS(f fs.FS) {
	localesFS = f
	entries, err := fs.ReadDir(f, ".")
	if err != nil {
		log.Printf("[netpulse] alertlang: no se pudo listar locales: %v", err)
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			supported[e.Name()] = true
		}
	}
	log.Printf("[netpulse] alertlang: idiomas disponibles para push: %v", keys(supported))
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Supported devuelve los idiomas disponibles (los del dist).
func Supported() []string { return keys(supported) }

func load(lang string) *catalog {
	mu.Lock()
	defer mu.Unlock()
	if c, ok := cache[lang]; ok {
		return c
	}
	if localesFS == nil {
		return nil
	}
	raw, err := fs.ReadFile(localesFS, lang+"/translation.json")
	if err != nil {
		log.Printf("[netpulse] alertlang: catálogo no encontrado: lang=%s (%v)", lang, err)
		return nil
	}
	var c catalog
	if err := json.Unmarshal(raw, &c); err != nil {
		log.Printf("[netpulse] alertlang: catálogo inválido: lang=%s (%v)", lang, err)
		return nil
	}
	cache[lang] = &c
	return &c
}

// Lang devuelve el idioma efectivo de las notificaciones (kv alerts.lang,
// validado contra los disponibles; default en).
func Lang(kv KV) string {
	if kv != nil {
		if v, ok := kv.Get(KeyLang); ok && supported[v] {
			return v
		}
	}
	return DefaultLang
}

// interpolate replica el formato de i18next usado por la app ({{var}}).
func interpolate(tpl string, vars map[string]string) string {
	if vars == nil {
		return tpl
	}
	return strings.NewReplacer(pairs(vars)...).Replace(tpl)
}

func pairs(vars map[string]string) []string {
	out := make([]string, 0, len(vars)*2)
	for k, v := range vars {
		out = append(out, "{{"+k+"}}", v)
	}
	return out
}

// Localize devuelve una copia del evento con title/description/hint en el
// idioma efectivo, compuestos desde el catálogo de ese idioma. Cae a los
// literales del server (inglés canónico, #1014) si el evento no tiene type,
// falta la clave o no hay catálogo: nunca peor que el literal nativo.
func Localize(ev alerts.AlertEvent, kv KV) alerts.AlertEvent {
	if ev.Type == "" {
		return ev
	}
	c := load(Lang(kv))
	if c == nil {
		return ev
	}
	entry, ok := c.Alerts.Types[ev.Type]
	if !ok {
		return ev
	}
	out := ev
	if entry.Title != "" {
		out.Title = interpolate(entry.Title, ev.Vars)
	}
	body := entry.Description
	if r := ev.Vars["result"]; r != "" && entry.Results != nil && entry.Results[r] != "" {
		body = entry.Results[r]
	}
	if body != "" {
		out.Description = interpolate(body, ev.Vars)
	}
	if ev.Hint != "" {
		if h := c.Alerts.Hints[ev.Type]; h != "" {
			out.Hint = h
		}
	}
	return out
}

// notifier traduce cada evento antes de pasarlo a la cadena real.
type notifier struct {
	kv   KV
	next alerts.Notifier
}

// Notifier envuelve la cadena ntfy/telegram/webhook/push para que los pushes
// salgan en el idioma del ajuste alerts.lang (#888, núcleo de #889).
func Notifier(kv KV, next alerts.Notifier) alerts.Notifier {
	return notifier{kv: kv, next: next}
}

func (n notifier) Notify(ev alerts.AlertEvent) {
	if n.next != nil {
		n.next.Notify(Localize(ev, n.kv))
	}
}
