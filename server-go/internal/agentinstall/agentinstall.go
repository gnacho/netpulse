// Package agentinstall embebe install-agent.sh en el servidor para servirlo
// desde GET /api/agents/install-agent.sh en vez de bajarlo de GitHub (#1751).
//
// install-agent.sh de la raíz del repositorio sigue siendo la copia canónica:
// es la que enlazan los docs y el respaldo manual. Este paquete guarda una
// copia byte a byte porque go:embed no puede salir del módulo (server-go/);
// TestScriptMatchesRepoCopy falla si las dos copias se separan.
package agentinstall

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
)

//go:embed install-agent.sh
var script []byte

// Script devuelve el instalador embebido. El llamante no debe modificarlo.
func Script() []byte { return script }

// Digest devuelve el sha256 del instalador en hexadecimal, para que el
// cliente pueda comprobar la descarga.
func Digest() string {
	sum := sha256.Sum256(script)
	return hex.EncodeToString(sum[:])
}
