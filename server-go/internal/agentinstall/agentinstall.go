// Package agentinstall embebe install-agent.sh en el servidor para servirlo
// desde GET /api/agents/install-agent.sh en vez de bajarlo de GitHub (#1751).
//
// install-agent.sh de la raíz del repositorio sigue siendo la copia canónica:
// es la que enlazan los docs y el respaldo manual. Este paquete guarda una
// copia byte a byte porque go:embed no puede salir del módulo (server-go/);
// TestScriptMatchesRepoCopy falla si las dos copias se separan.
package agentinstall

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
)

//go:embed install-agent.sh
var embedded []byte

// script es el instalador que se sirve, siempre con saltos LF. Un checkout con
// core.autocrlf=true (el default de Git en Windows) deja CRLF en el fichero
// embebido, y el busybox ash del router rechaza ese script con un error de
// sintaxis (#1812). La normalización es idempotente: un fichero LF se sirve
// tal cual, sin copia nueva.
var script = normalizeLF(embedded)

// normalizeLF convierte los finales de línea CRLF en LF.
func normalizeLF(b []byte) []byte {
	if !bytes.Contains(b, []byte("\r\n")) {
		return b
	}
	return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
}

// Script devuelve el instalador embebido. El llamante no debe modificarlo.
func Script() []byte { return script }

// Digest devuelve el sha256 del instalador en hexadecimal, para que el
// cliente pueda comprobar la descarga.
func Digest() string {
	sum := sha256.Sum256(script)
	return hex.EncodeToString(sum[:])
}
