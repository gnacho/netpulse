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

func TestIsLocallyAdministered(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"02 administrada localmente", "02:11:22:33:44:55", true},
		{"06 administrada localmente", "06:11:22:33:44:55", true},
		{"0A administrada localmente", "0A:11:22:33:44:55", true},
		{"0E administrada localmente", "0E:11:22:33:44:55", true},
		{"F2 administrada localmente", "F2:11:22:33:44:55", true},
		{"00 universal", "00:11:22:33:44:55", false},
		{"04 universal", "04:11:22:33:44:55", false},
		{"AC universal", "AC:11:22:33:44:55", false},
		{"minúsculas y guiones local", "0a-bb-cc-dd-ee-ff", true},
		{"minúsculas y guiones universal", "ac-bb-cc-dd-ee-ff", false},
		{"no es MAC", "02:11:22", false},
		{"vacía", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsLocallyAdministered(tc.in); got != tc.want {
				t.Errorf("IsLocallyAdministered(%q) = %v, esperaba %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeLenient(t *testing.T) {
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
		{"cadena que no es MAC", "  ab-cd  ", "AB:CD"},
		{"vacía", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeLenient(tc.in); got != tc.want {
				t.Errorf("NormalizeLenient(%q) = %q, esperaba %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormalizeSeparated(t *testing.T) {
	const want = "AA:BB:CC:DD:EE:FF"
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"dos puntos", "aa:bb:cc:dd:ee:ff", want},
		{"guiones", "AA-BB-CC-DD-EE-FF", want},
		{"espacios alrededor", "  aa:bb:cc:dd:ee:ff ", want},
		{"12 hex sin separador rechazado", "aabbccddeeff", ""},
		{"separadores mezclados", "aa:bb-cc:dd-ee:ff", ""},
		{"no hex", "gg:bb:cc:dd:ee:ff", ""},
		{"vacía", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeSeparated(tc.in); got != tc.want {
				t.Errorf("NormalizeSeparated(%q) = %q, esperaba %q", tc.in, got, tc.want)
			}
		})
	}
}
