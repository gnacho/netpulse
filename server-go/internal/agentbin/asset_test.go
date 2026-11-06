package agentbin

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"testing"
)

// #1476: el .apk de OpenWrt 25.12 no se fabrica aqui (lo firma el CI), asi que
// el servidor solo puede servirlo cuando el operador lo deja junto al binario.
// Una extension que nunca existe tiene que responder ausencia, nunca un
// fichero vacio ni un error de otra clase. No se comprueba "apk" a proposito:
// en un build con assets empaquetados el fichero puede estar y el test debe
// seguir siendo valido en los dos entornos.
func TestOpenAssetMissingIsNotExist(t *testing.T) {
	f, err := OpenAsset("arm64", "noexiste")
	if err == nil {
		f.Close()
		t.Fatal("OpenAsset(arm64, noexiste) devolvio un fichero; debe fallar")
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("OpenAsset(arm64, noexiste) error = %v, want fs.ErrNotExist", err)
	}
}

// El binario sigue abriendose con el nombre sin extension: Open y OpenAsset no
// comparten namespace y el helper extraido no cambio su contrato. Se compara
// contra openEmbedded en vez de exigir ausencia, porque un build con binarios
// embebidos (CI) si los tiene y un arbol de desarrollo no.
func TestOpenKeepsBinaryName(t *testing.T) {
	got, gotErr := Open("arm64")
	if got != nil {
		got.Close()
	}
	want, wantErr := openEmbedded("agents/netpulse-agent-arm64")
	if want != nil {
		want.Close()
	}
	if (gotErr == nil) != (wantErr == nil) {
		t.Fatalf("Open y openEmbedded discrepan: %v vs %v", gotErr, wantErr)
	}
	if gotErr != nil && !errors.Is(gotErr, fs.ErrNotExist) {
		t.Fatalf("Open error = %v, want fs.ErrNotExist", gotErr)
	}
}

// #1890: el agente se empaqueta con UPX para que quepa dos veces en la flash
// de un router con poco espacio (descarga + reemplazo). Un binario sin
// empaquetar pesa 7-9 MB y rompe esa actualización, así que un build que
// sirva agentes sin comprimir es un fallo, no una preferencia. Solo aplica
// cuando hay binarios embebidos: en un árbol de desarrollo no hay ninguno.
func TestEmbeddedAgentsAreUpxPacked(t *testing.T) {
	if !HasBinaries() {
		t.Skip("sin binarios embebidos (árbol de desarrollo)")
	}
	for _, arch := range []string{"amd64", "arm64", "arm", "mipsle", "mips"} {
		f, err := Open(arch)
		if err != nil {
			t.Fatalf("Open(%s): %v", arch, err)
		}
		b, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			t.Fatalf("leer %s: %v", arch, err)
		}
		// UPX mantiene la cabecera ELF válida y deja su marca al final.
		tail := b[len(b)-256:]
		if !bytes.Contains(tail, []byte("UPX!")) {
			t.Errorf("%s no está empaquetado con UPX (%d bytes)", arch, len(b))
		}
		if len(b) > 4<<20 {
			t.Errorf("%s pesa %d bytes; empaquetado debería rondar 2-3 MB", arch, len(b))
		}
	}
}

// normalizeArch sigue aplicandose en OpenAsset: aarch64 y arm64 piden el mismo
// asset, y un arch desconocido no inventa un nombre valido.
func TestOpenAssetNormalizesArch(t *testing.T) {
	_, errA := OpenAsset("aarch64", "apk")
	_, errB := OpenAsset("arm64", "apk")
	if (errA == nil) != (errB == nil) {
		t.Fatalf("aarch64 y arm64 deben resolver al mismo asset: %v vs %v", errA, errB)
	}
	if _, err := OpenAsset("sparc", "apk"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("arch desconocida: error = %v, want fs.ErrNotExist", err)
	}
}
