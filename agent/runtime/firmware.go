// firmware.go: upgrade de firmware OpenWrt vía SSE (#453).
//
// El servidor envía el evento "firmware_upgrade" con la URL de la imagen y
// checksum sha256. El agente descarga la imagen, hace backup de la config
// con sysupgrade -b, escribe un fichero pendiente en /etc/netpulse y ejecuta
// sysupgrade -c. Como el reinicio mata el proceso, el resultado final se
// reporta en el siguiente arranque si encuentra el fichero pendiente.
package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var (
	firmwarePendingFile = "/etc/netpulse/.firmware-upgrade-pending"
	firmwareTmpDir      = "/tmp"
)

// Variables de comando para poder inyectar fakes en tests.
var (
	sysupgradeBin = "sysupgrade"
)

// firmwareCmd es el payload del evento SSE "firmware_upgrade".
type firmwareCmd struct {
	UpgradeID  int64  `json:"upgradeId"`
	TargetURL  string `json:"targetUrl"`
	Checksum   string `json:"checksum"`
	KeepConfig bool   `json:"keepConfig"`
}

// pendingFirmware guarda el upgrade activo para recuperarlo tras el reboot.
type pendingFirmware struct {
	UpgradeID     int64  `json:"upgradeId"`
	TargetVersion string `json:"targetVersion"`
}

// handleFirmwareUpgrade procesa el comando firmware_upgrade del servidor.
func handleFirmwareUpgrade(opts Options, transport http.RoundTripper, data string) {
	log := opts.logger()

	var cmd firmwareCmd
	if err := jsonUnmarshal(data, &cmd); err != nil {
		log.Warn("[netpulse-agent] firmware_upgrade: parse error", "err", err)
		return
	}
	if cmd.TargetURL == "" {
		log.Warn("[netpulse-agent] firmware_upgrade: sin targetUrl")
		return
	}

	log.Info("[netpulse-agent] firmware_upgrade iniciado", "upgrade_id", cmd.UpgradeID, "url", cmd.TargetURL)
	hc := &http.Client{Transport: transport, Timeout: 5 * time.Minute}

	dest := filepath.Join(firmwareTmpDir, fmt.Sprintf("firmware-%d.bin", cmd.UpgradeID))

	reportFirmwareProgress(opts, transport, cmd.UpgradeID, "downloading", 0, "")
	if err := downloadImage(context.Background(), hc, cmd.TargetURL, dest, cmd.Checksum, func(pct int) {
		reportFirmwareProgress(opts, transport, cmd.UpgradeID, "downloading", pct, "")
	}); err != nil {
		log.Error("[netpulse-agent] firmware_upgrade: descarga/verificación falló", "err", err)
		reportFirmwareResult(opts, transport, cmd.UpgradeID, false, err.Error(), "", "")
		return
	}

	reportFirmwareProgress(opts, transport, cmd.UpgradeID, "backing_up", 0, "")
	backupPath := filepath.Join(firmwareTmpDir, fmt.Sprintf("netpulse-config-backup-%d.tar.gz", time.Now().Unix()))
	if err := backupConfig(backupPath); err != nil {
		log.Error("[netpulse-agent] firmware_upgrade: backup de config falló", "err", err)
		reportFirmwareResult(opts, transport, cmd.UpgradeID, false, err.Error(), "", "")
		return
	}

	if err := writePendingFirmware(pendingFirmware{UpgradeID: cmd.UpgradeID}); err != nil {
		log.Error("[netpulse-agent] firmware_upgrade: no se pudo escribir pending", "err", err)
	}

	reportFirmwareProgress(opts, transport, cmd.UpgradeID, "flashing", 0, "")
	if err := runSysupgrade(dest, cmd.KeepConfig); err != nil {
		log.Error("[netpulse-agent] firmware_upgrade: sysupgrade falló", "err", err)
		_ = clearPendingFirmware()
		reportFirmwareResult(opts, transport, cmd.UpgradeID, false, err.Error(), backupPath, "")
		return
	}

	// sysupgrade normalmente reinicia y este proceso muere; si por alguna
	// razón no lo hizo, limpiamos el pending y reportamos fallo.
	log.Error("[netpulse-agent] firmware_upgrade: sysupgrade terminó sin reiniciar")
	_ = clearPendingFirmware()
	reportFirmwareResult(opts, transport, cmd.UpgradeID, false, "sysupgrade terminó sin reiniciar", backupPath, "")
}

