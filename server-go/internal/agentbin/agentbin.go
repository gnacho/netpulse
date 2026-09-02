// Package agentbin — binarios del agente netpulse-agent embebidos en el
// servidor para servir vía GET /api/agents/{slug}/binary?arch=... sin
// dependencia de GitHub (Fase 6.2).
//
// El CI construye el agente para amd64/arm64/armv7, copia los binarios a
// agentbin/agents/ (netpulse-agent-amd64, netpulse-agent-arm64, etc.) y
// luego compila el servidor. En desarrollo el directorio solo contiene
// .gitkeep (lo ignora git) y el servidor devuelve 404 para el endpoint
// de binarios — el install-agent.sh con --binary o GitHub siguen funcionando.
package agentbin

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io"
	"io/fs"
	"sync"
)

// EmbeddedAgentVersion es la versión del binario de agente que el servidor
// embebe y sirve por GET /api/agents/{slug}/binary?arch=.... Se usa para calcular el
// campo updateAvailable de GET /api/agents: un agente cuya versión reportada
// difiere de esta versión tiene una actualización pendiente.
//
// Es una `var` (no const) porque el CI la inyecta con ldflags
// (`-X ...agentbin.EmbeddedAgentVersion=$AGENT_VERSION`) usando el MISMO
// valor que fija `main.Version` del agente, de modo que la constante del
// server siempre coincide con la versión real de los binarios embebidos.
// En desarrollo (sin ldflags) queda en el default 0.1.0.
var EmbeddedAgentVersion = "0.1.0"

// digestCache: sha256 por arquitectura. Los binarios son embebidos (no
// cambian en runtime), así que se calcula una vez (#284: verificación de
// integridad del upgrade del agente).
var (
	digestMu    sync.Mutex
	digestCache = map[string]string{}
)
// Digest devuelve el sha256 (hex) del binario embebido para arch, o "" si no
// existe o no se puede leer. Cacheado. Normaliza armv7 -> arm como Open.
func Digest(arch string) string {
	arch = normalizeArch(arch)
	digestMu.Lock()
	defer digestMu.Unlock()
	if d, ok := digestCache[arch]; ok {
		return d
	}
	d := ""
	if f, err := Open(arch); err == nil {
		h := sha256.New()
		if _, err := io.Copy(h, f); err == nil {
			d = hex.EncodeToString(h.Sum(nil))
		}
		f.Close()
	}
	digestCache[arch] = d
	return d
}

// HasBinaries informa de si el server se construyó con binarios de agente
// embebidos (en desarrollo solo hay .gitkeep). Sin binarios no tiene sentido
// señalar agentes "desactualizados" contra EmbeddedAgentVersion (default).
func HasBinaries() bool {
	f, err := Open("arm64")
	if err != nil {
		return false
	}
	f.Close()
	return true
}

//go:embed agents/*
var agentsFS embed.FS

func normalizeArch(arch string) string {
	switch arch {
	case "aarch64":
		return "arm64"
	case "armv7l", "armv7", "armhf":
		return "arm"
	case "x86_64":
		return "amd64"
	}
	return arch
}

// Open devuelve el binario para la arquitectura dada, o error si no existe.
// Las arquitecturas válidas son las mismas que las builds de goreleaser:
// "amd64", "arm64", "armv7". Normaliza sinónimos (armv7 -> arm) para que el
// one-liner del agente, que reporta GOARCH=armv7, encuentre el binario.
func Open(arch string) (fs.File, error) {
	name := "agents/netpulse-agent-" + normalizeArch(arch)
	f, err := agentsFS.Open(name)
	if err != nil {
		return nil, err
	}
	// El embed no distingue ficheros de directorios (todos tienen IsDir false,
	// incluso el directorio agents/ si está vacío). Si el fichero existe pero
	// no tiene contenido útil, el Stat devolverá tamaño 0 y lo tratamos como
	// no disponible.
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if st.Size() == 0 {
		f.Close()
		return nil, fs.ErrNotExist
	}
	return f, nil
}
