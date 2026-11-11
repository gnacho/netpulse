package adapters

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/sshkey"
	"golang.org/x/crypto/ssh/knownhosts"
)

// TestHostKeyCallbackRecoversFromCorruptKnownHosts: una línea corrupta en el
// known_hosts deja hoy sin SSH a TODAS las unidades, porque knownhosts.New
// rechaza el fichero entero (#2014). El callback debe sanear antes de cargar.
func TestHostKeyCallbackRecoversFromCorruptKnownHosts(t *testing.T) {
	pool := newTestPool(t)
	_, pub := genTestKey(t)
	good := knownhosts.Line([]string{knownhosts.Normalize("host-a")}, pub)
	content := good + "\nhost-corrupt ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAbc\n"
	if err := os.WriteFile(pool.khPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	// Premisa: el fichero de partida es inválido para la librería.
	if _, err := knownhosts.New(pool.khPath); err == nil {
		t.Fatal("premisa rota: el fichero de partida debería ser inválido")
	}

	cb, err := pool.hostKeyCallback()
	if err != nil {
		t.Fatalf("el callback debería recuperarse de una línea corrupta, obtuve: %v", err)
	}
	if cb == nil {
		t.Fatal("callback nil")
	}
	data, _ := os.ReadFile(pool.khPath)
	if strings.Contains(string(data), "host-corrupt") {
		t.Fatal("la línea corrupta sigue en el fichero")
	}
	if !strings.Contains(string(data), good) {
		t.Fatal("el saneado se comió la entrada válida")
	}
}

// TestRemoveHostKeyConcurrentWithAppends: la carrera del issue consistía en
// reescribir el fichero (truncate en sitio) mientras otro escritor hacía
// append. Con la reescritura atómica el fichero siempre queda legible.
func TestRemoveHostKeyConcurrentWithAppends(t *testing.T) {
	pool := newTestPool(t)
	const n = 40
	lines := make([]string, n)
	for i := 0; i < n; i++ {
		_, pub := genTestKey(t)
		lines[i] = knownhosts.Line([]string{knownhosts.Normalize(fmt.Sprintf("host-%d", i))}, pub)
	}
	if err := os.WriteFile(pool.khPath, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			if err := sshkey.AppendKnownHosts(pool.khPath, []string{lines[i]}); err != nil {
				t.Errorf("append %d: %v", i, err)
			}
		}(i)
		go func(i int) {
			defer wg.Done()
			if err := pool.RemoveHostKey(fmt.Sprintf("host-%d", i)); err != nil {
				t.Errorf("remove %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()

	if _, err := knownhosts.New(pool.khPath); err != nil {
		t.Fatalf("known_hosts ilegible tras la concurrencia: %v", err)
	}
}

// TestRemoveHostKeyKeepsOtherHostsAndSkipsNoop: sin coincidencias no se
// reescribe; con coincidencias se quita solo ese host.
func TestRemoveHostKeyKeepsOtherHostsAndSkipsNoop(t *testing.T) {
	pool := newTestPool(t)
	_, pubA := genTestKey(t)
	_, pubB := genTestKey(t)
	a := knownhosts.Line([]string{knownhosts.Normalize("host-a")}, pubA)
	b := knownhosts.Line([]string{knownhosts.Normalize("host-b")}, pubB)
	if err := os.WriteFile(pool.khPath, []byte(a+"\n"+b+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(pool.khPath)

	if err := pool.RemoveHostKey("host-inexistente"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(pool.khPath)
	if !os.SameFile(before, after) {
		t.Fatal("un RemoveHostKey sin coincidencias no debería reescribir el fichero")
	}

	if err := pool.RemoveHostKey("host-a"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(pool.khPath)
	s := string(data)
	if strings.Contains(s, "host-a") {
		t.Fatal("no se quitó host-a")
	}
	if !strings.Contains(s, b) {
		t.Fatal("se perdió host-b")
	}
}
