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
	"embed"
	"io/fs"
)

//go:embed agents/*
var agentsFS embed.FS

// Open devuelve el binario para la arquitectura dada, o error si no existe.
// Las arquitecturas válidas son las mismas que las builds de goreleaser:
// "amd64", "arm64", "armv7".
func Open(arch string) (fs.File, error) {
	name := "agents/netpulse-agent-" + arch
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
