package rack

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d.DB
}

func createTestRack(t *testing.T, db *sql.DB) Rack {
	t.Helper()
	r, err := CreateRack(db, Rack{Name: "rack-test", UHeight: 12})
	if err != nil {
		t.Fatalf("CreateRack: %v", err)
	}
	return r
}

func TestRackCRUD(t *testing.T) {
	db := openTestDB(t)

	r, err := CreateRack(db, Rack{Name: "lab", UHeight: 24, WidthStandard: Width10, Numbering: NumTopDown, Location: "despacho"})
	if err != nil {
		t.Fatalf("CreateRack: %v", err)
	}
	if r.ID == "" || r.WidthStandard != Width10 {
		t.Fatalf("rack creado incompleto: %+v", r)
	}

	got, err := GetRack(db, r.ID)
	if err != nil || got.Name != "lab" || got.Location != "despacho" {
		t.Fatalf("GetRack: %+v err=%v", got, err)
	}

	got.Name = "lab-2"
	if err := UpdateRack(db, got); err != nil {
		t.Fatalf("UpdateRack: %v", err)
	}

	list, err := ListRacks(db)
	if err != nil || len(list) != 1 || list[0].Name != "lab-2" {
		t.Fatalf("ListRacks: %+v err=%v", list, err)
	}

	if err := DeleteRack(db, r.ID); err != nil {
		t.Fatalf("DeleteRack: %v", err)
	}
	if _, err := GetRack(db, r.ID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetRack tras delete: err=%v", err)
	}
	// Update sobre id inexistente: ErrNoRows.
	if err := UpdateRack(db, got); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("UpdateRack inexistente: err=%v", err)
	}
}

func TestSaveLayoutAtomicAndOverlap(t *testing.T) {
	db := openTestDB(t)
	rack := createTestRack(t, db)

	m1 := Mount{FaceplateID: "blank-half-1u", UStart: 1, ColStart: 0}
	m2 := Mount{FaceplateID: "blank-half-1u", UStart: 1, ColStart: 6}
	if err := SaveLayout(db, LayoutChange{RackID: rack.ID, Upsert: []Mount{m1, m2}}); err != nil {
		t.Fatalf("SaveLayout mitades compartiendo U: %v", err)
	}
	rows, err := ListMountRows(db)
	if err != nil || len(rows) != 2 {
		t.Fatalf("ListMountRows: %d rows err=%v", len(rows), err)
	}

	// Tercera mitad en la misma U: solapa a una de las dos. Debe fallar y NO
	// persistir nada (atomicidad).
	m3 := Mount{UStart: 1, ColStart: 0, Label: "intruso"}
	if err := SaveLayout(db, LayoutChange{RackID: rack.ID, Upsert: []Mount{m3}}); !errors.Is(err, ErrOverlap) {
		t.Fatalf("SaveLayout solape: err=%v", err)
	}
	rows, _ = ListMountRows(db)
	if len(rows) != 2 {
		t.Fatalf("el layout rechazado persistió: %d rows", len(rows))
	}

	// Fuera del rack por arriba: perfil 2U en un rack de 12U con u_start 12.
	tall := Mount{DeviceMAC: "aa:bb:cc:dd:ee:ff", UStart: 12, ColStart: 0}
	if err := UpsertProfile(db, DeviceProfile{MAC: "aa:bb:cc:dd:ee:ff", UHeight: 2, ColSpan: 12}); err != nil {
		t.Fatalf("UpsertProfile: %v", err)
	}
	if err := SaveLayout(db, LayoutChange{RackID: rack.ID, Upsert: []Mount{tall}}); !errors.Is(err, ErrOverlap) {
		t.Fatalf("SaveLayout fuera de rack: err=%v", err)
	}

	// El montaje del dispositivo creó perfil por defecto; lo comprobamos con
	// un dispositivo nuevo sin perfil.
	noProfile := Mount{DeviceMAC: "11:22:33:44:55:66", UStart: 6, ColStart: 0}
	if err := SaveLayout(db, LayoutChange{RackID: rack.ID, Upsert: []Mount{noProfile}}); err != nil {
		t.Fatalf("SaveLayout dispositivo sin perfil: %v", err)
	}
	p, err := GetProfile(db, "11:22:33:44:55:66")
	if err != nil || p.UHeight != 1 || p.ColSpan != 12 {
		t.Fatalf("perfil por defecto no creado: %+v err=%v", p, err)
	}

	// Mover un montaje: upsert con id existente en otra posición.
	rows, _ = ListMountRows(db)
	var target string
	for _, m := range rows {
		if m.ColStart == 6 && m.UStart == 1 {
			target = m.ID
		}
	}
	if target == "" {
		t.Fatal("montaje esperado no encontrado")
	}
	mv := Mount{ID: target, UStart: 3, ColStart: 0}
	if err := SaveLayout(db, LayoutChange{RackID: rack.ID, Upsert: []Mount{mv}}); err != nil {
		t.Fatalf("SaveLayout mover: %v", err)
	}
	rows, _ = ListMountRows(db)
	moved := false
	for _, m := range rows {
		if m.ID == target && m.UStart == 3 {
			moved = true
		}
	}
	if !moved {
		t.Fatal("el montaje no se movió")
	}

	// Borrado por id.
	if err := SaveLayout(db, LayoutChange{RackID: rack.ID, Delete: []string{target}}); err != nil {
		t.Fatalf("SaveLayout delete: %v", err)
	}
	rows, _ = ListMountRows(db)
	if len(rows) != 2 {
		t.Fatalf("tras delete hay %d rows; want 2", len(rows))
	}
}

