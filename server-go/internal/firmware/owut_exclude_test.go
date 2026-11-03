package firmware

import (
	"errors"
	"strings"
	"testing"
)

// #1769: añadir un paquete que el router no tenía todavía escribe la lista y
// hace commit. Uno que ya estaba no se duplica.
func TestAddOwutRemovePkgsAppendsAndCommits(t *testing.T) {
	r := &fakeRunner{rules: []fakeRule{
		{contains: "uci show attendedsysupgrade.owut.remove", out: "attendedsysupgrade.owut.remove='netgrip'\n"},
		{contains: "uci -q add_list", out: ""},
	}}
	got, err := AddOwutRemovePkgs(r, "192.168.1.50", []string{"netgrip", "bandix"})
	if err != nil {
		t.Fatalf("AddOwutRemovePkgs: %v", err)
	}
	if !r.saw("uci -q add_list attendedsysupgrade.owut.remove=bandix") {
		t.Error("no añadió el paquete nuevo")
	}
	if r.saw("add_list attendedsysupgrade.owut.remove=netgrip") {
		t.Error("duplicó un paquete que ya estaba en la lista")
	}
	if !r.saw("uci -q commit attendedsysupgrade") {
		t.Error("no hizo commit de la configuración")
	}
	if len(got) != 1 || got[0] != "netgrip" {
		t.Fatalf("lista devuelta = %v", got)
	}
}

// Sin paquetes nuevos no se toca la configuración del router.
func TestAddOwutRemovePkgsSkipsWhenAlreadyPresent(t *testing.T) {
	r := &fakeRunner{rules: []fakeRule{
		{contains: "uci show attendedsysupgrade.owut.remove", out: "attendedsysupgrade.owut.remove='netgrip'\n"},
	}}
	if _, err := AddOwutRemovePkgs(r, "192.168.1.50", []string{"netgrip"}); err != nil {
		t.Fatalf("AddOwutRemovePkgs: %v", err)
	}
	if r.saw("add_list") || r.saw("commit") {
		t.Fatal("escribió en el router sin paquetes nuevos")
	}
}

// La lista llega del navegador: nada que no sea un nombre de paquete entra en
// la línea de shell.
func TestAddOwutRemovePkgsRejectsInjection(t *testing.T) {
	for _, bad := range []string{"a;rm -rf /", "$(id)", "`id`", "a b", "../x"} {
		r := &fakeRunner{}
		_, err := AddOwutRemovePkgs(r, "192.168.1.50", []string{bad})
		if !errors.Is(err, ErrOwutInvalidPkg) {
			t.Errorf("AddOwutRemovePkgs(%q) = %v, esperaba ErrOwutInvalidPkg", bad, err)
		}
		if r.saw("uci") {
			t.Errorf("ejecutó un comando con %q", bad)
		}
	}
}

// Un fallo del router se propaga y no se inventa una lista nueva.
func TestAddOwutRemovePkgsPropagatesError(t *testing.T) {
	r := &fakeRunner{rules: []fakeRule{
		{contains: "uci show attendedsysupgrade.owut.remove", out: "attendedsysupgrade.owut.remove='netgrip'\n"},
		{contains: "uci -q add_list", err: errors.New("uci: I/O error")},
	}}
	got, err := AddOwutRemovePkgs(r, "192.168.1.50", []string{"bandix"})
	if err == nil {
		t.Fatal("esperaba error del router")
	}
	if len(got) != 1 || !strings.Contains(got[0], "netgrip") {
		t.Fatalf("lista devuelta tras el fallo = %v", got)
	}
}
