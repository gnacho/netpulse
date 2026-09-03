package agentbin

import "testing"

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
