// Package sshkey — clave SSH propia de NetPulse (paridad src/sshkey.js):
// par ed25519 en DATA_DIR/.ssh generado la primera vez, pública expuesta vía
// /api/config/sshkey para autorizarla a mano en cada router. known_hosts
// JUNTO a la clave (imprescindible con systemd ProtectSystem=strict).
package sshkey

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
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

// khEntry es una línea de known_hosts desesquematizada: host(s) y host key.
type khEntry struct {
	hosts []string
	key   ssh.PublicKey
}

// PinHostKeys registra TODAS las host keys de un host en known_hosts usando
// ssh-keyscan (ed25519, ecdsa y rsa). Issue #651: el descubrimiento
// (openssh con StrictHostKeyChecking=accept-new) solo pinaba la clave que
// negociaba (ed25519), pero el pool de sondeo (crypto/ssh) negocia otra
// (ecdsa/rsa) con el mismo servidor; un host con varias host keys daba un
// falso "host key changed" que además caducaba en "re-onboard required".
// Al pinar todas las claves, el pool encuentra siempre la suya sea cual sea
// la que presente el servidor.
//
// Seguridad (no debilita #603): solo se AÑADEN claves ausentes, y solo si el
// host ya es de confianza (al menos una clave existente casa con la
// escaneada, es decir, identidad sin cambios) o si es la primera vez (TOFU).
// Si TODAS las claves cambiaron (p.ej. tras un reflash), no se toca nada y la
// detección de "host key changed" sigue intacta. Las entradas existentes no se
// modifican ni se borran, así que ninguna confianza previa se debilita.
func PinHostKeys(host, keyPath string) error {
	scan, err := scanHostKeys(host)
	if err != nil || len(scan) == 0 {
		return err
	}

	khPath := KnownHostsPath(keyPath)
	existing, err := readKnownHosts(khPath)
	if err != nil {
		return err
	}

	norm := knownhosts.Normalize(host)
	var existingForHost []khEntry
	for _, e := range existing {
		for _, h := range e.hosts {
			if knownhosts.Normalize(h) == norm {
				existingForHost = append(existingForHost, e)
				break
			}
		}
	}

	// Identidad cambiada (reflash): no añadir nada, se conserva la detección.
	if !shouldPinHostKeys(existingForHost, scan) {
		return nil
	}

	toAdd := pinHostKeysMerge(host, scan, existingForHost)
	if len(toAdd) == 0 {
		return nil
	}

	f, err := os.OpenFile(khPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(strings.Join(toAdd, "\n") + "\n")
	return err
}

// pinHostKeysMerge devuelve las líneas de claves escaneadas que faltan para
// `host` (host + algoritmo + claves no presentes ya). Es pura para poder
// probarla sin depender de ssh-keyscan.
func pinHostKeysMerge(host string, scan, existingForHost []khEntry) []string {
	var toAdd []string
	for _, s := range scan {
		dup := false
		for _, e := range existingForHost {
			if keySame(e.key, s.key) {
				dup = true
				break
			}
		}
		if !dup {
			toAdd = append(toAdd, knownhosts.Line([]string{host}, s.key))
		}
	}
	return toAdd
}

// scanHostKeys ejecuta ssh-keyscan sobre un host y devuelve sus host keys.
func scanHostKeys(host string) ([]khEntry, error) {
	cmd := exec.Command("ssh-keyscan", "-T", "3", "-t", "ed25519,ecdsa,rsa", host)
	out, err := cmd.Output()
	if err != nil {
		return nil, &ExecError{What: "ssh-keyscan", Err: err, Out: string(out)}
	}
	return parseKnownHosts(string(out))
}

func readKnownHosts(path string) ([]khEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return parseKnownHosts(string(data))
}

// parseKnownHosts parsea líneas de known_hosts (formato plain, sin hash de
// hostname) en entradas. Ignora comentarios y líneas malformadas; una línea
// es "<hosts> <tipo-clave> <base64>".
func parseKnownHosts(data string) ([]khEntry, error) {
	var out []khEntry
	for _, ln := range strings.Split(data, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		fields := strings.Fields(ln)
		if len(fields) < 3 {
			continue
		}
		blob, err := base64.StdEncoding.DecodeString(fields[2])
		if err != nil {
			continue
		}
		key, err := ssh.ParsePublicKey(blob)
		if err != nil {
			continue
		}
		out = append(out, khEntry{hosts: strings.Split(fields[0], ","), key: key})
	}
	return out, nil
}

// shouldPinHostKeys: true si procede pinar claves para el host. Es false solo
// cuando el host ya es conocido y ninguna clave existente casa con el barrido
// (identidad cambiada, p.ej. reflash): ahí se conserva la detección de "host
// key changed" en lugar de aceptar en silencio la nueva identidad.
func shouldPinHostKeys(existingForHost, scan []khEntry) bool {
	if len(existingForHost) == 0 {
		return true // host nuevo → TOFU
	}
	return sameIdentity(existingForHost, scan)
}

// keySame: true si dos host keys son idénticas (mismo tipo y material).
func keySame(a, b ssh.PublicKey) bool {
	return bytes.Equal(a.Marshal(), b.Marshal())
}

// sameIdentity: true si al menos una clave existente del host aparece en el
// barrido actual (identidad sin cambios).
func sameIdentity(existing, scan []khEntry) bool {
	for _, e := range existing {
		for _, s := range scan {
			if keySame(e.key, s.key) {
				return true
			}
		}
	}
	return false
}
