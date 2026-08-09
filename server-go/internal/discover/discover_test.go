package discover

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestPoolRespectsCancellation: con ctx ya cancelado antes de arrancar, los
// workers lo detectan en su primera iteración y pool devuelve sin procesar
// (o procesando como mucho los items que un worker pudo tomar antes del
// check). Comprueba que el issue #107 (scan que sigue tras cancelar la
// request) está cerrado a nivel de pool.
func TestPoolRespectsCancellation(t *testing.T) {
	items := make([]int, 100)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // ya cancelado antes de arrancar

	var processed atomic.Int32
	start := time.Now()
	out := pool(ctx, items, 16, func(i int) *int {
		processed.Add(1)
		v := i
		return &v
	})
	elapsed := time.Since(start)

	// Con ctx cancelado, pool debe volver casi al instante.
	if elapsed > 100*time.Millisecond {
		t.Errorf("pool tardó %v con ctx cancelado; debería volver enseguida", elapsed)
	}
	// Alguna worker pudo colar un item si arrancó justo entre el start del
	// goroutine y el check de ctx.Err(); toleramos hasta el nº de workers.
	if n := int(processed.Load()); n > 16 {
		t.Errorf("pool procesó %d items con ctx cancelado (esperaba ≤16)", n)
	}
	if len(out) > 16 {
		t.Errorf("pool devolvió %d items con ctx cancelado (esperaba ≤16)", len(out))
	}
}

// TestPoolCancelMidWay: cancela a mitad del barrido. pool debe dejar de
// aceptar items nuevos y devolver antes de procesar los N completos.
func TestPoolCancelMidWay(t *testing.T) {
	const n = 400
	items := make([]int, n)
	ctx, cancel := context.WithCancel(context.Background())

	var processed atomic.Int32
	// fn que tarda 2ms por item; con n=400 y workers=16 serían ~50ms sin cancel.
	go func() {
		time.Sleep(15 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	_ = pool(ctx, items, 16, func(i int) *int {
		time.Sleep(2 * time.Millisecond)
		processed.Add(1)
		return &items[i]
	})
	elapsed := time.Since(start)

	// Sin cancelación tardaría ~50ms; con cancel a los 15ms + drenar debe
	// quedar muy por debajo de n*2ms/16. Margen amplio para no ser flaky.
	if elapsed > 150*time.Millisecond {
		t.Errorf("pool tardó %v; la cancelación no cortó el barrido", elapsed)
	}
	if got := int(processed.Load()); got == n {
		t.Errorf("pool procesó los %d items a pesar de la cancelación", got)
	}
}

// TestPoolHappyPath: sin cancelación, pool procesa todos los items y los
// transforma correctamente.
func TestPoolHappyPath(t *testing.T) {
	items := []int{1, 2, 3, 4, 5, 6, 7}
	var mu sync.Mutex
	seen := map[int]bool{}
	out := pool(context.Background(), items, 4, func(i int) *int {
		mu.Lock()
		seen[i] = true
		mu.Unlock()
		v := i * 10
		return &v
	})
	if len(out) != len(items) {
		t.Fatalf("esperaba %d resultados, got %d", len(items), len(out))
	}
	got := map[int]bool{}
	for _, p := range out {
		got[*p] = true
	}
	for _, i := range items {
		if !got[i*10] {
			t.Errorf("falta el resultado %d", i*10)
		}
	}
	if len(seen) != len(items) {
		t.Errorf("no todos los items procesados: %d/%d", len(seen), len(items))
	}
}
