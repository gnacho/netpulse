package rack

import "testing"

func TestOverlaps(t *testing.T) {
	full := Footprint{UStart: 5, UHeight: 1, ColStart: 0, ColSpan: 12}
	cases := []struct {
		name string
		a, b Footprint
		want bool
	}{
		{"idénticas", full, full, true},
		{"U disjuntas", full, Footprint{UStart: 6, UHeight: 1, ColStart: 0, ColSpan: 12}, false},
		{"columnas disjuntas misma U",
			Footprint{UStart: 5, UHeight: 1, ColStart: 0, ColSpan: 6},
			Footprint{UStart: 5, UHeight: 1, ColStart: 6, ColSpan: 6}, false},
		{"solape parcial de columnas",
			Footprint{UStart: 5, UHeight: 1, ColStart: 0, ColSpan: 6},
			Footprint{UStart: 5, UHeight: 1, ColStart: 3, ColSpan: 6}, true},
		{"adyacentes en U sin compartir celda",
			Footprint{UStart: 5, UHeight: 2, ColStart: 0, ColSpan: 12},
			Footprint{UStart: 7, UHeight: 1, ColStart: 0, ColSpan: 12}, false},
	}
	for _, tc := range cases {
		if got := Overlaps(tc.a, tc.b); got != tc.want {
			t.Errorf("%s: Overlaps = %v; want %v", tc.name, got, tc.want)
		}
	}
}

func TestCanPlace(t *testing.T) {
	occupied := []Footprint{{UStart: 5, UHeight: 1, ColStart: 0, ColSpan: 6}}

	if !CanPlace(42, nil, Footprint{UStart: 10, UHeight: 2, ColStart: 0, ColSpan: 12}) {
		t.Error("colocación libre en rack vacío debe caber")
	}
	// Fuera del rack por arriba: U 42 + altura 2 en rack de 42U.
	if CanPlace(42, nil, Footprint{UStart: 42, UHeight: 2, ColStart: 0, ColSpan: 12}) {
		t.Error("huella que se sale por arriba del rack no debe caber")
	}
	// uStart 0 es inválido (1-based).
	if CanPlace(42, nil, Footprint{UStart: 0, UHeight: 1, ColStart: 0, ColSpan: 12}) {
		t.Error("uStart 0 no debe ser válido")
	}
	// Overflow de columnas.
	if CanPlace(42, nil, Footprint{UStart: 1, UHeight: 1, ColStart: 6, ColSpan: 12}) {
		t.Error("col_start 6 + span 12 excede las 12 columnas")
	}
	// Solape con la huella ocupada.
	if CanPlace(42, occupied, Footprint{UStart: 5, UHeight: 1, ColStart: 3, ColSpan: 6}) {
		t.Error("huella solapada con una existente no debe caber")
	}
	// La mitad libre de la U ocupada sí cabe.
	if !CanPlace(42, occupied, Footprint{UStart: 5, UHeight: 1, ColStart: 6, ColSpan: 6}) {
		t.Error("la mitad libre de una U compartida debe caber")
	}
}

func TestFindSlot(t *testing.T) {
	// Rack vacío: el drop cae en su sitio.
	f, ok := FindSlot(42, nil, 10, 0, 1, 12)
	if !ok || f.UStart != 10 || f.ColStart != 0 {
		t.Errorf("rack vacío: got %+v ok=%v; want U10 col0", f, ok)
	}

	// Drop sobre la mitad ocupada de una U: snap a la mitad libre de la MISMA U.
	occupied := []Footprint{{UStart: 10, UHeight: 1, ColStart: 0, ColSpan: 6}}
	f, ok = FindSlot(42, occupied, 10, 0, 1, 6)
	if !ok || f.UStart != 10 || f.ColStart != 6 {
		t.Errorf("snap a mitad libre: got %+v ok=%v; want U10 col6", f, ok)
	}

	// U completamente ocupada: camina hacia fuera, primero hacia abajo.
	occupied = []Footprint{{UStart: 10, UHeight: 1, ColStart: 0, ColSpan: 12}}
	f, ok = FindSlot(42, occupied, 10, 0, 1, 12)
	if !ok || f.UStart != 9 || f.ColStart != 0 {
		t.Errorf("U ocupada: got %+v ok=%v; want U9 col0 (la más cercana)", f, ok)
	}

	// Drop fuera del grid: clamp a los límites del rack.
	f, ok = FindSlot(42, nil, 99, 99, 1, 6)
	if !ok || f.UStart != 42 || f.ColStart != 6 {
		t.Errorf("clamp: got %+v ok=%v; want U42 col6", f, ok)
	}

	// Rack lleno: imposible, sin colisión silenciosa.
	var full []Footprint
	for u := 1; u <= 2; u++ {
		full = append(full, Footprint{UStart: u, UHeight: 1, ColStart: 0, ColSpan: 12})
	}
	if _, ok := FindSlot(2, full, 1, 0, 1, 12); ok {
		t.Error("rack lleno debe devolver ok=false")
	}

	// Dispositivo más alto que el rack.
	if _, ok := FindSlot(1, nil, 1, 0, 2, 12); ok {
		t.Error("dispositivo 2U en rack 1U debe devolver ok=false")
	}
}

func TestRackValid(t *testing.T) {
	valid := Rack{Name: "rack", UHeight: 12, WidthStandard: Width19, Numbering: NumBottomUp}
	if !valid.Valid() {
		t.Error("rack válido rechazado")
	}
	for _, mutate := range []func(*Rack){
		func(r *Rack) { r.UHeight = 0 },
		func(r *Rack) { r.Name = "" },
		func(r *Rack) { r.WidthStandard = "24" },
		func(r *Rack) { r.Numbering = "diagonal" },
	} {
		r := valid
		mutate(&r)
		if r.Valid() {
			t.Errorf("rack inválido aceptado: %+v", r)
		}
	}
}
