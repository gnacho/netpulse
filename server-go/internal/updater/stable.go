// stable.go — auto-apply en layout estable (single-binary de install.sh,
// issue #480). El server corre como usuario netpulse con sandbox
// ProtectSystem=strict: NO puede reemplazar /usr/local/bin/netpulse. Flujo:
//
//  1. El updater descarga el asset de la release estable (tarball +
//     checksums.txt) a <dataDir>/updates/ y verifica el sha256 del tarball
//     contra checksums.txt (mismo formato que usa install.sh).
//  2. Extrae el binario a <dataDir>/updates/netpulse.new, le calcula su
//     sha256 y escribe el marcador <dataDir>/.stable-update (atómico, vía
//     rename) con target/sha256/ruta del binario en escena.
//  3. La unidad root netpulse-stable-update.path (instalada por install.sh,
//     detectada por stableUnitInstalled) reacciona al marcador y lanza el
//     helper netpulse-stable-apply, que RE-VERIFICA el sha256, respalda el
//     binario actual, hace swap, escribe el marcador de éxito
//     .update-applied con el objetivo (patrón #444), reinicia el servicio y
//     comprueba health; ante cualquier fallo restaura el binario anterior.
//  4. El restart mata este proceso, así que el éxito se confirma en el
//     arranque siguiente: loadPendingApply compara la versión embebida con
//     el marcador persistido (#161) y marca el historial (patrón #444
//     extendido a boot-time).
package updater

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// stableMarkerName dispara la unidad root (PathChanged). Se escribe de
	// forma atómica (tmp + rename) para que el helper nunca lea a medias.
	stableMarkerName = ".stable-update"
	// stableAppliedName: objetivo escrito por el helper justo antes de
	// reiniciar (mismo papel que el .update-applied de update.sh en rolling,
	// issue #444).
	stableAppliedName = ".update-applied"
	// stableErrorName: código de fallo del helper (verificación, swap o
	// healthcheck con rollback). Informativo; el historial manda.
	stableErrorName = ".stable-update.error"
	// stableBinName: nombre del binario dentro del tarball de release y
	// base del nombre de asset (netpulse_<ver>_linux_<GOARCH>.tar.gz).
	stableBinName = "netpulse"
	// stableMaxBinSize cota el tamaño del binario extraído (defensa).
	stableMaxBinSize = 256 << 20
	// stableRestartWait: espera máxima al swap+restart. En producción el
	// proceso muere en segundos; si vence, el apply se marca fallido y se
	// retira el marcador para que no dispare un swap tardío.
	stableRestartWait = 3 * time.Minute
)

// downloadBase es la base de los assets de release (inyectable en tests).
var downloadBase = "https://github.com"

// downloadClient: el tarball ronda los 20 MB; el timeout de la API (8 s)
// no aplica aquí.
var downloadClient = &http.Client{Timeout: 10 * time.Minute}

// stableUnitInstalled detecta la unidad root que hace el swap: busca
// /etc/systemd/system/<binario>-stable-update.path (install.sh la nombra a
// partir del binario, igual que las unidades -restart). Var de paquete para
// poder sobreescribirla en tests.
var stableUnitInstalled = func() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	unit := filepath.Join("/etc/systemd/system", filepath.Base(exe)+"-stable-update.path")
	_, err = os.Stat(unit)
	return err == nil
}

// WithDataDir fija el dataDir real (ruta resuelta de DATA_DIR). Necesario
// para el auto-apply estable (#480): staging y marcadores viven ahí. Sin
// dataDir el apply estable falla con stable_no_datadir. Encadenable tras
// New(); debe llamarse antes de servir tráfico. Como New() ya lanzó
// loadPendingApply sin dataDir, aquí corre la confirmación estable
// pendiente (loadStablePending): es el "arranque siguiente" de un apply
// que murió con el restart.
func (u *Updater) WithDataDir(path string) *Updater {
	// Sin u.mu a propósito: dataDir es boot-time e inmutable tras la
	// construcción (paridad repoRoot/version en New), y loadStablePending
	// toma u.mu al confirmar (mutex no reentrante).
	u.dataDir = path
	if path != "" {
		u.loadStablePending()
	}
	return u
}

