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
	_, err = d.Exec(`INSERT INTO push_subscriptions (endpoint, keys_auth, keys_p256dh, user_agent, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(endpoint) DO UPDATE SET keys_auth=excluded.keys_auth,
			keys_p256dh=excluded.keys_p256dh, user_agent=excluded.user_agent`,
		s.Endpoint, s.Auth, s.P256dh, s.UserAgent, s.CreatedAt)
	return created, err
}

// DeleteSubscription borra por endpoint (idempotente: no error si no existe).
func DeleteSubscription(d *db.DB, endpoint string) error {
	_, err := d.Exec("DELETE FROM push_subscriptions WHERE endpoint = ?", endpoint)
	return err
}

// ListSubscriptions devuelve todas las suscripciones registradas.
func ListSubscriptions(d *db.DB) ([]Subscription, error) {
	rows, err := d.Query("SELECT endpoint, keys_auth, keys_p256dh, user_agent, created_at FROM push_subscriptions")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Subscription{}
	for rows.Next() {
		var s Subscription
		var ua sql.NullString
		if err := rows.Scan(&s.Endpoint, &s.Auth, &s.P256dh, &ua, &s.CreatedAt); err != nil {
			return nil, err
		}
		s.UserAgent = ua.String
		out = append(out, s)
	}
	return out, rows.Err()
}
