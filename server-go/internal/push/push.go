// Package push — Web Push nativo (SPEC-PUSH §1): par VAPID en kv, store de
// suscripciones (tabla push_subscriptions) y Notifier que implementa
// alerts.Notifier enviando a TODAS las suscripciones sin bloquear el poll
// (cola interna acotada + worker único).
//
// Claves kv:
//   - push.vapid.private / push.vapid.public: par VAPID (base64url) generado
//     en el primer arranque (o en el primer uso si falta).
package push

import (
	"database/sql"
	"errors"
	"fmt"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/gnacho/netpulse/server-go/internal/db"
)

// Claves kv del par VAPID (SPEC-PUSH §1).
const (
	vapidPrivateKey = "push.vapid.private"
	vapidPublicKey  = "push.vapid.public"
)

// Subscription es una fila de push_subscriptions.
type Subscription struct {
	Endpoint  string
	Auth      string
	P256dh    string
	UserAgent string
	CreatedAt int64 // epoch ms (convención del resto de la DB)
	// UserID: dueño autenticado de la suscripción (#1334); 0 = suscripción
	// legacy sin dueño (se conserva y sigue recibiendo).
	UserID int64
	// Language: idioma efectivo de la UI en el alta (#1334); "" = legacy (el
	// push sale en el idioma global del server, comportamiento anterior).
	Language string
}

// EnsureVAPIDKeys devuelve (public, private) del par VAPID; si no existe en
// kv lo genera y lo persiste (idempotente: la pública es estable entre
// llamadas y entre reinicios).
func EnsureVAPIDKeys(d *db.DB) (pub, priv string, err error) {
	pub = kvGet(d, vapidPublicKey)
	priv = kvGet(d, vapidPrivateKey)
	if pub != "" && priv != "" {
		return pub, priv, nil
	}
	priv, pub, err = webpush.GenerateVAPIDKeys()
	if err != nil {
		return "", "", fmt.Errorf("generar par VAPID: %w", err)
	}
	for _, kv := range [][2]string{{vapidPrivateKey, priv}, {vapidPublicKey, pub}} {
		if _, err := d.Exec(
			"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value",
			kv[0], kv[1]); err != nil {
			return "", "", fmt.Errorf("guardar %s: %w", kv[0], err)
		}
	}
	return pub, priv, nil
}

func kvGet(d *db.DB, key string) string {
	var v string
	if err := d.QueryRow("SELECT value FROM kv WHERE key = ?", key).Scan(&v); err != nil {
		return "" // sql.ErrNoRows incluido: tratado como "no existe"
	}
	return v
}

// UpsertSubscription inserta o actualiza por endpoint (la app re-suscribe el
// mismo endpoint al rotar claves). Devuelve created=true si era nueva.
func UpsertSubscription(d *db.DB, s Subscription) (created bool, err error) {
	var exists int
	err = d.QueryRow("SELECT 1 FROM push_subscriptions WHERE endpoint = ?", s.Endpoint).Scan(&exists)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	created = errors.Is(err, sql.ErrNoRows)
	if s.CreatedAt == 0 {
		s.CreatedAt = db.NowMS()
	}
	_, err = d.Exec(`INSERT INTO push_subscriptions (endpoint, keys_auth, keys_p256dh, user_agent, created_at, user_id, language)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(endpoint) DO UPDATE SET keys_auth=excluded.keys_auth,
			keys_p256dh=excluded.keys_p256dh, user_agent=excluded.user_agent,
			user_id=excluded.user_id, language=excluded.language`,
		s.Endpoint, s.Auth, s.P256dh, s.UserAgent, s.CreatedAt, nullableUserID(s.UserID), s.Language)
	return created, err
}

func nullableUserID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// SubscriptionForUser devuelve la suscripcion de un endpoint SOLO si
// pertenece al usuario (#1973): otra persona recibe el mismo "no existe" que
// un endpoint desconocido. El segundo valor es false si no hay fila.
func SubscriptionForUser(d *db.DB, endpoint string, userID int64) (Subscription, bool, error) {
	var s Subscription
	var ua, language sql.NullString
	var uid sql.NullInt64
	err := d.QueryRow(`SELECT endpoint, keys_auth, keys_p256dh, user_agent, created_at, user_id, language
		FROM push_subscriptions WHERE endpoint = ? AND user_id = ?`, endpoint, userID).
		Scan(&s.Endpoint, &s.Auth, &s.P256dh, &ua, &s.CreatedAt, &uid, &language)
	if errors.Is(err, sql.ErrNoRows) {
		return Subscription{}, false, nil
	}
	if err != nil {
		return Subscription{}, false, err
	}
	s.UserAgent, s.Language = ua.String, language.String
	s.UserID = uid.Int64
	return s, true, nil
}

// DeleteSubscription borra por endpoint (idempotente: no error si no existe).
func DeleteSubscription(d *db.DB, endpoint string) error {
	_, err := d.Exec("DELETE FROM push_subscriptions WHERE endpoint = ?", endpoint)
	return err
}

// ListSubscriptions devuelve todas las suscripciones registradas.
func ListSubscriptions(d *db.DB) ([]Subscription, error) {
	rows, err := d.Query("SELECT endpoint, keys_auth, keys_p256dh, user_agent, created_at, user_id, language FROM push_subscriptions")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Subscription{}
	for rows.Next() {
		var s Subscription
		var ua sql.NullString
		var userID sql.NullInt64
		var language sql.NullString
		if err := rows.Scan(&s.Endpoint, &s.Auth, &s.P256dh, &ua, &s.CreatedAt, &userID, &language); err != nil {
			return nil, err
		}
		s.UserAgent = ua.String
		if userID.Valid {
			s.UserID = userID.Int64
		}
		if language.Valid {
			s.Language = language.String
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
