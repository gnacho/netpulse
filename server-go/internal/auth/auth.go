// Package auth — autenticación multiusuario (paridad con server/src/auth.js):
//   - Cookie `session` = `id.hmac` (UUID v4 . hex(HMAC-SHA256)), httpOnly,
//     SameSite=Lax, 30 días; Secure según COOKIE_SECURE / X-Forwarded-Proto.
//   - Verificación: split por el ÚLTIMO punto, comparación timing-safe.
//   - Secret HMAC: SESSION_SECRET o autogenerado persistido en kv.
//   - Sesiones en SQLite (30 d), rotación tras login.
//   - Rate-limit persistente (login_attempts): bloqueo 5 min armado cuando
//     attempts previos >= 4 (5º fallo), 429 con retryAfterSec.
//   - bcrypt cost 10 (hashes bcryptjs $2a/$2b verificables con x/crypto).
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"

	"golang.org/x/crypto/bcrypt"

	"github.com/gnacho/netpulse/server-go/internal/config"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

const (
	// SessionCookie es el nombre de la cookie de sesión.
	SessionCookie = "session"
	// SessionTTLMS = 30 días en ms (Max-Age=2592000).
	SessionTTLMS = 30 * 24 * 60 * 60 * 1000
	// LockMS = 5 minutos en ms.
	LockMS = 5 * 60 * 1000
)

// ---------------------------------------------------------------------------
// Usuarios
// ---------------------------------------------------------------------------

// User fila pública de users (sin pass_hash).
type User struct {
	ID       int64
	Username string
	Role     string
}

// EnsureUsers crea el admin inicial (AUTH_USER/AUTH_PASS) si users está vacía.
func EnsureUsers(d *db.DB, cfg *config.Config) error {
	var count int
	if err := d.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.AuthPass), 10)
	if err != nil {
		return err
	}
	if _, err := d.Exec(
		"INSERT INTO users (username, pass_hash, role, created_at) VALUES (?, ?, 'admin', ?)",
		cfg.AuthUser, string(hash), db.NowMS(),
	); err != nil {
		return err
	}
	log.Printf("[netpulse] usuario admin inicial creado: %s", cfg.AuthUser)
	return nil
}

type userRow struct {
	ID       int64
	Username string
	PassHash string
	Role     string
}

// GetUserByName devuelve la fila de login (con pass_hash) o nil.
func GetUserByName(d *db.DB, username string) *userRow {
	var u userRow
	err := d.QueryRow("SELECT id, username, pass_hash, role FROM users WHERE username = ?", username).
		Scan(&u.ID, &u.Username, &u.PassHash, &u.Role)
	if err != nil {
		return nil
	}
	return &u
}

// GetUserByID devuelve {id, username, role} o nil.
func GetUserByID(d *db.DB, id int64) *User {
	var u User
	err := d.QueryRow("SELECT id, username, role FROM users WHERE id = ?", id).
		Scan(&u.ID, &u.Username, &u.Role)
	if err != nil {
		return nil
	}
	return &u
}

// CheckPassword verifica bcrypt (cost 10; hashes bcryptjs $2a/$2b portables).
func CheckPassword(password, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// HashPassword genera un hash bcrypt cost 10.
func HashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	return string(h), err
}

// ---------------------------------------------------------------------------
// Secret HMAC
// ---------------------------------------------------------------------------

// EnsureSessionSecret: SESSION_SECRET del env si existe; si no, kv
// `session_secret`; si no existe, se genera (64 chars hex) y se persiste.
func EnsureSessionSecret(d *db.DB, cfg *config.Config) (string, error) {
	if cfg.SessionSecret != "" {
		return cfg.SessionSecret, nil
	}
	var value string
	err := d.QueryRow("SELECT value FROM kv WHERE key = 'session_secret'").Scan(&value)
	if err == nil && value != "" {
		return value, nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	secret := hex.EncodeToString(buf)
	if _, err := d.Exec(
		"INSERT INTO kv (key, value) VALUES ('session_secret', ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		secret,
	); err != nil {
		return "", err
	}
	return secret, nil
}

// ---------------------------------------------------------------------------
// Cookie id.hmac
// ---------------------------------------------------------------------------

func hmacHex(secret, id string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(id))
	return hex.EncodeToString(m.Sum(nil))
}

// SignSessionID devuelve `${id}.${hmac}`.
func SignSessionID(secret, id string) string {
	return id + "." + hmacHex(secret, id)
}

// VerifySessionCookie valida `id.hmac`: split por el ÚLTIMO punto y
// comparación timing-safe. Devuelve el id o "".
func VerifySessionCookie(secret, value string) string {
	if value == "" {
		return ""
	}
	dot := strings.LastIndex(value, ".")
	if dot <= 0 {
		return ""
	}
	id := value[:dot]
	sig := value[dot+1:]
	expected := hmacHex(secret, id)
	// hmac.Equal es constant-time para entradas de la misma longitud
	// (paridad con safeEqual de auth.js:88-92).
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return ""
	}
	return id
}

