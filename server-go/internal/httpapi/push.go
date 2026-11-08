// push.go — endpoints Web Push (SPEC-PUSH §1), tras auth de sesión:
//
//	GET  /api/push/vapid-key   → {"key":"<base64url>"} (clave pública VAPID)
//	POST /api/push/subscribe   {endpoint, keys:{auth,p256dh}, language} → 201/200 (upsert)
//	POST /api/push/unsubscribe {endpoint} → 204
//	POST /api/push/test        {endpoint} → {ok, status, reason, expired} (#1973)
package httpapi

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/db"
	"github.com/gnacho/netpulse/server-go/internal/push"
)

// Límites defensivos de longitud (los push endpoints reales son < 1 KB).
const (
	maxEndpointLen = 2048
	maxPushKeyLen  = 512
)

// handlePushVapidKey (GET): clave pública VAPID para pushManager.subscribe().
// Asegura el par en kv si faltara (main ya lo genera en el arranque).
func (s *server) handlePushVapidKey(w http.ResponseWriter, _ *http.Request) {
	pub, _, err := push.EnsureVAPIDKeys(s.db)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "push_unavailable", "no se pudo obtener la clave VAPID")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": pub})
}

type pushSubscribeBody struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		Auth   string `json:"auth"`
		P256dh string `json:"p256dh"`
	} `json:"keys"`
	// Language (#1334): idioma efectivo de la UI en el alta (es|en); el push
	// se compone en ese idioma por suscriptor.
	Language string `json:"language"`
}

// handlePushSubscribe (POST): upsert por endpoint. 201 si es nueva, 200 si
// actualizó una existente (la app solo exige res.ok).
func (s *server) handlePushSubscribe(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFromContext(r.Context())
	if me == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body pushSubscribeBody
	if st := readJSONBody(w, r, &body); st != 0 {
		writeBodyError(w, st, "invalid_body", "JSON esperado: {endpoint, keys:{auth, p256dh}, language}")
		return
	}
	body.Endpoint = strings.TrimSpace(body.Endpoint)
	if body.Endpoint == "" || len(body.Endpoint) > maxEndpointLen ||
		body.Keys.Auth == "" || len(body.Keys.Auth) > maxPushKeyLen ||
		body.Keys.P256dh == "" || len(body.Keys.P256dh) > maxPushKeyLen ||
		(body.Language != "es" && body.Language != "en") {
		writeError(w, http.StatusBadRequest, "invalid_body", "endpoint, keys.auth, keys.p256dh y language (es|en) son obligatorios")
		return
	}
	if err := push.ValidateEndpoint(body.Endpoint); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_endpoint", err.Error())
		return
	}
	created, err := push.UpsertSubscription(s.db, push.Subscription{
		Endpoint:  body.Endpoint,
		Auth:      body.Keys.Auth,
		P256dh:    body.Keys.P256dh,
		UserAgent: r.UserAgent(),
		CreatedAt: db.NowMS(),
		UserID:    me.ID,
		Language:  body.Language,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "push_unavailable", "no se pudo guardar la suscripción")
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"ok": true})
}

// handlePushUnsubscribe (POST): baja por endpoint, SOLO de suscripciones del
// usuario autenticado (#1334). 204 siempre (idempotente: dar de baja dos
// veces no es error; la app no bloquea la baja local).
func (s *server) handlePushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFromContext(r.Context())
	if me == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if st := readJSONBody(w, r, &body); st != 0 {
		writeBodyError(w, st, "invalid_body", "endpoint es obligatorio")
		return
	}
	if strings.TrimSpace(body.Endpoint) == "" {
		writeError(w, http.StatusBadRequest, "invalid_body", "endpoint es obligatorio")
		return
	}
	if _, err := s.db.Exec(
		"DELETE FROM push_subscriptions WHERE endpoint = ? AND user_id = ?",
		body.Endpoint, me.ID,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "push_unavailable", "no se pudo borrar la suscripción")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PushTester lo implementa *push.Notifier; en tests se inyecta un fake para
// no depender de un push service real.
type PushTester interface {
	SendTest(sub push.Subscription) push.TestResult
}

// pushTestCooldown limita el boton de prueba a un envio por usuario y ventana
// (#1973). Var de paquete para poder acortarla en tests.
var pushTestCooldown = 10 * time.Second

// PushTestCooldownForTests cambia la ventana y devuelve la anterior. Solo para
// tests de otros paquetes: la ventana real no es configurable.
func PushTestCooldownForTests(d time.Duration) time.Duration {
	old := pushTestCooldown
	pushTestCooldown = d
	return old
}

// handlePushTest (POST): manda una notificacion de prueba a la suscripcion del
// propio usuario por el camino real de las alertas y devuelve el resultado.
// Un endpoint ajeno responde 404, igual que uno desconocido.
func (s *server) handlePushTest(w http.ResponseWriter, r *http.Request) {
	me := auth.UserFromContext(r.Context())
	if me == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if s.pushNotif == nil {
		writeError(w, http.StatusServiceUnavailable, "push_unavailable", "Web Push no esta configurado")
		return
	}
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if st := readJSONBody(w, r, &body); st != 0 {
		writeBodyError(w, st, "invalid_body", "endpoint es obligatorio")
		return
	}
	endpoint := strings.TrimSpace(body.Endpoint)
	if endpoint == "" || len(endpoint) > maxEndpointLen {
		writeError(w, http.StatusBadRequest, "invalid_body", "endpoint es obligatorio")
		return
	}
	sub, ok, err := push.SubscriptionForUser(s.db, endpoint, me.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "push_unavailable", "no se pudo leer la suscripcion")
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	now := time.Now()
	s.pushTestMu.Lock()
	if s.pushTestAt == nil {
		s.pushTestAt = map[int64]time.Time{}
	}
	if last, seen := s.pushTestAt[me.ID]; seen && now.Sub(last) < pushTestCooldown {
		s.pushTestMu.Unlock()
		writeError(w, http.StatusTooManyRequests, "rate_limited", "espera unos segundos antes de enviar otra prueba")
		return
	}
	s.pushTestAt[me.ID] = now
	s.pushTestMu.Unlock()

	res := s.pushNotif.SendTest(sub)
	if res.Expired {
		// El notifier real ya purga; se repite aqui para que el contrato no
		// dependa de la implementacion (un fake o un futuro canal lo cumplen).
		if err := push.DeleteSubscription(s.db, endpoint); err != nil {
			log.Printf("[netpulse] warning: could not purge expired push subscription: %v", err)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "expired": true, "status": res.Status, "reason": res.Reason,
		})
		return
	}
	if res.Error != "" || res.Status >= 400 {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": false, "status": res.Status, "reason": res.Reason, "error": res.Error,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": res.Status})
}
