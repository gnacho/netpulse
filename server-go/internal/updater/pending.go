// pending.go — confirmación post-update (issue #161): antes de aplicar se
// persiste un marcador from→to en kv; en el siguiente arranque se compara el
// commit actual. Si cambió → se expone la confirmación (toast "Actualizado a
// <sha>" en el frontend); si no → se limpia en silencio (el apply falló). El
// marcador se borra en ambos casos, así que la confirmación es de una sola vez.
package updater

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// pendingApplyKey es la clave kv del marcador pre-update.
const pendingApplyKey = "update.pending_apply"

// PendingApply es el marcador from→to y, tras un arranque con confirmación,
// el valor expuesto en /api/update/status (campo `pendingApply`). To es el
// SHA que quedó corriendo tras el arranque confirmado.
type PendingApply struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// persistPendingApply guarda el marcador en kv (best-effort; sin BD → no-op).
func (u *Updater) persistPendingApply(from string, to *string) {
	if u.db == nil {
		return
	}
	p := PendingApply{From: from}
	if to != nil {
		p.To = *to
	}
	data, err := json.Marshal(p)
	if err != nil {
		return
	}
	if _, err := u.db.Exec(
		`INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		pendingApplyKey, string(data)); err != nil {
		fmt.Printf("[netpulse] no se pudo persistir el marcador de update: %v\n", err)
	}
}

// clearPendingApply borra el marcador (apply falló o ya confirmado).
func (u *Updater) clearPendingApply() {
	if u.db == nil {
		return
	}
	_, _ = u.db.Exec(`DELETE FROM kv WHERE key = ?`, pendingApplyKey)
}

// readPendingApply lee el marcador del kv (false si no existe o es inválido).
func readPendingApply(db *sql.DB) (PendingApply, bool) {
	var v string
	if err := db.QueryRow("SELECT value FROM kv WHERE key = ?", pendingApplyKey).Scan(&v); err != nil {
		return PendingApply{}, false
	}
	var p PendingApply
	if json.Unmarshal([]byte(v), &p) != nil || p.From == "" {
		return PendingApply{}, false
	}
	return p, true
}

// loadPendingApply se ejecuta al crear el updater (cada arranque): finaliza
// historial interrumpido y procesa el marcador pendiente si lo hay.
func (u *Updater) loadPendingApply() {
	if u.db == nil {
		return
	}
	u.finalizeInterrupted()
	if u.mode != "rolling" {
		// #480: en estable el marcador de éxito vive en el dataDir, que
		// llega DESPUÉS de New (WithDataDir) → la confirmación va allí.
		return
	}
	p, ok := readPendingApply(u.db)
	if !ok {
		return
	}
	defer u.clearPendingApply()
	now := gitShort(u.repoRoot)
	if now == "" || now == p.From {
		return // sin cambio o commit desconocido → silencioso
	}
	u.mu.Lock()
	u.pendingApply = &PendingApply{From: p.From, To: now}
	u.mu.Unlock()
	fmt.Printf("[netpulse] update confirmado: %s → %s\n", p.From, now)
}

// loadStablePending confirma (o descarta en silencio) el apply estable
// pendiente una vez el dataDir está disponible (#480). Idempotente: sin
// marcador en kv no hace nada. El apply estable mata el proceso con el
// restart, así que este es el único sitio donde el éxito se puede registrar:
// exige el marcador de éxito del helper con el objetivo esperado (patrón
// #444) y re-marca el historial que finalizeInterrupted dejó como fallido.
func (u *Updater) loadStablePending() {
	if u.db == nil || u.dataDir == "" {
		return
	}
	p, ok := readPendingApply(u.db)
	if !ok {
		return
	}
	defer u.clearPendingApply()
	now := u.version
	if now == "" || now == "desconocido" || now == p.From {
		return // sin cambio o versión desconocida → silencioso
	}
	marker := readAppliedMarker(u.appliedMarkerPath())
	if marker == "" || (p.To != "" && marker != strings.TrimSpace(p.To)) {
		fmt.Printf("[netpulse] apply estable sin marcador de éxito (marker=%q, target=%q); no se confirma\n", marker, p.To)
		return
	}
	_ = os.Remove(u.appliedMarkerPath())
	u.markInterruptedAsSuccess(strings.TrimSpace(p.To))
	u.mu.Lock()
	u.pendingApply = &PendingApply{From: p.From, To: now}
	u.mu.Unlock()
	fmt.Printf("[netpulse] update confirmado: %s → %s\n", p.From, now)
}

// AckPending confirma y descarta el pendingApply en memoria (una sola vez:
// el frontend lo muestra y llama a POST /api/update/pending-confirm).
func (u *Updater) AckPending() {
	u.mu.Lock()
	u.pendingApply = nil
	u.mu.Unlock()
}
