package sshkey

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/crypto/ssh/knownhosts"
)

// corruptLine replica la forma que deja un append a medias sobre una
// reescritura: base64 truncado, que es lo que rechaza knownhosts.New.
const corruptLine = "host-corrupt ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAbc"

func writeKH(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func tmpLeftovers(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".known_hosts.tmp") {
			out = append(out, e.Name())
		}
	}
	return out
}

// TestSanitizeKnownHostsDropsMalformedLines: una línea corrupta no puede
// tumbar la carga del fichero. Premisa comprobada (el fichero de partida es
// inválido para la librería) y resultado comprobado tras el saneado.
func TestSanitizeKnownHostsDropsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	good1 := lineFor(t, "host-a", genPubKey(t, "ed25519"))
	good2 := lineFor(t, "host-b", genPubKey(t, "ecdsa"))
	path := writeKH(t, dir, "# comentario\n"+good1+"\n"+corruptLine+"\n"+good2+"\n")

	if _, err := knownhosts.New(path); err == nil {
		t.Fatal("premisa rota: el fichero de partida debería ser inválido para knownhosts.New")
	}

	dropped, err := SanitizeKnownHosts(path)
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 1 {
		t.Fatalf("esperaba 1 línea descartada, got=%d", dropped)
	}
	if _, err := knownhosts.New(path); err != nil {
		t.Fatalf("knownhosts.New sigue fallando tras el saneado: %v", err)
	}
	data, _ := os.ReadFile(path)
	s := string(data)
	if !strings.Contains(s, good1) || !strings.Contains(s, good2) {
		t.Fatal("el saneado se comió entradas válidas")
	}
	if !strings.Contains(s, "# comentario") {
		t.Fatal("el saneado se comió un comentario")
	}
	if strings.Contains(s, "host-corrupt") {
		t.Fatal("la línea corrupta sigue en el fichero")
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Fatalf("permisos tras el saneado = %v, esperaba 0600", st.Mode().Perm())
	}
	if left := tmpLeftovers(t, dir); len(left) != 0 {
		t.Fatalf("temporales sin limpiar: %v", left)
	}
}

// TestSanitizeKnownHostsLeavesHealthyFileUntouched: sin líneas malas no se
// reescribe nada (ni siquiera se toca el inodo).
func TestSanitizeKnownHostsLeavesHealthyFileUntouched(t *testing.T) {
	dir := t.TempDir()
	good := lineFor(t, "host-a", genPubKey(t, "ed25519")) + "\n"
	path := writeKH(t, dir, good)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	dropped, err := SanitizeKnownHosts(path)
	if err != nil || dropped != 0 {
		t.Fatalf("dropped=%d err=%v, esperaba 0/nil", dropped, err)
	}
	after, _ := os.Stat(path)
	if !os.SameFile(before, after) {
		t.Fatal("un fichero sano no debería reescribirse")
	}
	data, _ := os.ReadFile(path)
	if string(data) != good {
		t.Fatalf("contenido alterado: %q", string(data))
	}
}

// TestSanitizeKnownHostsKeepsMarkers: @revoked y @cert-authority son formato
// válido de known_hosts y no se descartan.
func TestSanitizeKnownHostsKeepsMarkers(t *testing.T) {
	dir := t.TempDir()
	revoked := "@revoked " + lineFor(t, "host-a", genPubKey(t, "ed25519")) + "\n"
	path := writeKH(t, dir, revoked)

	dropped, err := SanitizeKnownHosts(path)
	if err != nil || dropped != 0 {
		t.Fatalf("dropped=%d err=%v, esperaba 0/nil para una línea con marcador", dropped, err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != revoked {
		t.Fatalf("se alteró una línea con marcador: %q", string(data))
	}
}

// TestReplaceUsesRenameNotTruncate: la reescritura cambia el inodo, prueba de
// que no trunca en sitio (un append concurrente no puede caer "en medio").
func TestReplaceUsesRenameNotTruncate(t *testing.T) {
	dir := t.TempDir()
	path := writeKH(t, dir, lineFor(t, "host-a", genPubKey(t, "ed25519"))+"\n")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	newLine := lineFor(t, "host-b", genPubKey(t, "ed25519"))
	if err := UpdateKnownHosts(path, func([]byte) ([]byte, error) {
		return []byte(newLine + "\n"), nil
	}); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, after) {
		t.Fatal("la reescritura fue en sitio (truncate); debería haber sido un rename")
	}
	if after.Mode().Perm() != 0o600 {
		t.Fatalf("permisos = %v, esperaba 0600", after.Mode().Perm())
	}
	if _, err := knownhosts.New(path); err != nil {
		t.Fatalf("knownhosts.New falla tras la reescritura: %v", err)
	}
	if left := tmpLeftovers(t, dir); len(left) != 0 {
		t.Fatalf("temporales sin limpiar: %v", left)
	}
}

// TestUpdateKnownHostsNoOpKeepsInode: si fn no cambia el contenido, no se
// reescribe (no se pisa un append que acabe de entrar).
func TestUpdateKnownHostsNoOpKeepsInode(t *testing.T) {
	dir := t.TempDir()
	path := writeKH(t, dir, lineFor(t, "host-a", genPubKey(t, "ed25519"))+"\n")
	before, _ := os.Stat(path)

	if err := UpdateKnownHosts(path, func(data []byte) ([]byte, error) {
		return data, nil
	}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if !os.SameFile(before, after) {
		t.Fatal("un update sin cambios no debería reescribir el fichero")
	}
}

// TestAppendAndUpdateAreSerialized: appends y reescrituras en paralelo dejan
// siempre un fichero que la librería acepta. Es la carrera del issue.
func TestAppendAndUpdateAreSerialized(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	const n = 40
	lines := make([]string, n)
	for i := 0; i < n; i++ {
		lines[i] = lineFor(t, fmt.Sprintf("host-%d", i), genPubKey(t, "ed25519"))
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			if err := AppendKnownHosts(path, []string{lines[i]}); err != nil {
				t.Errorf("append %d: %v", i, err)
			}
		}(i)
		go func(i int) {
			defer wg.Done()
			// Reescribe el fichero entero mientras otros hacen append.
			if err := UpdateKnownHosts(path, func(data []byte) ([]byte, error) {
				return append(data, []byte(lines[(i+1)%n]+"\n")...), nil
			}); err != nil {
				t.Errorf("update %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	if _, err := knownhosts.New(path); err != nil {
		t.Fatalf("known_hosts ilegible tras la concurrencia: %v", err)
	}
	if left := tmpLeftovers(t, dir); len(left) != 0 {
		t.Fatalf("temporales sin limpiar: %v", left)
	}
}
