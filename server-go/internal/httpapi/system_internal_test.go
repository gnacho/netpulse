package httpapi

import "testing"

// Tests de los parseadores puros de /api/system/info (SPEC-65 D65-6) con
// fixtures literales de /etc/os-release, /proc/cpuinfo y /proc/meminfo.

func TestParseOsReleasePretty(t *testing.T) {
	cases := []struct {
		name, content, want string
	}{
		{"debian", "NAME=\"Debian GNU/Linux\"\nVERSION_ID=\"12\"\nPRETTY_NAME=\"Debian GNU/Linux 12 (bookworm)\"\n", "Debian GNU/Linux 12 (bookworm)"},
		{"sin comillas", "PRETTY_NAME=Ubuntu 24.04 LTS\nNAME=Ubuntu\n", "Ubuntu 24.04 LTS"},
		{"openwrt", "NAME=\"OpenWrt\"\nPRETTY_NAME=\"OpenWrt 24.10.1\"\n", "OpenWrt 24.10.1"},
		{"ausente", "NAME=\"Algo\"\nVERSION_ID=\"1\"\n", ""},
		{"vacío", "", ""},
	}
	for _, c := range cases {
		if got := parseOsReleasePretty(c.content); got != c.want {
			t.Fatalf("%s: parseOsReleasePretty=%q, want %q", c.name, got, c.want)
		}
	}
}

func TestParseCPUModel(t *testing.T) {
	x86 := "processor\t: 0\nvendor_id\t: GenuineIntel\nmodel name\t: Intel(R) N100\n\ncpu cores\t: 4\n"
	if got := parseCPUModel(x86); got != "Intel(R) N100" {
		t.Fatalf("x86: %q", got)
	}
	arm := "processor\t: 0\nBogoMIPS\t: 38.40\nFeatures\t: fp asimd\nCPU implementer\t: 0x41\nModel\t\t: Raspberry Pi 4 Model B Rev 1.4\n"
	if got := parseCPUModel(arm); got != "Raspberry Pi 4 Model B Rev 1.4" {
		t.Fatalf("arm: %q", got)
	}
	// model name gana aunque haya varias entradas (la primera)
	multi := "model name\t: Fake CPU A\n\nmodel name\t: Fake CPU B\n"
	if got := parseCPUModel(multi); got != "Fake CPU A" {
		t.Fatalf("multi: %q", got)
	}
	if got := parseCPUModel("nada\nque\tvalga\n"); got != "" {
		t.Fatalf("sin modelo: %q", got)
	}
}

func TestParseMemTotalMB(t *testing.T) {
	mem := "MemTotal:        8175476 kB\nMemFree:         1234567 kB\nMemAvailable:    4000000 kB\n"
	if got := parseMemTotalMB(mem); got != 8175476/1024 {
		t.Fatalf("memTotal: %d", got)
	}
	if got := parseMemTotalMB("MemFree: 10 kB\n"); got != 0 {
		t.Fatalf("sin MemTotal: %d", got)
	}
	if got := parseMemTotalMB("MemTotal: abc kB\n"); got != 0 {
		t.Fatalf("MemTotal inválido: %d", got)
	}
	if got := parseMemTotalMB(""); got != 0 {
		t.Fatalf("vacío: %d", got)
	}
}
