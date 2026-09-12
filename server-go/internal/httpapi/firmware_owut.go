// firmware_owut.go - ciclo owut completo y recurrencia de firmware (#761).
//
// Endpoints: lista de versiones (dropdown), instalación del paquete owut,
// upgrade desatendido (máquina de estados running -> rebooting -> done) y
// programación recurrente (once/weekly/monthly, idempotente). El resultado
// de cada intento (éxito o fallo) se emite como alerta urgent, que llega al
// feed, push, webhook y Telegram con la configuración existente.
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
	"github.com/gnacho/netpulse/server-go/internal/alerts"
	"github.com/gnacho/netpulse/server-go/internal/auth"
	"github.com/gnacho/netpulse/server-go/internal/firmware"
	"github.com/gnacho/netpulse/server-go/internal/routerstore"
)

// Estados no terminales de un upgrade (clásico + owut).
func owutUpgradeActive(status string) bool {
	return status != "" && status != "done" && status != "failed"
}

// Cinturones de plataforma (#761): firmware de fabricante fuera, y sin owut
// instalado no se lanza nada.
var (
	errVendorFirmware   = errors.New("firmware de fabricante (GL.iNet) sin soporte")
	ErrOwutNotInstalled = errors.New("owut no está instalado en el router")
)

// alertEmitter lo cumple *alerts.Engine (mismo patrón que speedtest).
type alertEmitter interface {
	Emit(ev alerts.AlertEvent) bool
}

// platformCacheEntry cachea la plataforma (owut + vendor) por router (1 h):
// el loop de recurrencia y los endpoints la consultan y sondear por SSH en
// cada operación sería ruido innecesario.
type platformCacheEntry struct {
	p  firmware.Platform
	at time.Time
}

const (
	owutDetectCacheTTL = time.Hour
	// owutRebootGrace: margen para que el router reinicie, vuelva el sondeo
	// y el board info reporte la versión nueva.
	owutRebootGrace = 15 * time.Minute
	// owutSSHMinRun: si la sesión SSH muere antes de este umbral, no fue el
	// reinicio del sysupgrade: el comando ni arrancó.
	owutSSHMinRun = 30 * time.Second
	recTickEvery  = time.Minute
)

