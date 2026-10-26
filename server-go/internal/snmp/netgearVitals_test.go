// netgearVitals_test.go — parsers de los vitales Netgear FastPath con
// muestras reales capturadas de un XS708T (firmware 7.0.0.24).
package snmp

import "testing"

func TestParseNetgearCpuUtil(t *testing.T) {
	vals := []string{
		"    5 Sec (  0.19%)   60 Sec (  0.16%)  300 Sec (  0.15%) ",
		"    5 Sec (  0.00%)   60 Sec (  0.18%)  300 Sec (  0.23%) ",
		"    5 Sec (  5.75%)   60 Sec (  5.39%)  300 Sec (  5.38%) ",
		"    5 Sec (  1.34%)   60 Sec (  0.22%)  300 Sec (  0.44%) ",
	}
	cpu, ok := ParseNetgearCpuUtil(vals)
	if !ok {
		t.Fatal("ok = false, want true con strings válidos")
	}
	// 0.19+0.00+5.75+1.34 = 7.28 → redondeo 7.
	if cpu != 7 {
		t.Fatalf("cpu = %d, want 7 (suma de la ventana de 5s)", cpu)
	}
}

func TestParseNetgearCpuUtilGarbage(t *testing.T) {
	if cpu, ok := ParseNetgearCpuUtil([]string{"firmware nuevo sin formato", ""}); ok || cpu != 0 {
		t.Fatalf("formato inparseable debe dar (0,false), tuve (%d,%v)", cpu, ok)
	}
}

func TestParseNetgearCpuUtilCapsAt100(t *testing.T) {
	vals := []string{"5 Sec ( 60.0%)", "5 Sec ( 55.0%)"}
	cpu, ok := ParseNetgearCpuUtil(vals)
	if !ok || cpu != 100 {
		t.Fatalf("cpu = %d/%v, want 100 techado", cpu, ok)
	}
}

func TestParseNetgearTemps(t *testing.T) {
	// XS708T: dos sensores, 48 y 56 °C; el punto caliente manda.
	temp, ok := ParseNetgearTemps([]int{48, 56})
	if !ok || temp != 56 {
		t.Fatalf("temp = %d/%v, want 56 (máximo de sensores)", temp, ok)
	}
	if _, ok := ParseNetgearTemps(nil); ok {
		t.Fatal("sin sensores ok debe ser false")
	}
}
