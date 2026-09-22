// Package telemetry: aviso diario ANÓNIMO de instancia activa (#822).
//
// Manda, como mucho una vez al día, un GET a un endpoint del proyecto con un
// identificador aleatorio de instalación (generado en local y guardado en kv),
// la versión y el os/arch. No envía ni IPs, ni MACs, ni hostnames, ni datos de
// red, y no guarda nada fuera de su propia instalación. Es fail-silent (un
// fallo no afecta a la app) y se desactiva con NETPULSE_TELEMETRY=0.
package telemetry

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

const (
	defaultURL  = "https://netpulse.cloudless.club/instances"
	kvInstallID = "telemetry.install_id"
	kvLastSent  = "telemetry.last_sent"

	// minGap: no reenviar antes de 23 h (el objetivo es ~1/día; el margen
	// evita reenvíos por reinicios con reloj ligeramente distinto).
	minGap     = 23 * time.Hour
	checkEvery = time.Hour
	maxJitter  = 3 * time.Minute
)

// Agent envía el ping anónimo diario.
type Agent struct {
	db      *db.DB
	url     string
	version string
	enabled bool

	client *http.Client
	now    func() time.Time
	newID  func() (string, error)
}

// New construye el agente desde el entorno. Queda desactivado si DEMO_MODE=1
// (una demo no es una instancia real) o si NETPULSE_TELEMETRY es 0/false/off/no.
// NETPULSE_TELEMETRY_URL permite cambiar el endpoint (por defecto, el del proyecto).
func New(d *db.DB, version string, demo bool) *Agent {
	a := &Agent{
		db:      d,
		url:     defaultURL,
		version: version,
		enabled: !demo,
		client:  &http.Client{Timeout: 10 * time.Second},
		now:     time.Now,
		newID:   randomID,
	}
	if v := strings.TrimSpace(os.Getenv("NETPULSE_TELEMETRY_URL")); v != "" {
		a.url = strings.TrimRight(v, "/")
	}
	if v, ok := os.LookupEnv("NETPULSE_TELEMETRY"); ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "0", "false", "off", "no":
			a.enabled = false
		}
	}
	return a
}

// Enabled indica si el aviso está activo.
func (a *Agent) Enabled() bool { return a.enabled }

func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// installID devuelve el id de instalación, creándolo (una vez) si no existe.
func (a *Agent) installID() (string, error) {
	var v string
	err := a.db.QueryRow("SELECT value FROM kv WHERE key = ?", kvInstallID).Scan(&v)
	if err == nil && v != "" {
		return v, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return "", err
	}
	id, idErr := a.newID()
	if idErr != nil {
		return "", idErr
	}
	if _, err := a.setKV(kvInstallID, id); err != nil {
		return "", err
	}
	return id, nil
}

func (a *Agent) setKV(key, value string) (sql.Result, error) {
	return a.db.Exec(
		"INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
		key, value)
}

func (a *Agent) lastSent() time.Time {
	var v string
	if err := a.db.QueryRow("SELECT value FROM kv WHERE key = ?", kvLastSent).Scan(&v); err != nil {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}
	}
	return t
}

// Due indica si toca enviar: nunca se envió o hace más de minGap.
func (a *Agent) Due() bool {
	last := a.lastSent()
	if last.IsZero() {
		return true
	}
	return a.now().Sub(last) >= minGap
}

// pingURL construye la URL del ping. Solo lleva id, versión y os/arch.
func (a *Agent) pingURL(id string) string {
	q := url.Values{}
	q.Set("v", a.version)
	q.Set("os", runtime.GOOS+"-"+runtime.GOARCH)
	return a.url + "/" + url.PathEscape(id) + "?" + q.Encode()
}

// SendOnce envía el ping si está activo y toca, y persiste last_sent. Nunca
// devuelve error al llamante en el bucle: es fail-silent por diseño.
func (a *Agent) SendOnce(ctx context.Context) error {
	if !a.enabled || !a.Due() {
		return nil
	}
	id, err := a.installID()
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.pingURL(id), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "netpulse-instance")
	res, err := a.client.Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode >= 400 {
		return fmt.Errorf("telemetry: HTTP %d", res.StatusCode)
	}
	_, _ = a.setKV(kvLastSent, a.now().UTC().Format(time.RFC3339))
	return nil
}

// Start lanza el bucle en segundo plano (jitter inicial + chequeo horario).
// No hace nada si el aviso está desactivado.
func (a *Agent) Start(ctx context.Context) {
	if !a.enabled {
		return
	}
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(jitter()):
		}
		_ = a.SendOnce(ctx)
		t := time.NewTicker(checkEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				_ = a.SendOnce(ctx)
			}
		}
	}()
}

// jitter reparte los envíos para no sincronizar todas las instancias.
func jitter() time.Duration {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return 0
	}
	ms := int64(b[0])<<24 | int64(b[1])<<16 | int64(b[2])<<8 | int64(b[3])
	return time.Duration(ms%int64(maxJitter/time.Millisecond)) * time.Millisecond
}
