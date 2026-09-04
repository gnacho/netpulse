// mem_test.go — contraste de memUsagePct con mediciones reales (issue #513).
package adapters

import "testing"

func TestMemUsagePct(t *testing.T) {
	cases := []struct {
		name                                  string
		total, free, buff, cach, av, usedProc float64
		want                                  int
	}{
		{
			// Con usedProc (RSS de procesos) se usa ese valor directamente: es
			// lo que el usuario percibe como uso real (netgrip #279).
			name: "rt3 con usedProc rss", total: 414264, free: 57364, buff: 0, cach: 94136, av: 99684, usedProc: 78000, want: 19,
		},
		{
			// Sin usedProc pero con Cached (fórmula clásica) baja del 79% al 67%.
			name: "rt3 con cached", total: 414264, free: 45780, buff: 0, cach: 90464, av: 88088, want: 67,
		},
		{
			// Sin Cached (payload viejo/ubus): se mantiene el comportamiento
			// anterior basado en MemAvailable.
			name: "rt3 fallback available", total: 414264, free: 45780, buff: 0, cach: 0, av: 88088, want: 79,
		},
		{
			// rt4 AX6: 60% con la fórmula previa; la clásica baja.
			name: "rt4 con cached", total: 414264, free: 120000, buff: 0, cach: 48876, av: 165268, want: 59,
		},
		{
			// Sin available, cae a free+buffered.
			name: "sin available", total: 1000, free: 100, buff: 50, cach: 0, av: 0, want: 85,
		},
		{
			// usedProc mayor que total se ignora y cae a la fórmula clásica
			// (con Cached): used = total - free - buffered - cached.
			name: "usedProc invalido", total: 1000, free: 100, buff: 0, cach: 400, av: 0, usedProc: 5000, want: 50,
		},
		{name: "cached mayor que el resto", total: 100, free: 0, buff: 0, cach: 500, av: 0, want: 0},
		{name: "total cero", total: 0, free: 0, buff: 0, cach: 0, av: 0, want: 0},
	}
	for _, tc := range cases {
		got := memUsagePct(tc.total, tc.free, tc.buff, tc.cach, tc.av, tc.usedProc)
		if got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}
