package rack

import "strings"

// accessorySizes es el catálogo mínimo de accesorios (montajes sin
// dispositivo): faceplate ID -> {altura U, span de columnas}. La fase 1.3
// extenderá cada entrada a plantilla SVG con puertos sembrados; la
// validación de montaje solo necesita tamaños. IDs desconocidos caen al
// fallback 1U x ancho completo.
var accessorySizes = map[string][2]int{
	"blank-1u":         {1, 12},
	"blank-2u":         {2, 12},
	"blank-3u":         {3, 12},
	"blank-half-1u":    {1, 6},
	"shelf-1u":         {1, 12},
	"cable-manager-1u": {1, 12},
	"patch-panel-1u":   {1, 12},
	"patch-panel-12p":  {1, 12},
	"patch-panel-48p":  {2, 12},
	"pdu-1u":           {1, 12},
}

// accessoryFallback: tamaño por defecto para faceplates ajenos al catálogo.
var accessoryFallback = [2]int{1, 12}

// AccessorySize devuelve la altura U y el span de columnas del accesorio.
func AccessorySize(faceplateID string) (uHeight, colSpan int) {
	if s, ok := accessorySizes[faceplateID]; ok {
		return s[0], s[1]
	}
	return accessoryFallback[0], accessoryFallback[1]
}

// IsPassThrough marca los montajes pass-through (patch panels): su puerto
// admite 2 cables (tirada de pared al rear + patch al switch al front) en
// vez de 1.
func IsPassThrough(faceplateID string) bool {
	return strings.HasPrefix(faceplateID, "patch-panel")
}
