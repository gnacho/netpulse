package macaddr

import "testing"

func TestNormalize(t *testing.T) {
	const want = "AA:BB:CC:DD:EE:FF"
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"dos puntos en minúsculas", "aa:bb:cc:dd:ee:ff", want},
		{"dos puntos en mayúsculas", "AA:BB:CC:DD:EE:FF", want},
		{"con guiones", "aa-bb-cc-dd-ee-ff", want},
		{"12 hex sin separador", "aabbccddeeff", want},
		{"espacios alrededor", "  aa:bb:cc:dd:ee:ff\t\n", want},
		{"separadores mezclados", "aa:bb-cc:dd-ee:ff", ""},
		{"separador incorrecto", "aa.bb.cc.dd.ee.ff", ""},
		{"no hex", "gg:bb:cc:dd:ee:ff", ""},
		{"no hex sin separador", "aabbccddeefg", ""},
		{"demasiado corta", "aa:bb:cc:dd:ee", ""},
		{"demasiado larga", "aa:bb:cc:dd:ee:ff:00", ""},
		{"vacía", "", ""},
		{"nula aceptada", "00:00:00:00:00:00", "00:00:00:00:00:00"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Normalize(tc.in); got != tc.want {
				t.Errorf("Normalize(%q) = %q, esperaba %q", tc.in, got, tc.want)
			}
		})
	}
}
