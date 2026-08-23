// push_api_test.go — contrato de los endpoints Web Push (SPEC-PUSH §1):
// auth de sesión obligatoria en los 3, vapid-key estable, subscribe 201/200
// (upsert por endpoint) y unsubscribe 204 idempotente.
package httpapi_test

import (
	"io"
	"strings"
	"testing"
)

func TestPushAuthRequired(t *testing.T) {
	ts := makeTestServer(t)
	for _, tc := range []struct{ method, path, body string }{
		{"GET", "/api/push/vapid-key", ""},
		{"POST", "/api/push/subscribe", `{"endpoint":"https://x/y","keys":{"auth":"a","p256dh":"p"}}`},
		{"POST", "/api/push/unsubscribe", `{"endpoint":"https://x/y"}`},
	} {
		res := doJSON(t, tc.method, ts.URL, tc.path, "", tc.body)
		if res.StatusCode != 401 {
			t.Fatalf("%s %s sin sesión: %d, esperaba 401", tc.method, tc.path, res.StatusCode)
		}
		res.Body.Close()
	}
}

func TestPushEndpointsContract(t *testing.T) {
	ts := makeTestServer(t)
	_, cookie, _ := loginCookie(t, ts.URL, "admin", "test123456")

	// GET vapid-key: {"key":...} no vacía y ESTABLE entre llamadas; sin '\n'.
	res := get(t, ts.URL, "/api/push/vapid-key", cookie)
	if res.StatusCode != 200 {
		t.Fatalf("vapid-key: %d", res.StatusCode)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if strings.HasSuffix(string(raw), "\n") {
		t.Fatal("writeJSON no debe terminar en \\n")
	}
	s := string(raw)
	if !strings.HasPrefix(s, `{"key":"`) || !strings.HasSuffix(s, `"}`) || len(s) <= len(`{"key":""}`) {
		t.Fatalf("vapid-key body inesperado: %s", s)
	}
	res = get(t, ts.URL, "/api/push/vapid-key", cookie)
	raw2, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if string(raw) != string(raw2) {
		t.Fatalf("vapid-key no estable: %s vs %s", raw, raw2)
	}

	count := func() int {
		var n int
		if err := ts.db.QueryRow("SELECT COUNT(*) FROM push_subscriptions").Scan(&n); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}

	// POST subscribe: 201 la primera vez.
	body := `{"endpoint":"https://push.example/abc","keys":{"auth":"a1","p256dh":"p1"}}`
	res = doJSON(t, "POST", ts.URL, "/api/push/subscribe", cookie, body)
	if res.StatusCode != 201 {
		t.Fatalf("subscribe nuevo: %d, esperaba 201", res.StatusCode)
	}
	res.Body.Close()
	if count() != 1 {
		t.Fatalf("suscripciones: %d, esperaba 1", count())
	}
	// Upsert del mismo endpoint (claves nuevas): 200, sin duplicar.
	body = `{"endpoint":"https://push.example/abc","keys":{"auth":"a2","p256dh":"p2"}}`
	res = doJSON(t, "POST", ts.URL, "/api/push/subscribe", cookie, body)
	if res.StatusCode != 200 {
		t.Fatalf("subscribe upsert: %d, esperaba 200", res.StatusCode)
	}
	res.Body.Close()
	if count() != 1 {
		t.Fatalf("upsert duplicó: %d filas", count())
	}
	var auth string
	if err := ts.db.QueryRow("SELECT keys_auth FROM push_subscriptions WHERE endpoint = ?", "https://push.example/abc").Scan(&auth); err != nil || auth != "a2" {
		t.Fatalf("upsert no actualizó claves: auth=%q err=%v", auth, err)
	}
	// Cuerpos inválidos → 400.
	for _, bad := range []string{
		`nope`,
		`{"endpoint":"","keys":{"auth":"a","p256dh":"p"}}`,
		`{"endpoint":"https://x/y"}`,
		`{"endpoint":"https://x/y","keys":{"auth":"","p256dh":"p"}}`,
	} {
		res = doJSON(t, "POST", ts.URL, "/api/push/subscribe", cookie, bad)
		if res.StatusCode != 400 {
			t.Fatalf("subscribe body %s: %d, esperaba 400", bad, res.StatusCode)
		}
		res.Body.Close()
	}

	// POST unsubscribe: 204 y la fila desaparece; idempotente.
	res = doJSON(t, "POST", ts.URL, "/api/push/unsubscribe", cookie, `{"endpoint":"https://push.example/abc"}`)
	if res.StatusCode != 204 {
		t.Fatalf("unsubscribe: %d, esperaba 204", res.StatusCode)
	}
	res.Body.Close()
	if count() != 0 {
		t.Fatalf("tras unsubscribe: %d filas", count())
	}
	res = doJSON(t, "POST", ts.URL, "/api/push/unsubscribe", cookie, `{"endpoint":"https://push.example/abc"}`)
	if res.StatusCode != 204 {
		t.Fatalf("unsubscribe idempotente: %d, esperaba 204", res.StatusCode)
	}
	res.Body.Close()
	// Unsubscribe sin endpoint → 400.
	res = doJSON(t, "POST", ts.URL, "/api/push/unsubscribe", cookie, `{"endpoint":""}`)
	if res.StatusCode != 400 {
		t.Fatalf("unsubscribe sin endpoint: %d, esperaba 400", res.StatusCode)
	}
	res.Body.Close()
}
