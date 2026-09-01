// Package sshkey — clave SSH propia de NetPulse (paridad src/sshkey.js):
// par ed25519 en DATA_DIR/.ssh generado la primera vez, pública expuesta vía
// /api/config/sshkey para autorizarla a mano en cada router. known_hosts
// JUNTO a la clave (imprescindible con systemd ProtectSystem=strict).
package sshkey

import (
	"fmt"
	"io"
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
// Si no existe y hay un backup previo (<keyPath>.bak.<epoch>), lo restaura
// en lugar de generar un par nuevo (defensa contra perdida accidental de
// .ssh/ durante updates, issue #425).
func EnsureKeypair(keyPath string) error {
	if keyExists(keyPath) {
		return nil
	}
	// Intentar restaurar desde backup antes de generar clave nueva.
	if restored, _ := restoreLatestBackup(keyPath); restored {
		return nil
	}
	return generateKeypair(keyPath)
}

func keyExists(keyPath string) bool {
	if _, err := os.Stat(keyPath); err != nil {
		return false
	}
	if _, err := os.Stat(keyPath + ".pub"); err != nil {
		return false
	}
	return true
}

// restoreLatestBackup busca backups <keyPath>.bak.<epoch> y restaura el mas
// reciente. Devuelve true si restauro algo.
func restoreLatestBackup(keyPath string) (bool, error) {
	dir := filepath.Dir(keyPath)
	base := filepath.Base(keyPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	var latest string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, base+".bak.") && !strings.HasSuffix(name, ".pub") {
			if latest == "" || name > latest {
				latest = name
			}
		}
	}
	if latest == "" {
		return false, nil
	}
	bakKey := filepath.Join(dir, latest)
	bakPub := bakKey + ".pub"
	if _, err := os.Stat(bakPub); err != nil {
		return false, err
	}
	if err := copyFile(bakKey, keyPath); err != nil {
		return false, err
	}
	if err := copyFile(bakPub, keyPath+".pub"); err != nil {
		return false, err
	}
	_ = os.Chmod(keyPath, 0o600)
	return true, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	cerr := out.Close()
	if err != nil {
		return err
	}
	return cerr
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
	if keyExists(keyPath) {
		bak := fmt.Sprintf("%s.bak.%d", keyPath, time.Now().Unix())
		if err := os.Rename(keyPath, bak); err != nil {
			return nil, &ExecError{What: "rotate-backup", Err: err}
		}
		if _, err := os.Stat(keyPath + ".pub"); err == nil {
			_ = os.Rename(keyPath+".pub", bak+".pub")
		}
	}
	// Generar directamente, sin pasar por EnsureKeypair, para evitar que
	// restaure el backup que acabamos de crear (issue #425).
	if err := generateKeypair(keyPath); err != nil {
		return nil, err
	}
	key := GetPublicKey(keyPath)
	if key == nil {
		return nil, &ExecError{What: "rotate-public-key", Err: os.ErrNotExist}
	}
	return key, nil
}

func generateKeypair(keyPath string) error {
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
