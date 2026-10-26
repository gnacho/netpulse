// weak_signal_test.go - ciclo de vida de la alerta de señal débil (#1452).
package alerts

import "testing"

func intPtr(v int) *int { return &v }

func TestWeakSignalStepFiresOncePerEpisode(t *testing.T) {
	var st WeakSignalState
	thr, hyst, samples := -70, 3, 3

	fire, resolve, _ := WeakSignalStep(&st, "aa", intPtr(-80), true, thr, hyst, samples, 1000)
	if fire == "" {
		t.Fatal("debe disparar con señal bajo el umbral")
	}
	if st.OpenID != fire {
		t.Fatalf("el episodio abierto debe ser el disparado: %q vs %q", st.OpenID, fire)
	}
	// Segunda muestra mala: sin re-disparo (un episodio = una alerta).
	fire, resolve, _ = WeakSignalStep(&st, "aa", intPtr(-78), true, thr, hyst, samples, 2000)
	if fire != "" || resolve != "" {
		t.Fatal("con el episodio abierto no hay ni disparo ni cierre")
	}
}

func TestWeakSignalStepResolvesAfterNGoodSamples(t *testing.T) {
	var st WeakSignalState
	thr, hyst, samples := -70, 3, 3

	fire, _, _ := WeakSignalStep(&st, "aa", intPtr(-80), true, thr, hyst, samples, 1000)
	// -66 >= -70+3: buena. Dos muestras no cierran (N=3).
	for i := 0; i < 2; i++ {
		f, r, _ := WeakSignalStep(&st, "aa", intPtr(-66), true, thr, hyst, samples, int64(2000+i))
		if f != "" || r != "" {
			t.Fatalf("muestra %d: no debe cerrar aún", i+1)
		}
	}
	f, r, start := WeakSignalStep(&st, "aa", intPtr(-65), true, thr, hyst, samples, 3000)
	if f != "" || r != fire {
		t.Fatalf("la tercera muestra buena cierra el episodio: fire=%q resolve=%q", f, r)
	}
	if start != 1000 {
		t.Fatalf("el ts de apertura del episodio es %d, quiero 1000", start)
	}
	if st.OpenID != "" || st.Good != 0 {
		t.Fatal("tras cerrar, el episodio queda cerrado y la racha a cero")
	}
	// #1521: LastFire se conserva al cerrar (el guarda de 24 h es entre
	// EPISODIOS, no se anula con la resolución).
	if st.LastFire != 1000 {
		t.Fatalf("LastFire tras cerrar = %d, quiero 1000 (el disparo original)", st.LastFire)
	}
}

func TestWeakSignalStepDeadZoneBreaksStreak(t *testing.T) {
	var st WeakSignalState
	thr, hyst, samples := -70, 3, 3

	fire, _, _ := WeakSignalStep(&st, "aa", intPtr(-80), true, thr, hyst, samples, 1000)
	_ = fire
	// Dos buenas, una en zona muerta (-69: >= thr pero < thr+hyst), una buena.
	WeakSignalStep(&st, "aa", intPtr(-66), true, thr, hyst, samples, 2000)
	WeakSignalStep(&st, "aa", intPtr(-66), true, thr, hyst, samples, 3000)
	WeakSignalStep(&st, "aa", intPtr(-69), true, thr, hyst, samples, 4000)
	f, r, _ := WeakSignalStep(&st, "aa", intPtr(-66), true, thr, hyst, samples, 5000)
	if f != "" || r != "" {
		t.Fatal("la zona muerta rompe la racha: una buena tras ella no cierra")
	}
	// Dos buenas más sí cierran (racha nueva de 3).
	WeakSignalStep(&st, "aa", intPtr(-66), true, thr, hyst, samples, 6000)
	f, r, _ = WeakSignalStep(&st, "aa", intPtr(-66), true, thr, hyst, samples, 7000)
	if r == "" {
		t.Fatal("tras recomponer la racha de 3 buenas, el episodio cierra")
	}
}

func TestWeakSignalStepNoDataNeverResolves(t *testing.T) {
	var st WeakSignalState
	thr, hyst, samples := -70, 3, 2

	fire, _, _ := WeakSignalStep(&st, "aa", intPtr(-80), true, thr, hyst, samples, 1000)
	_ = fire
	// Offline: no cierra ni rompe la racha de buenas acumulada.
	WeakSignalStep(&st, "aa", intPtr(-66), true, thr, hyst, samples, 2000)
	WeakSignalStep(&st, "aa", nil, false, thr, hyst, samples, 3000)
	WeakSignalStep(&st, "aa", nil, true, thr, hyst, samples, 4000)
	if st.Good != 1 {
		t.Fatalf("sin datos no debe tocar la racha: good=%d", st.Good)
	}
	f, r, _ := WeakSignalStep(&st, "aa", intPtr(-66), true, thr, hyst, samples, 5000)
	if f != "" || r != fire {
		t.Fatal("la racha continúa tras el hueco sin datos y cierra en la 2ª buena")
	}
}

func TestWeakSignalStepRefireGuard(t *testing.T) {
	var st WeakSignalState
	thr, hyst, samples := -70, 3, 1

	f, _, _ := WeakSignalStep(&st, "aa", intPtr(-80), true, thr, hyst, samples, 1000)
	if f == "" {
		t.Fatal("primer episodio dispara")
	}
	// Cierra al instante (N=1).
	_, r, _ := WeakSignalStep(&st, "aa", intPtr(-66), true, thr, hyst, samples, 2000)
	if r == "" {
		t.Fatal("cierra con una buena cuando N=1")
	}
	// Caída inmediata de nuevo (#1521): DENTRO del guarda de 24 h desde el
	// último disparo real. No abre episodio (un cliente oscilando no vuelve a
	// alertar el mismo día, aunque haya resuelto entremedias).
	f, r, _ = WeakSignalStep(&st, "aa", intPtr(-81), true, thr, hyst, samples, 3000)
	if f != "" || r != "" {
		t.Fatalf("re-disparo a los 2 s de cerrar: fire=%q resolve=%q, esperaba nada", f, r)
	}
	// Pasado el guarda (24 h desde el disparo de 1000), vuelve a alertar.
	f, _, _ = WeakSignalStep(&st, "aa", intPtr(-81), true, thr, hyst, samples, 1000+weakRefireGapMs+1)
	if f == "" {
		t.Fatal("tras 24 h del último disparo, un episodio nuevo dispara")
	}
}

func TestWeakSignalStepDefaultGetters(t *testing.T) {
	if got := WeakSignalHysteresisDbm(nil); got != DefaultWeakSignalHysteresisDbm {
		t.Fatalf("default histéresis: %d", got)
	}
	if got := WeakSignalResolveSamples(nil); got != DefaultWeakSignalResolveSamples {
		t.Fatalf("default muestras: %d", got)
	}
	if WeakSignalDbm(nil) != DefaultWeakSignalDbm {
		t.Fatal("default dbm")
	}
}