func TestProfileCRUD(t *testing.T) {
	db := openTestDB(t)
	p := DeviceProfile{
		MAC: "aa:bb:cc:00:00:01", FaceplateID: "server-2u", UHeight: 2, ColSpan: 12,
		Color: "#334455", Ports: []Port{{ID: "eth0", Kind: PortRJ45, X: 0.1, Y: 0.5}},
	}
	if err := UpsertProfile(db, p); err != nil {
		t.Fatalf("UpsertProfile: %v", err)
	}
	got, err := GetProfile(db, p.MAC)
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if got.UHeight != 2 || len(got.Ports) != 1 || got.Ports[0].ID != "eth0" {
		t.Fatalf("perfil redondeado mal: %+v", got)
	}
	// Upsert actualiza.
	p.Color = "#000000"
	if err := UpsertProfile(db, p); err != nil {
		t.Fatalf("UpsertProfile update: %v", err)
	}
	got, _ = GetProfile(db, p.MAC)
	if got.Color != "#000000" {
		t.Fatalf("update no aplicado: %+v", got)
	}
	list, err := ListProfiles(db)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListProfiles: %v err=%v", list, err)
	}
}

func TestCablesCapacityAndCascade(t *testing.T) {
	db := openTestDB(t)
	rack := createTestRack(t, db)

	// Dos switches con perfil con puertos sembrados.
	sw1 := "aa:bb:cc:00:00:01"
	sw2 := "aa:bb:cc:00:00:02"
	for i, mac := range []string{sw1, sw2} {
		if err := UpsertProfile(db, DeviceProfile{MAC: mac, UHeight: 1, ColSpan: 12,
			Ports: []Port{{ID: "eth0", Kind: PortRJ45}, {ID: "eth1", Kind: PortRJ45}}}); err != nil {
			t.Fatalf("UpsertProfile: %v", err)
		}
		if err := SaveLayout(db, LayoutChange{RackID: rack.ID, Upsert: []Mount{{DeviceMAC: mac, UStart: i + 1, ColStart: 0}}}); err != nil {
			t.Fatalf("SaveLayout: %v", err)
		}
	}
	// Un patch panel (accesorio pass-through).
	if err := SaveLayout(db, LayoutChange{RackID: rack.ID, Upsert: []Mount{{FaceplateID: "patch-panel-1u", UStart: 3, ColStart: 0}}}); err != nil {
		t.Fatalf("SaveLayout patch panel: %v", err)
	}
	rows, _ := ListMountRows(db)
	var mSw1, mSw2, mPP string
	for _, m := range rows {
		switch {
		case m.DeviceMAC == sw1:
			mSw1 = m.ID
		case m.DeviceMAC == sw2:
			mSw2 = m.ID
		case m.FaceplateID == "patch-panel-1u":
			mPP = m.ID
		}
	}

	// Cable válido sw1:eth0 -> sw2:eth0.
	c, err := AddCable(db, Cable{FromMount: mSw1, FromPort: "eth0", ToMount: mSw2, ToPort: "eth0"})
	if err != nil {
		t.Fatalf("AddCable: %v", err)
	}
	// Segundo cable sobre el mismo puerto: capacidad 1.
	if _, err := AddCable(db, Cable{FromMount: mSw1, FromPort: "eth0", ToMount: mSw2, ToPort: "eth1"}); !errors.Is(err, ErrPortBusy) {
		t.Fatalf("puerto ocupado: err=%v", err)
	}
	// Puerto no declarado por el dispositivo.
	if _, err := AddCable(db, Cable{FromMount: mSw1, FromPort: "eth9", ToMount: mSw2, ToPort: "eth1"}); !errors.Is(err, ErrUnknownPort) {
		t.Fatalf("puerto no declarado: err=%v", err)
	}
	// Montaje inexistente.
	if _, err := AddCable(db, Cable{FromMount: "nope", FromPort: "eth0", ToMount: mSw2, ToPort: "eth1"}); !errors.Is(err, ErrUnknownMount) {
		t.Fatalf("montaje inexistente: err=%v", err)
	}
	// Patch panel: admite 2 cables en el mismo puerto.
	if _, err := AddCable(db, Cable{FromMount: mSw1, FromPort: "eth1", ToMount: mPP, ToPort: "1"}); err != nil {
		t.Fatalf("patch panel primer cable: %v", err)
	}
	if _, err := AddCable(db, Cable{FromMount: mSw2, FromPort: "eth1", ToMount: mPP, ToPort: "1"}); err != nil {
		t.Fatalf("patch panel segundo cable: %v", err)
	}
	if _, err := AddCable(db, Cable{FromMount: mPP, FromPort: "1", ToMount: mPP, ToPort: "1"}); !errors.Is(err, ErrPortBusy) {
		t.Fatalf("patch panel tercer cable: err=%v", err)
	}

	cables, err := ListCables(db)
	if err != nil || len(cables) != 3 {
		t.Fatalf("ListCables: %d err=%v", len(cables), err)
	}
	if err := DeleteCable(db, c.ID); err != nil {
		t.Fatalf("DeleteCable: %v", err)
	}

	// Cascada: borrar el rack se lleva montajes y cables.
	if err := DeleteRack(db, rack.ID); err != nil {
		t.Fatalf("DeleteRack: %v", err)
	}
	cables, _ = ListCables(db)
	if len(cables) != 0 {
		t.Fatalf("cables huérfanos tras DeleteRack: %d", len(cables))
	}
	rows, _ = ListMountRows(db)
	if len(rows) != 0 {
		t.Fatalf("mounts huérfanos tras DeleteRack: %d", len(rows))
	}
}
