// owut_dup_1978_test.go - el -r duplicado de #1978.
//
// owut declara -r como opción exclusiva (storex) y además carga
// attendedsysupgrade.owut.remove como argumento por defecto (add_config). Si
// la lista está en la config UCI y el comando la repite con -r, owut aborta
// con "may only be used once per invocation". La fuente de verdad es la UCI.
package firmware

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// uciStateRunner simula uci con estado: add_list añade y show devuelve lo que
// hay, que es lo que hace el router de verdad.
type uciStateRunner struct {
	remove []string
	cmds   []string
	failOn string
}

func (r *uciStateRunner) Run(host, cmd string, _ time.Duration) (string, error) {
	r.cmds = append(r.cmds, cmd)
	if r.failOn != "" && strings.Contains(cmd, r.failOn) {
		return "", errors.New("uci failed")
	}
	switch {
	case strings.Contains(cmd, "add_list"):
		for _, part := range strings.Split(cmd, "&&") {
			part = strings.TrimSpace(part)
			if !strings.HasPrefix(part, "uci -q add_list attendedsysupgrade.owut.remove=") {
				continue
			}
			pkg := strings.TrimPrefix(part, "uci -q add_list attendedsysupgrade.owut.remove=")
			known := false
			for _, p := range r.remove {
				if p == pkg {
					known = true
				}
			}
			if !known {
				r.remove = append(r.remove, pkg)
			}
		}
		return "", nil
	case strings.Contains(cmd, "uci show attendedsysupgrade.owut.remove"):
		if len(r.remove) == 0 {
			return "", nil
		}
		quoted := make([]string, 0, len(r.remove))
		for _, p := range r.remove {
			quoted = append(quoted, "'"+p+"'")
		}
		return "attendedsysupgrade.owut.remove=" + strings.Join(quoted, " "), nil
	}
	return "", nil
}

func TestEnsureOwutRemoveInUCIWritesAndReports(t *testing.T) {
	r := &uciStateRunner{}
	if !EnsureOwutRemoveInUCI(r, "h", []string{"netgrip"}) {
		t.Fatalf("con la escritura hecha debe devolver true: %v", r.cmds)
	}
	if len(r.remove) != 1 || r.remove[0] != "netgrip" {
		t.Fatalf("la lista quedo en %v", r.remove)
	}
	sawCommit := false
	for _, c := range r.cmds {
		if strings.Contains(c, "uci -q commit attendedsysupgrade") {
			sawCommit = true
		}
	}
	if !sawCommit {
		t.Fatalf("falta el commit: %v", r.cmds)
	}
}

func TestEnsureOwutRemoveInUCILeavesAnExistingListAlone(t *testing.T) {
	r := &uciStateRunner{remove: []string{"netgrip"}}
	if !EnsureOwutRemoveInUCI(r, "h", []string{"netgrip"}) {
		t.Fatal("la lista ya estaba y debe devolver true")
	}
	for _, c := range r.cmds {
		if strings.Contains(c, "add_list") {
			t.Fatalf("no debe reescribir lo que ya está: %v", r.cmds)
		}
	}
}

func TestEnsureOwutRemoveInUCIReportsFalseWhenWriteFails(t *testing.T) {
	r := &uciStateRunner{failOn: "add_list"}
	if EnsureOwutRemoveInUCI(r, "h", []string{"netgrip"}) {
		t.Fatal("si la escritura falla hay que volver al -r")
	}
}

func TestEnsureOwutRemoveInUCIIgnoresEmptyLists(t *testing.T) {
	r := &uciStateRunner{}
	if EnsureOwutRemoveInUCI(r, "h", []string{"", " "}) {
		t.Fatal("sin paquetes no hay nada que dejar en UCI")
	}
	if len(r.cmds) != 0 {
		t.Fatalf("no debe tocar el router: %v", r.cmds)
	}
}

// El comando construido no repite -r cuando la lista ya está en UCI.
func TestOwutCommandsSkipRemoveFlagWhenInUCI(t *testing.T) {
	pkgs := []string{"netgrip", "bandix"}
	if got := OwutDownloadCmd("25.12.6", "24.10.8", pkgs, true); strings.Contains(got, "-r") {
		t.Fatalf("download con la lista en UCI no lleva -r: %q", got)
	}
	if got := OwutUpgradeCmd("25.12.6", "24.10.8", pkgs, true); strings.Contains(got, "-r") {
		t.Fatalf("upgrade con la lista en UCI no lleva -r: %q", got)
	}
	if got := OwutDownloadCmd("25.12.6", "24.10.8", pkgs, false); !strings.Contains(got, "-r 'netgrip bandix'") {
		t.Fatalf("sin UCI el flag sigue siendo necesario: %q", got)
	}
}

// #1978: el check expone el comando exacto que se lanzó.
func TestOwutCheckReportsTheCommand(t *testing.T) {
	r := &fakeRunner{rules: []fakeRule{
		{contains: "command -v owut", out: "/usr/bin/owut\n"},
		{contains: "owut check", out: checkOKOut},
		{contains: "attendedsysupgrade.owut.remove", out: ""},
	}}
	st := CheckOwutTarget(r, "192.168.1.50", "25.12.6")
	if st.Command != "owut check -v -V '25.12.6'" {
		t.Fatalf("comando reportado: %q", st.Command)
	}
}
