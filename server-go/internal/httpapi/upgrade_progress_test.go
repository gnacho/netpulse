// upgrade_progress_test.go — #284: progreso en vivo del self-update.
// Contrato de POST /api/agents/{slug}/upgrade-progress (Bearer del agente),
// la semilla "requested" al enviar el comando upgrade/upgrade-all, la
// transición a restarting/failed desde upgrade-result y la exposición del
// campo `upgrade` en GET /api/agents.
package httpapi_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

// postProgress hace POST /api/agents/{slug}/upgrade-progress con el Bearer dado.
func postProgress(t *testing.T, ts *upgradeTestServer, slug, token, body string) int {
	t.Helper()
	req, _ := http.NewRequest("POST", ts.URL+"/api/agents/"+slug+"/upgrade-progress", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upgrade-progress: %v", err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	return res.StatusCode
}

// agentUpgradeField devuelve el campo `upgrade` del slug en GET /api/agents
// (nil si no viene).
func agentUpgradeField(t *testing.T, ts *upgradeTestServer, slug string) map[string]any {
	t.Helper()
	res := get(t, ts.URL, "/api/agents", ts.cookie)
	body := readJSON(t, res)
	for _, a := range body["agents"].([]any) {
		m, _ := a.(map[string]any)
		if m["slug"] == slug {
			u, _ := m["upgrade"].(map[string]any)
			return u
		}
	}
	t.Fatalf("%s ausente en /api/agents: %v", slug, body)
	return nil
}

// TestUpgradeProgressAuth: 401 sin token o con token inválido; 200 con el
// token del agente; 400 con step desconocido.
func TestUpgradeProgressAuth(t *testing.T) {
	ts := makeUpgradeTestServer(t)
	st, tok := createUpgradeToken(t, ts, "patio")
	if st != 201 || tok == "" {
		t.Fatalf("create: %d", st)
	}

	if got := postProgress(t, ts, "patio", "", `{"step":"downloading"}`); got != 401 {
		t.Fatalf("sin token: got %d want 401", got)
	}
	if got := postProgress(t, ts, "patio", "deadbeef", `{"step":"downloading"}`); got != 401 {
		t.Fatalf("token malo: got %d want 401", got)
	}
	if got := postProgress(t, ts, "patio", tok, `{"step":"nonsense"}`); got != 400 {
		t.Fatalf("step inválido: got %d want 400", got)
	}
	if got := postProgress(t, ts, "patio", tok, `not-json`); got != 400 {
		t.Fatalf("body roto: got %d want 400", got)
	}
	if got := postProgress(t, ts, "patio", tok, `{"step":"downloading","pct":42}`); got != 200 {
		t.Fatalf("válido: got %d want 200", got)
	}
}

// TestUpgradeProgressExposedInAgentsList: el comando upgrade siembra
// "requested", los reportes del agente actualizan el paso y upgrade-result
// transita a restarting (ok) o failed (error). El campo caduca por TTL.
func TestUpgradeProgressExposedInAgentsList(t *testing.T) {
	ts := makeUpgradeTestServer(t)
	st, tok := createUpgradeToken(t, ts, "patio")
	if st != 201 {
		t.Fatalf("create: %d", st)
	}
	cancel, done := openStream(t, ts, "patio", tok)
	defer func() { cancel(); <-done }()

	// Comando enviado → paso "requested".
	status, _ := postUpgrade(t, ts, "patio", ts.cookie)
	if status != 202 {
		t.Fatalf("upgrade: got %d want 202", status)
	}
	u := agentUpgradeField(t, ts, "patio")
	if u["step"] != "requested" {
		t.Fatalf("tras upgrade: step %v, want requested", u["step"])
	}

	// Paso intermedio del agente → downloading con pct.
	if got := postProgress(t, ts, "patio", tok, `{"step":"downloading","pct":40}`); got != 200 {
		t.Fatalf("progress: got %d want 200", got)
	}
	u = agentUpgradeField(t, ts, "patio")
	if u["step"] != "downloading" {
		t.Fatalf("step: %v, want downloading", u["step"])
	}
	if u["pct"].(float64) != 40 {
		t.Fatalf("pct: %v, want 40", u["pct"])
	}

	// pct fuera de rango → se clampea a 100.
	if got := postProgress(t, ts, "patio", tok, `{"step":"downloading","pct":250}`); got != 200 {
		t.Fatalf("progress clamp: got %d want 200", got)
	}
	u = agentUpgradeField(t, ts, "patio")
	if u["pct"].(float64) != 100 {
		t.Fatalf("pct clamp: %v, want 100", u["pct"])
	}

	// Resultado ok → restarting (el agente se dispone a reiniciarse).
	req, _ := http.NewRequest("POST", ts.URL+"/api/agents/patio/upgrade-result", strings.NewReader(`{"ok":true}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	res, _ := http.DefaultClient.Do(req)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("upgrade-result: got %d want 200", res.StatusCode)
	}
	u = agentUpgradeField(t, ts, "patio")
	if u["step"] != "restarting" {
		t.Fatalf("tras ok: step %v, want restarting", u["step"])
	}

	// Resultado con error → failed con el mensaje.
	req, _ = http.NewRequest("POST", ts.URL+"/api/agents/patio/upgrade-result", strings.NewReader(`{"ok":false,"error":"swap failed"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	res, _ = http.DefaultClient.Do(req)
	res.Body.Close()
	u = agentUpgradeField(t, ts, "patio")
	if u["step"] != "failed" {
		t.Fatalf("tras error: step %v, want failed", u["step"])
	}
	if u["error"] != "swap failed" {
		t.Fatalf("error: %v, want swap failed", u["error"])
	}
}
