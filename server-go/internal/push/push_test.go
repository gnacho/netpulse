// push_test.go — contrato del Bloque C (SPEC-PUSH §1): VAPID estable en kv,
// roundtrip del store de suscripciones, purga de suscripciones muertas
// (404/410) y Notify con payload cifrado verificable contra un push service
// simulado con httptest (descifrado aes128gcm RFC 8291/8188).
package push_test

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/push"
	"golang.org/x/crypto/hkdf"
)

func openDB(t *testing.T) (*db.DB, string) {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(dir)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d, dir
}

func TestVAPIDStable(t *testing.T) {
	d, dir := openDB(t)
	pub1, priv1, err := push.EnsureVAPIDKeys(d)
	if err != nil {
		t.Fatalf("EnsureVAPIDKeys: %v", err)
	}
	if pub1 == "" || priv1 == "" || pub1 == priv1 {
		t.Fatalf("par VAPID inválido: pub=%q priv=%q", pub1, priv1)
	}
	// Segunda llamada: mismo par (no regenera).
	pub2, priv2, _ := push.EnsureVAPIDKeys(d)
	if pub1 != pub2 || priv1 != priv2 {
		t.Fatal("VAPID no estable entre llamadas")
	}
	// Reinicio (reabrir la misma DB): mismo par (persistido en kv).
	d.Close()
	d2, err := db.Open(dir)
	if err != nil {
		t.Fatalf("reabrir db: %v", err)
	}
	defer d2.Close()
	pub3, priv3, _ := push.EnsureVAPIDKeys(d2)
	if pub1 != pub3 || priv1 != priv3 {
		t.Fatal("VAPID no estable entre reinicios")
	}
}

func TestStoreRoundtrip(t *testing.T) {
	d, _ := openDB(t)
	sub := push.Subscription{Endpoint: "https://push.example/abc", Auth: "a", P256dh: "p", UserAgent: "test-ua"}
	created, err := push.UpsertSubscription(d, sub)
	if err != nil || !created {
		t.Fatalf("upsert nuevo: created=%v err=%v", created, err)
	}
	// Upsert del mismo endpoint: actualiza, no duplica.
	sub.Auth = "a2"
	created, err = push.UpsertSubscription(d, sub)
	if err != nil || created {
		t.Fatalf("upsert existente: created=%v err=%v", created, err)
	}
	subs, err := push.ListSubscriptions(d)
	if err != nil || len(subs) != 1 {
		t.Fatalf("list: %v %v", subs, err)
	}
	if subs[0].Auth != "a2" || subs[0].P256dh != "p" || subs[0].UserAgent != "test-ua" || subs[0].CreatedAt <= 0 {
		t.Fatalf("fila: %+v", subs[0])
	}
	if err := push.DeleteSubscription(d, sub.Endpoint); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// Idempotente: borrar de nuevo no es error.
	if err := push.DeleteSubscription(d, sub.Endpoint); err != nil {
		t.Fatalf("delete idempotente: %v", err)
	}
	subs, _ = push.ListSubscriptions(d)
	if len(subs) != 0 {
		t.Fatalf("tras delete: %d suscripciones", len(subs))
	}
}

// browserSub simula una PushSubscription del navegador: par ECDH P-256 y
// auth secret (16 B), devueltos para poder descifrar en el test.
type browserSub struct {
	auth  []byte
	priv  []byte
	pub   []byte // uncompressed point (65 B)
	authB string
	pubB  string
}

func newBrowserSub(t *testing.T) browserSub {
	t.Helper()
	priv, x, y, err := elliptic.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("keypair navegador: %v", err)
	}
	pub := elliptic.Marshal(elliptic.P256(), x, y)
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatalf("auth secret: %v", err)
	}
	return browserSub{
		auth:  auth,
		priv:  priv,
		pub:   pub,
		authB: base64.RawURLEncoding.EncodeToString(auth),
		pubB:  base64.RawURLEncoding.EncodeToString(pub),
	}
}

type capturedPush struct {
	method string
	header http.Header
	body   []byte
}

