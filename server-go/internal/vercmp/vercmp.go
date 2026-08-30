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
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "- +"); i >= 0 {
		v = v[:i]
	}
	var x [3]int
	if n, _ := fmt.Sscanf(v, "%d.%d.%d", &x[0], &x[1], &x[2]); n != 3 {
		return x, false
	}
	return x, true
}
