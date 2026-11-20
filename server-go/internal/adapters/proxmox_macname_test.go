package adapters

import "testing"

func TestLooksLikeMACName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"AA:BB:CC:DD:EE:FF", true},
		{"aa-bb-cc-dd-ee-ff", true},
		{"aabbccddeeff", true},
		{"servidor-web", false},
		{"", false},
	}
	for _, c := range cases {
		if got := looksLikeMACName(c.name); got != c.want {
			t.Errorf("looksLikeMACName(%q) = %v, esperaba %v", c.name, got, c.want)
		}
	}
}
