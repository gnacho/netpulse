// upgrade.go — Fase 6.3 (issue #243): self-update del agente.
//
// Al recibir el evento SSE "upgrade", el agente descarga el binario embebido
// del propio servidor (GET /api/agents/{slug}/binary?arch=...), lo verifica,
// lo intercambia atómicamente por el binario en marcha y reinicia su servicio
// procd (el proceso actual muere y procd relanza el binario nuevo). El
// resultado se reporta a POST /api/agents/{slug}/upgrade-result ANTES del
// reinicio (que mata este proceso).
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"log/slog"
)

// Ruta del binario en marcha (instalación estándar) y del init script procd.
// install-agent.sh los fija así; la variante --tmp usa /tmp/netpulse-agent
// (se resuelve dinámicamente con os.Executable en currentBinPath).
const (
	defaultBinPath  = "/usr/sbin/netpulse-agent"
	agentInitScript = "/etc/init.d/netpulse-agent"
	// tmpBinPath: descarga temporal del binario nuevo antes del swap atómico.
	tmpBinPath = "/tmp/netpulse-agent.new"
)

// netpulseArch traduce runtime.GOARCH al nombre de binario que sirve el
// servidor (agentbin: amd64/arm64/armv7). GOARCH "arm" (armv7) no casa con
// el sufijo del fichero embebido, así que se normaliza.
func netpulseArch(goarch string) string {
	switch goarch {
	case "arm":
		return "armv7"
	case "amd64", "arm64":
		return goarch
	default:
		return goarch
	}
}

// currentBinPath devuelve la ruta del binario del agente en marcha, para
// intercambiarlo en el sitio correcto (resuelve la variante --tmp en RAM).
// Es una var para poder redirigirla en tests.
var currentBinPath = func() string {
	if exe, err := os.Executable(); err == nil && exe != "" {
		return exe
	}
	return defaultBinPath
}

// progressReader cuenta los bytes leídos y dispara onPct al cruzar cada
// frontera del 5% del total (100% incluido). Los reportes son síncronos:
// el callback hace el POST del paso desde el hilo de la copia (#284: 5%
// para que la timeline muestre movimiento incluso en descargas rápidas).
type progressReader struct {
	r        io.Reader
	total    int64
	done     int64
	boundary int // última frontera del 5% reportada
	onPct    func(pct int)
}

func (p *progressReader) Read(buf []byte) (int, error) {
	n, err := p.r.Read(buf)
	p.done += int64(n)
	if p.total > 0 && p.onPct != nil {
		// Emitir TODAS las fronteras del 5% cruzadas en esta lectura (una
		// Read grande puede saltarse varias).
		for b := int(p.done * 20 / p.total); p.boundary < b; p.boundary++ {
			p.onPct((p.boundary + 1) * 5)
		}
	}
	return n, err
}

// downloadBinary descarga el binario desde url (con Bearer token) y lo deja
// en dest con permisos 0755. Error si la petición no es 200 o el cuerpo está
// vacío. onPct (opcional) recibe el progreso 0-100 de la descarga (#284).
// Devuelve el valor de la cabecera X-Checksum-Sha256 (vacía si el server no
// la manda) para que el llamante verifique la integridad.
// Extraída a función propia para poder testearla con httptest.
func downloadBinary(ctx context.Context, hc *http.Client, url, token, dest string, onPct func(pct int)) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	res, err := hc.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("binario no disponible (HTTP %d)", res.StatusCode)
	}
	digest := strings.TrimSpace(strings.TrimPrefix(res.Header.Get("X-Checksum-Sha256"), "sha256:"))

	var body io.Reader = res.Body
	if onPct != nil && res.ContentLength > 0 {
		body = &progressReader{r: res.Body, total: res.ContentLength, onPct: onPct}
	}

	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return "", err
	}
	n, copyErr := io.Copy(f, body)
	closeErr := f.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if n == 0 {
		return "", errors.New("binario vacío")
	}
	if err := os.Chmod(dest, 0o755); err != nil {
		return "", err
	}
	return digest, nil
}

