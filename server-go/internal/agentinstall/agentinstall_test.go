package agentinstall

import (
	"bytes"
	"os"
	"testing"
)

// repoScript: la copia canónica vive en la raíz del repositorio. El test se
// ejecuta con el directorio del paquete como CWD, así que sube cuatro niveles
// (agentinstall -> internal -> server-go -> raíz).
const repoScript = "../../../install-agent.sh"

// TestScriptMatchesRepoCopy falla cuando el instalador embebido se separa del
// que se publica en el repositorio: el servidor serviría un script viejo y la
// línea de instalación dejaría de coincidir con lo que dicen los docs (#1751).
func TestScriptMatchesRepoCopy(t *testing.T) {
	want, err := os.ReadFile(repoScript)
	if err != nil {
		t.Fatalf("read %s: %v", repoScript, err)
	}
	// #1812: la comparación ignora los finales de línea. Un checkout con
	// autocrlf deja CRLF en las dos copias y Script() los normaliza, así que
	// comparar en crudo rompería en Windows sin que las copias se hayan
	// separado de verdad.
	if !bytes.Equal(normalizeLF(want), Script()) {
		t.Fatalf("install-agent.sh embebido y el del repositorio difieren: copia de nuevo el fichero a internal/agentinstall/install-agent.sh")
	}
	if len(want) == 0 {
		t.Fatal("install-agent.sh vacío")
	}
}

func TestScriptIsShell(t *testing.T) {
	if !bytes.HasPrefix(Script(), []byte("#!/bin/sh")) {
		t.Fatalf("el instalador embebido no empieza por #!/bin/sh")
	}
}

func TestDigestIsStable(t *testing.T) {
	d := Digest()
	if len(d) != 64 {
		t.Fatalf("digest = %q, se esperaba sha256 hex", d)
	}
	if d != Digest() {
		t.Fatal("Digest no es estable entre llamadas")
	}
}

// #1812: el instalador servido nunca lleva CRLF, sea cual sea el checkout con
// el que se compiló el servidor.
func TestScriptServesLF(t *testing.T) {
	if bytes.Contains(Script(), []byte("\r\n")) {
		t.Fatal("el instalador embebido conserva CRLF: busybox ash lo rechaza")
	}
}

// La normalización no cambia un fichero que ya está en LF.
func TestNormalizeLFIsIdempotent(t *testing.T) {
	lf := []byte("#!/bin/sh\necho ok\n")
	if got := normalizeLF(lf); !bytes.Equal(got, lf) {
		t.Fatalf("normalizeLF alteró un fichero LF: %q", got)
	}
	crlf := []byte("#!/bin/sh\r\necho ok\r\n")
	if got := normalizeLF(crlf); bytes.Contains(got, []byte("\r\n")) {
		t.Fatalf("normalizeLF dejó CRLF: %q", got)
	}
}