// cookieRe replica /(?:^|;\s*)session=([^;]+)/ (primera ocurrencia).
var cookieRe = regexp.MustCompile(`(?:^|;\s*)session=([^;]+)`)

func sessionCookieValue(r *http.Request) string {
	m := cookieRe.FindStringSubmatch(r.Header.Get("Cookie"))
	if m == nil {
		return ""
	}
	return m[1]
}

// IsSecureRequest: URL https, o primer X-Forwarded-Proto == "https" SOLO si
// TRUST_PROXY (despliegue detrás de proxy con TLS terminado fuera). Sin
// TRUST_PROXY, un cliente de la LAN no puede forzar la cookie Secure
// (hallazgo #1 de la auditoría de seguridad).
func IsSecureRequest(r *http.Request) bool {
	if r.TLS != nil || r.URL.Scheme == "https" {
		return true
	}
	if !trustProxy {
		return false
	}
	proto := r.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		return false
	}
	first := strings.TrimSpace(strings.Split(proto, ",")[0])
	return first == "https"
}

// CookieSecureFlag: always→true, never→false, auto→IsSecureRequest.
func CookieSecureFlag(cfg *config.Config, r *http.Request) bool {
	switch cfg.CookieSecure {
	case "always":
		return true
	case "never":
		return false
	default:
		return IsSecureRequest(r)
	}
}

// BuildSessionCookie compone el header Set-Cookie (literal de auth.js:119-129).
func BuildSessionCookie(cfg *config.Config, r *http.Request, signedID string, maxAgeSec int64) string {
	parts := []string{
		fmt.Sprintf("%s=%s", SessionCookie, signedID),
		"Path=/",
		"HttpOnly",
		"SameSite=Lax",
		fmt.Sprintf("Max-Age=%d", maxAgeSec),
	}
	if CookieSecureFlag(cfg, r) {
		parts = append(parts, "Secure")
	}
	return strings.Join(parts, "; ")
}

// ClearSessionCookie borra la cookie (literal, SIN Secure aunque sea HTTPS).
const ClearSessionCookie = "session=; Path=/; HttpOnly; SameSite=Lax; Max-Age=0"

// ---------------------------------------------------------------------------
// Sesiones en SQLite
// ---------------------------------------------------------------------------

// newUUIDv4 genera un UUID v4 canónico (8-4-4-4-12 hex) con crypto/rand.
func newUUIDv4() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40 // versión 4
	b[8] = (b[8] & 0x3f) | 0x80 // variante RFC 4122
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// CreateSession crea una sesión (UUID v4, TTL 30 d, ua truncado a 255).
func CreateSession(d *db.DB, ua string, userID int64) string {
	id := newUUIDv4()
	now := db.NowMS()
	if len(ua) > 255 {
		ua = ua[:255]
	}
	_, _ = d.Exec(
		"INSERT INTO sessions (id, created_at, expires_at, ua, user_id) VALUES (?, ?, ?, ?, ?)",
		id, now, now+SessionTTLMS, ua, userID,
	)
	return id
}

// DestroySession borra una sesión por id.
func DestroySession(d *db.DB, id string) {
	if id != "" {
		_, _ = d.Exec("DELETE FROM sessions WHERE id = ?", id)
	}
}

// DestroyUserSessions borra todas las sesiones de un usuario (re-login forzado).
func DestroyUserSessions(d *db.DB, userID int64) {
	_, _ = d.Exec("DELETE FROM sessions WHERE user_id = ?", userID)
}

// Session es la fila viva de sessions.
type Session struct {
	ID        string
	ExpiresAt int64
	UserID    sql.NullInt64
}

// GetSession devuelve la sesión si existe y no expiró (si expiró, la borra).
func GetSession(d *db.DB, id string) *Session {
	if id == "" {
		return nil
	}
	var s Session
	err := d.QueryRow("SELECT id, expires_at, user_id FROM sessions WHERE id = ?", id).
		Scan(&s.ID, &s.ExpiresAt, &s.UserID)
	if err != nil {
		return nil
	}
	if s.ExpiresAt < db.NowMS() {
		DestroySession(d, id)
		return nil
	}
	return &s
}

// SessionUser es el resultado de sessionUserFromRequest.
type SessionUser struct {
	SessionID string
	User      *User
}

