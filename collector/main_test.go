// main_test.go — primeros tests del collector (contrato C4, Fase 2).
package main

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// Bug C4.3: colisión de slug → sufijo -2, -3… (nunca ignorar el segundo target).
func TestDedupeSlugs(t *testing.T) {
	targets := []Target{
		{Name: slug("Router A"), Addr: "10.0.0.1:22"},   // "router-a"
		{Name: slug("router_a"), Addr: "10.0.0.2:22"},   // colisiona: "router-a"
		{Name: slug("ROUTER A"), Addr: "10.0.0.3:22"},   // colisiona otra vez
		{Name: slug("Router A 2"), Addr: "10.0.0.4:22"}, // "router-a-2": colisiona con el sufijo
		{Name: slug("ap"), Addr: "10.0.0.5:22"},
	}
	got := dedupeSlugs(targets)
	var names []string
	for _, tg := range got {
		names = append(names, tg.Name)
	}
	want := []string{"router-a", "router-a-2", "router-a-3", "router-a-2-2", "ap"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("slugs = %v, quiero %v", names, want)
	}
	// Las direcciones se conservan (no se pierde ningún target).
	for i, tg := range got {
		if tg.Addr != targets[i].Addr {
			t.Errorf("target[%d].Addr = %s, quiero %s", i, tg.Addr, targets[i].Addr)
		}
	}
}

// spyReconcile monta callbacks que simulan el supervisor real sin DB ni red.
type spyReconcile struct {
	started  []Target
	stopped  []string
	canceled []string
}

func (s *spyReconcile) children(entries ...Target) map[string]childProc {
	m := map[string]childProc{}
	for _, e := range entries {
		name := e.Name
		m[name] = childProc{
			addr:   e.Addr,
			cancel: func() { s.canceled = append(s.canceled, name) },
		}
	}
	return m
}

func (s *spyReconcile) onStart(children map[string]childProc) func(Target) {
	return func(t Target) {
		s.started = append(s.started, t)
		children[t.Name] = childProc{addr: t.Addr, cancel: func() {}}
	}
}

func (s *spyReconcile) onStop() func(string) {
	return func(name string) { s.stopped = append(s.stopped, name) }
}

// Bug C4.2: un router que cambia de host reinicia su sonda (cancel + start).
func TestReconcileHostChange(t *testing.T) {
	s := &spyReconcile{}
	children := s.children(Target{Name: "gw", Addr: "192.168.1.1:22"})
	targets := []Target{{Name: "gw", Addr: "192.168.1.254:22"}} // mismo nombre, host nuevo

	reconcileTargets(children, targets, s.onStart(children), s.onStop())

	if !reflect.DeepEqual(s.canceled, []string{"gw"}) {
		t.Errorf("canceladas = %v, quiero [gw]", s.canceled)
	}
	if len(s.started) != 1 || s.started[0].Addr != "192.168.1.254:22" {
		t.Errorf("arrancadas = %+v, quiero gw con el host nuevo", s.started)
	}
	if children["gw"].addr != "192.168.1.254:22" {
		t.Errorf("children[gw].addr = %s, quiero el host nuevo", children["gw"].addr)
	}
	if len(s.stopped) != 0 {
		t.Errorf("eliminadas = %v, quiero ninguna (el target sigue existiendo)", s.stopped)
	}
}

// Sin cambios de host ni de lista: no se toca ninguna sonda.
func TestReconcileNoChanges(t *testing.T) {
	s := &spyReconcile{}
	children := s.children(Target{Name: "gw", Addr: "192.168.1.1:22"})
	reconcileTargets(children, []Target{{Name: "gw", Addr: "192.168.1.1:22"}}, s.onStart(children), s.onStop())
	if len(s.started)+len(s.stopped)+len(s.canceled) != 0 {
		t.Errorf("sin cambios no debe haber acciones: %+v", s)
	}
}

// Bug C4.4: al eliminar un target se para la sonda y se purga `last` (vía onStop).
func TestReconcileRemovePurges(t *testing.T) {
	s := &spyReconcile{}
	children := s.children(
		Target{Name: "gw", Addr: "192.168.1.1:22"},
		Target{Name: "ap", Addr: "192.168.1.2:22"},
	)
	last := map[string]probeResult{"gw": {OK: true}, "ap": {OK: true}}
	onStop := func(name string) {
		delete(last, name) // como hace el onStop real de main
		s.onStop()(name)
	}

	reconcileTargets(children, []Target{{Name: "gw", Addr: "192.168.1.1:22"}}, s.onStart(children), onStop)

	if !reflect.DeepEqual(s.canceled, []string{"ap"}) {
		t.Errorf("canceladas = %v, quiero [ap]", s.canceled)
	}
	if !reflect.DeepEqual(s.stopped, []string{"ap"}) {
		t.Errorf("onStop = %v, quiero [ap]", s.stopped)
	}
	if _, ok := children["ap"]; ok {
		t.Error("children aún contiene ap")
	}
	if _, ok := last["ap"]; ok {
		t.Error("last aún contiene ap (stale en /healthz y state.json)")
	}
	if _, ok := last["gw"]; !ok {
		t.Error("last perdió gw, que sigue activo")
	}
}

// El slug base no cambia con la dedup (regresión del helper slug()).
func TestSlug(t *testing.T) {
	cases := map[string]string{
		"Router A":     "router-a",
		"AP_Patio 2":   "ap-patio-2",
		"gw.local!":    "gwlocal",
		"Ya-Slug-Ok-1": "yaslugok1", // los guiones se eliminan (comportamiento actual)
	}
	for in, want := range cases {
		if got := slug(in); got != want {
			t.Errorf("slug(%q) = %q, quiero %q", in, got, want)
		}
	}
}

// EXTRA_TARGETS pasa por slug + dedup igual que los routers de la DB.
func TestExtraTargetsDedup(t *testing.T) {
	var targets []Target
	for _, extra := range []string{"internet=1.1.1.1:443", "Internet=8.8.8.8:443"} {
		name, addr, _ := strings.Cut(extra, "=")
		targets = append(targets, Target{Name: slug(name), Addr: addr})
	}
	got := dedupeSlugs(targets)
	if got[0].Name != "internet" || got[1].Name != "internet-2" {
		t.Errorf("extras dedup = %v, quiero [internet internet-2]", got)
	}
}

// smoke: reconcileTargets arranca targets nuevos con ctx real cancelable.
func TestReconcileStartsNew(t *testing.T) {
	children := map[string]childProc{}
	var started []string
	onStart := func(tg Target) {
		started = append(started, tg.Name)
		_, cancel := context.WithCancel(context.Background())
		children[tg.Name] = childProc{cancel: cancel, addr: tg.Addr}
	}
	reconcileTargets(children, []Target{{Name: "a", Addr: "1.1.1.1:22"}, {Name: "b", Addr: "2.2.2.2:22"}}, onStart, func(string) {})
	if len(started) != 2 || len(children) != 2 {
		t.Errorf("started=%v children=%d, quiero 2 y 2", started, len(children))
	}
}
