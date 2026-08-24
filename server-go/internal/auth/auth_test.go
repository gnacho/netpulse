package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

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
	return &config.Config{AuthUser: "admin", AuthPass: "test123456", CookieSecure: "auto"}
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
	// El test simula la IP del cliente vía X-Forwarded-For; en producción
	// solo se confía con TRUST_PROXY (auditoría #1).
	SetTrustProxy(true)
	t.Cleanup(func() { SetTrustProxy(false) })
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

func TestLoginAttemptsDecayTrasLockExpired(t *testing.T) {
	// Issue #211: tras expirar el lockout (5 min), un fallo suelto ya NO
	// rearma el bloqueo — la cuenta decae a 1.
	SetTrustProxy(true)
	t.Cleanup(func() { SetTrustProxy(false) })
	d := testDB(t)
	newReq := func() *http.Request {
		r := httptest.NewRequest("POST", "/api/auth/login", nil)
		r.Header.Set("X-Forwarded-For", "10.9.9.9, 10.0.0.1")
		return r
	}
	for i := 0; i < 5; i++ {
		RegisterLoginFail(d, newReq())
	}
	if limited, _ := LoginRateLimited(d, newReq()); !limited {
		t.Fatal("tras 5 fallos debe estar bloqueado")
	}
	// El lockout expira.
	_, _ = d.Exec("UPDATE login_attempts SET locked_until = ? WHERE ip = '10.9.9.9'", db.NowMS()-1)
	// Un fallo suelto tras la expiración no rearma el lockout.
	RegisterLoginFail(d, newReq())
	if limited, _ := LoginRateLimited(d, newReq()); limited {
		t.Fatal("un fallo tras expirar el lockout no debe rearmarlo")
	}
	var attempts int
	var lockedUntil int64
	if err := d.QueryRow("SELECT attempts, locked_until FROM login_attempts WHERE ip = '10.9.9.9'").
		Scan(&attempts, &lockedUntil); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || lockedUntil != 0 {
		t.Fatalf("tras el decay esperaba attempts=1 locked_until=0, got %d %d", attempts, lockedUntil)
	}
}

func TestLoginAttemptsRearmanTrasBurstNuevo(t *testing.T) {
	// Issue #211: tras el decay, una ráfaga nueva de 5 fallos vuelve a
	// armar el bloqueo (no se queda desarmado para siempre).
	SetTrustProxy(true)
	t.Cleanup(func() { SetTrustProxy(false) })
	d := testDB(t)
	newReq := func() *http.Request {
		r := httptest.NewRequest("POST", "/api/auth/login", nil)
		r.Header.Set("X-Forwarded-For", "10.9.9.9, 10.0.0.1")
		return r
	}
	for i := 0; i < 5; i++ {
		RegisterLoginFail(d, newReq())
	}
	_, _ = d.Exec("UPDATE login_attempts SET locked_until = ? WHERE ip = '10.9.9.9'", db.NowMS()-1)
	// Decay: primer fallo tras la expiración resetea a 1.
	RegisterLoginFail(d, newReq())
	if limited, _ := LoginRateLimited(d, newReq()); limited {
		t.Fatal("el primer fallo tras expirar no debe rearmar")
	}
	// Ráfaga nueva completa: 5 fallos seguidos vuelven a armar el lockout.
	for i := 0; i < 5; i++ {
		RegisterLoginFail(d, newReq())
	}
	if limited, _ := LoginRateLimited(d, newReq()); !limited {
		t.Fatal("una ráfaga nueva de 5 fallos debe rearmar el lockout")
	}
}

func TestRequireSameOrigin(t *testing.T) {
	// Issue #213: mutaciones (POST/PUT/DELETE) validan Origin contra el host.
	SetTrustProxy(true)
	t.Cleanup(func() { SetTrustProxy(false) })
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := RequireSameOrigin(next)

	// POST sin Origin (cliente no-navegador) → pasa.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "http://192.168.1.226:3000/api/x", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("POST sin Origin debe pasar, got %d", rec.Code)
	}

	// POST con Origin == Host → pasa.
	req := httptest.NewRequest("POST", "http://192.168.1.226:3000/api/x", nil)
	req.Header.Set("Origin", "http://192.168.1.226:3000")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusNoContent {
		t.Fatalf("POST Origin==Host debe pasar, got %d", rec2.Code)
	}

	// POST con Origin distinto → 403 cross_origin.
	req2 := httptest.NewRequest("POST", "http://192.168.1.226:3000/api/x", nil)
	req2.Header.Set("Origin", "https://evil.example")
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req2)
	if rec3.Code != http.StatusForbidden {
		t.Fatalf("POST Origin distinto debe dar 403, got %d", rec3.Code)
	}

	// POST con Origin null (iframe sandbox) → 403.
	req3 := httptest.NewRequest("POST", "http://192.168.1.226:3000/api/x", nil)
	req3.Header.Set("Origin", "null")
	rec4 := httptest.NewRecorder()
	h.ServeHTTP(rec4, req3)
	if rec4.Code != http.StatusForbidden {
		t.Fatalf("POST Origin null debe dar 403, got %d", rec4.Code)
	}

	// GET no se valida (no es mutación).
	req4 := httptest.NewRequest("GET", "http://192.168.1.226:3000/api/x", nil)
	req4.Header.Set("Origin", "https://evil.example")
	rec5 := httptest.NewRecorder()
	h.ServeHTTP(rec5, req4)
	if rec5.Code != http.StatusNoContent {
		t.Fatalf("GET con Origin distinto no debe validarse, got %d", rec5.Code)
	}

	// Sec-Fetch-Site: cross-site sin Origin → 403.
	req5 := httptest.NewRequest("POST", "http://192.168.1.226:3000/api/x", nil)
	req5.Header.Set("Sec-Fetch-Site", "cross-site")
	rec6 := httptest.NewRecorder()
	h.ServeHTTP(rec6, req5)
	if rec6.Code != http.StatusForbidden {
		t.Fatalf("POST Sec-Fetch-Site=cross-site debe dar 403, got %d", rec6.Code)
	}
}

