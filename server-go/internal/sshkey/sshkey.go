// Package sshkey — clave SSH propia de NetPulse (paridad src/sshkey.js):
// par ed25519 en DATA_DIR/.ssh generado la primera vez, pública expuesta vía
// /api/config/sshkey para autorizarla a mano en cada router. known_hosts
// JUNTO a la clave (imprescindible con systemd ProtectSystem=strict).
package sshkey

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// BaseArgs replica sshBaseArgs (src/sshkey.js:16-24): los args SSH comunes.
// known_hosts junto a la clave (dentro de DATA_DIR).
func BaseArgs(keyPath string) []string {
	return []string{
		"-i", keyPath,
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=4",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "UserKnownHostsFile=" + filepath.Join(filepath.Dir(keyPath), "known_hosts"),
	}
}

// KnownHostsPath devuelve la ruta del known_hosts propio (junto a la clave).
func KnownHostsPath(keyPath string) string {
	return filepath.Join(filepath.Dir(keyPath), "known_hosts")
}

// EnsureKeypair garantiza que existe el par de claves en keyPath
// (ssh-keygen -t ed25519 -N ” -C netpulse); best-effort en permisos.
func EnsureKeypair(keyPath string) error {
	if _, err := os.Stat(keyPath); err == nil {
		if _, err := os.Stat(keyPath + ".pub"); err == nil {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return err
	}
	cmd := exec.Command("ssh-keygen", "-t", "ed25519", "-f", keyPath, "-N", "", "-C", "netpulse", "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		return &ExecError{What: "ssh-keygen", Err: err, Out: string(out)}
	}
	_ = os.Chmod(filepath.Dir(keyPath), 0o700)
	_ = os.Chmod(keyPath, 0o600)
	return nil
}

// ExecError envuelve un fallo de proceso externo.
type ExecError struct {
	What string
	Err  error
	Out  string
}

func (e *ExecError) Error() string {
	return e.What + ": " + e.Err.Error() + " " + strings.TrimSpace(e.Out)
}
func (e *ExecError) Unwrap() error { return e.Err }

// PublicKey es la respuesta de /api/config/sshkey.
type PublicKey struct {
	PublicKey   string `json:"publicKey"`
	Fingerprint string `json:"fingerprint"`
}

var fpRe = regexp.MustCompile(`^\d+\s+(\S+)`)

// GetPublicKey lee la clave pública y su fingerprint (ssh-keygen -lf, 2º
// campo). Devuelve (nil, nil) si no se puede leer (→ 500 no_key).
func GetPublicKey(keyPath string) *PublicKey {
	data, err := os.ReadFile(keyPath + ".pub")
	if err != nil {
		return nil
	}
	pub := strings.TrimSpace(string(data))
	out, err := exec.Command("ssh-keygen", "-lf", keyPath+".pub").Output()
	fp := ""
	if err == nil {
		if m := fpRe.FindStringSubmatch(strings.TrimSpace(string(out))); m != nil {
			fp = m[1]
		}
	}
	return &PublicKey{PublicKey: pub, Fingerprint: fp}
}

// RotateKeypair regenera el par de claves (issue #242): respalda el par actual
// como <keyPath>.bak.<epoch> y genera uno nuevo. known_hosts NO se toca (las
// host keys de los routers son independientes del par del cliente). Devuelve
// la nueva clave pública para reautorizarla en los routers. Si el par actual
// no existe, simplemente genera uno (sin backup).
func RotateKeypair(keyPath string) (*PublicKey, error) {
	// Backup del par actual si existe.
	if _, err := os.Stat(keyPath); err == nil {
		bak := fmt.Sprintf("%s.bak.%d", keyPath, time.Now().Unix())
		if err := os.Rename(keyPath, bak); err != nil {
			return nil, &ExecError{What: "rotate-backup", Err: err}
		}
		if _, err := os.Stat(keyPath + ".pub"); err == nil {
			_ = os.Rename(keyPath+".pub", bak+".pub")
		}
	}
	if err := EnsureKeypair(keyPath); err != nil {
		return nil, err
	}
	key := GetPublicKey(keyPath)
	if key == nil {
		return nil, &ExecError{What: "rotate-public-key", Err: os.ErrNotExist}
	}
	return key, nil
}
