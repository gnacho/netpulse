// Package vercmp compara versiones semver x.y.z para señales de
// actualización (agente desactualizado, etc.).
package vercmp

import (
	"fmt"
	"strings"
)

// Cmp compara dos versiones x.y.z (sufijos -rN/+ se ignoran): -1, 0, 1.
// Cualquiera de las dos malparsea → 0 (sin señal de novedad), para no
// alertar con etiquetas no semver (dev, local builds...).
func Cmp(a, b string) int {
	pa, okA := Parse3(a)
	pb, okB := Parse3(b)
	if !okA || !okB {
		return 0
	}
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] > pb[i] {
				return 1
			}
			return -1
		}
	}
	return 0
}

// Parse3 devuelve [major, minor, patch]; false si no parsea como x.y.z.
func Parse3(v string) ([3]int, bool) {
	x, _, ok := parse3Build(v)
	return x, ok
}

// CmpBuild compara dos versiones x.y.z incluyendo el número de build del
// sufijo (-N/+N, también -rN): 2.25.0-437 < 2.25.0-443. Una versión sin
// sufijo numérico válido cuenta como build 0, de modo que 2.25.0 < 2.25.0-437.
// Malparseo → 0 (misma política que Cmp). Las señales de actualización que
// dependen del build (updateAvailable, cierre del ciclo de upgrade) DEBEN
// usar esta variante: Cmp ignora los sufijos y considera iguales dos builds
// distintos del mismo x.y.z.
func CmpBuild(a, b string) int {
	pa, ba, okA := parse3Build(a)
	pb, bb, okB := parse3Build(b)
	if !okA || !okB {
		return 0
	}
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] > pb[i] {
				return 1
			}
			return -1
		}
	}
	switch {
	case ba > bb:
		return 1
	case ba < bb:
		return -1
	}
	return 0
}

// parse3Build: [major, minor, patch] + build numérico del sufijo (-437, +2,
// -r3). Sufijo no numérico (beta, dev) → build 0.
func parse3Build(v string) ([3]int, int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	build := 0
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		suf := v[i+1:]
		v = v[:i]
		if len(suf) > 0 && suf[0] == 'r' {
			suf = suf[1:]
		}
		if n, _ := fmt.Sscanf(suf, "%d", &build); n != 1 {
			build = 0
		}
	}
	var x [3]int
	if n, _ := fmt.Sscanf(v, "%d.%d.%d", &x[0], &x[1], &x[2]); n != 3 {
		return x, 0, false
	}
	return x, build, true
}
