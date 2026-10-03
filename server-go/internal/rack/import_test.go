package rack

import (
	"database/sql"
	"testing"
)

// escenarioImport monta un switch (con sfps) y dos servidores, con un cable
// manual previo entre switch y servidor1.
func escenarioImport(t *testing.T, db *sql.DB) (MountRow, MountRow, MountRow) {
	t.Helper()
	rack := createTestRack(t, db)

	sw := "aa:bb:cc:00:01:00"
	s1 := "aa:bb:cc:00:01:01"
	s2 := "aa:bb:cc:00:01:02"
	if err := UpsertProfile(db, DeviceProfile{MAC: sw, FaceplateID: "switch", UHeight: 1, ColSpan: 12, Ports: []Port{
		{ID: "lan1", Kind: PortRJ45}, {ID: "lan2", Kind: PortRJ45}, {ID: "sfp1", Kind: PortSFPPlus},
	}}); err != nil {
		t.Fatalf("UpsertProfile sw: %v", err)
	}
	for _, mac := range []string{s1, s2} {
		if err := UpsertProfile(db, DeviceProfile{MAC: mac, FaceplateID: "server-1u", UHeight: 1, ColSpan: 12, Ports: []Port{
			{ID: "eth0", Kind: PortRJ45}, {ID: "sfp0", Kind: PortSFPPlus},
		}}); err != nil {
			t.Fatalf("UpsertProfile %s: %v", mac, err)
		}
	}
	err := SaveLayout(db, LayoutChange{RackID: rack.ID, Upsert: []Mount{
		{DeviceMAC: sw, UStart: 1, ColStart: 0},
		{DeviceMAC: s1, UStart: 2, ColStart: 0},
		{DeviceMAC: s2, UStart: 3, ColStart: 0},
	}})
	if err != nil {
		t.Fatalf("SaveLayout: %v", err)
	}
	mounts, err := ListMountRows(db)
	if err != nil || len(mounts) != 3 {
		t.Fatalf("ListMountRows: %v n=%d", err, len(mounts))
	}
	byMac := map[string]MountRow{}
	for _, m := range mounts {
		byMac[m.DeviceMAC] = m
	}
	return byMac[sw], byMac[s1], byMac[s2]
}

func TestImportCablesCreatesWithPortFallback(t *testing.T) {
	db := openTestDB(t)
	sw, s1, s2 := escenarioImport(t, db)

	// Hint 1: evidencia directa (LLDP vio el puerto lan2, rj45): el port
	// hint manda sobre el medio declarado "fiber" -> cable ETHERNET en lan2.
	// Hint 2: fibra sin port hint: elige sfp1 (switch) y sfp0 (server).
	res, err := ImportCables(db, []CableHint{
		{FromDeviceID: sw.DeviceMAC, ToDeviceID: s1.DeviceMAC, Medium: CableFiber, FromPortHint: "lan2", Source: "lldp"},
		{FromDeviceID: sw.DeviceMAC, ToDeviceID: s2.DeviceMAC, Medium: CableFiber, Source: "fdb"},
	})
	if err != nil {
		t.Fatalf("ImportCables: %v", err)
	}
	if len(res.Created) != 2 || res.Skipped != 0 || len(res.NoFreePort) != 0 {
		t.Fatalf("resultado: %+v", res)
	}
	ev := res.Created[0]
	if ev.FromPort != "lan2" || ev.Type != CableEthernet {
		t.Fatalf("el port hint (evidencia) debe mandar sobre el medio: %+v", ev)
	}
	fib := res.Created[1]
	if fib.Type != CableFiber || fib.FromPort != "sfp1" || fib.ToPort != "sfp0" || fib.Origin != OriginDetected {
		t.Fatalf("cable fibra: %+v", fib)
	}
	// Idempotencia por construcción: re-ejecutar salta los pares ya
	// cableados (en cualquier puerto y en cualquier orden de hint).
	res2, err := ImportCables(db, []CableHint{
		{FromDeviceID: s1.DeviceMAC, ToDeviceID: sw.DeviceMAC, Source: "fdb"}, // orden invertido
	})
	if err != nil {
		t.Fatalf("ImportCables 2: %v", err)
	}
	if len(res2.Created) != 0 || res2.Skipped != 1 {
		t.Fatalf("no idempotente: %+v", res2)
	}
}

