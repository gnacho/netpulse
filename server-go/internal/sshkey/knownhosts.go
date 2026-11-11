// knownhosts.go — acceso sincronizado al known_hosts propio (#2014).
//
// Tres escritores de Go trabajan sobre el mismo fichero: el callback
// accept-new del pool (append), PinHostKeys (append de ssh-keyscan) y
// RemoveHostKey (re-onboard, que reescribe el fichero entero). Sin coordinación,
// un append que caía entre el truncate y la reescritura dejaba una línea a
// medias, y como golang.org/x/crypto/ssh/knownhosts rechaza el fichero COMPLETO
// con la primera línea ilegible, el SSH a todas las unidades se caía de golpe.
//
// Hay un cuarto escritor que no puede tomar este lock: los procesos ssh y
// ssh-keyscan externos, que OpenSSH lanza con UserKnownHostsFile apuntando al
// mismo fichero (BaseArgs). Por eso la garantía no es el mutex sino el rename
// atómico: quien lee ve siempre un fichero completo, y una entrada que se
// pierda en el relevo se vuelve a añadir por TOFU en el siguiente dial. El
// mutex solo evita que los tres escritores de Go se pisen entre ellos.
package sshkey

import (
	"bytes"
	"encoding/base64"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
)

// khMu serializa lecturas-modificación-escritura sobre el known_hosts.
var khMu sync.Mutex

// AppendKnownHosts añade líneas al known_hosts. Van en un único write con
// O_APPEND, así que un lector nunca ve media línea.
func AppendKnownHosts(khPath string, lines []string) error {
	if len(lines) == 0 {
		return nil
	}
	khMu.Lock()
	defer khMu.Unlock()
	return appendKnownHostsLocked(khPath, lines)
}

func appendKnownHostsLocked(khPath string, lines []string) error {
	f, err := os.OpenFile(khPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(strings.Join(lines, "\n") + "\n"); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// UpdateKnownHosts aplica fn al contenido actual del fichero y lo reemplaza de
// forma atómica, todo bajo el lock del paquete. Si fn devuelve el contenido
// sin cambios no se reescribe nada, para no pisar un append ajeno.
func UpdateKnownHosts(khPath string, fn func(data []byte) ([]byte, error)) error {
	khMu.Lock()
	defer khMu.Unlock()
	data, err := os.ReadFile(khPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	out, err := fn(data)
	if err != nil {
		return err
	}
	if bytes.Equal(out, data) {
		return nil
	}
	return replaceKnownHostsLocked(khPath, out)
}

// replaceKnownHostsLocked escribe el fichero entero en un temporal de la misma
// carpeta, lo sincroniza y lo renombra encima: nunca trunca en sitio.
func replaceKnownHostsLocked(khPath string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(khPath), ".known_hosts.tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, khPath); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// SanitizeKnownHosts descarta las líneas que knownhosts.New rechazaría y
// devuelve cuántas quitó. Es la red de seguridad: una sola entrada corrupta no
// puede dejar sin SSH a toda la flota. Sin cambios en el fichero, no lo toca.
func SanitizeKnownHosts(khPath string) (int, error) {
	khMu.Lock()
	defer khMu.Unlock()
	data, err := os.ReadFile(khPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	var keep []string
	dropped := 0
	firstBad := 0
	for i, ln := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		if !validKnownHostsLine(ln) {
			dropped++
			if firstBad == 0 {
				firstBad = i + 1
			}
			continue
		}
		keep = append(keep, ln)
	}
	if dropped == 0 {
		return 0, nil
	}
	log.Printf("[netpulse:ssh] known_hosts %s: %d línea(s) ilegible(s) descartada(s), la primera en la %d (#2014)", khPath, dropped, firstBad)
	if err := replaceKnownHostsLocked(khPath, []byte(strings.Join(keep, "\n")+"\n")); err != nil {
		return 0, err
	}
	return dropped, nil
}

// validKnownHostsLine aplica el criterio de knownhosts: "<marcador opcional>
// <hosts> <tipo> <base64>". Un comentario se conserva.
func validKnownHostsLine(line string) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
		return true
	}
	if strings.HasPrefix(fields[0], "@") {
		// @cert-authority y @revoked anteponen un marcador.
		fields = fields[1:]
	}
	if len(fields) < 3 {
		return false
	}
	blob, err := base64.StdEncoding.DecodeString(fields[2])
	if err != nil {
		return false
	}
	_, err = ssh.ParsePublicKey(blob)
	return err == nil
}
