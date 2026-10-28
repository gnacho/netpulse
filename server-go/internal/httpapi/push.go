// push.go — endpoints Web Push (SPEC-PUSH §1), tras auth de sesión:
//
//	GET  /api/push/vapid-key   → {"key":"<base64url>"} (clave pública VAPID)
//	POST /api/push/subscribe   {endpoint, keys:{auth,p256dh}, language} → 201/200 (upsert)
//	POST /api/push/unsubscribe {endpoint} → 204
package httpapi

import (
	"net/http"
	"strings"

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
