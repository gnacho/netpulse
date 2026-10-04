package rack

// sync.go — sincronización automática de cables detectados (#1186): la
// evidencia FDB (los hints) manda sobre los cables origin=detected. En cada
// lectura de /api/racks: los cables detected cuya pareja ya no está en la
// evidencia (o cuyos puertos cambiaron) se borran; los pares nuevos los crea
// ImportCables (idempotente por pareja). Los cables origin=manual NUNCA se
// tocan. Sin hints (sin evidencia, o demo) no cambia nada.

import (
	"database/sql"
)

// SyncDetectedCables ajusta los cables origin=detected a la evidencia actual
// y devuelve el resultado de la creación de los nuevos.
func SyncDetectedCables(db *sql.DB, hints []CableHint) (ImportResult, error) {
	var res ImportResult
	if len(hints) == 0 {
		return res, nil // sin evidencia no hay nada que sincronizar
	}
	mounts, err := ListMountRows(db)
	if err != nil {
		return res, err
	}
	macToMount := map[string]string{}
	for _, m := range mounts {
		if m.DeviceMAC != "" {
			macToMount[normMac(m.DeviceMAC)] = m.ID
		}
	}
	// Pares deseados según los hints (ambos extremos montados).
	want := map[string]CableHint{}
	for _, h := range hints {
		ma, okA := macToMount[normMac(h.FromDeviceID)]
		mb, okB := macToMount[normMac(h.ToDeviceID)]
		if !okA || !okB || ma == mb {
			continue
		}
		want[pairKey(ma, mb)] = h
	}
	if len(want) == 0 {
		return res, nil
	}

	cables, err := ListCables(db)
	if err != nil {
		return res, err
	}
	cabledPairs := map[string]bool{}
	detected := map[string]Cable{}
	for _, c := range cables {
		k := pairKey(c.FromMount, c.ToMount)
		cabledPairs[k] = true
		if c.Origin == OriginDetected || c.Origin == OriginImported {
			detected[k] = c
		}
	}

	tx, err := db.Begin()
	if err != nil {
		return res, err
	}
	defer tx.Rollback()

	for k, c := range detected {
		h, wanted := want[k]
		portsMoved := wanted &&
			((h.FromPortHint != "" && h.FromPortHint != c.FromPort) ||
				(h.ToPortHint != "" && h.ToPortHint != c.ToPort))
		if !wanted || portsMoved {
			// La evidencia ya no respalda este cable (o se recableó y cambiaron
			// los puertos): fuera; los pares vivos los recrea ImportCables.
			if _, err := tx.Exec(`DELETE FROM rack_cables WHERE id = ?`, c.ID); err != nil {
				return res, err
			}
			res.Removed++
		}
	}
	if err := tx.Commit(); err != nil {
		return res, err
	}

	created, err := ImportCables(db, hints)
	if err != nil {
		return res, err
	}
	res.Created = created.Created
	res.Skipped = created.Skipped
	res.NoFreePort = created.NoFreePort
	return res, nil
}
