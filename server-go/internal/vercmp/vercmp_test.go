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
