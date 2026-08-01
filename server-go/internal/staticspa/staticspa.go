// Package staticspa — estáticos + SPA fallback (paridad app.js:44-60).
//
//   - Sirve ficheros existentes del root (STATIC_DIR del env si se define;
//     si no, el dist EMBEBIDO — ver abajo).
//   - index.html se cachea en memoria al arranque; si falta → warning y el
//     fallback devuelve 503 texto.
//   - Fallback GET: /api/* → 404 JSON {error:'not_found'}; /assets/* → 404
//     plano; resto → 200 HTML con el index.html cacheado (SPA fallback).
//
// EMBED: go:embed no puede referenciar ../../app/dist (fuera del módulo), así
// que el binario embebe internal/staticspa/dist. CI debe copiar app/dist a
// server-go/internal/staticspa/dist antes de `go build` (hasta entonces hay
// un index.html placeholder). STATIC_DIR siempre tiene prioridad sobre el
// embed (override para desarrollo/despliegue clásico).
package staticspa

import (
	"embed"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
)

//go:embed dist
var embeddedDist embed.FS

// Handler sirve los estáticos y el SPA fallback.
type Handler struct {
	files     fs.FS
	indexHTML []byte // nil si no hay index.html
}

// New crea el handler. Si staticDir != "" sirve ese directorio del disco
// (override STATIC_DIR); si no, usa el dist embebido en el binario.
// Lee index.html en memoria al arranque (warning si falta, como app.js:51).
func New(staticDir string) *Handler {
	h := &Handler{}
	desc := staticDir
	if staticDir != "" {
		h.files = os.DirFS(staticDir)
	} else {
		sub, err := fs.Sub(embeddedDist, "dist")
		if err != nil {
			sub = embeddedDist
		}
		h.files = sub
		desc = "(embed)"
	}
	if data, err := fs.ReadFile(h.files, "index.html"); err == nil {
		h.indexHTML = data
	} else {
		log.Printf("[netpulse] STATIC_DIR=%s sin index.html (¿falta \"npm run build\" en app/?)", desc)
	}
	return h
}

// serveFile sirve el fichero `name` si existe y no es directorio.
func (h *Handler) serveFile(w http.ResponseWriter, r *http.Request, name string) bool {
	if !fs.ValidPath(name) {
		return false
	}
	f, err := h.files.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		return false
	}
	rs, ok := f.(io.ReadSeeker)
	if !ok {
		return false
	}
	http.ServeContent(w, r, st.Name(), st.ModTime(), rs)
	return true
}

// notFoundPlain replica el 404 por defecto de Hono (c.notFound()): cuerpo de
// texto "404 Not Found", no el "404 page not found\n" de net/http.
func notFoundPlain(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte("404 Not Found"))
}

// ServeHTTP implementa http.Handler: estático si existe; si no, fallback.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path

	// /api/* nunca llega aquí (el mux lo enruta antes), pero por seguridad:
	if strings.HasPrefix(p, "/api/") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not_found"}`))
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		notFoundPlain(w)
		return
	}

	// Estático: sirve el fichero si existe (raíz → index.html).
	name := strings.TrimPrefix(path.Clean("/"+p), "/")
	if name == "" {
		name = "index.html"
	}
	if h.serveFile(w, r, name) {
		return
	}
	if h.serveFile(w, r, path.Join(name, "index.html")) {
		return
	}

	// Fallback (GET *):
	if strings.HasPrefix(p, "/assets/") {
		notFoundPlain(w) // 404 plano, sin JSON (cuerpo "404 Not Found" como Hono)
		return
	}
	if h.indexHTML == nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("NetPulse: frontend no compilado (cd app && npm run build)"))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(h.indexHTML)
}
