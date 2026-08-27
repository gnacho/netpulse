// upgrade.go - Fase 6.3 (issue #243): self-update del agente usando el binario
// que ya sirve el servidor (GET /api/agents/{slug}/binary).
//
//	POST /api/agents/{slug}/upgrade          - admin: envía el comando "upgrade"
//	                                          al agente vía SSE para que descargue
//	                                          el binario embebido, lo intercambie
//	                                          y reinicie su servicio procd.
//	POST /api/agents/{slug}/upgrade-progress - el agente reporta pasos
//	                                          intermedios (downloading con
//	                                          porcentaje, swapping, restarting).
//	                                          Auth por token de agente (#284).
//	POST /api/agents/{slug}/upgrade-result   - el agente reporta el resultado
//	                                          (auth por token de agente).
//	POST /api/agents/upgrade-all             - admin (#251): envía "upgrade" a
//	                                          todos los agentes con update
//	                                          disponible y devuelve el resultado
//	                                          por slug.
package httpapi

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/agentbin"
)

// Pasos del progreso de upgrade (los intermedios los envía el agente; los
// terminales los fija el server a partir de upgrade-result).
const (
	upgradeStepRequested   = "requested"   // comando enviado por SSE
	upgradeStepDownloading = "downloading" // descargando el binario (con pct)
	upgradeStepVerifying   = "verifying"   // comprobando sha256 de la descarga
	upgradeStepSwapping    = "swapping"    // intercambio atómico del binario
	upgradeStepRestarting  = "restarting"  // swap ok, reinicio del servicio
	upgradeStepFailed      = "failed"      // upgrade-result con error
	upgradeStepQueued      = "queued"      // en cola hasta que el agente conecte
)

// upgradeStateTTL: cuánto se expone un paso sin actividad antes de dejar de
// incluirlo en GET /api/agents. Cubre el ciclo completo (descarga + swap +
// reinicio + primer push) con margen amplio. El estado "queued" vive más:
// el agente puede tardar minutos en re-conectar su stream (backoff SSE).
const (
	upgradeStateTTL  = 3 * time.Minute
	upgradeQueuedTTL = 30 * time.Minute
	upgradeHistoryCap = 16
)

// upgradeStepEntry es un paso de la historia del upgrade (timeline de la UI).
type upgradeStepEntry struct {
	Step string
	Pct  int
	Ts   time.Time
}

// upgradeState es el último paso conocido del self-update de un agente, con
// la historia de pasos recorridos (para la timeline aunque vayan rápido).
type upgradeState struct {
	Step    string
	Pct     int    // 0-100, solo tiene sentido en "downloading"
	Error   string // solo en "failed"
	Ts      time.Time
	History []upgradeStepEntry
}

// upgradeTracker mantiene en memoria el último paso por slug (no se persiste:
// es estado efímero de una operación en marcha). El server lo alimenta desde
// los comandos upgrade/upgrade-all y los reportes del agente, y lo expone en
// GET /api/agents para que la UI muestre progreso en vivo (#284). Además
// guarda los slugs con upgrade ENCOLA (agente sin stream SSE): se envían
// cuando el agente vuelve a conectar (flush on-connect).
type upgradeTracker struct {
	mu      sync.Mutex
	states  map[string]upgradeState
	pending map[string]bool
	now     func() time.Time // inyectable en tests
}

func newUpgradeTracker() *upgradeTracker {
	return &upgradeTracker{states: map[string]upgradeState{}, pending: map[string]bool{}, now: time.Now}
}

// set registra un paso del slug y lo añade a la historia (mismo paso
// consecutivo → refresca pct/ts del último entry en vez de duplicar).
func (u *upgradeTracker) set(slug, step string, pct int, errMsg string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.setLocked(slug, step, pct, errMsg)
}

func (u *upgradeTracker) setLocked(slug, step string, pct int, errMsg string) {
	st := u.states[slug]
	ts := u.now()
	if n := len(st.History); n > 0 && st.History[n-1].Step == step {
		st.History[n-1].Pct = pct
		st.History[n-1].Ts = ts
	} else {
		st.History = append(st.History, upgradeStepEntry{Step: step, Pct: pct, Ts: ts})
		if len(st.History) > upgradeHistoryCap {
			st.History = st.History[len(st.History)-upgradeHistoryCap:]
		}
	}
	st.Step, st.Pct, st.Error, st.Ts = step, pct, errMsg, ts
	u.states[slug] = st
	if step != upgradeStepQueued {
		delete(u.pending, slug)
	}
}

