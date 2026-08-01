package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

func testDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func testCfg() *config.Config {
	return &config.Config{AuthUser: "admin", AuthPass: "test1234", CookieSecure: "auto"}
}

func TestSignVerifyRoundtrip(t *testing.T) {
	secret := strings.Repeat("ab", 32)
	id := "f47ac10b-58cc-4372-a567-0e02b2c3d479"
	signed := SignSessionID(secret, id)
	if got := VerifySessionCookie(secret, signed); got != id {
		t.Fatalf("roundtrip: got %q, want %q", got, id)
	}
	// El id puede contener puntos: el split es por el ÚLTIMO punto.
	weird := "id.con.puntos"
	if got := VerifySessionCookie(secret, SignSessionID(secret, weird)); got != weird {
		t.Fatalf("split último punto: got %q", got)
	}
}

func TestVerifyRejects(t *testing.T) {
	secret := strings.Repeat("ab", 32)
	id := "f47ac10b-58cc-4372-a567-0e02b2c3d479"
	signed := SignSessionID(secret, id)
	cases := map[string]string{
		"vacío":                          "",
		"sin punto":                      "abcdef",
		"punto al inicio":                ".abcdef",
		"firma alterada":                 signed[:len(signed)-2] + "00",
		"otro secret":                    SignSessionID(strings.Repeat("cd", 32), id),
		"misma longitud, distinta firma": tamperedSignature(secret, id),
	}
	for name, v := range cases {
		if got := VerifySessionCookie(secret, v); got != "" {
			t.Errorf("%s: esperaba rechazo, got %q", name, got)
		}
	}
}

// tamperedSignature cambia el último carácter hex de la firma (misma longitud).
func tamperedSignature(secret, id string) string {
	signed := SignSessionID(secret, id)
	last := signed[len(signed)-1]
	repl := byte('0')
	if last == '0' {
		repl = '1'
	}
	return signed[:len(signed)-1] + string(repl)
}

func TestCookieExtraction(t *testing.T) {
	d := testDB(t)
	secret, err := EnsureSessionSecret(d, testCfg())
	if err != nil {
		t.Fatal(err)
	}
	if len(secret) != 64 {
		t.Fatalf("secret autogenerado debe ser 64 hex, got %d", len(secret))
	}
	// Persistido en kv: segunda llamada devuelve el mismo.
	again, _ := EnsureSessionSecret(d, testCfg())
	if again != secret {
		t.Fatal("el secret debe persistirse en kv")
	}

	if err := EnsureUsers(d, testCfg()); err != nil {
		t.Fatal(err)
	}
	admin := GetUserByName(d, "admin")
	if admin == nil {
		t.Fatal("seed admin")
	}
	id := CreateSession(d, "TestAgent/1.0", admin.ID)
	signed := SignSessionID(secret, id)
	r := httptest.NewRequest("GET", "/api/auth/me", nil)
	r.Header.Set("Cookie", "other=1; session="+signed+"; theme=dark")
	su := SessionUserFromRequest(d, secret, r)
	if su == nil {
		t.Fatal("cookie válida debe autenticar")
	}
	if su.SessionID != id {
		t.Fatalf("sessionId: got %q want %q", su.SessionID, id)
	}
	// Primera ocurrencia gana (regex del JS).
	r2 := httptest.NewRequest("GET", "/x", nil)
	r2.Header.Set("Cookie", "session="+signed+"; session=garbage.garbage")
	if SessionIDFromRequest(d, secret, r2) != id {
		t.Fatal("debe usar la primera ocurrencia de session=")
	}
}

func TestSessionExpiry(t *testing.T) {
	d := testDB(t)
	id := CreateSession(d, "", 1)
	// Expira la sesión manualmente.
	_, _ = d.Exec("UPDATE sessions SET expires_at = ? WHERE id = ?", db.NowMS()-1000, id)
	if GetSession(d, id) != nil {
		t.Fatal("sesión expirada debe ser inválida")
	}
	// Y se borra de la tabla.
	var n int
	_ = d.QueryRow("SELECT COUNT(*) FROM sessions WHERE id = ?", id).Scan(&n)
	if n != 0 {
		t.Fatal("sesión expirada debe borrarse al leerla")
	}
}