func (s *server) registerFirmwareOwutRoutes(mux *http.ServeMux) {
	if s.firmware == nil {
		return
	}

	// GET versions: candidatos del dropdown (requiere owut en el router).
	mux.Handle("GET /api/firmware-upgrades/{routerId}/owut-versions", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("routerId")
		host := s.hostOfRouter(id)
		if host == "" {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		if s.pool == nil {
			writeError(w, http.StatusServiceUnavailable, "no_ssh_pool")
			return
		}
		resp := struct {
			Current        string                 `json:"current"`
			OwutAvailable  bool                   `json:"owutAvailable"`
			VendorFirmware string                 `json:"vendorFirmware,omitempty"`
			Versions       []firmware.OwutVersion `json:"versions"`
			Error          string                 `json:"error,omitempty"`
		}{Current: s.detectedFirmwareVersion(id)}
		plat := s.platform(id, host)
		if plat.Vendor != "" {
			// Firmware de fabricante: sus updates los gestiona el vendor.
			resp.VendorFirmware = plat.Vendor
			resp.Versions = []firmware.OwutVersion{}
			writeJSON(w, http.StatusOK, resp)
			return
		}
		if !plat.Owut {
			resp.Versions = []firmware.OwutVersion{}
			writeJSON(w, http.StatusOK, resp)
			return
		}
		resp.OwutAvailable = true
		versions, err := firmware.ListOwutVersions(s.pool, host, resp.Current)
		if err != nil {
			resp.Error = err.Error()
			resp.Versions = []firmware.OwutVersion{}
		} else {
			resp.Versions = versions
		}
		writeJSON(w, http.StatusOK, resp)
	})))

	// POST owut-install: instala el paquete owut con apk u opkg.
	mux.Handle("POST /api/firmware-upgrades/{routerId}/owut-install", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("routerId")
		host := s.hostOfRouter(id)
		if host == "" {
			writeError(w, http.StatusNotFound, "not_found")
			return
		}
		if s.pool == nil {
			writeError(w, http.StatusServiceUnavailable, "no_ssh_pool")
			return
		}
		if s.platform(id, host).Vendor != "" {
			writeError(w, http.StatusUnprocessableEntity, "vendor_firmware",
				"El router lleva firmware del fabricante (GL.iNet): NetPulse no gestiona sus actualizaciones, usa su propio panel.")
			return
		}
		output, err := firmware.InstallOwut(s.pool, host)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "owut_install_failed", err.Error())
			return
		}
		s.invalidatePlatformCache(id)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "output": output})
	})))

	// POST owut-upgrade: lanza el attended upgrade desatendido (ASU).
	mux.Handle("POST /api/firmware-upgrades/{routerId}/owut-upgrade", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("routerId")
		var body struct {
			TargetVersion string   `json:"targetVersion"`
			RemovePkgs    []string `json:"removePackages"`
		}
		if st := readJSONBody(w, r, &body); st != 0 {
			return
		}
		if body.TargetVersion == "" {
			writeError(w, http.StatusBadRequest, "invalid_body", "targetVersion es requerido")
			return
		}
		if host := s.hostOfRouter(id); host != "" && s.platform(id, host).Vendor != "" {
			writeError(w, http.StatusUnprocessableEntity, "vendor_firmware",
				"El router lleva firmware del fabricante (GL.iNet): NetPulse no gestiona sus actualizaciones, usa su propio panel.")
			return
		}
		upgradeID, err := s.startOwutUpgrade(id, body.TargetVersion, "manual", body.RemovePkgs)
		switch {
		case errors.Is(err, firmware.ErrUpgradeInProgress):
			writeError(w, http.StatusConflict, "upgrade_in_progress", "Ya hay un upgrade en curso")
		case errors.Is(err, errVendorFirmware):
			writeError(w, http.StatusUnprocessableEntity, "vendor_firmware",
				"El router lleva firmware del fabricante (GL.iNet): NetPulse no gestiona sus actualizaciones, usa su propio panel.")
		case errors.Is(err, ErrOwutNotInstalled):
			writeError(w, http.StatusPreconditionFailed, "owut_not_installed",
				"owut no está instalado en el router: instálalo primero con el botón Instalar owut.")
		case errors.Is(err, errRouterNotFound):
			writeError(w, http.StatusNotFound, "not_found")
		case err != nil:
			writeError(w, http.StatusInternalServerError, "firmware_error", err.Error())
		default:
			writeJSON(w, http.StatusAccepted, map[string]any{"upgradeId": upgradeID, "engine": "owut"})
		}
	})))

	// GET/PUT/DELETE recurrence: programación once/weekly/monthly.
	mux.Handle("GET /api/firmware-upgrades/{routerId}/recurrence", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("routerId")
		rc := firmware.LoadRecurrence(s.db.DB, id)
		out := map[string]any{"recurrence": rc}
		if rc.Enabled {
			next := firmware.NextRecurrenceAt(rc, time.Now())
			if !next.IsZero() {
				out["nextRunMs"] = next.UnixMilli()
			}
		}
		writeJSON(w, http.StatusOK, out)
	})))

	mux.Handle("PUT /api/firmware-upgrades/{routerId}/recurrence", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("routerId")
		var body firmware.Recurrence
		if st := readJSONBody(w, r, &body); st != 0 {
			return
		}
		prev := firmware.LoadRecurrence(s.db.DB, id)
		body.LastRunMs = prev.LastRunMs
		if err := firmware.SaveRecurrence(s.db.DB, id, body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})))

	mux.Handle("DELETE /api/firmware-upgrades/{routerId}/recurrence", auth.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("routerId")
		firmware.DisableRecurrence(s.db.DB, id)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})))
}