// checkPendingFirmwareUpgrade se ejecuta al arrancar. Si existe un upgrade
// pendiente, reporta éxito (asumimos que el reboot fue causado por el upgrade).
func checkPendingFirmwareUpgrade(opts Options, transport http.RoundTripper) {
	pending, err := readPendingFirmware()
	if err != nil || pending == nil {
		return
	}
	_ = clearPendingFirmware()
	log := opts.logger()
	version := currentOpenWrtVersion()
	log.Info("[netpulse-agent] firmware_upgrade: completado tras reboot", "upgrade_id", pending.UpgradeID, "version", version)
	reportFirmwareResult(opts, transport, pending.UpgradeID, true, "", "", version)
}

// downloadImage descarga la imagen y verifica el sha256. onPct recibe 0-100.
func downloadImage(ctx context.Context, hc *http.Client, url, dest, checksum string, onPct func(int)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	res, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("imagen no disponible (HTTP %d)", res.StatusCode)
	}

	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	var body io.Reader = res.Body
	if onPct != nil && res.ContentLength > 0 {
		body = &progressReader{r: res.Body, total: res.ContentLength, onPct: onPct}
	}
	n, err := io.Copy(f, body)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if n == 0 {
		return fmt.Errorf("imagen vacía")
	}
	if checksum != "" {
		if err := verifyImageSha256(dest, checksum); err != nil {
			return err
		}
	}
	return nil
}

func verifyImageSha256(path, want string) error {
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

func backupConfig(dest string) error {
	cmd := exec.Command(sysupgradeBin, "-b", dest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sysupgrade -b: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func runSysupgrade(image string, keepConfig bool) error {
	args := []string{}
	if keepConfig {
		args = append(args, "-c")
	}
	args = append(args, image)
	cmd := exec.Command(sysupgradeBin, args...)
	// sysupgrade reinicia y mata este proceso; si retorna, devolvemos error.
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("sysupgrade: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func currentOpenWrtVersion() string {
	// /etc/openwrt_release contiene DISTRIB_RELEASE='23.05.4'.
	out, err := exec.Command("sh", "-c", ". /etc/openwrt_release 2>/dev/null && echo $DISTRIB_RELEASE").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func reportFirmwareProgress(opts Options, transport http.RoundTripper, upgradeID int64, status string, percent int, message string) {
	m := map[string]any{
		"upgradeId": upgradeID,
		"status":    status,
	}
	if percent > 0 {
		m["percent"] = percent
	}
	if message != "" {
		m["message"] = message
	}
	postJSON(opts, transport, "/api/agents/"+opts.Slug+"/firmware-progress", m)
}

func reportFirmwareResult(opts Options, transport http.RoundTripper, upgradeID int64, ok bool, errMsg, backupPath, newVersion string) {
	m := map[string]any{
		"upgradeId": upgradeID,
		"ok":        ok,
	}
	if errMsg != "" {
		m["error"] = errMsg
	}
	if backupPath != "" {
		m["backupPath"] = backupPath
	}
	if newVersion != "" {
		m["newVersion"] = newVersion
	}
	postJSON(opts, transport, "/api/agents/"+opts.Slug+"/firmware-result", m)
}

func writePendingFirmware(p pendingFirmware) error {
	if err := os.MkdirAll(filepath.Dir(firmwarePendingFile), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return os.WriteFile(firmwarePendingFile, b, 0o600)
}

func readPendingFirmware() (*pendingFirmware, error) {
	b, err := os.ReadFile(firmwarePendingFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var p pendingFirmware
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func clearPendingFirmware() error {
	return os.Remove(firmwarePendingFile)
}
