package agentbin

import (
	"errors"
	"io/fs"
	"testing"
)

// #1476: el .apk de OpenWrt 25.12 no se fabrica aqui (lo firma el CI), asi que
// el servidor solo puede servirlo cuando el operador lo deja junto al binario.
// Sin el, OpenAsset tiene que responder igual que Open sin binarios: ausencia,
// nunca un fichero vacio ni un error de otra clase.
func TestOpenAssetMissingIsNotExist(t *testing.T) {
	for _, ext := range []string{"apk", "noexiste"} {
		f, err := OpenAsset("arm64", ext)
		if err == nil {
			f.Close()
			t.Fatalf("OpenAsset(arm64, %q) devolvio un fichero; sin assets debe fallar", ext)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("OpenAsset(arm64, %q) error = %v, want fs.ErrNotExist", ext, err)
		}
	}
}

// El binario sigue abriendose con el nombre sin extension: Open y OpenAsset no
// comparten namespace y el helper extraido no cambio su contrato.
func TestOpenKeepsBinaryName(t *testing.T) {
	if _, err := Open("arm64"); err == nil {
		t.Fatal("sin binarios embebidos Open debe fallar")
	} else if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Open error = %v, want fs.ErrNotExist", err)
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