// swapBinary intercambia atómicamente src por dst. Si src y dst están en
// distintos filesystems (p. ej. /tmp tmpfs → /usr/sbin overlay en OpenWrt),
// os.Rename falla con EXDEV y se cae a copiar a un temp en el dir de destino
// y renombrar allí (atómico dentro de ese filesystem). El rename sobre un
// binario en ejecución es seguro en Linux: el proceso vivo conserva su inode.
func swapBinary(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// restartService reinicia el servicio procd del agente. Sin init script,
// devuelve error (no hay forma fiable de reiniciarse a sí mismo).
func restartService() error {
	if _, err := os.Stat(agentInitScript); err != nil {
		return fmt.Errorf("init script %s no encontrado", agentInitScript)
	}
	cmd := exec.Command(agentInitScript, "restart")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s restart: %v (%s)", agentInitScript, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// verifySha256 comprueba que el fichero descargado coincide con el digest
// esperado (hex, sin prefijo). Vacío o distinto → error (#284).
func verifySha256(path, want string) error {
	if want == "" {
		return errors.New("el servidor no publicó digest de integridad")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("sha256 no coincide (esperado %s, descargado %s)", want, got)
	}
	return nil
}

// reportUpgradeProgress POSTea un paso intermedio del upgrade al servidor
// (#284: progreso en vivo). Fire-and-forget: un fallo se loguea y se sigue
// (el progreso es best-effort; el resultado final sí es fiable).
func reportUpgradeProgress(cfg config, transport http.RoundTripper, step string, pct int) {
	m := map[string]any{"slug": cfg.slug, "step": step}
	if pct > 0 {
		m["pct"] = pct
	}
	body, _ := json.Marshal(m)
	req, err := http.NewRequest("POST", cfg.server+"/api/agents/"+cfg.slug+"/upgrade-progress", strings.NewReader(string(body)))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.token)
	hc := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		slog.Warn("[netpulse-agent] upgrade: progress POST falló", "step", step, "err", err)
		return
	}
	resp.Body.Close()
}

// reportUpgradeResult POSTea el resultado del upgrade al servidor.
func reportUpgradeResult(cfg config, transport http.RoundTripper, ok bool, errMsg string) {
	m := map[string]any{"slug": cfg.slug, "ok": ok}
	if errMsg != "" {
		m["error"] = errMsg
	}
	body, _ := json.Marshal(m)
	req, err := http.NewRequest("POST", cfg.server+"/api/agents/"+cfg.slug+"/upgrade-result", strings.NewReader(string(body)))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.token)
	hc := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		slog.Warn("[netpulse-agent] upgrade: result POST falló", "err", err)
		return
	}
	resp.Body.Close()
}

// handleUpgrade procesa el evento SSE "upgrade": descarga el binario, lo
// verifica, hace el swap atómico, reporta el resultado y reinicia el servicio
// (que mata este proceso; procd relanza el binario nuevo).
func handleUpgrade(cfg config, transport http.RoundTripper, data string) {
	arch := runtime.GOARCH
	if data != "" {
		var p struct {
			Arch string `json:"arch"`
		}
		if err := json.Unmarshal([]byte(data), &p); err == nil && p.Arch != "" {
			arch = p.Arch
		}
	}
	arch = netpulseArch(arch)

	slog.Info("[netpulse-agent] upgrade iniciado", "slug", cfg.slug, "arch", arch)
	url := cfg.server + "/api/agents/" + cfg.slug + "/binary?arch=" + arch
	hc := &http.Client{Transport: transport, Timeout: 2 * time.Minute}

	reportUpgradeProgress(cfg, transport, "downloading", 0)
	digest, err := downloadBinary(context.Background(), hc, url, cfg.token, tmpBinPath, func(pct int) {
		reportUpgradeProgress(cfg, transport, "downloading", pct)
	})
	if err != nil {
		slog.Error("[netpulse-agent] upgrade: descarga falló", "err", err)
		reportUpgradeResult(cfg, transport, false, err.Error())
		return
	}

	// Integridad (#284): sha256 contra la cabecera del servidor, ANTES del swap.
	if digest != "" {
		reportUpgradeProgress(cfg, transport, "verifying", 0)
		if err := verifySha256(tmpBinPath, digest); err != nil {
			slog.Error("[netpulse-agent] upgrade: verificación falló", "err", err)
			reportUpgradeResult(cfg, transport, false, err.Error())
			return
		}
	}

	dst := currentBinPath()
	reportUpgradeProgress(cfg, transport, "swapping", 0)
	if err := swapBinary(tmpBinPath, dst); err != nil {
		slog.Error("[netpulse-agent] upgrade: swap falló", "err", err, "dst", dst)
		reportUpgradeResult(cfg, transport, false, err.Error())
		return
	}

	slog.Info("[netpulse-agent] upgrade: binario intercambiado, reiniciando servicio", "dst", dst)
	reportUpgradeProgress(cfg, transport, "restarting", 0)
	// Reportar ANTES de reiniciar: el reinicio mata este proceso.
	reportUpgradeResult(cfg, transport, true, "")
	if err := restartService(); err != nil {
		slog.Warn("[netpulse-agent] upgrade: reinicio falló", "err", err)
	}
}
