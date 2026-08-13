// demoreadonly.go — Guard de modo demo read-only (issue #118). En modo demo
// (DEMO_MODE=1) el dataset canónico vive en memoria y la BD debe quedar
// pristina: cualquier mutación HTTP (no GET/HEAD) se rechaza con 409
// "demo_read_only" salvo una allowlist mínima.
package httpapi

import (
	"net/http"
	"strings"
)

// demoReadOnlyNext permite pasar al siguiente handler.
// demoAllowlist: rutas de mutación que SÍ deben funcionar en modo demo.
//   - /api/auth/* : login/logout (ciclo de vida de sesión; sin login no hay demo)
//   - /api/demo/enable : la única vía de SALIR del demo (si se bloquea, el
//     admin queda atrapado en demo para siempre)
//   - /api/refresh : no escribe en BD; el botón "Refrescar" sigue vivo
//   - /api/push/* y /api/users/me/* : preferencias de usuario/UI (idioma,
//     nombre, push), no tocan el dataset canónico
//   - /api/ingest/agent : los colectores/scrapers externos siguen empujando;
//     en demo el handler los acepta (202) como no-op sin escribir (issue #168)
func demoAllowlist(path string) bool {
	if strings.HasPrefix(path, "/api/auth/") {
		return true
	}
	switch path {
	case "/api/demo/enable", "/api/refresh", "/api/ingest/agent":
		return true
	}
	if strings.HasPrefix(path, "/api/push/") || strings.HasPrefix(path, "/api/users/me/") {
		return true
	}
	return false
}

// demoReadOnly envuelve el mux de API: en modo demo, rechaza mutaciones fuera
// de la allowlist. Ubicado DENTRO de RequireAuth (ve el User del contexto) y
// ANTES de noStoreMux. Los endpoints de agente (Bearer) también lo atraviesan,
// pero en demo no hay agentes reales.
//
// Fuente de verdad: cfg.DemoMode (lo que declara el operador en DEMO_MODE).
// En producción, DEMO_MODE=1 → adapter NewDemo (coinciden). En el fallback de
// arranque sin clave SSH el adapter cae a demo pero cfg sigue live: las
// mutaciones de BD/UI permanecen operativas (no hay red que gestionar de todos
// modos). Los tests de mutación usan DEMO_MODE=0 sin depender de adapter.
func (s *server) demoReadOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg != nil && s.cfg.DemoMode && r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !demoAllowlist(r.URL.Path) {
				writeError(w, http.StatusConflict, "demo_read_only", "Modo demo: las escrituras están deshabilitadas para preservar el dataset de ejemplo.")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