func TestImportCablesNoFreePortAndUnmounted(t *testing.T) {
	db := openTestDB(t)
	sw, _, _ := escenarioImport(t, db)

	// s3: un solo puerto, montado y agotado con un cable manual al switch.
	s3 := "aa:bb:cc:00:01:03"
	if err := UpsertProfile(db, DeviceProfile{MAC: s3, UHeight: 1, ColSpan: 12, Ports: []Port{{ID: "eth0", Kind: PortRJ45}}}); err != nil {
		t.Fatalf("UpsertProfile s3: %v", err)
	}
	rackRows, err := ListMountRows(db)
	if err != nil {
		t.Fatalf("ListMountRows: %v", err)
	}
	rackID := rackRows[0].RackID
	if err := SaveLayout(db, LayoutChange{RackID: rackID, Upsert: []Mount{{DeviceMAC: s3, UStart: 4, ColStart: 0}}}); err != nil {
		t.Fatalf("SaveLayout s3: %v", err)
	}
	mS1 := mustMount(t, db, "aa:bb:cc:00:01:01")
	mS3 := mustMount(t, db, s3)
	// Agota el único puerto de s3 CONTRA OTRO PAR (s1): el par s3<->sw
	// queda sin cable, pero s3 no tiene puerto libre -> NoFreePort.
	if _, err := AddCable(db, Cable{FromMount: mS3.ID, FromPort: "eth0", ToMount: mS1.ID, ToPort: "eth0"}); err != nil {
		t.Fatalf("AddCable: %v", err)
	}

	res, err := ImportCables(db, []CableHint{
		{FromDeviceID: s3, ToDeviceID: sw.DeviceMAC, Source: "fdb"},                  // s3 sin puertos libres
		{FromDeviceID: "cc:cc:cc:cc:cc:cc", ToDeviceID: sw.DeviceMAC, Source: "fdb"}, // no montado
	})
	if err != nil {
		t.Fatalf("ImportCables: %v", err)
	}
	if len(res.NoFreePort) != 1 || res.Skipped != 1 || len(res.Created) != 0 {
		t.Fatalf("esperaba 1 NoFreePort + 1 Skipped: %+v", res)
	}
}

func mustMount(t *testing.T, db *sql.DB, mac string) MountRow {
	t.Helper()
	mounts, err := ListMountRows(db)
	if err != nil {
		t.Fatalf("ListMountRows: %v", err)
	}
	for _, m := range mounts {
		if m.DeviceMAC == mac {
			return m
		}
	}
	t.Fatalf("montaje no encontrado para %s", mac)
	return MountRow{}
}

func TestAuditCables(t *testing.T) {
	db := openTestDB(t)
	sw, s1, s2 := escenarioImport(t, db)
	mSw, mS1 := mustMount(t, db, sw.DeviceMAC), mustMount(t, db, s1.DeviceMAC)

	manual, err := AddCable(db, Cable{FromMount: mSw.ID, FromPort: "lan2", ToMount: mS1.ID, ToPort: "eth0"})
	if err != nil {
		t.Fatalf("AddCable: %v", err)
	}
	mounts, _ := ListMountRows(db)
	cables, _ := ListCables(db)

	// Sin hints: el manual es manual-only.
	audit := AuditCables(cables, nil, mounts)
	if audit[manual.ID] != AuditManualOnly {
		t.Fatalf("sin hints: %v", audit[manual.ID])
	}
	// Con hint del par: pasa a confirmed.
	audit = AuditCables(cables, []CableHint{{FromDeviceID: s1.DeviceMAC, ToDeviceID: sw.DeviceMAC, Source: "fdb"}}, mounts)
	if audit[manual.ID] != AuditConfirmed {
		t.Fatalf("con hint: %v", audit[manual.ID])
	}
	// Cable detectado: siempre detected. Y el par de s2 (sin cable) no
	// afecta.
	res, err := ImportCables(db, []CableHint{{FromDeviceID: s2.DeviceMAC, ToDeviceID: sw.DeviceMAC, Source: "fdb"}})
	if err != nil || len(res.Created) != 1 {
		t.Fatalf("import: %v %+v", err, res)
	}
	cables, _ = ListCables(db)
	audit = AuditCables(cables, nil, mounts)
	if audit[res.Created[0].ID] != AuditDetected || audit[manual.ID] != AuditManualOnly {
		t.Fatalf("audit mixto: %+v", audit)
	}
}
