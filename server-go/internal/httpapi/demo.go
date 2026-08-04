// demo.go — POST /api/demo/enable (issue #4): activa (o desactiva) el modo
// demo desde la UI sin reinstalar. El instalador ya no pregunta por demo
// (default BD limpia); este endpoint es la vía de activarla después.
//
// Body opcional: {"enable": true|false} (por defecto true).
//
// Mecanismo: reescribe DEMO_MODE en el .env del servidor y toca
// <dataDir>/.restart-me; una unidad systemd.path (netpulse-go-restart.path)
// vigila ese flag y reinicia el servicio como root. Al arrancar, main.go
// vuelve a leer el .env y construye el adaptador demo o live. El flag
// .restart-me es el mismo que usa el updater (deploy/update.sh), así que no
// se introduce ningún mecanismo nuevo.
package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gnacho/netpulse/server-go/internal/auth"
)

// setDemoModeInEnv reescribe el .env del serverRoot cambiando (o añadiendo)
// la clave DEMO_MODE. Conserva las demás líneas.
func setDemoModeInEnv(envPath string, on bool) error {
	raw, err := os.ReadFile(envPath)
	if err != nil {
		raw = []byte{} // no existe: lo creamos
	}
	value := "0"
	if on {
		value = "1"
	}
	lines := strings.Split(string(raw), "\n")
	found := false
	for i, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "DEMO_MODE=") {
			lines[i] = "DEMO_MODE=" + value
			found = true
			break
		}
	}
	if !found {
		lines = append(lines, "DEMO_MODE="+value)
	}
	out := strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
	return os.WriteFile(envPath, []byte(out), 0o600)
}

// handleDemoEnable cambia el modo demo (solo admin). 200 {ok, restarting} si
// se programó el reinicio; 409 si ya estábamos en el estado pedido.
func (s *server) handleDemoEnable(w http.ResponseWriter, r *http.Request) {
	on := true
	if body, err := io.ReadAll(io.LimitReader(r.Body, 1024)); err == nil && len(body) > 0 {
		var req struct {
			Enable *bool `json:"enable"`
		}
		if json.Unmarshal(body, &req) == nil && req.Enable != nil {
			on = *req.Enable
		}
	}
	if s.cfg.DemoMode == on {
		writeError(w, http.StatusConflict, map[bool]string{true: "already_demo", false: "already_live"}[on])
		return
	}
	envPath := filepath.Join(s.cfg.ServerRoot, ".env")
	if err := setDemoModeInEnv(envPath, on); err != nil {
		writeError(w, http.StatusInternalServerError, "env_write_failed", err.Error())
		return
	}
	// Flag de reinicio que vigila netpulse-go-restart.path (systemd).
	flag := filepath.Join(s.cfg.DataDir, ".restart-me")
	_ = os.WriteFile(flag, []byte("demo-switch\n"), 0o600)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"restarting": true,
		"mode":       map[bool]string{true: "demo", false: "live"}[on],
	})
}

// registerDemoRoutes registra POST /api/demo/enable tras RequireAdmin.
func (s *server) registerDemoRoutes(mux *http.ServeMux) {
	mux.Handle("POST /api/demo/enable", auth.RequireAdmin(http.HandlerFunc(s.handleDemoEnable)))
}
