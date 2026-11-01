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
	if !bytes.Equal(want, Script()) {
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
