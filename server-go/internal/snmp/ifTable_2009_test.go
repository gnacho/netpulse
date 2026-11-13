package snmp

// #2009: un switch gestionado expone su interfaz de CPU como ifType other(1),
// que no está en el mapa de no-físicas por la defensa de #1115. Pasaba el
// filtro `!p.family` (rack y panel de puertos) y sumaba un puerto fantasma al
// frontal. Se reconoce por nombre y SOLO cuando el ifType es other(1).

import "testing"

func TestIfFamilyForCPUInterface2009(t *testing.T) {
	cases := []struct {
		name  string
		ps    PortStats
		want  string
		notes string
	}{
		{
			name: "XS708T CPU (ifDescr)",
			ps: PortStats{Type: 1, Index: 17,
				Descr: "CPU Interface for Slot: 3 Port: 1", Name: "CPU Interface:  3/1"},
			want: "cpu",
		},
		{
			name: "S3300 CPU (ifDescr de unidad)",
			ps: PortStats{Type: 1, Index: 313,
				Descr: "CPU Interface for Unit: 0 Slot: 5 Port: 1", Name: "CPU Interface:  0/5/1"},
			want: "cpu",
		},
		{
			name: "nombre suelto cpu0",
			ps:   PortStats{Type: 1, Name: "cpu0"},
			want: "cpu",
		},
		{
			name: "other(1) con nombre normal sigue siendo físico",
			ps:   PortStats{Type: 1, Name: "Port 9", Descr: "10G Ethernet"},
			want: "",
		},
		{
			name: "puerto real que se llama CPU pero es ethernet: físico",
			ps:   PortStats{Type: 6, Name: "CPU Interface: 1/1"},
			want: "",
		},
		{
			name: "cpu con ifType propVirtual mantiene virtual",
			ps:   PortStats{Type: 53, Name: "cpu0"},
			want: "virtual",
		},
		{
			name: "lag sigue siendo lag",
			ps:   PortStats{Type: 161, Name: "po1"},
			want: "lag",
		},
	}
	for _, c := range cases {
		if got := ifFamilyFor(c.ps); got != c.want {
			t.Errorf("%s: ifFamilyFor(%+v) = %q, esperaba %q", c.name, c.ps, got, c.want)
		}
	}
}
