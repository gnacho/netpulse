// engine.go — motor compartido de upgrades de firmware (#494).
//
// El flujo manual (POST /api/firmware-upgrades/{routerId}/upgrade) y el
// scheduler de upgrades programados usan las MISMAS transiciones de estado y
// el MISMO comando SSE "firmware_upgrade" hacia el agente. La validación ASU,
// el backup pre-upgrade y el flash los ejecuta el agente tras recibir ese
// comando; aquí solo se orquesta el inicio (fila requested + envío) y la
// transición a failed si el agente no está disponible.
package firmware

import (
	"errors"

	"github.com/gnacho/netpulse/server-go/internal/sse"
)

// Errores centinela del motor, mapeados por httpapi a los códigos HTTP que el
// flujo manual ya devolvía (no_target 400, upgrade_in_progress 409,
// agent_not_connected 503, insecure_agent_transport 409).
var (
	ErrNoTarget          = errors.New("firmware: no target configurado")
	ErrUpgradeInProgress = errors.New("firmware: upgrade en curso")
	ErrAgentNotConnected = errors.New("firmware: agente no conectado")
	ErrInsecureTransport = errors.New("firmware: el agente no usa HTTPS")
)

// Sender envía comandos al agente vía SSE. Lo implementa *sse.AgentHub.
type Sender interface {
	SendPrivileged(slug, event string, data any) error
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
	id, err := e.store.BeginUpgradeIfIdle(routerID, target.TargetVersion, target.TargetURL, target.Checksum)
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
	if err := e.send.SendPrivileged(routerID, "firmware_upgrade", cmd); err != nil {
		if errors.Is(err, sse.ErrInsecureAgentTransport) {
			_ = e.store.SetStatus(id, "failed", "insecure agent transport", "")
			return ErrInsecureTransport
		}
		_ = e.store.SetStatus(id, "failed", "agent not connected", "")
		return ErrAgentNotConnected
	}
	_ = e.store.SetStatus(id, "requested", "", "")
	return nil
}
