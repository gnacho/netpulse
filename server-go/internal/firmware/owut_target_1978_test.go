// owut_target_1978_test.go - el check debe evaluar la version elegida en la
// UI (#1978) y decir algo cuando no deja salida (#1977).
package firmware

import (
	"strings"
	"testing"
)

func TestOwutCheckCmdFor(t *testing.T) {
	cases := []struct {
		target string
		want   string
		absent string
	}{
		{"25.12.6", "owut check -v -V '25.12.6'", ""},
		{" 24.10.8 ", "-V '24.10.8'", ""},
		{"", "owut check -v", "-V"},
		{"latest", "owut check -v", "-V"},
		{"25.12.6; reboot", "owut check -v", "-V"},
	}
	for _, c := range cases {
		got := OwutCheckCmdFor(c.target)
		if !strings.Contains(got, c.want) {
			t.Fatalf("OwutCheckCmdFor(%q) = %q, esperaba contener %q", c.target, got, c.want)
		}
		if c.absent != "" && strings.Contains(got, c.absent) {
			t.Fatalf("OwutCheckCmdFor(%q) = %q, no debia contener %q", c.target, got, c.absent)
		}
		if !strings.Contains(got, "__owut_exit__") {
			t.Fatalf("falta el wrapper de exit code: %q", got)
		}
	}
}

func TestCheckOwutTargetPassesVersion(t *testing.T) {
	r := &fakeRunner{rules: []fakeRule{
		{contains: "command -v owut", out: "/usr/bin/owut\n"},
		{contains: "owut check", out: checkOKOut},
		{contains: "attendedsysupgrade.owut.remove", out: ""},
	}}
	st := CheckOwutTarget(r, "192.168.1.50", "25.12.6")
	if !st.CheckRan || !st.Buildable || !st.UpgradeAvailable {
		t.Fatalf("status: %+v", st)
	}
	if !r.saw("owut check -v -V '25.12.6'") {
		t.Fatalf("el check no llevo el target: %v", r.cmds)
	}
	if st.Message != "" {
		t.Fatalf("sin mensaje cuando el check corre: %q", st.Message)
	}
}

func TestCheckOwutExplainsEmptyOutput(t *testing.T) {
	// owut presente, pero el comando no escribe nada (el caso del reporte).
	r := &fakeRunner{rules: []fakeRule{
		{contains: "command -v owut", out: "/usr/bin/owut\n"},
		{contains: "owut check", out: ""},
	}}
	st := CheckOwut(r, "192.168.1.50")
	if !st.OwutAvailable {
		t.Fatalf("owut deberia estar disponible: %+v", st)
	}
	if st.CheckRan {
		t.Fatalf("no deberia marcar el check como ejecutado: %+v", st)
	}
	if st.Message == "" {
		t.Fatal("sin salida debe haber un mensaje para la UI (#1977)")
	}
}
