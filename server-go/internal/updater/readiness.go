// readiness.go — pre-flight checks del apply (issue #160): espacio en disco
// para descarga+backup, working tree de git limpio, red hacia GitHub y sin
// update concurrente. Se expone en /api/update/status (campo `readiness`) y
// el frontend bloquea el botón de aplicar si algo falla.
//
// El cómputo se cachea readinessTTL: el network check hace una petición HTTP
// y no debe bloquear el polling de status (2,5 s durante el apply).
package updater

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

const (
	// readinessTTL: cuánto tiempo se sirve el readiness cacheado (el network
	// check toca red; no recalcular en cada poll de status).
	readinessTTL = 30 * time.Second
	// readinessNetTimeout: tope para el check de red hacia GitHub.
	readinessNetTimeout = 5 * time.Second
)

// minDiskFreeBytes es el espacio libre mínimo exigido en repoRoot (descarga
// del binario de CI ~40 MB + backup del binario actual + margen de build).
// Var de paquete para poder bajarlo en tests.
var minDiskFreeBytes int64 = 500 * 1024 * 1024

// CheckResult es el resultado de un check individual.
type CheckResult struct {
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// Readiness agrupa los pre-flight checks del apply (issue #160).
type Readiness struct {
	Disk       CheckResult `json:"disk"`
	Git        CheckResult `json:"git"`
	Network    CheckResult `json:"network"`
	Concurrent CheckResult `json:"concurrent"`
	Ready      bool        `json:"ready"`
}

// Readiness devuelve los checks (cacheados readinessTTL). Nil si el layout no
// permite auto-apply (modo estable install.sh): el frontend ya muestra el
// enlace a releases en vez del botón de aplicar.
func (u *Updater) Readiness() *Readiness {
	if !u.canApply {
		return nil
	}
	u.mu.Lock()
	if u.readiness != nil && time.Since(u.readinessAt) < readinessTTL {
		r := u.readiness
		u.mu.Unlock()
		return r
	}
	u.mu.Unlock()

	r := u.computeReadiness()

	u.mu.Lock()
	u.readiness = &r
	u.readinessAt = time.Now()
	u.mu.Unlock()
	return &r
}

// computeReadiness ejecuta los 4 checks sin cache.
func (u *Updater) computeReadiness() Readiness {
	r := Readiness{
		Disk:       u.checkDisk(),
		Git:        u.checkGit(),
		Network:    u.checkNetwork(),
		Concurrent: u.checkConcurrent(),
	}
	r.Ready = u.canApply && r.Disk.OK && r.Git.OK && r.Network.OK && r.Concurrent.OK
	return r
}

// checkDisk verifica espacio libre en repoRoot (donde se backupa el binario
// y se trabaja durante el update).
func (u *Updater) checkDisk() CheckResult {
	path := u.repoRoot
	if path == "" {
		path = "."
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return CheckResult{OK: false, Detail: "no se pudo leer el espacio en disco"}
	}
	free := int64(st.Bavail) * int64(st.Bsize)
	if free < minDiskFreeBytes {
		return CheckResult{
			OK:     false,
			Detail: fmt.Sprintf("%d MB libres, se necesitan %d", free/(1<<20), minDiskFreeBytes/(1<<20)),
		}
	}
	return CheckResult{OK: true, Detail: fmt.Sprintf("%d MB libres", free/(1<<20))}
}

// checkGit verifica que el working tree está limpio (update.sh hace
// git reset --hard: cualquier cambio sin commitear se perdería).
func (u *Updater) checkGit() CheckResult {
	if u.mode != "rolling" {
		return CheckResult{OK: true, Detail: "no aplica (layout estable)"}
	}
	out, err := exec.Command("git", "-C", u.repoRoot, "status", "--porcelain").Output()
	if err != nil {
		return CheckResult{OK: false, Detail: "no se pudo leer el estado de git"}
	}
	if len(strings.TrimSpace(string(out))) > 0 {
		return CheckResult{OK: false, Detail: "hay cambios sin commitear"}
	}
	return CheckResult{OK: true}
}

// checkNetwork comprueba que GitHub es alcanzable (cualquier respuesta HTTP
// cuenta: un 4xx también significa que hay conectividad).
func (u *Updater) checkNetwork() CheckResult {
	ctx, cancel := context.WithTimeout(context.Background(), readinessNetTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", APIBase, nil)
	if err != nil {
		return CheckResult{OK: false, Detail: "URL de API inválida"}
	}
	req.Header.Set("User-Agent", "netpulse-updater")
	client := &http.Client{Timeout: readinessNetTimeout}
	res, err := client.Do(req)
	if err != nil {
		return CheckResult{OK: false, Detail: "no se pudo contactar con GitHub"}
	}
	res.Body.Close()
	return CheckResult{OK: true}
}

// checkConcurrent verifica que no hay ya un update en curso (mismo flag que
// Apply usa para devolver 409 already_updating).
func (u *Updater) checkConcurrent() CheckResult {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.updatingStep != nil {
		return CheckResult{OK: false, Detail: "hay una actualización en curso"}
	}
	return CheckResult{OK: true}
}
