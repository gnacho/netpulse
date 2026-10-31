package rintegration

import (
	"database/sql"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Códigos estables de motivo de caída de una integración (issue #1703). La UI
// los traduce a texto; un código desconocido se muestra tal cual.
const (
	ReasonAuthFailed  = "pusher_auth_failed"
	ReasonAPINotFound = "pusher_api_not_found"
	ReasonConnRefused = "pusher_conn_refused"
	ReasonStopped     = "pusher_stopped"
	ReasonError       = "pusher_error"
)

// logTailBytes acota la lectura del log del pusher: los fallos se repiten en
// cada ciclo (el intervalo típico es de 30-60 s), así que el último error de
// poll está siempre en la cola del fichero.
const logTailBytes = 16 * 1024

// LastPollError devuelve el mensaje del último "collection failed" del log del
// pusher de slug (vacío si no hay ninguno o el log no existe). El pusher
// registra cada fallo de poll con slog y reintenta en el ciclo siguiente, así
// que la cola del log es la única fuente del motivo concreto (401, conexión
// rechazada, endpoint ausente...) cuando deja de empujar.
func (m *Manager) LastPollError(slug string) string {
	if m == nil || slug == "" {
		return ""
	}
	data, err := readLastBytes(filepath.Join(m.dir, slug+".log"), logTailBytes)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := lines[i]
		if !strings.Contains(line, "collection failed") {
			continue
		}
		return slogErrValue(line)
	}
	return ""
}

// readLastBytes devuelve como máximo los últimos n bytes de path.
func readLastBytes(path string, n int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() > n {
		if _, err := f.Seek(-n, 2); err != nil {
			return nil, err
		}
	}
	buf := make([]byte, min64(n, st.Size()))
	if _, err := f.Read(buf); err != nil && len(buf) == 0 {
		return nil, err
	}
	return buf, nil
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// slogErrValue extrae el valor de err="..." de una línea de slog en formato
// texto, des-escapando las secuencias que escribe el handler.
func slogErrValue(line string) string {
	i := strings.LastIndex(line, `err="`)
	if i < 0 {
		// Forma sin comillas (valor sin espacios): resto de la línea.
		if j := strings.LastIndex(line, "err="); j >= 0 {
			return strings.TrimSpace(line[j+4:])
		}
		return ""
	}
	rest := line[i+5:]
	var b strings.Builder
	for k := 0; k < len(rest); k++ {
		c := rest[k]
		if c == '\\' && k+1 < len(rest) {
			k++
			switch rest[k] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				b.WriteByte(rest[k])
			}
			continue
		}
		if c == '"' {
			break
		}
		b.WriteByte(c)
	}
	return b.String()
}

// ClassifyPollError mapea el texto de error de un pusher a un código estable
// de motivo. Los patrones cubren los fallos observables de un poll: credenciales
// rechazadas (401/unauthorized), endpoint REST ausente (404: firmware viejo,
// el REST llega en RouterOS 7) y familia de conexión (refused/timeout/route).
// Cualquier otro error no vacío cae al código genérico.
func ClassifyPollError(msg string) string {
	m := strings.ToLower(msg)
	switch {
	case m == "":
		return ""
	case strings.Contains(m, "401"),
		strings.Contains(m, "unauthorized"),
		strings.Contains(m, "invalid user name or password"),
		strings.Contains(m, "bad username"):
		return ReasonAuthFailed
	case strings.Contains(m, "404"), strings.Contains(m, "not found"):
		return ReasonAPINotFound
	case strings.Contains(m, "connection refused"),
		strings.Contains(m, "no route to host"),
		strings.Contains(m, "i/o timeout"),
		strings.Contains(m, "host is down"),
		strings.Contains(m, "connection reset"):
		return ReasonConnRefused
	default:
		return ReasonError
	}
}

// StatusReasons calcula slug -> código de motivo para las integraciones cuya
// unidad no está empujando (issue #1703): la tarjeta del router lo muestra
// junto al estado offline. Se omite una integración deshabilitada (la UI ya la
// pinta "paused") y una con push fresco (los códigos de frescura son los
// mismos que usa la vista de integraciones: 3 intervalos). El estado "stopped"
// es motivo por sí mismo; el resto clasifica el último error registrado
// (LastError de instalación, o el log del pusher para fallos de poll).
func StatusReasons(db *sql.DB, m *Manager) map[string]string {
	if db == nil || m == nil {
		return nil
	}
	rows, err := NewStore(db).List()
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, r := range rows {
		if !r.Enabled {
			continue
		}
		code := ""
		switch r.State {
		case "stopped":
			code = ReasonStopped
		default:
			if freshPush(db, r) {
				continue
			}
			if r.State == "error" && r.LastError != "" {
				code = ClassifyPollError(r.LastError)
			}
			if code == "" || code == ReasonError {
				if msg := m.LastPollError(r.Slug); msg != "" {
					code = ClassifyPollError(msg)
				}
			}
		}
		if code != "" {
			out[r.Slug] = code
		}
	}
	return out
}

// freshPush: la integración empujó dentro de 3 intervalos (misma regla que
// integrationItemOf en httpapi). Lee agent.last_push_ts.<slug> del kv.
func freshPush(db *sql.DB, r Row) bool {
	if r.IntervalS <= 0 {
		return false
	}
	val, ok := kvGet(db, "agent.last_push_ts."+r.Slug)
	if !ok {
		return false
	}
	sec, err := strconv.ParseInt(val, 10, 64)
	if err != nil || sec <= 0 {
		return false
	}
	return time.Now().Unix()-sec <= int64(r.IntervalS)*3
}