// SessionUserFromRequest: cookie válida + sesión en DB con user_id NO nulo +
// usuario existente (una sesión legacy sin user_id NO autentica).
func SessionUserFromRequest(d *db.DB, secret string, r *http.Request) *SessionUser {
	id := VerifySessionCookie(secret, sessionCookieValue(r))
	sess := GetSession(d, id)
	if sess == nil || !sess.UserID.Valid {
		return nil
	}
	user := GetUserByID(d, sess.UserID.Int64)
	if user == nil {
		return nil
	}
	return &SessionUser{SessionID: id, User: user}
}

// SessionIDFromRequest devuelve el id de sesión válida (sin exigir user_id).
func SessionIDFromRequest(d *db.DB, secret string, r *http.Request) string {
	id := VerifySessionCookie(secret, sessionCookieValue(r))
	if GetSession(d, id) == nil {
		return ""
	}
	return id
}

// ---------------------------------------------------------------------------
// Rate-limit en SQLite (persiste tras reinicios)
// ---------------------------------------------------------------------------

// trustProxy es la configuración global del paquete: si es true, ClientIP
// confía en X-Forwarded-For (despliegue detrás de un proxy/reverse); si es
// false (defecto), usa la IP remota del socket y XFF se ignora — un cliente
// de la LAN no puede falsear su IP para evadir el rate-limit de login o de
// ingesta (auditoría de seguridad, hallazgo #1). Se fija una vez al arrancar
// desde config (TRUST_PROXY).
var trustProxy bool

// SetTrustProxy fija si las cabeceras X-Forwarded-For/-Proto son confiables.
func SetTrustProxy(on bool) { trustProxy = on }

// TrustProxy devuelve el valor actual (útil en tests).
func TrustProxy() bool { return trustProxy }

// ClientIP: primer X-Forwarded-For (solo si TRUST_PROXY) o la IP remota.
func ClientIP(r *http.Request) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if first := strings.TrimSpace(strings.Split(xff, ",")[0]); first != "" {
				return first
			}
		}
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil && host != "" {
		return host
	}
	if r.RemoteAddr != "" {
		return r.RemoteAddr
	}
	return "unknown"
}

// LoginRateLimited: si la IP está bloqueada → retryAfterSec = ceil(rest/1000).
func LoginRateLimited(d *db.DB, r *http.Request) (limited bool, retryAfterSec int64) {
	ip := ClientIP(r)
	var lockedUntil int64
	err := d.QueryRow("SELECT locked_until FROM login_attempts WHERE ip = ?", ip).Scan(&lockedUntil)
	if err != nil {
		return false, 0
	}
	remaining := lockedUntil - db.NowMS()
	if remaining > 0 {
		return true, (remaining + 999) / 1000
	}
	return false, 0
}

// RegisterLoginFail registra un fallo: bloquea 5 min cuando el valor ANTERIOR
// de attempts >= 4 (el bloqueo se arma en el 5º fallo — UPSERT literal).
func RegisterLoginFail(d *db.DB, r *http.Request) {
	ip := ClientIP(r)
	_, _ = d.Exec(`
		INSERT INTO login_attempts (ip, attempts, locked_until)
		VALUES (?, 1, 0)
		ON CONFLICT(ip) DO UPDATE SET
		  attempts = attempts + 1,
		  locked_until = CASE
		    WHEN attempts >= 4 THEN ?
		    ELSE locked_until
		  END
	`, ip, db.NowMS()+LockMS)
}

// LoginOk borra la fila de login_attempts de la IP.
func LoginOk(d *db.DB, r *http.Request) {
	_, _ = d.Exec("DELETE FROM login_attempts WHERE ip = ?", ClientIP(r))
}

// ---------------------------------------------------------------------------
// Login / logout
// ---------------------------------------------------------------------------

// LoginResult es el resultado de HandleLogin.
type LoginResult struct {
	SessionID string
	User      *User
}

// HandleLogin valida credenciales y crea sesión nueva (rotación: invalida la
// sesión previa de la cookie si existía). Nil si credenciales inválidas.
func HandleLogin(d *db.DB, secret string, r *http.Request, username, password string) *LoginResult {
	if username == "" || password == "" {
		return nil
	}
	u := GetUserByName(d, username)
	if u == nil || !CheckPassword(password, u.PassHash) {
		return nil
	}
	// Rotación de sesión
	if prevID := SessionIDFromRequest(d, secret, r); prevID != "" {
		DestroySession(d, prevID)
	}
	id := CreateSession(d, r.Header.Get("User-Agent"), u.ID)
	return &LoginResult{SessionID: id, User: &User{ID: u.ID, Username: u.Username, Role: u.Role}}
}

// HandleLogout destruye la sesión de la cookie (si existe y es válida).
func HandleLogout(d *db.DB, secret string, r *http.Request) {
	if id := SessionIDFromRequest(d, secret, r); id != "" {
		DestroySession(d, id)
	}
}
