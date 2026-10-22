// temp_test.go - selección de la zona térmica de la CPU (#1484).
//
// El comando se ejecuta contra un sysfs falso (misma cadena con la ruta
// reescrita): así se prueba el comportamiento real del shell, no solo que la
// constante contenga ciertas palabras.
package probe

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeThermal construye un /sys/class/thermal de mentira con las zonas dadas.
func fakeThermal(t *testing.T, zones map[string][2]string) string {
	t.Helper()
	root := t.TempDir()
	for name, z := range zones {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "type"), []byte(z[0]+"\n"), 0o644); err != nil {
			t.Fatalf("type: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "temp"), []byte(z[1]+"\n"), 0o644); err != nil {
			t.Fatalf("temp: %v", err)
		}
	}
	return root
}

func runCmdTemp(t *testing.T, root string) string {
	t.Helper()
	cmd := strings.ReplaceAll(CmdTemp, "/sys/class/thermal", root)
	out, err := exec.Command("sh", "-c", cmd).Output()
	if err != nil {
		t.Fatalf("CmdTemp: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// El caso del issue: x86 con acpitz en la zona 0 y x86_pkg_temp en la 2. Debe
// ganar la zona de CPU aunque la primera legible sea el ambiente ACPI.
func TestCmdTempPrefersCPUZone(t *testing.T) {
	root := fakeThermal(t, map[string][2]string{
		"thermal_zone0": {"acpitz", "27800"},
		"thermal_zone1": {"TCPU_PCI", "63000"},
		"thermal_zone2": {"x86_pkg_temp", "63000"},
	})
	if got := runCmdTemp(t, root); got != "63000" {
		t.Fatalf("temperatura %q, want 63000 (zona de CPU, no acpitz)", got)
	}
	if c := ParseTempC(runCmdTemp(t, root)); c == nil || *c != 63 {
		t.Fatalf("ParseTempC: %v, want 63", c)
	}
}

// ARM/SBC: cpu-thermal también entra por prioridad.
func TestCmdTempPrefersArmCPUZone(t *testing.T) {
	root := fakeThermal(t, map[string][2]string{
		"thermal_zone0": {"soc-thermal", "41000"},
		"thermal_zone1": {"cpu-thermal", "52000"},
	})
	if got := runCmdTemp(t, root); got != "52000" {
		t.Fatalf("temperatura %q, want 52000 (cpu-thermal)", got)
	}
}

// Sin zona de CPU: se conserva el comportamiento de siempre (primera legible).
func TestCmdTempFallsBackToFirstReadable(t *testing.T) {
	root := fakeThermal(t, map[string][2]string{
		"thermal_zone0": {"acpitz", "27800"},
		"thermal_zone1": {"iwlwifi_1", "45000"},
	})
	if got := runCmdTemp(t, root); got != "27800" {
		t.Fatalf("temperatura %q, want 27800 (fallback a la primera zona)", got)
	}
}

// Sin zonas legibles la salida va vacía y ParseTempC devuelve nil (no 0).
func TestCmdTempNoZones(t *testing.T) {
	root := t.TempDir()
	if got := runCmdTemp(t, root); got != "" {
		t.Fatalf("salida %q, want vacía", got)
	}
	if c := ParseTempC(runCmdTemp(t, root)); c != nil {
		t.Fatalf("ParseTempC %v, want nil", *c)
	}
}
