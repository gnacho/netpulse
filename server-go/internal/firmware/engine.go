// engine.go — motor compartido de upgrades de firmware (#494).
//
// El flujo manual (POST /api/firmware-upgrades/{routerId}/upgrade) y el
// scheduler de upgrades programados usan las MISMAS transiciones de estado y
// el MISMO comando SSE "firmware_upgrade" hacia el agente. La validación ASU,
// el backup pre-upgrade y el flash los ejecuta el agente tras recibir ese
// comando; aquí solo se orquesta el inicio (fila requested + envío) y la
// transición a failed si el agente no está disponible.
package firmware

import "errors"

// Errores centinela del motor, mapeados por httpapi a los códigos HTTP que el
// flujo manual ya devolvía (no_target 400, upgrade_in_progress 409,
// agent_not_connected 503).
var (
	ErrNoTarget           = errors.New("firmware: no target configurado")
	ErrUpgradeInProgress  = errors.New("firmware: upgrade en curso")
	ErrAgentNotConnected  = errors.New("firmware: agente no conectado")
)

// Sender envía comandos al agente vía SSE. Lo implementa *sse.AgentHub.
type Sender interface {
	Send(slug, event string, data any) bool
}

// Engine ejecuta upgrades con el motor compartido (store + sender).
type Engine struct {
	store *Store
	send  Sender
}

// NewEngine construye el motor sobre el store y el transport al agente.
func NewEngine(store *Store, send Sender) *Engine {
	return &Engine{store: store, send: send}
}

// StartUpgrade inicia un upgrade manual: valida el target, comprueba que no
// haya otro upgrade en curso, crea la fila 'requested' y envía el comando al
// agente. Devuelve el id del upgrade o un error centinela.
func (e *Engine) StartUpgrade(routerID string) (int64, error) {
	target, err := e.store.GetTarget(routerID)
	if err != nil {
		return 0, err
	}
	if target == nil || target.TargetURL == "" {
		return 0, ErrNoTarget
	}
	if up, _ := e.store.LatestUpgrade(routerID); up != nil && up.Status != "done" && up.Status != "failed" {
		return 0, ErrUpgradeInProgress
	}
	id, err := e.store.BeginUpgrade(routerID, target.TargetVersion, target.TargetURL, target.Checksum)
	if err != nil {
		return 0, err
	}
	if err := e.dispatch(id, routerID, target.TargetURL, target.Checksum); err != nil {
		return id, err
	}
	return id, nil
}

// LaunchScheduled lanza una programación vencida (#494): transiciona la fila
// 'scheduled' a 'requested' y envía el comando. No-op si la fila ya fue
// lanzada o cancelada (StartScheduled devuelve false).
func (e *Engine) LaunchScheduled(u Upgrade) error {
	ok, err := e.store.StartScheduled(u.ID)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	return e.dispatch(u.ID, u.RouterID, u.TargetURL, u.Checksum)
}

// dispatch envía el comando firmware_upgrade y fija el estado inicial
// (requested si salió, failed si no). Secuencia idéntica al flujo manual.
func (e *Engine) dispatch(id int64, routerID, targetURL, checksum string) error {
	if e.send == nil {
		_ = e.store.SetStatus(id, "failed", "no agent hub", "")
		return ErrAgentNotConnected
	}
	cmd := map[string]any{
		"upgradeId":  id,
		"targetUrl":  targetURL,
		"checksum":   checksum,
		"keepConfig": true,
	}
	if !e.send.Send(routerID, "firmware_upgrade", cmd) {
		_ = e.store.SetStatus(id, "failed", "agent not connected", "")
		return ErrAgentNotConnected
	}
	_ = e.store.SetStatus(id, "requested", "", "")
	return nil
}