// startOwutUpgrade crea la fila del upgrade y lanza la goroutine que lo
// ejecuta por SSH. El POST responde 202 inmediatamente; el progreso se sigue
// por GET /api/firmware-upgrades (item.upgrade).
func (s *server) startOwutUpgrade(routerID, targetVersion, origin string, removePkgs []string) (int64, error) {
	host := s.hostOfRouter(routerID)
	if host == "" {
		return 0, errRouterNotFound
	}
	if s.pool == nil {
		return 0, errors.New("sin pool SSH")
	}
	// Cinturones de platform (#761): firmware de fabricante fuera, y sin
	// owut instalado no se lanza nada (el comando moriría en 127).
	if plat := s.platform(routerID, host); plat.Vendor != "" {
		return 0, errVendorFirmware
	} else if !plat.Owut {
		return 0, ErrOwutNotInstalled
	}
	if up, _ := s.firmware.LatestUpgrade(routerID); up != nil && owutUpgradeActive(up.Status) {
		return 0, firmware.ErrUpgradeInProgress
	}
	id, err := s.firmware.BeginUpgrade(routerID, targetVersion, "", "")
	if err != nil {
		return 0, err
	}
	_ = s.firmware.SetEngine(id, "owut")
	go s.runOwutUpgrade(id, routerID, host, targetVersion, origin, removePkgs)
	return id, nil
}

// runOwutUpgrade ejecuta `owut upgrade` y decide la transición. La muerte de
// la sesión SSH tras >owutSSHMinRun es el final NORMAL del flash (el sysupgrade
// reinicia el router); el resultado se confirma mirando el board info.
func (s *server) runOwutUpgrade(id int64, routerID, host, target, origin string, removePkgs []string) {
	from := ""
	if s.boardVersionFn != nil {
		from = s.boardVersionFn(routerID)
	}
	cmd := firmware.OwutUpgradeCmd(target, from, removePkgs)
	_ = s.firmware.SetStatus(id, "running", "", "")
	start := time.Now()
	exit, out, err := firmware.RunOwutUpgrade(s.pool, host, cmd)
	dur := time.Since(start)
	switch {
	case err == nil && exit == 0 && strings.Contains(out, "no changes"):
		_ = s.firmware.SetStatus(id, "done", "sin cambios: ya estaba al día", "")
	case (err == nil && exit == 0) || (err != nil && dur >= owutSSHMinRun):
		s.awaitReboot(id, routerID, target, origin, from)
	default:
		msg := owutTail(out, err)
		_ = s.firmware.SetStatus(id, "failed", msg, "")
		s.emitFirmwareResult(routerID, from, target, origin, false, msg)
	}
}

// awaitReboot espera a que el router vuelva del sysupgrade y su board info
// reporte la versión objetivo. Deadline: owutRebootGrace.
func (s *server) awaitReboot(id int64, routerID, target, origin, from string) {
	_ = s.firmware.SetStatus(id, "rebooting", "", "")
	deadline := time.Now().Add(owutRebootGrace)
	for time.Now().Before(deadline) {
		time.Sleep(20 * time.Second)
		if v := s.detectedFirmwareVersion(routerID); v == target && target != "" {
			_ = s.firmware.SetStatus(id, "done", "", "")
			s.emitFirmwareResult(routerID, from, target, origin, true, "")
			return
		}
	}
	msg := "sin confirmación tras el reinicio (¿versión instalada distinta del objetivo?)"
	_ = s.firmware.SetStatus(id, "failed", msg, "")
	s.emitFirmwareResult(routerID, from, target, origin, false, msg)
}

// detectedFirmwareVersion: última versión reportada por el board info.
func (s *server) detectedFirmwareVersion(routerID string) string {
	if s.adapter == nil {
		return ""
	}
	if bi := s.adapter.BoardInfoFor(routerID); bi != nil {
		return bi.Release.Version
	}
	return ""
}

