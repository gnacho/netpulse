// gen-demo-canon — genera app/src/data/demo-canon.json desde el canon Go
// (SPEC-65 D65-1: la fuente de verdad única del demo es Go).
//
// Uso: go run ./cmd/gen-demo-canon [-out ruta.json]
// Sin -out, resuelve <repo>/app/src/data/demo-canon.json localizando la raíz
// del repo (directorio que contiene server-go/go.mod) desde el cwd.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
)

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if st, err := os.Stat(filepath.Join(dir, "server-go", "go.mod")); err == nil && !st.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no se encuentra la raíz del repo (server-go/go.mod) desde el cwd")
		}
		dir = parent
	}
}

func main() {
	out := flag.String("out", "", "ruta de salida (por defecto <repo>/app/src/data/demo-canon.json)")
	flag.Parse()
	path := *out
	if path == "" {
		root, err := repoRoot()
		if err != nil {
			fmt.Fprintln(os.Stderr, "gen-demo-canon:", err)
			os.Exit(1)
		}
		path = filepath.Join(root, "app", "src", "data", "demo-canon.json")
	}
	canon := adapters.BuildDemoCanon()
	b, err := json.MarshalIndent(canon, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "gen-demo-canon:", err)
		os.Exit(1)
	}
	b = append(b, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "gen-demo-canon:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "gen-demo-canon:", err)
		os.Exit(1)
	}
	fmt.Printf("demo-canon: %d devices → %s (%d bytes)\n", len(canon.Devices), path, len(b))
}
