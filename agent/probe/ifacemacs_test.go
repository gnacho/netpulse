package probe

import (
	"reflect"
	"testing"
)

// #1765: el equipo reporta las MACs de todas sus interfaces para que el
// servidor las absorba como identidades propias.
func TestParseIfaceMACs(t *testing.T) {
	out := "" +
		"lo 00:00:00:00:00:00\n" +
		"br-lan 94:83:C4:00:00:01\n" +
		"eth0 94:83:C4:00:00:02\n" +
		"eth0.20 94:83:C4:00:00:02\n" +
		"wlan0 94:83:C4:12:34:56\n" +
		"lan1 04:f4:1c:f3:46:66\n" +
		"basura\n" +
		"wan 94:83:C4:00:00:0\n"
	got := ParseIfaceMACs(out)
	want := []string{
		"04:F4:1C:F3:46:66",
		"94:83:C4:00:00:01",
		"94:83:C4:00:00:02",
		"94:83:C4:12:34:56",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// Una salida vacía no inventa identidades.
func TestParseIfaceMACsEmpty(t *testing.T) {
	if got := ParseIfaceMACs(""); len(got) != 0 {
		t.Fatalf("got %v, want empty", got)
	}
}