// boardInfoFor expone el board info completo (tests y debug).
func (s *server) boardInfoFor(routerID string) *adapters.BoardInfo {
	if s.adapter == nil {
		return nil
	}
	return s.adapter.BoardInfoFor(routerID)
}

// owutTail recorta la salida de owut para el campo error (últimas líneas).
func owutTail(out string, err error) string {
	msg := strings.TrimSpace(out)
	if err != nil {
		if msg != "" {
			msg += "; "
		}
		msg += err.Error()
	}
	if len(msg) > 400 {
		msg = "..." + msg[len(msg)-400:]
	}
	return msg
}

// emitFirmwareResult notifica el resultado de un intento de upgrade (éxito o
// fallo, manual o programado). Urgent para cruzar el Notifier (Telegram,
// push, webhook); severity info/warn según resultado.
func (s *server) emitFirmwareResult(routerID, from, to, origin string, ok bool, detail string) {
	if s.alertEmitter == nil {
		return
	}
	name := routerID
	if rt := s.routerName(routerID); rt != "" {
		name = rt
	}
	originTxt := "manual"
	if origin == "scheduled" {
		originTxt = "programado"
	}
	ev := alerts.AlertEvent{
		ID:       fmt.Sprintf("alert-firmware-upgrade-%s-%d", routerID, time.Now().UnixMilli()),
		Category: alerts.CatSystem,
		Urgent:   true,
		RouterID: routerID,
		Time:     "ahora mismo",
		Ts:       time.Now().Unix(),
		Vars:     map[string]string{"router": name, "from": from, "to": to, "origin": originTxt},
	}
	if ok {
		ev.Severity = "info"
		ev.Type = "firmware-upgrade-done"
		ev.Title = "Firmware actualizado"
		ev.Description = fmt.Sprintf("%s: %s -> %s (owut, %s)", name, from, to, originTxt)
	} else {
		ev.Severity = "warn"
		ev.Type = "firmware-upgrade-failed"
		ev.Title = "Actualización de firmware fallida"
		ev.Description = fmt.Sprintf("%s: objetivo %s (owut, %s): %s", name, to, originTxt, detail)
		ev.Vars["detail"] = detail
	}
	s.alertEmitter.Emit(ev)
}

// emitFirmwareConfigAlert avisa de una recurrencia desactivada por config
// inválida (sin target guardado, sin motor disponible).
func (s *server) emitFirmwareConfigAlert(routerID, reason string) {
	if s.alertEmitter == nil {
		return
	}
	name := routerID
	if rt := s.routerName(routerID); rt != "" {
		name = rt
	}
	s.alertEmitter.Emit(alerts.AlertEvent{
		ID:          fmt.Sprintf("alert-firmware-recurrence-%s-%d", routerID, time.Now().UnixMilli()),
		Category:    alerts.CatSystem,
		Urgent:      true,
		Severity:    "warn",
		RouterID:    routerID,
		Title:       "Programación de firmware desactivada",
		Description: fmt.Sprintf("%s: %s", name, reason),
		Time:        "ahora mismo",
		Ts:          time.Now().Unix(),
	})
}

// routerName resuelve el nombre visible del router (fallback: su ID).
func (s *server) routerName(routerID string) string {
	if s.db == nil {
		return ""
	}
	for _, rt := range routerstore.ListRouters(s.db.DB) {
		if rt.ID == routerID {
			return rt.Name
		}
	}
	return ""
}

// --- Detección owut cacheada ---

func (s *server) platform(routerID, host string) firmware.Platform {
	s.owutMu.Lock()
	defer s.owutMu.Unlock()
	if s.owutCache == nil {
		s.owutCache = map[string]platformCacheEntry{}
	}
	if e, ok := s.owutCache[routerID]; ok && time.Since(e.at) < owutDetectCacheTTL {
		return e.p
	}
	p := firmware.DetectPlatform(s.pool, host)
	s.owutCache[routerID] = platformCacheEntry{p: p, at: time.Now()}
	return p
}

