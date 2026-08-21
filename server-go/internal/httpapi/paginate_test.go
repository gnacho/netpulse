// paginate_test.go — prueba de paginate ante páginas extremas (issue #201).
package httpapi

import (
	"math"
	"testing"
)

// TestPaginateHugePageNoPanic: una page enorme (p.ej. 9223372036854774784)
// desbordaba (page-1)*pageSize y producía un índice negativo → panic
// "slice bounds out of range" (issue #201). No debe paniquear.
func TestPaginateHugePageNoPanic(t *testing.T) {
	items := make([]int, 10)
	for i := range items {
		items[i] = i
	}
	// El valor exacto que desbordaba int64 con pageSize 50 (coerceInt max).
	out := paginate(items, math.MaxInt64-134217729, 50)
	if out == nil {
		t.Fatal("paginate devolvió nil")
	}
	if items, ok := out["items"].([]int); ok {
		if len(items) != 0 {
			t.Errorf("esperaba 0 items (página fuera de rango), got %d", len(items))
		}
	} else {
		t.Errorf("items con tipo inesperado: %T", out["items"])
	}
}

// TestPaginatePrimeraPagina: la página 1 sigue devolviendo los items en orden.
func TestPaginatePrimeraPagina(t *testing.T) {
	items := []string{"a", "b", "c", "d"}
	out := paginate(items, 1, 2)
	got := out["items"].([]string)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("esperaba [a b], got %v", got)
	}
	if out["total"] != 4 || out["page"] != int64(1) {
		t.Errorf("meta incorrecta: %v", out)
	}
}
