package orchestr

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeRunner implementa CommandRunner para tests.
type fakeRunner struct {
	out string
	err error
}

func (f fakeRunner) Run(_, _ string, _ time.Duration) (string, error) {
	return f.out, f.err
}

// Fixtures reales capturados por SSH el 9-Ago-2026.

// Flint2 (GL.iNet GL-MT6000): fork gl-sdk4-adguardhome presente → abort.
const flint2Probe = `** WARNING: connection is not using a post-quantum key exchange algorithm.
===OPKG_INST===
adguardhome-conntrack - 0.107.73-r2
gl-sdk4-adguardhome - git-2026.110.30455-cffe4de-r1
gl-sdk4-ui-adguardhome - git-2026.077.31761-77d358e-r1
===APK_INST===
===ARCH===
aarch64
===OPKG_AVAIL===
===APK_AVAIL===
===BINARY===
present
===END==="
`

// Redmi AX6 (rt2) con OpenWrt stock limpio: nada instalado, no en feeds → binary.
const redmiAX6Probe = `===OPKG_INST===
===APK_INST===
===ARCH===
aarch64
===OPKG_AVAIL===
===APK_AVAIL===
===BINARY===
absent
===END===`

// Router con adguard-home en feeds opkg (sintético).
const opkgAvailProbe = `===OPKG_INST===
===APK_INST===
===ARCH===
mips
===OPKG_AVAIL===
adguard-home - 0.107.52-1
===APK_AVAIL===
===BINARY===
absent
===END===`

// Router OpenWrt 24+ (apk) con adguard-home en apk search.
const apkAvailProbe = `===OPKG_INST===
===APK_INST===
===ARCH===
x86_64
===OPKG_AVAIL===
===APK_AVAIL===
adguard-home-0.107.52-1
===BINARY===
absent
===END===`

// Binario oficial ya instalado (instalación previa nuestra, sin fork GL.iNet).
const binaryPresentProbe = `===OPKG_INST===
adguard-home - 0.107.52-1
===APK_INST===
===ARCH===
aarch64
===OPKG_AVAIL===
adguard-home - 0.107.52-1
===APK_AVAIL===
===BINARY===
present
===END===`

func TestParseFlint2DetectaForkGLiNet(t *testing.T) {
	sc := parseAdGuardProbe(flint2Probe)
	if !sc.ManagedByFirmware {
		t.Error("Flint2 debería tener ManagedByFirmware=true (gl-sdk4-adguardhome presente)")
	}
	if len(sc.GLinetPackages) != 2 {
		t.Errorf("esperaba 2 paquetes gl-sdk4-*, got %d: %v", len(sc.GLinetPackages), sc.GLinetPackages)
	}
	if sc.Arch != "aarch64" {
		t.Errorf("Arch: got %q want aarch64", sc.Arch)
	}
	if sc.AdguardSuffix != "arm64" {
		t.Errorf("AdguardSuffix: got %q want arm64", sc.AdguardSuffix)
	}
	if sc.OpkgAvailable {
		t.Error("Flint2 no tiene adguard-home en opkg feeds")
	}
	if !sc.BinaryPresent {
		t.Error("Flint2 tiene /usr/bin/AdGuardHome (del fork) → BinaryPresent esperado true")
	}
	if sc.InstallMethod() != "abort" {
		t.Errorf("InstallMethod: got %q want abort", sc.InstallMethod())
	}
}

func TestParseRedmiAX6EscenarioBinary(t *testing.T) {
	sc := parseAdGuardProbe(redmiAX6Probe)
	if sc.ManagedByFirmware {
		t.Error("Redmi AX6 limpio no debería tener ManagedByFirmware")
	}
	if sc.Arch != "aarch64" || sc.AdguardSuffix != "arm64" {
		t.Errorf("arch/suffix: got %q/%q want aarch64/arm64", sc.Arch, sc.AdguardSuffix)
	}
	if sc.BinaryPresent {
		t.Error("Redmi AX6 limpio no tiene /usr/bin/AdGuardHome")
	}
	if sc.OpkgAvailable || sc.ApkAvailable {
		t.Error("Redmi AX6 limpio no tiene adguard-home en feeds")
	}
	if sc.InstallMethod() != "binary" {
		t.Errorf("InstallMethod: got %q want binary", sc.InstallMethod())
	}
}

func TestParseOpkgAvailEscenarioOpkg(t *testing.T) {
	sc := parseAdGuardProbe(opkgAvailProbe)
	if sc.InstallMethod() != "opkg" {
		t.Errorf("InstallMethod: got %q want opkg", sc.InstallMethod())
	}
	if sc.AdguardSuffix != "mips" {
		t.Errorf("mips arch: suffix got %q want mips", sc.AdguardSuffix)
	}
}

func TestParseApkAvailEscenarioApk(t *testing.T) {
	sc := parseAdGuardProbe(apkAvailProbe)
	if sc.InstallMethod() != "apk" {
		t.Errorf("InstallMethod: got %q want apk", sc.InstallMethod())
	}
	if sc.AdguardSuffix != "amd64" {
		t.Errorf("x86_64 arch: suffix got %q want amd64", sc.AdguardSuffix)
	}
}

