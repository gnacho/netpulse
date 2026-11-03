package httpapi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFirmwareBackupTimestampMatchesExactUnit(t *testing.T) {
	cases := []struct {
		name string
		slug string
		ts   int64
		ok   bool
	}{
		{"gateway-1793293266-gateway.tar.gz", "gateway", 1793293266, true},
		{"AP1-1793224785-netgear-wax220.tar.gz", "netgear-wax220", 1793224785, true},
		{"AP1-1793224785-netgear-wax220-2.tar.gz", "netgear-wax220-2", 1793224785, true},
		{"AP1-1793224785-netgear-wax220-2.tar.gz", "netgear-wax220", 0, false},
		{"netgear-wax220-1793224785.tar.gz", "netgear-wax220", 1793224785, true},
		{"netgear-wax220-2-1793087496.tar.gz", "netgear-wax220", 0, false},
		{"netgear-wax220-2-1793087496.tar.gz", "netgear-wax220-2", 1793087496, true},
		{"gateway-1793293266.tar.gz", "gateway", 1793293266, true},
		{"gateway-179329326-gateway.tar.gz", "gateway", 0, false},
		{"gateway.tar.gz", "gateway", 0, false},
	}
	for _, tc := range cases {
		ts, ok := firmwareBackupTimestamp(tc.name, tc.slug)
		if ts != tc.ts || ok != tc.ok {
			t.Errorf("firmwareBackupTimestamp(%q,%q) = (%d,%v), want (%d,%v)", tc.name, tc.slug, ts, ok, tc.ts, tc.ok)
		}
	}
}

func TestPruneFirmwareBackupsKeepsOnlyExactUnit(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 7; i++ {
		name := firmwareBackupFileName("AP1", "netgear-wax220", int64(1793224785+i))
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	other := firmwareBackupFileName("AP2", "netgear-wax220-2", 1793087496)
	if err := os.WriteFile(filepath.Join(dir, other), []byte("x"), 0o640); err != nil {
		t.Fatal(err)
	}

	pruneFirmwareBackups(dir, "netgear-wax220")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != maxFirmwareBackupsPerRouter+1 {
		t.Fatalf("quedan %d archivos, quiero %d", len(entries), maxFirmwareBackupsPerRouter+1)
	}
	if _, err := os.Stat(filepath.Join(dir, other)); err != nil {
		t.Fatalf("el backup de la otra unidad se borró: %v", err)
	}
	for _, want := range []string{
		firmwareBackupFileName("AP1", "netgear-wax220", 1793224789),
		firmwareBackupFileName("AP1", "netgear-wax220", 1793224791),
	} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Fatalf("falta el backup reciente %q: %v", want, err)
		}
	}
	oldest := firmwareBackupFileName("AP1", "netgear-wax220", 1793224785)
	if _, err := os.Stat(filepath.Join(dir, oldest)); !os.IsNotExist(err) {
		t.Fatalf("el backup más antiguo debería haberse podado: %v", err)
	}
}

func TestFirmwareBackupFileNameUsesReadableUnit(t *testing.T) {
	got := firmwareBackupFileName(" AP1 salón ", "netgear-wax220", 1793224785)
	want := "AP1-salón-1793224785-netgear-wax220.tar.gz"
	if got != want {
		t.Fatalf("nombre = %q, quiero %q", got, want)
	}
	if got := sanitizeBackupLabel(" --- "); got != "netpulse" {
		t.Fatalf("fallback = %q", got)
	}
}

func TestEnsureBackupDirGroupAccess(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "backups", "firmware")
	if err := ensureBackupDir(dir); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := st.Mode().Perm(); got != 0o770 {
		t.Fatalf("permisos = %o, quiero 770", got)
	}
}