func (s *server) invalidatePlatformCache(routerID string) {
	s.owutMu.Lock()
	defer s.owutMu.Unlock()
	delete(s.owutCache, routerID)
}

// --- Loop de recurrencia (#761) ---

// firmwareRecurrenceLoop: tick corto; el vencimiento se deriva del last_run
// persistido (reboot-safe). Solo en live (en demo no hay datos que actualizar).
func (s *server) firmwareRecurrenceLoop(ctx context.Context, tickEvery time.Duration) {
	ticker := time.NewTicker(tickEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.recurrenceTick()
		}
	}
}

func (s *server) recurrenceTick() {
	if s.db == nil || s.firmware == nil {
		return
	}
	now := time.Now()
	for _, rt := range routerstore.ListRouters(s.db.DB) {
		if !agentUpgradeable(rt.Type) {
			continue
		}
		rc := firmware.LoadRecurrence(s.db.DB, rt.ID)
		if !firmware.RecurrenceDue(rc, now) {
			continue
		}
		s.recMu.Lock()
		if s.recInFlight[rt.ID] {
			s.recMu.Unlock()
			continue
		}
		if s.recInFlight == nil {
			s.recInFlight = map[string]bool{}
		}
		s.recInFlight[rt.ID] = true
		s.recMu.Unlock()
		go func(id string) {
			defer func() {
				s.recMu.Lock()
				delete(s.recInFlight, id)
				s.recMu.Unlock()
			}()
			s.runRecurrenceShot(id)
		}(rt.ID)
	}
}

// runRecurrenceShot ejecuta un disparo vencido. Idempotente por contrato: si
// la versión instalada ya coincide con el target, no-op silencioso; el
// resultado real (éxito/fallo) se notifica desde la máquina de estados owut.
func (s *server) runRecurrenceShot(routerID string) {
	target, err := s.firmware.GetTarget(routerID)
	if err != nil || target == nil || target.TargetVersion == "" {
		firmware.DisableRecurrence(s.db.DB, routerID)
		s.emitFirmwareConfigAlert(routerID, "sin target de firmware guardado")
		return
	}
	current := ""
	if s.boardVersionFn != nil {
		current = s.boardVersionFn(routerID)
	}
	if current != "" && current == target.TargetVersion {
		// Al día: el disparo consume el slot sin flashear nada (#761).
		firmware.SetRecurrenceLastRun(s.db.DB, routerID, time.Now().UnixMilli())
		return
	}
	firmware.SetRecurrenceLastRun(s.db.DB, routerID, time.Now().UnixMilli())
	host := s.hostOfRouter(routerID)
	if host != "" && s.pool != nil {
		if plat := s.platform(routerID, host); plat.Vendor != "" {
			firmware.DisableRecurrence(s.db.DB, routerID)
			s.emitFirmwareConfigAlert(routerID, "router con firmware del fabricante (GL.iNet): sus actualizaciones las gestiona su propio panel")
			return
		}
	}
	if host != "" && s.pool != nil && s.platform(routerID, host).Owut {
		if _, err := s.startOwutUpgrade(routerID, target.TargetVersion, "scheduled", nil); err != nil {
			s.emitFirmwareResult(routerID, current, target.TargetVersion, "scheduled", false, err.Error())
		}
		return
	}
	// Sin owut: motor clásico si hay imagen URL+checksum configurada.
	if target.TargetURL != "" && s.firmwareEngine != nil {
		if _, err := s.firmwareEngine.StartUpgrade(routerID); err != nil {
			s.emitFirmwareResult(routerID, current, target.TargetVersion, "scheduled", false, err.Error())
		}
		return
	}
	firmware.DisableRecurrence(s.db.DB, routerID)
	s.emitFirmwareConfigAlert(routerID, "router sin owut y sin URL de imagen para el motor clásico")
}