func TestParseBinaryPresentEscenarioNone(t *testing.T) {
	// Binario oficial ya instalado, sin fork → "none" (no reinstalar).
	sc := parseAdGuardProbe(binaryPresentProbe)
	if sc.ManagedByFirmware {
		t.Error("binario oficial no es fork → ManagedByFirmware false")
	}
	if sc.InstallMethod() != "none" {
		t.Errorf("InstallMethod: got %q want none (binario ya presente)", sc.InstallMethod())
	}
}

func TestParseProbeVacio(t *testing.T) {
	sc := parseAdGuardProbe("")
	if sc.ManagedByFirmware || sc.BinaryPresent || sc.OpkgAvailable || sc.ApkAvailable {
		t.Errorf("probe vacío debe dar escenario limpio: %+v", sc)
	}
	if sc.InstallMethod() != "binary" {
		t.Errorf("InstallMethod en probe vacío: got %q want binary (fallback)", sc.InstallMethod())
	}
}

func TestParseGLinetForkEnApk(t *testing.T) {
	// GL.iNet futuro que migre a apk: el fork podría listing por apk.
	probe := `===OPKG_INST===
===APK_INST===
gl-sdk4-adguardhome-0.107.73-r0
===ARCH===
aarch64
===OPKG_AVAIL===
===APK_AVAIL===
===BINARY===
present
===END===`
	sc := parseAdGuardProbe(probe)
	if !sc.ManagedByFirmware {
		t.Error("fork GL.iNet listado por apk debería detectarse")
	}
}

func TestArchToSuffix(t *testing.T) {
	cases := map[string]string{
		"aarch64":  "arm64",
		"arm64":    "arm64",
		"x86_64":   "amd64",
		"amd64":    "amd64",
		"armv7l":   "armv7",
		"mips":     "mips",
		"mipsle":   "mipsle",
		"mips64le": "mips64le",
		"ppc64le":  "ppc64le",
		"i686":     "386",
		"unknown":  "",
	}
	for arch, want := range cases {
		if got := archToSuffix(arch); got != want {
			t.Errorf("archToSuffix(%q): got %q want %q", arch, got, want)
		}
	}
}

func TestSectionMarker(t *testing.T) {
	cases := map[string]string{
		"===ARCH===":      "ARCH",
		"===OPKG_INST===": "OPKG_INST",
		"===END===":       "",
		"present":         "",
		"":                "",
		"==":              "",
		"====":            "",
		"===ARCH":         "", // no termina en ===
	}
	for line, want := range cases {
		if got := sectionMarker(line); got != want {
			t.Errorf("sectionMarker(%q): got %q want %q", line, got, want)
		}
	}
}

func TestDetectAdGuardPropagaError(t *testing.T) {
	run := fakeRunner{err: errors.New("ssh: connection refused")}
	_, err := DetectAdGuard(run, "192.168.1.1")
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("DetectAdGuard debe propagar el error de SSH: %v", err)
	}
}

func TestDetectAdGuardUsaRunner(t *testing.T) {
	run := fakeRunner{out: redmiAX6Probe}
	sc, err := DetectAdGuard(run, "rt2")
	if err != nil {
		t.Fatalf("DetectAdGuard inesperado error: %v", err)
	}
	if sc.Arch != "aarch64" {
		t.Errorf("DetectAdGuard no parseó arch: %+v", sc)
	}
}

// TestParseFreeMem: extrae la columna available de `free -m` (busybox).
func TestParseFreeMem(t *testing.T) {
	cases := []struct {
		name, out string
		want      int
	}{
		{
			"busybox_con_available",
			"              total        used        free      shared  buff/cache   available\nMem:           512         300          80          10         132         212\nSwap:            0           0           0",
			212,
		},
		{
			"free_antiguo_sin_available_fallback_free",
			"             total       used       free     shared    buffers     cached\nMem:           512        300        200         10          0          0\n-/+ buffers/cache:        300        212\nSwap:            0          0          0",
			200, // fallback a columna free
		},
		{
			"sin_mem",
			"===END===",
			0,
		},
		{
			"mem_vacio_entre_marcadores",
			"===MEM===\n===END===",
			0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parseFreeMem(c.out); got != c.want {
				t.Errorf("parseFreeMem: got %d want %d", got, c.want)
			}
		})
	}
}

// TestParseAdGuardProbeMembríaIncluyeAvailableRAM: el probe parsea RAM.
func TestParseAdGuardProbeIncluyeAvailableRAM(t *testing.T) {
	out := `===MEM===
              total        used        free      shared  buff/cache   available
Mem:           512         300          80          10         132         212
===END===`
	sc := parseAdGuardProbe(out)
	if sc.AvailableRAM != 212 {
		t.Errorf("AvailableRAM: got %d want 212", sc.AvailableRAM)
	}
}
