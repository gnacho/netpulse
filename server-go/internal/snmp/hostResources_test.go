// hostResources_test.go — parsers de HOST-RESOURCES con la muestra real de un
// CRS305 (RouterOS 7.25) capturada por #1725, y los casos en los que el árbol
// no existe o no dice nada útil.
package snmp

import (
	"math/big"
	"testing"

	"github.com/gosnmp/gosnmp"
)

// Walk real (192.168.2.15, community del SNMP del equipo):
//
//	hrProcessorLoad.1 = 1  (cpu0)
//	hrProcessorLoad.2 = 1  (cpu1)
//	hrStorageDescr.65536 = "main memory"  hrStorageSize.65536 = 524288
//	hrStorageUsed.65536  = 89840          allocationUnits = 1024 B
//	hrStorageDescr.131072 = "system disk" (16384 KB)
//
// → CPU 1 %, RAM 89840/524288 = 17 %. El propio API de RouterOS decía
// cpu-load=1 y free-memory=446193664 de total=536870912 (17 % usado): las dos
// fuentes cuadran.
func TestParseHrProcessorLoadsRealRouterOS(t *testing.T) {
	cpu, ok := ParseHrProcessorLoads([]int{1, 1})
	if !ok || cpu != 1 {
		t.Fatalf("cpu = %d/%v, want 1/true", cpu, ok)
	}
}

// El máximo, no la media: un núcleo al 73 % en un dual es un equipo ocupado, y
// la media (37) escondería justo lo que la tarjeta tiene que avisar.
func TestParseHrProcessorLoadsTakesMax(t *testing.T) {
	cpu, ok := ParseHrProcessorLoads([]int{2, 73, 15})
	if !ok || cpu != 73 {
		t.Fatalf("cpu = %d/%v, want 73 (máximo por núcleo)", cpu, ok)
	}
}

func TestParseHrProcessorLoadsRejectsGarbage(t *testing.T) {
	if cpu, ok := ParseHrProcessorLoads([]int{200, 300}); ok || cpu != 0 {
		t.Errorf("fuera de 0..100 no es una utilización: (%d,%v)", cpu, ok)
	}
	if _, ok := ParseHrProcessorLoads(nil); ok {
		t.Error("sin filas ok debe ser false (null en la UI, no 0)")
	}
	// Un agente que publica un bicho suelto sí tiene lectura: el descarte no
	// puede arrastrar a las filas válidas.
	if cpu, ok := ParseHrProcessorLoads([]int{42, 7000}); !ok || cpu != 42 {
		t.Errorf("cpu = %d/%v, want 42/true", cpu, ok)
	}
}

func TestParseHrStorageRAMRealRouterOS(t *testing.T) {
	rows := []HrStorageRow{
		{Descr: "virtual memory", Size: 524288, Used: 0},
		{Descr: "main memory", Size: 524288, Used: 89840},
		{Descr: "system disk", Size: 16384, Used: 4096},
	}
	pct, ok := ParseHrStorageRAM(rows)
	if !ok || pct != 17 {
		t.Fatalf("ram = %d/%v, want 17 (89840/524288)", pct, ok)
	}
}

// La fila se reconoce por su descripción, nunca por el índice: si no hay
// "main memory", la RAM se queda en null, aunque el equipo publique discos y
// memoria virtual. Un 100 % de "system disk" pintado como RAM es peor que un
// null.
func TestParseHrStorageRAMNoMainMemoryRowStaysUnknown(t *testing.T) {
	rows := []HrStorageRow{
		{Descr: "system disk", Size: 16384, Used: 15000},
		{Descr: "virtual memory", Size: 524288, Used: 10},
	}
	if pct, ok := ParseHrStorageRAM(rows); ok || pct != 0 {
		t.Fatalf("ram = %d/%v, want 0/false sin fila de memoria principal", pct, ok)
	}
}

func TestParseHrStorageRAMVariantsAndBrokenRows(t *testing.T) {
	if pct, ok := ParseHrStorageRAM([]HrStorageRow{{Descr: "Main Memory", Size: 1000, Used: 250}}); !ok || pct != 25 {
		t.Errorf("la descripción se compara sin importar mayúsculas: (%d,%v)", pct, ok)
	}
	if _, ok := ParseHrStorageRAM([]HrStorageRow{{Descr: "main memory", Size: 0, Used: 0}}); ok {
		t.Error("size 0 = fila sin dimensionar, no 0 % ni división por cero")
	}
	if _, ok := ParseHrStorageRAM([]HrStorageRow{{Descr: "main memory", Size: 100, Used: 140}}); ok {
		t.Error("used > size es un contador roto: no se publica un 140 %")
	}
	// Redondeo: 1/3 = 33,3 %; 2/3 = 66,7 %.
	if pct, _ := ParseHrStorageRAM([]HrStorageRow{{Descr: "main memory", Size: 3, Used: 1}}); pct != 33 {
		t.Errorf("1/3 = %d, want 33", pct)
	}
	if pct, _ := ParseHrStorageRAM([]HrStorageRow{{Descr: "main memory", Size: 3, Used: 2}}); pct != 67 {
		t.Errorf("2/3 = %d, want 67", pct)
	}
}

func TestPduIntAceptaNumericosRechazaLoDemas(t *testing.T) {
	if v, ok := pduInt(gosnmp.SnmpPDU{Type: gosnmp.Gauge32, Value: big.NewInt(73)}); !ok || v != 73 {
		t.Errorf("gauge 73: (%d,%v)", v, ok)
	}
	if _, ok := pduInt(gosnmp.SnmpPDU{Type: gosnmp.OctetString, Value: "73"}); ok {
		t.Error("un string no es una utilización")
	}
	if _, ok := pduInt(gosnmp.SnmpPDU{Type: gosnmp.NoSuchInstance}); ok {
		t.Error("NoSuchInstance debe dar ok=false")
	}
	if _, ok := pduInt(gosnmp.SnmpPDU{Type: gosnmp.Counter32, Value: big.NewInt(-1)}); ok {
		t.Error("un negativo no es una utilización")
	}
}
