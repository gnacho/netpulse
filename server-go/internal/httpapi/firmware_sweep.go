package httpapi

// firmware_sweep.go - barrido periódico de versiones de firmware (#1777):
// una comprobación por router owut-capable y semana. Compara el Version-to
// que reporta `owut check` con la versión instalada y avisa una sola vez por
// router+versión. El check de ASU puede tardar y fallar por causas externas,
// así que el barrido es tolerante: un fallo no marca la comprobación y se
// reintenta en el siguiente tick. Nunca es urgent: es información, no una
// acción pendiente (alineado con #1776).

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/alerts"
	"github.com/gnacho/netpulse/server-go/internal/firmware"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
)

const (
	// fwSweepTick: frecuencia del loop interno. El check real por router se
	// rate-limita a fwSweepInterval con kv; el tick solo recorre la lista.
	fwSweepTick = time.Hour
	// fwSweepInterval: una comprobación por router y semana (#1777).
	fwSweepInterval = 7 * 24 * time.Hour
)

// Claves kv por router:
//
//	fwsweep.checked.<routerID> = unix de la última comprobación con éxito
//	fwsweep.alerted.<routerID> = última versión anunciada (dedup)
func fwSweepCheckedKey(routerID string) string { return "fwsweep.checked." + routerID }
func fwSweepAlertedKey(routerID string) string { return "fwsweep.alerted." + routerID }

// firmwareSweepLoop corre en background junto al resto de loops (#1777).
func (s *server) firmwareSweepLoop(ctx context.Context, tick time.Duration) {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.firmwareSweepTick()
		}
	}
}

// firmwareSweepTick lanza (en goroutine) la comprobación de los routers cuya
// ventana semanal venció.
func (s *server) firmwareSweepTick() {
	now := time.Now()
	for _, rt := range routerstore.ListRouters(s.db.DB) {
		if !agentUpgradeable(rt.Type) {
			continue
		}
		if s.fwSweepDue(rt.ID, now) {
			go s.firmwareSweepRouter(rt.ID, rt.Host)
		}
	}
}

// fwSweepDue: venció la ventana semanal de este router.
func (s *server) fwSweepDue(routerID string, now time.Time) bool {
	v := strings.TrimSpace(kvGet(s.db.DB, fwSweepCheckedKey(routerID)))
	if v == "" {
		return true
	}
	last, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return true
	}
	return now.Unix()-last >= int64(fwSweepInterval/time.Second)
}

// firmwareSweepRouter: un check de owut + alerta con dedup. Una sola
// ejecución por router a la vez (patrón recInFlight).
func (s *server) firmwareSweepRouter(routerID, host string) {
	s.sweepMu.Lock()
	if s.sweepInFlight[routerID] {
		s.sweepMu.Unlock()
		return
	}
	if s.sweepInFlight == nil {
		s.sweepInFlight = map[string]bool{}
	}
	s.sweepInFlight[routerID] = true
	s.sweepMu.Unlock()
	defer func() {
		s.sweepMu.Lock()
		delete(s.sweepInFlight, routerID)
		s.sweepMu.Unlock()
	}()

	status := firmware.CheckOwut(s.pool, host)
	if status.Error != "" || !status.CheckRan || status.VersionTo == "" {
		// Sin dato fiable: no marcar la comprobación (reintento al
		// siguiente tick) y sin alerta.
		return
	}
	if err := kvSetStr(s.db.DB, fwSweepCheckedKey(routerID), strconv.FormatInt(time.Now().Unix(), 10)); err != nil {
		log.Printf("[firmware] sweep %s: no pude marcar la comprobación: %v", routerID, err)
		return
	}

	installed := s.detectedFirmwareVersion(routerID)
	if installed == "" && s.boardVersionFn != nil {
		installed = s.boardVersionFn(routerID)
	}
	if installed == "" || status.VersionTo == installed {
		return
	}
	if prev := kvGet(s.db.DB, fwSweepAlertedKey(routerID)); prev == status.VersionTo {
		return // ya avisada: una alerta por router+versión (#1777)
	}
	if err := kvSetStr(s.db.DB, fwSweepAlertedKey(routerID), status.VersionTo); err != nil {
		log.Printf("[firmware] sweep %s: no pude guardar el dedup: %v", routerID, err)
	}
	s.emitFirmwareAvailable(routerID, installed, status.VersionTo)
}

// emitFirmwareAvailable avisa de una nueva versión disponible (#1777).
func (s *server) emitFirmwareAvailable(routerID, installed, available string) {
	if s.alertEmitter == nil {
		return
	}
	name := routerID
	if rt := s.routerName(routerID); rt != "" {
		name = rt
	}
	ev := alerts.AlertEvent{
		ID:       fmt.Sprintf("alert-firmware-available-%s-%d", routerID, time.Now().UnixMilli()),
		Category: alerts.CatSystem,
		Urgent:   false,
		Severity: "info",
		RouterID: routerID,
		Time:     "just now",
		Ts:       time.Now().Unix(),
		Type:     "firmware-available",
		Title:    "Nuevo firmware disponible",
		Description: fmt.Sprintf("%s: %s -> %s (detectado por owut check)", name, installed, available),
		Vars:     map[string]string{"router": name, "from": installed, "to": available},
	}
	s.alertEmitter.Emit(ev)
}
