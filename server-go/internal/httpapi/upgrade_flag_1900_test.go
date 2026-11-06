// upgrade_flag_1900_test.go - issue #1900: la bandera de novedad y el comando
// upgrade-all tienen que mirar lo mismo. Desde la migración a external
// (2.49.x) una unidad conserva su agente nativo, así que el tipo de la fila ya
// no basta para decidir si puede actualizarse.
package httpapi_test

import (
	"testing"
	"time"
)

func TestAgentsListUpdateAvailableExternalWithLiveAgent(t *testing.T) {
	ts := makeUpgradeTestServer(t)

	for _, slug := range []string{"asus-live", "asus-dark"} {
		if _, err := ts.db.Exec(
			`INSERT INTO routers (id, name, host, type, is_gateway, created_at) VALUES (?,?,?,?,?,?)`,
			slug, slug, "10.0.0.9", "external", 0, time.Now().UnixMilli(),
		); err != nil {
			t.Fatalf("insert %s: %v", slug, err)
		}
	}

	agentItem := func(slug string) map[string]any {
		res := get(t, ts.URL, "/api/agents", ts.cookie)
		body := readJSON(t, res)
		for _, a := range body["agents"].([]any) {
			if m, _ := a.(map[string]any); m["slug"] == slug {
				return m
			}
		}
		t.Fatalf("%s ausente: %v", slug, body)
		return nil
	}

	st, tokLive := createUpgradeToken(t, ts, "asus-live")
	if st != 201 {
		t.Fatalf("create asus-live: %d", st)
	}
	upgradeIngest(t, ts, tokLive, upgradePayloadFor("asus-live", "0.0.9"))
	cancel, done := openStream(t, ts, "asus-live", tokLive)
	defer func() { cancel(); <-done }()

	st, tokDark := createUpgradeToken(t, ts, "asus-dark")
	if st != 201 {
		t.Fatalf("create asus-dark: %d", st)
	}
	upgradeIngest(t, ts, tokDark, upgradePayloadFor("asus-dark", "0.0.9"))

	// Con stream abierto el comando llega: hay novedad.
	if m := agentItem("asus-live"); m["updateAvailable"] != true {
		t.Fatalf("external con agente vivo y versión vieja debe ofrecer upgrade: %v", m)
	}
	// Sin stream sigue siendo un scraper: nada que actualizar.
	if m := agentItem("asus-dark"); m["updateAvailable"] != false {
		t.Fatalf("external sin agente conectado no debe ofrecer upgrade: %v", m)
	}
}
