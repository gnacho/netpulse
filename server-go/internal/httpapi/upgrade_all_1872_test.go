package httpapi_test

import (
	"testing"
	"time"
)

// TestAgentsUpgradeAllExternalWithLiveAgent — #1872: el gate de upgrade-all
// miraba routers.type y devolvía not_openwrt a una unidad con agente nativo y
// fila external. Con el stream SSE abierto el comando sí puede salir; sin
// stream, un scraper sigue dando not_openwrt.
func TestAgentsUpgradeAllExternalWithLiveAgent(t *testing.T) {
	ts := makeUpgradeTestServer(t)

	// Tres filas external: una con agente conectado, otra sin conectar y una
	// tercera con tipo openwrt sin conectar (control: sigue encolando).
	for _, slug := range []string{"asus-live", "asus-dark", "owrt-dark"} {
		typ := "external"
		if slug == "owrt-dark" {
			typ = "openwrt"
		}
		if _, err := ts.db.Exec(
			`INSERT INTO routers (id, name, host, type, is_gateway, created_at) VALUES (?,?,?,?,?,?)`,
			slug, slug, "10.0.0.9", typ, 0, time.Now().UnixMilli(),
		); err != nil {
			t.Fatalf("insert %s: %v", slug, err)
		}
	}

	// asus-live: agente nativo desactualizado, conectado por SSE → sent.
	st, tok := createUpgradeToken(t, ts, "asus-live")
	if st != 201 {
		t.Fatalf("create asus-live: %d", st)
	}
	upgradeIngest(t, ts, tok, upgradePayloadFor("asus-live", "0.0.9"))
	cancel, done := openStream(t, ts, "asus-live", tok)
	defer func() { cancel(); <-done }()

	// asus-dark: el mismo tipo external pero sin stream → not_openwrt.
	if st, tokD := createUpgradeToken(t, ts, "asus-dark"); st != 201 {
		t.Fatalf("create asus-dark: %d", st)
	} else {
		upgradeIngest(t, ts, tokD, upgradePayloadFor("asus-dark", "0.0.9"))
	}

	// owrt-dark: tipo aceptado sin conexión → queued (sin regresión).
	if st, tokO := createUpgradeToken(t, ts, "owrt-dark"); st != 201 {
		t.Fatalf("create owrt-dark: %d", st)
	} else {
		upgradeIngest(t, ts, tokO, upgradePayloadFor("owrt-dark", "0.0.9"))
	}

	res := doReq(t, "POST", ts.URL+"/api/agents/upgrade-all", ts.cookie, "")
	if res.StatusCode != 200 {
		t.Fatalf("upgrade-all: got %d", res.StatusCode)
	}
	body := readJSON(t, res)
	bySlug := map[string]string{}
	for _, a := range body["agents"].([]any) {
		m := a.(map[string]any)
		bySlug[m["slug"].(string)] = m["status"].(string)
	}
	if bySlug["asus-live"] != "sent" {
		t.Fatalf("external con agente conectado debería ser sent: %v", bySlug)
	}
	if bySlug["asus-dark"] != "not_openwrt" {
		t.Fatalf("external sin agente debería seguir en not_openwrt: %v", bySlug)
	}
	if bySlug["owrt-dark"] != "queued" {
		t.Fatalf("openwrt sin agente debería seguir en queued: %v", bySlug)
	}
}

// TestAgentHubConnected — el helper del hub que sostiene el gate de #1872.
func TestAgentHubConnected(t *testing.T) {
	ts := makeUpgradeTestServer(t)
	st, tok := createUpgradeToken(t, ts, "hub-check")
	if st != 201 {
		t.Fatalf("create: %d", st)
	}
	if ts.hub.Connected("hub-check") {
		t.Fatal("sin stream no debería estar conectado")
	}
	cancel, done := openStream(t, ts, "hub-check", tok)
	if !ts.hub.Connected("hub-check") {
		t.Fatal("con stream debería estar conectado")
	}
	cancel()
	<-done
}