func TestSessionWithoutUserIDDoesNotAuthenticate(t *testing.T) {
	d := testDB(t)
	secret, _ := EnsureSessionSecret(d, testCfg())
	// Sesión legacy sin user_id.
	_, _ = d.Exec("INSERT INTO sessions (id, created_at, expires_at, ua, user_id) VALUES ('legacy', ?, ?, '', NULL)",
		db.NowMS(), db.NowMS()+SessionTTLMS)
	r := httptest.NewRequest("GET", "/api/overview", nil)
	r.Header.Set("Cookie", "session="+SignSessionID(secret, "legacy"))
	if SessionUserFromRequest(d, secret, r) != nil {
		t.Fatal("sesión sin user_id NO debe autenticar")
	}
}

func TestRateLimitFifthFailArmsLock(t *testing.T) {
	d := testDB(t)
	newReq := func() *http.Request {
		r := httptest.NewRequest("POST", "/api/auth/login", nil)
		r.Header.Set("X-Forwarded-For", "10.9.9.9, 10.0.0.1")
		return r
	}
	for i := 1; i <= 5; i++ {
		if limited, _ := LoginRateLimited(d, newReq()); limited {
			t.Fatalf("fallo %d: no debe estar limitado aún", i)
		}
		RegisterLoginFail(d, newReq())
	}
	limited, retry := LoginRateLimited(d, newReq())
	if !limited {
		t.Fatal("tras el 5º fallo la IP debe estar bloqueada")
	}
	if retry <= 0 || retry > 300 {
		t.Fatalf("retryAfterSec debe estar en (0, 300], got %d", retry)
	}
	// Persistencia en SQLite.
	var attempts int
	var lockedUntil int64
	if err := d.QueryRow("SELECT attempts, locked_until FROM login_attempts WHERE ip = '10.9.9.9'").
		Scan(&attempts, &lockedUntil); err != nil {
		t.Fatal(err)
	}
	if attempts < 5 || lockedUntil <= db.NowMS() {
		t.Fatalf("fila esperada attempts>=5 y locked_until futuro: %d %d", attempts, lockedUntil)
	}
	// LoginOk limpia la fila.
	LoginOk(d, newReq())
	if limited, _ := LoginRateLimited(d, newReq()); limited {
		t.Fatal("LoginOk debe limpiar el bloqueo")
	}
}

func TestBuildSessionCookieFlags(t *testing.T) {
	cfg := &config.Config{CookieSecure: "auto"}
	r := httptest.NewRequest("POST", "http://x/api/auth/login", nil)
	c := BuildSessionCookie(cfg, r, "id.sig", 2592000)
	want := "session=id.sig; Path=/; HttpOnly; SameSite=Lax; Max-Age=2592000"
	if c != want {
		t.Fatalf("cookie http:\n got %q\nwant %q", c, want)
	}
	// X-Forwarded-Proto https → Secure (auto).
	r2 := httptest.NewRequest("POST", "http://x/api/auth/login", nil)
	r2.Header.Set("X-Forwarded-Proto", "https, http")
	if got := BuildSessionCookie(cfg, r2, "id.sig", 2592000); !strings.HasSuffix(got, "; Secure") {
		t.Fatalf("auto + X-Forwarded-Proto=https debe añadir Secure: %q", got)
	}
	// always / never.
	cfgAlways := &config.Config{CookieSecure: "always"}
	if got := BuildSessionCookie(cfgAlways, r, "i.s", 1); !strings.HasSuffix(got, "; Secure") {
		t.Fatal("always debe forzar Secure")
	}
	cfgNever := &config.Config{CookieSecure: "never"}
	r3 := httptest.NewRequest("POST", "https://x/", nil)
	if got := BuildSessionCookie(cfgNever, r3, "i.s", 1); strings.Contains(got, "Secure") {
		t.Fatal("never debe omitir Secure incluso en https")
	}
}
