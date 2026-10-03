package rack

// Footprint es la huella ocupada por un montaje en el grid del rack:
// U 1-based desde el rail inferior, columnas 0-based.
type Footprint struct {
	UStart   int
	UHeight  int
	ColStart int
	ColSpan  int
}

// Overlaps informa si dos huellas comparten alguna celda del grid.
func Overlaps(a, b Footprint) bool {
	return a.UStart < b.UStart+b.UHeight && b.UStart < a.UStart+a.UHeight &&
		a.ColStart < b.ColStart+b.ColSpan && b.ColStart < a.ColStart+a.ColSpan
}

// FitsRack informa si la huella cabe dentro del grid del rack.
func FitsRack(rackUHeight int, f Footprint) bool {
	return f.UStart >= 1 && f.UHeight >= 1 && f.UStart+f.UHeight-1 <= rackUHeight &&
		f.ColStart >= 0 && f.ColSpan >= 1 && f.ColStart+f.ColSpan <= RackColumns
}

// CanPlace informa si la huella candidata cabe en el rack y no se solapa con
// ninguna huella existente. El servidor lo re-valida al guardar: dos montajes
// solapados en el mismo rack nunca se persisten. existing debe EXCLUIR la
// huella del montaje que se está moviendo (un drag sin cambio de sitio sigue
// siendo válido).
func CanPlace(rackUHeight int, existing []Footprint, cand Footprint) bool {
	if !FitsRack(rackUHeight, cand) {
		return false
	}
	for _, e := range existing {
		if Overlaps(e, cand) {
			return false
		}
	}
	return true
}

// FindSlot aplica el snap del drop: busca primero en la U de destino y luego
// camina hacia fuera (±1, ±2...). En cada U prueba primero la columna deseada
// (clampada al grid) y luego el barrido de columnas de izquierda a derecha.
// Si nada cabe, devuelve false y el caller muestra preview en rojo: un drop
// imposible nunca colisiona en silencio.
func FindSlot(rackUHeight int, existing []Footprint, dropU, dropCol, uHeight, colSpan int) (Footprint, bool) {
	if uHeight < 1 || colSpan < 1 {
		return Footprint{}, false
	}
	maxU := rackUHeight - uHeight + 1
	maxCol := RackColumns - colSpan
	if maxU < 1 || maxCol < 0 {
		return Footprint{}, false
	}
	col := dropCol
	if col < 0 {
		col = 0
	}
	if col > maxCol {
		col = maxCol
	}
	targetU := dropU
	if targetU < 1 {
		targetU = 1
	}
	if targetU > maxU {
		targetU = maxU
	}
	for offset := 0; offset < rackUHeight; offset++ {
		us := []int{targetU}
		if offset > 0 {
			us = []int{targetU - offset, targetU + offset}
		}
		for _, u := range us {
			if u < 1 || u > maxU {
				continue
			}
			if f, ok := tryAt(rackUHeight, existing, u, col, uHeight, colSpan); ok {
				return f, true
			}
			for c := 0; c <= maxCol; c++ {
				if c == col {
					continue
				}
				if f, ok := tryAt(rackUHeight, existing, u, c, uHeight, colSpan); ok {
					return f, true
				}
			}
		}
	}
	return Footprint{}, false
}

func tryAt(rackUHeight int, existing []Footprint, u, c, uHeight, colSpan int) (Footprint, bool) {
	f := Footprint{UStart: u, UHeight: uHeight, ColStart: c, ColSpan: colSpan}
	if !CanPlace(rackUHeight, existing, f) {
		return Footprint{}, false
	}
	return f, true
}
