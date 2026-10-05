package agentbin

import (
	"testing"
	"testing/fstest"
)

func TestOpenArmVariants(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64", "arm", "armv7", "armv7l", "aarch64", "x86_64", "mipsle", "mips"} {
		f, err := Open(arch)
		if err != nil {
			t.Fatalf("Open(%q) failed: %v", arch, err)
		}
		st, _ := f.Stat()
		if st.Size() == 0 {
			t.Fatalf("Open(%q) size 0", arch)
		}
		f.Close()
		t.Logf("Open(%q) ok size=%d", arch, st.Size())
	}
}

// #1246: guard de coherencia versión embebida.
func TestVerifyFSVersion(t *testing.T) {
	good := []byte("ELF-fake netpulse-agent version 3.0.11 build xyz")
	fsysOK := fstest.MapFS{
		"agents/netpulse-agent-amd64": {Data: good},
		"agents/netpulse-agent-arm64": {Data: good},
	}
	if ok, bad := verifyFSVersion(fsysOK, "3.0.11"); !ok || bad != "" {
		t.Fatalf("binarios coherentes: ok=%v bad=%q", ok, bad)
	}

	fsysStale := fstest.MapFS{
		"agents/netpulse-agent-amd64": {Data: []byte("ELF-fake version 3.0.10 older")},
		"agents/netpulse-agent-arm64": {Data: good},
	}
	ok, bad := verifyFSVersion(fsysStale, "3.0.11")
	if ok || bad != "amd64" {
		t.Fatalf("stale amd64: ok=%v bad=%q", ok, bad)
	}

	// Dev default / vacío: nada que verificar.
	if ok, bad := verifyFSVersion(fsysStale, "0.1.0"); !ok || bad != "" {
		t.Fatalf("versión default: ok=%v bad=%q", ok, bad)
	}
	if ok, _ := verifyFSVersion(fsysStale, ""); !ok {
		t.Fatal("versión vacía debe pasar")
	}
}

func TestEmbeddedMismatchFlag(t *testing.T) {
	SetEmbeddedMismatch(true)
	if !EmbeddedMismatch() {
		t.Fatal("mismatch true no se refleja")
	}
	SetEmbeddedMismatch(false)
	if EmbeddedMismatch() {
		t.Fatal("mismatch false no se refleja")
	}
}
