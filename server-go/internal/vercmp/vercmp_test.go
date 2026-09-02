package vercmp

import "testing"

func TestCmp(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2.20.0", "2.19.9", 1},
		{"2.19.9", "2.20.0", -1},
		{"v2.20.0", "2.20.0", 0},
		{"0.26.1-r3", "0.26.1", 0},
		{"v0.26.1", "0.26.0", 1},
		{"dev", "2.20.0", 0},
		{"2.20", "2.20.0", 0},
		{"", "1.0.0", 0},
	}
	for _, c := range cases {
		if got := Cmp(c.a, c.b); got != c.want {
			t.Errorf("Cmp(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestCmpBuild(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		// x.y.z manda igual que Cmp.
		{"2.25.0-437", "2.24.9-999", 1},
		{"2.24.0-443", "2.25.0-437", -1},
		// Mismo x.y.z: decide el build (#447).
		{"2.25.0-437", "2.25.0-443", -1},
		{"2.25.0-443", "2.25.0-437", 1},
		{"2.25.0-443", "2.25.0-443", 0},
		{"2.25.0+2", "2.25.0+10", -1},
		{"0.26.1-r3", "0.26.1-r4", -1},
		// Sin sufijo numérico → build 0 (desnuda < con build).
		{"2.25.0", "2.25.0-437", -1},
		{"v0.26.1", "0.26.1", 0},
		{"v0.26.1", "0.26.1-r1", -1},
		// Sufijo no numérico → build 0.
		{"2.25.0-beta", "2.25.0-437", -1},
		{"2.25.0-beta", "2.25.0", 0},
		// Malparseo → 0 (sin señal).
		{"dev", "2.25.0-437", 0},
		{"2.25", "2.25.0-437", 0},
		{"", "2.25.0-437", 0},
	}
	for _, c := range cases {
		if got := CmpBuild(c.a, c.b); got != c.want {
			t.Errorf("CmpBuild(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
