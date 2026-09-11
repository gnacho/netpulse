// owut_test.go - tests de detección y check de owut (#695) con un Runner
// fakeable. Sin SSH real: un fake por subcadena de comando cubre owut ausente,
// owut presente + check OK con upgrade, y owut presente + check con fallo de
// infra ASU.
package firmware

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// errNoCommand simula `command -v owut` sin resultado (owut ausente).
// errSSHDown simula un fallo de transporte SSH (router caído).
var (
	errNoCommand = errors.New("exit status 127")
	errSSHDown   = errors.New("ssh 192.168.1.50: dial tcp: connection refused")
)

// fakeRunner devuelve, por subcadena del comando, una salida y un error fijos.
type fakeRunner struct {
	rules []fakeRule
	cmds  []string
}

type fakeRule struct {
	contains string
	out      string
	err      error
}

func (f *fakeRunner) Run(_ string, cmd string, _ time.Duration) (string, error) {
	f.cmds = append(f.cmds, cmd)
	for _, r := range f.rules {
		if strings.Contains(cmd, r.contains) {
			return r.out, r.err
		}
	}
	return "", nil
}

func (f *fakeRunner) saw(substr string) bool {
	for _, c := range f.cmds {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}

// checkOKOut simula la salida de `owut check` con upgrade disponible y
// buildable (exit 0), según la fuente oficial de owut.
const checkOKOut = `ASU-Server     https://sysupgrade.openwrt.org
Device         Redmi AX6
3 packages are out-of-date
It is safe to proceed with an upgrade (re-run with '--verbose' for details)
__owut_exit__=0
`

// checkInfraFailOut simula la salida de `owut check` cuando la infra ASU no
// puede construir (exit 1: paquetes missing en el target).
const checkInfraFailOut = `ASU-Server     https://sysupgrade.openwrt.org
Device         Redmi AX6
2 packages missing in target version, cannot upgrade
ERROR: Checks reveal errors, do not upgrade (re-run with '--verbose' for details)
__owut_exit__=1
`

func TestCheckOwutAbsent(t *testing.T) {
	r := &fakeRunner{rules: []fakeRule{
		{contains: "command -v owut", err: errNoCommand},
	}}
	got := CheckOwut(r, "192.168.1.50")
	if got.OwutAvailable {
		t.Fatalf("OwutAvailable = true, esperaba false")
	}
	if got.CheckRan {
		t.Fatalf("CheckRan = true, esperaba false sin owut")
	}
	if r.saw("owut check") {
		t.Fatalf("no debería lanzarse owut check si owut no existe")
	}
}

func TestCheckOwutPresentUpgradeAvailable(t *testing.T) {
	r := &fakeRunner{rules: []fakeRule{
		{contains: "command -v owut", out: "/usr/bin/owut\n"},
		{contains: "owut check", out: checkOKOut},
	}}
	got := CheckOwut(r, "192.168.1.50")
	if !got.OwutAvailable {
		t.Fatalf("OwutAvailable = false, esperaba true")
	}
	if !got.CheckRan {
		t.Fatalf("CheckRan = false, esperaba true")
	}
	if !got.UpgradeAvailable {
		t.Fatalf("UpgradeAvailable = false, esperaba true (out-of-date)")
	}
	if !got.Buildable {
		t.Fatalf("Buildable = false, esperaba true (exit 0)")
	}
	if strings.Contains(got.RawOutput, "__owut_exit__") {
		t.Fatalf("RawOutput no debe contener el marcador __owut_exit__: %q", got.RawOutput)
	}
	if !strings.Contains(got.RawOutput, "safe to proceed") {
		t.Fatalf("RawOutput perdió contenido: %q", got.RawOutput)
	}
}

func TestCheckOwutPresentInfraFailure(t *testing.T) {
	r := &fakeRunner{rules: []fakeRule{
		{contains: "command -v owut", out: "/usr/bin/owut\n"},
		{contains: "owut check", out: checkInfraFailOut},
	}}
	got := CheckOwut(r, "192.168.1.50")
	if !got.OwutAvailable {
		t.Fatalf("OwutAvailable = false, esperaba true")
	}
	if !got.CheckRan {
		t.Fatalf("CheckRan = false, esperaba true (el check corrió aunque fallara)")
	}
	if got.Buildable {
		t.Fatalf("Buildable = true, esperaba false (exit 1, cannot upgrade)")
	}
	if got.UpgradeAvailable {
		t.Fatalf("UpgradeAvailable = true, esperaba false (cannot upgrade)")
	}
	if !strings.Contains(got.RawOutput, "cannot upgrade") {
		t.Fatalf("RawOutput debe conservar el fallo: %q", got.RawOutput)
	}
}

func TestCheckOwutSSHFailure(t *testing.T) {
	// Detección OK, pero el check falla por transporte (router caído): el
	// resultado lleva el Error y CheckRan queda en false.
	r := &fakeRunner{rules: []fakeRule{
		{contains: "command -v owut", out: "/usr/bin/owut\n"},
		{contains: "owut check", err: errSSHDown},
	}}
	got := CheckOwut(r, "192.168.1.50")
	if !got.OwutAvailable {
		t.Fatalf("OwutAvailable = false, esperaba true")
	}
	if got.CheckRan {
		t.Fatalf("CheckRan = true, esperaba false (SSH caído)")
	}
	if got.Error == "" {
		t.Fatalf("Error vacío, esperaba el fallo de transporte")
	}
}

func TestCheckOwutNilRunner(t *testing.T) {
	got := CheckOwut(nil, "192.168.1.50")
	if got.OwutAvailable || got.CheckRan || got.Buildable || got.UpgradeAvailable {
		t.Fatalf("runner nil debe devolver todo a false: %+v", got)
	}
}
