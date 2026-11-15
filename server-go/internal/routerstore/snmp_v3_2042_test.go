package routerstore

import (
	"slices"
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

// Valores sintéticos de baja entropía: passphrases de test, no credenciales.
const (
	testV3Auth = "aaaaaaaa"
	testV3Priv = "bbbbbbbb"
)

// #2042: la versión SNMP y el bloque USM sobreviven al alta, a la lectura y
// a una edición parcial. La versión desconocida cae a "2c" (comportamiento
// histórico con comunidad) y los defaults de v3 se escriben explícitos.
func TestSnmpV3RoundTrip(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	cfg, err := AddRouter(d.DB, AddInput{
		Name: "swv3", Host: "192.168.1.60", Type: "managed-switch",
		SnmpEnabled: true, SnmpVersion: "3", SnmpV3User: "monitor",
		SnmpV3AuthProto: "SHA256", SnmpV3AuthPass: testV3Auth,
		SnmpV3PrivProto: "AES256", SnmpV3PrivPass: testV3Priv,
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if cfg.SnmpVersion != "3" || cfg.SnmpV3User != "monitor" {
		t.Fatalf("alta v3 = %+v", cfg)
	}
	if cfg.SnmpV3SecurityLevel != "authPriv" {
		t.Fatalf("nivel por defecto = %q", cfg.SnmpV3SecurityLevel)
	}
	if !slices.Equal([]string{cfg.SnmpV3AuthPass, cfg.SnmpV3PrivPass}, []string{testV3Auth, testV3Priv}) {
		t.Fatal("passphrases no guardadas")
	}

	listed := ListRouters(d.DB)
	if len(listed) != 1 {
		t.Fatalf("ListRouters devolvió %d filas", len(listed))
	}
	if listed[0].SnmpVersion != "3" || listed[0].SnmpV3PrivProto != "AES256" {
		t.Fatalf("lectura v3 = %+v", listed[0])
	}

	// Edición parcial: cambiar el nivel no borra usuario ni passphrases.
	lvl := "authNoPriv"
	upd, ok := UpdateRouter(d.DB, cfg.ID, UpdateInput{SnmpV3SecurityLevel: &lvl})
	if !ok {
		t.Fatal("UpdateRouter devolvió ok=false")
	}
	if upd.SnmpV3SecurityLevel != "authNoPriv" {
		t.Fatalf("nivel tras update = %q", upd.SnmpV3SecurityLevel)
	}
	if upd.SnmpV3User != "monitor" || !slices.Equal([]string{upd.SnmpV3AuthPass, upd.SnmpV3PrivPass}, []string{testV3Auth, testV3Priv}) {
		t.Fatalf("edición parcial pisó credenciales: %+v", upd)
	}

	// Borrar passphrase con "" explícito.
	empty := ""
	upd, _ = UpdateRouter(d.DB, cfg.ID, UpdateInput{SnmpV3AuthPass: &empty})
	if upd.SnmpV3AuthPass != "" {
		t.Fatalf("passphrase tras borrar = %q", upd.SnmpV3AuthPass)
	}

	// Versión desconocida → 2c.
	weird := "4"
	upd, _ = UpdateRouter(d.DB, cfg.ID, UpdateInput{SnmpVersion: &weird})
	if upd.SnmpVersion != "2c" {
		t.Fatalf("versión normalizada = %q; want 2c", upd.SnmpVersion)
	}
}

// #2042: filas anteriores a la migración (columna con el default de SQLite o
// vacía) se leen como v2c, que es lo que hacían antes de existir la columna.
func TestSnmpVersionDefaultsTo2c(t *testing.T) {
	d, err := db.Open(t.TempDir())
	if err != nil {
		t.Fatalf("db: %v", err)
	}
	defer d.Close()

	cfg, err := AddRouter(d.DB, AddInput{Name: "sw", Host: "192.168.1.61", Type: "managed-switch"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if cfg.SnmpVersion != "2c" {
		t.Fatalf("versión por defecto = %q; want 2c", cfg.SnmpVersion)
	}

	// Simula una fila vieja: columna a NULL/"" (la migración pone '2c' pero
	// una fila importada puede venir vacía).
	if _, err := d.DB.Exec("UPDATE routers SET snmp_version = '' WHERE id = ?", cfg.ID); err != nil {
		t.Fatalf("update: %v", err)
	}
	listed := ListRouters(d.DB)
	if len(listed) != 1 || listed[0].SnmpVersion != "2c" {
		t.Fatalf("fila vieja = %+v", listed)
	}
}