// queue marca un upgrade como pendiente para cuando el agente reconecte.
func (u *upgradeTracker) queue(slug string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.pending[slug] = true
	u.setLocked(slug, upgradeStepQueued, 0, "")
}

// takeQueued devuelve true (una sola vez) si el slug tenía upgrade en cola.
func (u *upgradeTracker) takeQueued(slug string) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.pending[slug] {
		return false
	}
	delete(u.pending, slug)
	return true
}

// snapshot devuelve el paso del slug si no ha caducado.
func (u *upgradeTracker) snapshot(slug string) (upgradeState, bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	st, ok := u.states[slug]
	if !ok {
		return upgradeState{}, false
	}
	ttl := upgradeStateTTL
	if st.Step == upgradeStepQueued {
		ttl = upgradeQueuedTTL
	}
	if u.now().Sub(st.Ts) > ttl {
		return upgradeState{}, false
	}
	return st, true
}

// upgradeResult es el cuerpo que el agente envía a upgrade-result tras
// intentar el auto-upgrade.
type upgradeResult struct {
	Slug  string `json:"slug"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// upgradeProgress es el cuerpo que el agente envía a upgrade-progress en cada
// paso intermedio del self-update (#284).
type upgradeProgress struct {
	Slug string `json:"slug"`
	Step string `json:"step"`
	Pct  int    `json:"pct,omitempty"`
}

// handleAgentUpgrade (admin): ordena a un agente conectado que se actualice
// descargando el binario embebido del propio servidor.
//
//	Códigos: 404 slug desconocido (sin token), 409 agente no conectado por SSE,
//	         202 comando enviado, 503 sin AgentHub.
func (s *server) handleAgentUpgrade(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if !agentSlugRe.MatchString(slug) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	if s.agentHub == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "SSE agentHub no configurado")
		return
	}
	// 404 si el slug no tiene token registrado (agente desconocido).
	if !s.agentTokenExists(slug) {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	// El agente conoce su propia arquitectura (runtime.GOARCH); no hace falta
	// pasársela. Data vacía {}. Si no está conectado por SSE, el upgrade se
	// ENCOLA y se enviará en cuanto vuelva a conectar (#284).
	status := s.sendOrQueueUpgrade(slug)
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "status": status})
}

// sendOrQueueUpgrade envía el comando upgrade por SSE; si el agente no está
// conectado, lo encola para el flush on-connect del hub. Devuelve el estado.
func (s *server) sendOrQueueUpgrade(slug string) string {
	if s.agentHub.Send(slug, "upgrade", map[string]any{}) {
		// Paso inicial del progreso en vivo (#284): el comando salió.
		s.upgrades.set(slug, upgradeStepRequested, 0, "")
		return "sent"
	}
	s.upgrades.queue(slug)
	return "queued"
}

// FlushQueuedUpgrade envía el upgrade pendiente de un agente que acaba de
// conectar su stream SSE (#284). Lo invoca el hub on-connect; si el envío
// vuelve a fallar (carrera), re-encola para el próximo reintento.
func (s *server) FlushQueuedUpgrade(slug string) {
	if s.agentHub == nil {
		return
	}
	if !s.agentTokenExists(slug) || !s.upgrades.takeQueued(slug) {
		return
	}
	if s.agentHub.Send(slug, "upgrade", map[string]any{}) {
		s.upgrades.set(slug, upgradeStepRequested, 0, "")
	} else {
		s.upgrades.queue(slug)
	}
}

// handleAgentUpgradeProgress: el agente reporta un paso intermedio del
// self-update (downloading/swapping/restarting). Auth por token de agente
// (Bearer), igual que upgrade-result (#284).
func (s *server) handleAgentUpgradeProgress(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	token := bearerToken(r)
	if !s.checkAgentToken(slug, token) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body upgradeProgress
	if st := readJSONBody(w, r, &body); st != 0 {
		writeBodyError(w, st, "invalid_body", "")
		return
	}
	switch body.Step {
	case upgradeStepDownloading, upgradeStepVerifying, upgradeStepSwapping, upgradeStepRestarting:
	default:
		writeError(w, http.StatusBadRequest, "invalid_body",
			"step debe ser downloading, verifying, swapping o restarting")
		return
	}
	pct := body.Pct
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	s.upgrades.set(slug, body.Step, pct, "")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleAgentUpgradeResult: el agente reporta el resultado del auto-upgrade.
// Auth por token de agente (Bearer), igual que binary/stream/apply-result.
func (s *server) handleAgentUpgradeResult(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	token := bearerToken(r)
	if !s.checkAgentToken(slug, token) {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body upgradeResult
	if st := readJSONBody(w, r, &body); st != 0 {
		writeBodyError(w, st, "invalid_body", "")
		return
	}
	if body.OK {
		log.Printf("[netpulse] upgrade: agente %s actualizado correctamente", slug)
		// El agente reporta el ok ANTES de reiniciarse: el paso visible pasa
		// a ser "restarting" hasta que el binario nuevo empuje su versión.
		s.upgrades.set(slug, upgradeStepRestarting, 0, "")
	} else {
		log.Printf("[netpulse] upgrade: agente %s falló: %s", slug, body.Error)
		s.upgrades.set(slug, upgradeStepFailed, 0, body.Error)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// agentTokenExists informa de si hay un token registrado para el slug
// (independientemente de si el agente ha empujado o no).
func (s *server) agentTokenExists(slug string) bool {
	if s.db == nil {
		return false
	}
	var value string
	err := s.db.QueryRow("SELECT value FROM kv WHERE key = ?", agentTokenKey(slug)).Scan(&value)
	return err == nil
}

// upgradeAllItem es el resultado por slug de POST /api/agents/upgrade-all.
type upgradeAllItem struct {
	Slug     string `json:"slug"`
	Status   string `json:"status"` // "sent" | "not_connected" | "up_to_date"
	Version  string `json:"version,omitempty"`
	Upgraded bool   `json:"upgraded"`
}

// handleAgentsUpgradeAll (#251): envía "upgrade" a todos los agentes que
// reportan una versión distinta de la embebida (updateAvailable) y están
// conectados por SSE. Devuelve el resultado por slug para que la UI muestre
// cuáles quedaron pendientes.
func (s *server) handleAgentsUpgradeAll(w http.ResponseWriter, r *http.Request) {
	if s.agentHub == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "SSE agentHub no configurado")
		return
	}
	slugs, err := s.registeredAgentSlugs()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "db_error")
		return
	}
	out := []upgradeAllItem{}
	sent := 0
	for _, slug := range slugs {
		item := upgradeAllItem{Slug: slug}
		if s.agents != nil {
			if _, version, _, _, ok := s.agents.Info(slug); ok {
				item.Version = version
				// Solo agentes nativos OpenWrt: los de type managed-switch/
				// external son scrapers que no entienden el comando "upgrade".
				if !s.routerUpgradeable(slug) {
					item.Status = "not_openwrt"
				} else if version != "" && version != agentbin.EmbeddedAgentVersion {
					item.Status = s.sendOrQueueUpgrade(slug)
					item.Upgraded = item.Status == "sent"
					if item.Status == "sent" {
						sent++
					}
				} else {
					item.Status = "up_to_date"
				}
			} else {
				item.Status = "not_connected"
			}
		} else {
			item.Status = "not_connected"
		}
		out = append(out, item)
	}
	queued, external := 0, 0
	for _, it := range out {
		switch it.Status {
		case "queued":
			queued++
		case "not_openwrt":
			external++
		}
	}
	log.Printf("[netpulse] upgrade-all: %d/%d enviados, %d en cola, %d externos", sent, len(out), queued, external)
	msg := fmt.Sprintf("upgrade enviado a %d de %d agentes", sent, len(out))
	if queued > 0 {
		msg += fmt.Sprintf(", %d en cola hasta que conecten", queued)
	}
	if external > 0 {
		msg += fmt.Sprintf(", %d externos sin acción", external)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"agents":  out,
		"sent":    sent,
		"total":   len(out),
		"message": msg,
	})
}

// registeredAgentSlugs devuelve los slugs con token registrado en kv
// (misma fuente que handleAgentsList).
func (s *server) registeredAgentSlugs() ([]string, error) {
	if s.db == nil {
		return nil, nil
	}
	rows, err := s.db.Query("SELECT key FROM kv WHERE key LIKE ? ORDER BY key", agentTokenKeyPrefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var key string
		if rows.Scan(&key) != nil {
			continue
		}
		out = append(out, strings.TrimPrefix(key, agentTokenKeyPrefix))
	}
	return out, nil
}