// applyStable ejecuta el flujo completo de staging (download → verify →
// install → restart) emitiendo los mismos pasos del contrato #280. Al final
// espera la muerte del proceso (el restart de la unidad root); si no llega,
// aborta limpio.
func (u *Updater) applyStable(historyID int64, tag string, startedAt time.Time) {
	fail := func(code string) {
		u.mu.Lock()
		u.updatingStep = nil
		u.err = &code
		u.broadcastLocked()
		u.mu.Unlock()
		u.finishHistory(historyID, "failed", code, time.Since(startedAt))
		u.clearPendingApply()
		fmt.Printf("[netpulse:update] apply estable fallido: %s\n", code)
	}

	if u.dataDir == "" {
		fail("stable_no_datadir")
		return
	}
	ver := strings.TrimPrefix(tag, "v")
	asset := fmt.Sprintf("%s_%s_linux_%s.tar.gz", stableBinName, ver, runtime.GOARCH)

	u.mu.Lock()
	u.setStepLocked("download")
	u.broadcastLocked()
	u.mu.Unlock()
	fmt.Printf("[netpulse:update] estable: descargando %s (%s)\n", asset, tag)

	staging := filepath.Join(u.dataDir, "updates")
	_ = os.RemoveAll(staging) // restos de intentos previos
	_ = os.Remove(filepath.Join(u.dataDir, stableErrorName))
	if err := os.MkdirAll(staging, 0o755); err != nil {
		fail("stable_staging_failed")
		return
	}
	tgz := filepath.Join(staging, asset)
	base := fmt.Sprintf("%s/%s/releases/download/%s", downloadBase, u.repo, tag)
	if err := downloadToFile(base+"/"+asset, tgz); err != nil {
		fmt.Printf("[netpulse:update] descarga de %s falló: %v\n", asset, err)
		fail("stable_download_failed")
		return
	}
	if err := downloadToFile(base+"/checksums.txt", filepath.Join(staging, "checksums.txt")); err != nil {
		fmt.Printf("[netpulse:update] descarga de checksums.txt falló: %v\n", err)
		fail("stable_download_failed")
		return
	}

	u.mu.Lock()
	u.setStepLocked("verify")
	u.broadcastLocked()
	u.mu.Unlock()

	want, err := checksumFor(filepath.Join(staging, "checksums.txt"), asset)
	if err != nil {
		fail("stable_checksum_missing")
		return
	}
	got, err := fileSHA256(tgz)
	if err != nil {
		fail("stable_checksum_missing")
		return
	}
	if !strings.EqualFold(want, got) {
		fmt.Printf("[netpulse:update] sha256 mismatch del tarball: want %s got %s\n", want, got)
		fail("stable_checksum_mismatch")
		return
	}

	binNew, err := extractBinary(tgz, staging)
	if err != nil {
		fail("stable_extract_failed")
		return
	}
	// sha256 del binario YA extraído: el helper lo re-verifica antes del
	// swap (defensa en profundidad: el marcador vive en disco).
	binSHA, err := fileSHA256(binNew)
	if err != nil {
		fail("stable_extract_failed")
		return
	}

	u.mu.Lock()
	u.setStepLocked("install")
	u.broadcastLocked()
	u.mu.Unlock()

	marker := filepath.Join(u.dataDir, stableMarkerName)
	tmp := marker + ".tmp"
	content := fmt.Sprintf("target=%s\nsha256=%s\nstaged=%s\n", tag, binSHA, binNew)
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		fail("stable_marker_failed")
		return
	}
	if err := os.Rename(tmp, marker); err != nil {
		fail("stable_marker_failed")
		return
	}

	u.mu.Lock()
	u.setStepLocked("restart")
	u.broadcastLocked()
	u.mu.Unlock()
	fmt.Printf("[netpulse:update] estable %s en escena; esperando swap+restart (unidad root)\n", tag)

	// En producción el helper reinicia el servicio y el proceso muere aquí
	// con el binario nuevo. En tests, Stop() desbloquea sin marcar nada
	// (el éxito se confirma en el "arranque siguiente", ver loadPendingApply).
	select {
	case <-u.stopCh:
		return
	case <-time.After(stableRestartWait):
		_ = os.Remove(marker)
		fail("stable_restart_timeout")
	}
}

func downloadToFile(url, dst string) error {
	resp, err := downloadClient.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("http %d para %s", resp.StatusCode, url)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, io.LimitReader(resp.Body, stableMaxBinSize))
	return err
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// checksumFor extrae de checksums.txt el sha256 del asset dado (líneas
// "<hex>  <nombre>", mismo formato que grepea install.sh).
func checksumFor(sumsFile, asset string) (string, error) {
	data, err := os.ReadFile(sumsFile)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == asset {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("%s no está en %s", asset, sumsFile)
}

// extractBinary extrae la entrada "netpulse" (posible prefijo ./) del
// tarball a staging como netpulse.new con 0755. Rechaza entradas raras.
func extractBinary(tgz, staging string) (string, error) {
	f, err := os.Open(tgz)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return "", fmt.Errorf("tarball sin entrada %s", stableBinName)
		}
		if err != nil {
			return "", err
		}
		name := strings.TrimPrefix(hdr.Name, "./")
		if name != stableBinName || hdr.Typeflag != tar.TypeReg {
			continue
		}
		if hdr.Size > stableMaxBinSize {
			return "", fmt.Errorf("entrada %s demasiado grande (%d)", name, hdr.Size)
		}
		dst := filepath.Join(staging, "netpulse.new")
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(out, io.LimitReader(tr, stableMaxBinSize)); err != nil {
			out.Close()
			return "", err
		}
		if err := out.Close(); err != nil {
			return "", err
		}
		return dst, nil
	}
}
