package adapters

import "testing"

// #1894: el script de instalación cuenta todo por stderr. Sin juntarlo con
// stdout, la salida que iba al journal y a la UI llegaba vacía.
func TestMergeScriptOutput_1894(t *testing.T) {
	cases := []struct {
		name           string
		stdout, stderr string
		want           string
	}{
		{"solo stdout", "arrancado\n", "", "arrancado\n"},
		{"solo stderr", "", "fallo\n", "fallo\n"},
		{"sin salto final en stdout", "arrancado", "fallo\n", "arrancado\nfallo\n"},
		{"los dos", "arrancado\n", "aviso\n", "arrancado\naviso\n"},
		{"vacio", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mergeScriptOutput(c.stdout, c.stderr); got != c.want {
				t.Fatalf("mergeScriptOutput(%q, %q) = %q, queria %q", c.stdout, c.stderr, got, c.want)
			}
		})
	}
}