// decryptAES128GCM descifra el cuerpo tal y como lo haría el navegador
// (RFC 8291 §3.4 + RFC 8188 §2: header salt|rs|idlen|keyid en el cuerpo,
// registro único con delimitador 0x02 y padding de ceros al final).
func decryptAES128GCM(t *testing.T, ua browserSub, body []byte) []byte {
	t.Helper()
	if len(body) < 16+4+1+65+17 {
		t.Fatalf("cuerpo cifrado demasiado corto: %d B", len(body))
	}
	salt := body[:16]
	rs := binary.BigEndian.Uint32(body[16:20])
	idlen := int(body[20])
	asPub := body[21 : 21+idlen]
	ciphertext := body[21+idlen:]
	if rs < uint32(len(ciphertext)) {
		t.Fatalf("record size incoherente: rs=%d ct=%d", rs, len(ciphertext))
	}

	curve := elliptic.P256()
	x, y := elliptic.Unmarshal(curve, asPub)
	if x == nil {
		t.Fatal("clave efímera del servidor no es un punto válido")
	}
	sx, _ := curve.ScalarMult(x, y, ua.priv)
	secret := make([]byte, 32)
	sx.FillBytes(secret)

	read := func(prkSecret, hkdfSalt, info []byte, n int) []byte {
		out := make([]byte, n)
		if _, err := io.ReadFull(hkdf.New(sha256.New, prkSecret, hkdfSalt, info), out); err != nil {
			t.Fatalf("hkdf: %v", err)
		}
		return out
	}
	info := append(append([]byte("WebPush: info\x00"), ua.pub...), asPub...)
	ikm := read(secret, ua.auth, info, 32)
	cek := read(ikm, salt, []byte("Content-Encoding: aes128gcm\x00"), 16)
	nonce := read(ikm, salt, []byte("Content-Encoding: nonce\x00"), 12)

	block, err := aes.NewCipher(cek)
	if err != nil {
		t.Fatalf("aes: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("gcm: %v", err)
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		t.Fatalf("descifrado GCM (¿claves mal derivadas?): %v", err)
	}
	// Quitar padding: ceros finales + delimitador 0x02 (registro final).
	i := len(plain) - 1
	for i >= 0 && plain[i] == 0 {
		i--
	}
	if i < 0 || plain[i] != 0x02 {
		t.Fatalf("delimitador 0x02 no encontrado (plain=%d B)", len(plain))
	}
	return plain[:i]
}

func testEvent() alerts.AlertEvent {
	return alerts.AlertEvent{
		ID:          "alert-test-1",
		Category:    alerts.CatRouter,
		Urgent:      true,
		Severity:    "critical",
		Title:       "Router sin respuesta",
		Description: "Sin respuesta de 192.168.8.1",
	}
}

func newNotifier(t *testing.T, d *db.DB) *push.Notifier {
	t.Helper()
	pub, priv, err := push.EnsureVAPIDKeys(d)
	if err != nil {
		t.Fatalf("vapid: %v", err)
	}
	n := push.NewNotifier(d, pub, priv)
	t.Cleanup(n.Close)
	return n
}

// El Notifier envía un POST cifrado aes128gcm con cabecera VAPID al endpoint
// suscrito, y el payload descifrado cumple el contrato del SW.
func TestNotifierSendsEncryptedPayload(t *testing.T) {
	d, _ := openDB(t)
	pub, _, _ := push.EnsureVAPIDKeys(d)
	ua := newBrowserSub(t)

	got := make(chan capturedPush, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got <- capturedPush{method: r.Method, header: r.Header.Clone(), body: body}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	if _, err := push.UpsertSubscription(d, push.Subscription{
		Endpoint: srv.URL, Auth: ua.authB, P256dh: ua.pubB,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	n := newNotifier(t, d)
	n.Notify(testEvent())

	var req capturedPush
	select {
	case req = <-got:
	case <-time.After(15 * time.Second):
		t.Fatal("el push service simulado no recibió nada en 15 s")
	}

	if req.method != http.MethodPost {
		t.Fatalf("método: %s, esperaba POST", req.method)
	}
	if enc := req.header.Get("Content-Encoding"); enc != "aes128gcm" {
		t.Fatalf("Content-Encoding: %q", enc)
	}
	if ttl := req.header.Get("TTL"); ttl != "3600" {
		t.Fatalf("TTL: %q", ttl)
	}
	authz := req.header.Get("Authorization")
	if !strings.HasPrefix(authz, "vapid t=") || !strings.Contains(authz, ", k=") {
		t.Fatalf("Authorization VAPID: %q", authz)
	}
	if !strings.Contains(authz, ", k="+pub) {
		t.Fatal("la cabecera VAPID no lleva la clave pública servida en /api/push/vapid-key")
	}

	plain := decryptAES128GCM(t, ua, req.body)
	var p struct {
		Title    string `json:"title"`
		Body     string `json:"body"`
		Category string `json:"category"`
		Severity string `json:"severity"`
		URL      string `json:"url"`
		Tag      string `json:"tag"`
	}
	if err := json.Unmarshal(plain, &p); err != nil {
		t.Fatalf("payload no es JSON: %v (%q)", err, plain)
	}
	if p.Title != "Router sin respuesta" || p.Body != "Sin respuesta de 192.168.8.1" ||
		p.Category != "router" || p.Severity != "critical" || p.URL != "/alerts" || p.Tag != "alert-test-1" {
		t.Fatalf("payload: %+v", p)
	}
}

// Un push service que responde 410 (suscripción expirada) provoca la purga
// de la fila; el resto de suscripciones se conserva.
func TestNotifierPurgesGoneSubscription(t *testing.T) {
	d, _ := openDB(t)
	ua := newBrowserSub(t)

	got := make(chan struct{}, 2)
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		got <- struct{}{}
		w.WriteHeader(http.StatusGone)
	}))
	defer dead.Close()
	alive := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		got <- struct{}{}
		w.WriteHeader(http.StatusCreated)
	}))
	defer alive.Close()

	for _, ep := range []string{dead.URL, alive.URL} {
		if _, err := push.UpsertSubscription(d, push.Subscription{Endpoint: ep, Auth: ua.authB, P256dh: ua.pubB}); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}

	n := newNotifier(t, d)
	n.Notify(testEvent())
	for i := 0; i < 2; i++ {
		select {
		case <-got:
		case <-time.After(15 * time.Second):
			t.Fatalf("solo %d/2 envíos recibidos", i)
		}
	}
	n.Close() // drena el worker: la purga ya ocurrió

	subs, err := push.ListSubscriptions(d)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(subs) != 1 || subs[0].Endpoint != alive.URL {
		t.Fatalf("tras purga: %+v", subs)
	}
}

// TestNotifierCloseConNotifyConcurrentesNoPanic reproduce el bug #202:
// Close() cerraba el canal de la cola y un Notify concurrente podía elegir
// la rama de envío tras el close → panic "send on closed channel".
func TestNotifierCloseConNotifyConcurrentesNoPanic(t *testing.T) {
	d, _ := openDB(t)
	pub, _, _ := push.EnsureVAPIDKeys(d)
	n := newNotifier(t, d)
	_ = pub
	done := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					n.Notify(testEvent())
				}
			}
		}()
	}
	time.Sleep(5 * time.Millisecond)
	n.Close()
	close(done)
	wg.Wait()
}
