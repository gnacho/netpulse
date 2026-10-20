package auth

import (
	"net/http/httptest"
	"testing"
)

// Regresión #1462: el agente sube el backup de config a
// /api/agents/{slug}/firmware-backup con su Bearer. Si la ruta no está en la
// allowlist de rutas de agente, RequireAuth la rechaza con 401 antes de que
// el handler valide el token (el upload del flujo de firmware #1414 fallaba
// en silencio por esto).
func TestIsAgentReportPathFirmwareBackup(t *testing.T) {
	cases := []string{
		"/api/agents/rt4/firmware-backup",
		"/api/agents/rt4/firmware-progress",
		"/api/agents/rt4/firmware-result",
		"/api/agents/rt4/upgrade-progress",
		"/api/agents/rt4/binary",
	}
	for _, p := range cases {
		if !isAgentReportPath(p) {
			t.Errorf("%s debería ser ruta de agente", p)
		}
		if !IsAgentRequest(httptest.NewRequest("POST", p, nil)) {
			t.Errorf("%s debería pasar el gate IsAgentRequest", p)
		}
	}
	// Rutas de admin con el mismo prefijo NO deben colarse como agente.
	if isAgentReportPath("/api/agents") {
		t.Error("/api/agents no es una ruta de reporte")
	}
}
