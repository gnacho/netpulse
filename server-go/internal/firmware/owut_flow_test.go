// owut_flow_test.go - tests del ciclo owut completo (#761): parse de
// versiones, comparadores, construcción del comando de upgrade e instalación.
package firmware

import (
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeSSHRunner struct {
	out string
	err error
	got string
}

func (f *fakeSSHRunner) Run(host, cmd string, timeout time.Duration) (string, error) {
	f.got = cmd
	return f.out, f.err
}

// owutVersionsOut reproduce el formato real de `owut versions` (verificado en
// un router OpenWrt 25.12 con owut 2026.07).
const owutVersionsOut = `Available 'version-to' values from https://sysupgrade.openwrt.org:
  22.03 release branch
    22.03.0-rc1
    22.03.7 (latest)
    22.03-SNAPSHOT
  23.05 release branch
    23.05.4
    23.05.5 (latest)
  24.10 release branch
    24.10.0
    24.10.2 (latest)
  25.12 release branch
    25.12.4
    25.12.5
    25.12.7 (latest)
`

func TestParseOwutVersions(t *testing.T) {
	vs := ParseOwutVersions(owutVersionsOut, "25.12.5")
	if len(vs) != 8 {
		t.Fatalf("esperaba 8 versiones estables (rc y SNAPSHOT fuera), got %d: %+v", len(vs), vs)
	}
	byVer := map[string]OwutVersion{}
	for _, v := range vs {
		byVer[v.Version] = v
	}
	if v := byVer["25.12.7"]; !v.Latest || !v.Newer || v.Branch != "25.12" {
		t.Fatalf("25.12.7: %+v", v)
	}
	if v := byVer["25.12.5"]; v.Latest || v.Newer {
		t.Fatalf("25.12.5 (instalada): %+v", v)
	}
	if v := byVer["23.05.5"]; v.Branch != "23.05" {
		t.Fatalf("23.05.5: %+v", v)
	}
	if _, ok := byVer["22.03.0-rc1"]; ok {
		t.Fatalf("un rc no debe aparecer: %+v", byVer["22.03.0-rc1"])
	}
	if _, ok := byVer["22.03-SNAPSHOT"]; ok {
		t.Fatalf("un SNAPSHOT no debe aparecer")
	}
}

func TestVersionCmp(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"25.12.5", "25.12.5", 0},
		{"25.12.7", "25.12.5", 1},
		{"23.05.5", "24.10.0", -1},
		{"25.12", "25.12.0", 0},
		{"25.12.1", "25.12", 1},
		{"", "", 0},
	}
	for _, c := range cases {
		if got := VersionCmp(c.a, c.b); got != c.want {
			t.Fatalf("VersionCmp(%q, %q) = %d, esperaba %d", c.a, c.b, got, c.want)
		}
	}
}

func TestMajorJump(t *testing.T) {
	cases := []struct {
		current, target string
		want            bool
	}{
		{"23.05.5", "24.10.0", true},
		{"23.05.5", "23.05.7", false},
		{"25.12.5", "25.12.7", false},
		{"", "24.10.0", false},
		{"23.05.5", "", false},
	}
	for _, c := range cases {
		if got := MajorJump(c.current, c.target); got != c.want {
			t.Fatalf("MajorJump(%q, %q) = %v, esperaba %v", c.current, c.target, got, c.want)
		}
	}
}

func TestOwutUpgradeCmd(t *testing.T) {
	if got := OwutUpgradeCmd("25.12.7", "25.12.5"); got != "owut upgrade -q -V '25.12.7'" {
		t.Fatalf("salto de versión: %q", got)
	}
	if got := OwutUpgradeCmd("25.12.5", "25.12.5"); got != "owut upgrade -q" {
		t.Fatalf("misma versión (rebuild de paquetes): %q", got)
	}
	if got := OwutUpgradeCmd("25.12.7", ""); got != "owut upgrade -q" {
		t.Fatalf("current desconocida: %q", got)
	}
}

func TestRunOwutUpgradeParsesExit(t *testing.T) {
	f := &fakeSSHRunner{out: "There are no changes to upgrade\n__owut_exit__=0\n"}
	exit, out, err := RunOwutUpgrade(f, "h", OwutUpgradeCmd("25.12.5", "25.12.5"))
	if err != nil || exit != 0 || !strings.Contains(out, "no changes") {
		t.Fatalf("exit=%d err=%v out=%q", exit, err, out)
	}
	if !strings.Contains(f.got, "__owut_exit__") {
		t.Fatalf("el comando debe llevar el wrapper de exit: %q", f.got)
	}
}

func TestRunOwutUpgradeTransportDeath(t *testing.T) {
	// La sesión SSH muere con el reinicio del sysupgrade: err != nil, sin
	// línea de exit (el caller decide con la duración).
	f := &fakeSSHRunner{err: errors.New("ssh: connection reset")}
	exit, _, err := RunOwutUpgrade(f, "h", "owut upgrade -q")
	if err == nil {
		t.Fatalf("debe propagar el error de transporte")
	}
	if exit != 0 {
		t.Fatalf("sin línea de exit el código es 0 por defecto, got %d", exit)
	}
}

func TestInstallOwutNoPackageManager(t *testing.T) {
	f := &fakeSSHRunner{out: "__no_pm__\n__owut_exit__=0\n"}
	if _, err := InstallOwut(f, "h"); err == nil {
		t.Fatalf("sin apk ni opkg debe devolver error")
	}
}

func TestInstallOwutFails(t *testing.T) {
	f := &fakeSSHRunner{out: "ERROR: unable to select packages\n__owut_exit__=1\n"}
	if _, err := InstallOwut(f, "h"); err == nil {
		t.Fatalf("exit 1 del gestor debe ser error")
	}
}
