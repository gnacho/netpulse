// weak_signal.go - umbral configurable de la alerta de señal débil (#904)
// y su ciclo de vida completo (#1452).
//
// Hasta ahora la alerta "Señal débil" se disparaba con -70 dBm hardcodeado
// y el ajuste "Weak signal" de Ajustes solo alimentaba un caption local del
// navegador: cambiarlo no alteraba ni la alerta ni nada observable. El
// umbral vive ahora en kv (alerts.weak_signal_dbm, default -70, clamp
// -90..-50) para que sea server-wide (mismo valor para todos los clientes)
// y pueda alimentar la alerta, la app móvil y el matrix filter.
//
// #1452: la alerta también se CIERRA sola. La resolución exige que la
// señal se mantenga por encima del umbral + margen de histéresis durante
// N muestras consecutivas (ambos kv, ajustables en "Datos y umbrales").
// Un cliente sin datos (offline o sin señal reportada) NO cuenta como
// recuperado: desaparecer es el caso "device offline", que tiene su
// propia alerta. El itinerancia entre APs no abre ni cierra nada por sí
// sola: se juzga la señal del cliente, no el AP al que cuelga.
package alerts

import (
	"database/sql"
	"fmt"
	"strconv"
)

const KeyWeakSignalDbm = "alerts.weak_signal_dbm"

const KeyWeakSignalHysteresisDbm = "alerts.weak_signal_hysteresis_dbm"

const KeyWeakSignalResolveSamples = "alerts.weak_signal_resolve_samples"

const (
	DefaultWeakSignalDbm = -70
	minWeakSignalDbm     = -90
	maxWeakSignalDbm     = -50
)

// Margen de histéresis y muestras consecutivas para la auto-resolución
// (#1452). El margen evita el flapping abierto/cerrado rodeando el umbral;
// las muestras evitan cerrar por un único pico bueno. Defaults: 3 dBm y 3
// muestras (el issue pedía 2-3 como default).
const (
	DefaultWeakSignalHysteresisDbm  = 3
	minWeakSignalHysteresisDbm      = 1
	maxWeakSignalHysteresisDbm      = 10
	DefaultWeakSignalResolveSamples = 3
	minWeakSignalResolveSamples     = 1
	maxWeakSignalResolveSamples     = 10
)

// WeakSignalDbm devuelve el umbral efectivo (dBm). Valores corruptos o fuera
// de rango devuelven el default.
func WeakSignalDbm(db *sql.DB) int {
	if db == nil {
		return DefaultWeakSignalDbm
	}
	var raw string
	if err := db.QueryRow("SELECT value FROM kv WHERE key = ?", KeyWeakSignalDbm).Scan(&raw); err != nil {
		return DefaultWeakSignalDbm
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < minWeakSignalDbm || n > maxWeakSignalDbm {
		return DefaultWeakSignalDbm
	}
	return n
}

func weakSignalInt(db *sql.DB, key string, def, min, max int) int {
	if db == nil {
		return def
	}
	var raw string
	if err := db.QueryRow("SELECT value FROM kv WHERE key = ?", key).Scan(&raw); err != nil {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < min || n > max {
		return def
	}
	return n
}

// WeakSignalHysteresisDbm devuelve el margen de histéresis (dBm) por encima
// del umbral a partir del cual una muestra cuenta como "buena".
func WeakSignalHysteresisDbm(db *sql.DB) int {
	return weakSignalInt(db, KeyWeakSignalHysteresisDbm, DefaultWeakSignalHysteresisDbm, minWeakSignalHysteresisDbm, maxWeakSignalHysteresisDbm)
}

// WeakSignalResolveSamples devuelve cuántas muestras buenas consecutivas
// cierran una alerta de señal débil abierta.
func WeakSignalResolveSamples(db *sql.DB) int {
	return weakSignalInt(db, KeyWeakSignalResolveSamples, DefaultWeakSignalResolveSamples, minWeakSignalResolveSamples, maxWeakSignalResolveSamples)
}

// weakRefireGapMs es el guarda mínimo entre episodios: mientras una alerta
// está abierta no hay re-disparo (una alerta = un episodio) y, tras cerrarse,
// LastFire se conserva (#1521): un cliente que oscila entre señal débil y
// buena no vuelve a alertar hasta 24 h después del último disparo. Sin esto,
// cada ciclo débil->bueno->débil abría un episodio nuevo cada pocos minutos y
// el log se llenaba de "Señal débil" del mismo dispositivo.
const weakRefireGapMs = int64(24 * 3600 * 1000)

// WeakSignalState es el estado del ciclo de vida por MAC.
type WeakSignalState struct {
	// OpenID es el ID de la alerta del episodio abierto ("" si ninguno).
	OpenID string
	// OpenTs es el instante (ms) en que se abrió el episodio: alimenta la
	// duración que muestra el evento de cierre.
	OpenTs int64
	// Good es la racha de muestras buenas consecutivas.
	Good int
	// LastFire es el último disparo (ms) para el guarda entre episodios.
	// Se conserva al cerrar el episodio (#1521): el guarda es entre EPISODIOS.
	LastFire int64
}

// WeakSignalStep avanza la máquina de estados de una MAC un ciclo y devuelve
// la acción a ejecutar: fireID != "" pide emitir una alerta nueva con ese
// ID; resolveID != "" pide resolver la alerta con ese ID (episodeStartMs es
// el ts de apertura del episodio cerrado, para la duración del cierre).
// Pure (sin I/O): testeable. signal == nil o online == false significa "sin
// datos" y NO cierra ni abre nada.
func WeakSignalStep(st *WeakSignalState, mac string, signal *int, online bool, thr, hyst, samples int, nowMs int64) (fireID, resolveID string, episodeStartMs int64) {
	if st == nil {
		return "", "", 0
	}
	if !online || signal == nil {
		return "", "", 0
	}
	switch {
	case *signal < thr:
		// Señal mala: la racha de buenas se rompe. Solo se dispara cuando
		// no hay episodio abierto y pasó el guarda entre episodios (LastFire
		// == 0 = nunca disparado).
		st.Good = 0
		if st.OpenID == "" && (st.LastFire == 0 || nowMs-st.LastFire > weakRefireGapMs) {
			st.LastFire = nowMs
			st.OpenTs = nowMs
			st.OpenID = fmt.Sprintf("alert-weak-%s-%d", mac, nowMs)
			return st.OpenID, "", 0
		}
		return "", "", 0
	case *signal >= thr+hyst:
		// Señal establemente buena (umbral + histéresis): cuenta muestra.
		st.Good++
		if st.OpenID != "" && st.Good >= samples {
			id, start := st.OpenID, st.OpenTs
			st.OpenID = ""
			st.OpenTs = 0
			st.Good = 0
			// LastFire NO se toca (#1521): el guarda de 24 h cuenta desde el
			// último disparo REAL, no desde la última resolución. Resetearlo
			// aquí anulaba el guarda para cualquier cliente oscilando.
			return "", id, start
		}
		return "", "", 0
	default:
		// Zona muerta (entre umbral y umbral+histéresis): ni buena ni mala,
		// rompe la racha de buenas sin cerrar nada.
		st.Good = 0
		return "", "", 0
	}
}