func TestEffectiveHostConfiaEnXFH(t *testing.T) {
	// Con TRUST_PROXY, X-Forwarded-Host (primer valor) es el host efectivo.
	SetTrustProxy(true)
	t.Cleanup(func() { SetTrustProxy(false) })
	r := httptest.NewRequest("POST", "http://internal:3000/api/x", nil)
	r.Header.Set("X-Forwarded-Host", "netpulse.example.com, internal:3000")
	if got := effectiveHost(r); got != "netpulse.example.com" {
		t.Fatalf("effectiveHost con XFH: %q", got)
	}
	// Sin TRUST_PROXY, XFH se ignora.
	SetTrustProxy(false)
	if got := effectiveHost(r); got != "internal:3000" {
		t.Fatalf("effectiveHost sin TRUST_PROXY: %q", got)
	}
}

func TestBuildSessionCookieFlags(t *testing.T) {
	// El caso X-Forwarded-Proto=https requiere TRUST_PROXY (auditoría #1).
	SetTrustProxy(true)
	t.Cleanup(func() { SetTrustProxy(false) })
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

func TestClientIPSinTrustProxyIgnoraXFF(t *testing.T) {
	// Auditoría #1: sin TRUST_PROXY, X-Forwarded-For se IGNORA — la IP del
	// cliente es la del socket, no se puede falsear para evadir rate-limit.
	SetTrustProxy(false)
	t.Cleanup(func() { SetTrustProxy(false) })
	r := httptest.NewRequest("POST", "/api/auth/login", nil)
	r.RemoteAddr = "192.168.1.55:50000"
	r.Header.Set("X-Forwarded-For", "10.9.9.9")
	if got := ClientIP(r); got != "192.168.1.55" {
		t.Fatalf("sin TRUST_PROXY debe usar la IP del socket: %q", got)
	}
	// Con TRUST_PROXY, XFF es la IP del cliente real (detrás de proxy).
	SetTrustProxy(true)
	if got := ClientIP(r); got != "10.9.9.9" {
		t.Fatalf("con TRUST_PROXY debe usar XFF: %q", got)
	}
}

func TestIsSecureRequestSinTrustProxyIgnoraXFP(t *testing.T) {
	// Auditoría #1: sin TRUST_PROXY, X-Forwarded-Proto se IGNORA (un cliente
	// de la LAN no puede forzar la cookie Secure).
	SetTrustProxy(false)
	t.Cleanup(func() { SetTrustProxy(false) })
	r := httptest.NewRequest("POST", "http://x/", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	if IsSecureRequest(r) {
		t.Fatal("sin TRUST_PROXY, XFP=https no debe considerarse seguro")
	}
	SetTrustProxy(true)
	if !IsSecureRequest(r) {
		t.Fatal("con TRUST_PROXY, XFP=https debe considerarse seguro")
	}
}

func TestLoginDummyHashValido(t *testing.T) {
	// El dummy de timing del login debe ser un hash bcrypt VÁLIDO: si no lo
	// es (p.ej. caracteres ilegales como '_'), CompareHashAndPassword falla
	// en microsegundos y el login se convierte en un oráculo de enumeración
	// de usuarios (issue #209).
	err := bcrypt.CompareHashAndPassword([]byte(dummyPasswordHash), []byte("cualquier-password"))
	if err == nil {
		t.Fatal("comparar contra el dummy con una password cualquiera no debe acertar")
	}
	if strings.Contains(err.Error(), "illegal base64 data") {
		t.Fatalf("dummyPasswordHash no es un hash bcrypt válido: %v", err)
	}
}
