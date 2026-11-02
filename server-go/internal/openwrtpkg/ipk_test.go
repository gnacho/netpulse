package openwrtpkg

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// #1476: el paquete lleva su propia copia de los ficheros de deploy porque
// go:embed no puede salir del modulo. Si alguien edita el original y olvida la
// copia, el .ipk generado serviria un init o un config viejos.
func TestEmbeddedFilesMatchDeploy(t *testing.T) {
	pairs := map[string]string{
		"files/netpulse-agent.init":     "netpulse-agent.init",
		"files/netpulse-agent.config":   "netpulse-agent.config",
		"files/netpulse-agent.defaults": "netpulse-agent.defaults",
	}
	for embedded, deployName := range pairs {
		want, err := os.ReadFile(filepath.Join(repoRel, "deploy/openwrt/netpulse-agent/files", deployName))
		if err != nil {
			t.Fatalf("leer %s: %v", deployName, err)
		}
		got, err := files.ReadFile(embedded)
		if err != nil {
			t.Fatalf("leer %s del embed: %v", embedded, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s difiere de deploy/openwrt/netpulse-agent/files/%s; copia el fichero al paquete", embedded, deployName)
		}
	}
}

func TestOpenWrtArch(t *testing.T) {
	cases := map[string]string{
		"amd64":  "x86_64",
		"arm64":  "aarch64_cortex-a53",
		"arm":    "arm_cortex-a7_neon-vfpv4",
		"mipsle": "mipsel_24kc",
		"mips":   "mips_24kc",
		"riscv":  "",
	}
	for in, want := range cases {
		if got := OpenWrtArch(in); got != want {
			t.Errorf("OpenWrtArch(%q) = %q, want %q", in, got, want)
		}
	}
}

// #1476: el .ipk de OpenWrt 24.10 es un tar.gz exterior con tres miembros.
// Esta prueba lo desmonta igual que `tar tzf` sobre un paquete real.
func TestBuildIPKLayout(t *testing.T) {
	ipk, err := BuildIPK("arm64", "3.0.31", bytes.Repeat([]byte("binary"), 100))
	if err != nil {
		t.Fatalf("BuildIPK: %v", err)
	}
	outer := ungzip(t, ipk)
	members := listTar(t, outer)
	wantOrder := []string{"./debian-binary", "./data.tar.gz", "./control.tar.gz"}
	if len(members) != len(wantOrder) {
		t.Fatalf("miembros = %v, want %v", members, wantOrder)
	}
	for i, want := range wantOrder {
		if members[i] != want {
			t.Fatalf("miembro %d = %q, want %q", i, members[i], want)
		}
	}
	if got := string(tarFile(t, outer, "./debian-binary")); got != "2.0\n" {
		t.Errorf("debian-binary = %q", got)
	}

	control := ungzip(t, tarFile(t, outer, "./control.tar.gz"))
	controlFiles := listTar(t, control)
	for _, want := range []string{"./control", "./conffiles", "./postinst", "./prerm"} {
		if !contains(controlFiles, want) {
			t.Errorf("control.tar.gz no lleva %s: %v", want, controlFiles)
		}
	}
	controlBody := string(tarFile(t, control, "./control"))
	for _, want := range []string{
		"Package: netpulse-agent",
		"Version: 3.0.31",
		"Architecture: aarch64_cortex-a53",
		"Depends: libc, iw",
	} {
		if !bytes.Contains([]byte(controlBody), []byte(want)) {
			t.Errorf("control no contiene %q:\n%s", want, controlBody)
		}
	}
	if got := string(tarFile(t, control, "./conffiles")); got != "/etc/config/netpulse-agent\n" {
		t.Errorf("conffiles = %q, want la config UCI", got)
	}

	data := ungzip(t, tarFile(t, outer, "./data.tar.gz"))
	dataFiles := listTar(t, data)
	for _, want := range []string{
		"./usr/sbin/netpulse-agent",
		"./etc/init.d/netpulse-agent",
		"./etc/config/netpulse-agent",
		"./etc/uci-defaults/90-netpulse-agent",
	} {
		if !contains(dataFiles, want) {
			t.Errorf("data.tar.gz no lleva %s: %v", want, dataFiles)
		}
	}
	// opkg necesita el binario y el init ejecutables; la config, 0644.
	modes := tarModes(t, data)
	for name, want := range map[string]int64{
		"./usr/sbin/netpulse-agent":            0o755,
		"./etc/init.d/netpulse-agent":          0o755,
		"./etc/uci-defaults/90-netpulse-agent": 0o755,
		"./etc/config/netpulse-agent":          0o644,
	} {
		if got := modes[name]; got != want {
			t.Errorf("modo de %s = %o, want %o", name, got, want)
		}
	}
}

func TestBuildIPKDeterministic(t *testing.T) {
	bin := bytes.Repeat([]byte("x"), 64)
	a, err := BuildIPK("arm64", "1.2.3", bin)
	if err != nil {
		t.Fatalf("BuildIPK: %v", err)
	}
	b, err := BuildIPK("arm64", "1.2.3", bin)
	if err != nil {
		t.Fatalf("BuildIPK: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("dos generaciones del mismo paquete deben ser identicas")
	}
}

func TestBuildIPKRejectsUnsupported(t *testing.T) {
	if _, err := BuildIPK("riscv", "1.0.0", []byte("x")); err == nil {
		t.Fatal("esperaba error para una arquitectura no soportada")
	}
	if _, err := BuildIPK("arm64", "1.0.0", nil); err == nil {
		t.Fatal("esperaba error sin binario")
	}
}

func ungzip(t *testing.T, raw []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer zr.Close()
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gzip read: %v", err)
	}
	return out
}

func listTar(t *testing.T, raw []byte) []string {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(raw))
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		names = append(names, hdr.Name)
	}
	return names
}

func tarFile(t *testing.T, raw []byte, name string) []byte {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(raw))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		if hdr.Name != name {
			continue
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("tar read %s: %v", name, err)
		}
		return body
	}
	t.Fatalf("no encontrado %s", name)
	return nil
}

func tarModes(t *testing.T, raw []byte) map[string]int64 {
	t.Helper()
	tr := tar.NewReader(bytes.NewReader(raw))
	out := map[string]int64{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		out[hdr.Name] = hdr.Mode
	}
	return out
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
