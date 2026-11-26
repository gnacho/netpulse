package db_test

import (
	"testing"

	"github.com/gnacho/netpulse/server-go/internal/db"
)

// TestOpenMigratesLowercaseMACTables verifica que la migración de arranque
// normaliza a la forma canónica (AA:BB:CC:DD:EE:FF) las tablas con MACs en
// minúsculas o con guiones, resuelve colisiones de clave primaria, deja
// intactos los valores que no son MAC (como routerId:puerto en parent) y que
// sus sentencias son idempotentes si vuelven a correr.
func TestOpenMigratesLowercaseMACTables(t *testing.T) {
	dataDir := t.TempDir()
	first, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("primer db.Open: %v", err)
	}

	seed := []string{
		// device_overrides: minúsculas, guiones y colisión (gana updated_at más reciente).
		`INSERT INTO device_overrides (mac, icon, name, device_type, banned_bands, notify_offline, created_at, updated_at) VALUES
			('aa:bb:cc:dd:ee:01', 'tv', 'TV Salón', 'tv', '', 0, 10, 10),
			('aa-bb-cc-dd-ee-02', 'laptop', 'Portátil', 'ordenador', '', 0, 10, 10),
			('aa:bb:cc:dd:ee:03', 'server', 'Nuevo', 'servidor', '', 1, 10, 200),
			('AA:BB:CC:DD:EE:03', 'router', 'Viejo', 'router', '', 0, 10, 100)`,

		// topology_overrides: MAC y parent MAC en minúsculas, colisión (gana
		// updated_at), y valores que no son MAC con ':' y no deben tocarse: ids
		// de router que parecen una MAC con guiones o 12 dígitos hex, un parent
		// routerId:puerto y uno con ':' que no es hex.
		`INSERT INTO topology_overrides (id, mac, kind, name, parent, enabled, created_at, updated_at) VALUES
			('aa:bb:cc:00:00:01', 'aa:bb:cc:00:00:01', 'attach', 'vm1', 'c8:ff:bf:08:6f:ba', 1, 10, 10),
			('aa-bb-cc-00-00-02', 'aa-bb-cc-00-00-02', 'attach', 'vm2', 'sw1:two-gigabitEthernet 1/0/21', 1, 10, 10),
			('cafebabe1234', 'cafebabe1234', 'attach', 'vm4', 'ab-cd-ef-01-02-03', 1, 10, 10),
			('zz-router', 'zz-router', 'attach', 'vm5', 'ab:cd:ef:gh:ij:kl', 1, 10, 10),
			('aa:bb:cc:00:00:03', 'aa:bb:cc:00:00:03', 'switch', 'sw-viejo', NULL, 1, 10, 50),
			('AA:BB:CC:00:00:03', 'AA:BB:CC:00:00:03', 'hypervisor', 'hv-nuevo', NULL, 1, 10, 150),
			('g7615v2', 'g7615v2', 'attach', 'ont', 'gw:eth2', 1, 10, 10)`,

		// client_bw_raw, client_bw_5m, client_bw_daily: minúsculas, guiones,
		// colisión (sobrevive la fila ya canónica) y valor corrupto no-MAC ("19:21:68:11").
		`INSERT INTO client_bw_raw (mac, router_id, ts, rx_bytes, tx_bytes, rx_bps, tx_bps) VALUES
			('aa:bb:cc:dd:ee:01', 'gw', 1000, 100, 200, 800, 1600),
			('aa-bb-cc-dd-ee-02', 'gw', 1000, 300, 400, 2400, 3200),
			('aa:bb:cc:dd:ee:03', 'gw', 1000, 111, 222, 888, 1776),
			('AA:BB:CC:DD:EE:03', 'gw', 1000, 500, 600, 4000, 4800),
			('19:21:68:11', 'gw', 1000, 1, 2, 8, 16)`,
		`INSERT INTO client_bw_5m (mac, router_id, bucket_ts, n, rx_bytes, tx_bytes) VALUES
			('aa:bb:cc:dd:ee:01', 'gw', 300000, 1, 100, 200),
			('aa:bb:cc:dd:ee:03', 'gw', 300000, 1, 111, 222),
			('AA:BB:CC:DD:EE:03', 'gw', 300000, 2, 500, 600)`,
		`INSERT INTO client_bw_daily (mac, router_id, date, n, rx_bytes, tx_bytes) VALUES
			('aa-bb-cc-dd-ee-01', 'gw', '2026-11-20', 1, 100, 200),
			('aa:bb:cc:dd:ee:03', 'gw', '2026-11-20', 1, 111, 222),
			('AA:BB:CC:DD:EE:03', 'gw', '2026-11-20', 2, 500, 600)`,

		// device_rack_profile y rack_mounts: perfil en minúsculas con montaje
		// apuntándole por FK, y colisión de perfil minúsculas/mayúsculas.
		`INSERT INTO racks (id, name, u_height) VALUES ('r1', 'Lab', 12)`,
		`INSERT INTO device_rack_profile (mac, faceplate_id, u_height, col_span, color, ports_json) VALUES
			('aa:bb:cc:00:01:01', 'server-2u', 2, 12, '#112233', '[{"id":"eth0","kind":"rj45","x":0.5,"y":0.5}]'),
			('aa:bb:cc:00:01:02', 'blank-1u', 1, 12, '', '[]'),
			('AA:BB:CC:00:01:02', 'switch', 2, 12, '#445566', '[]')`,
		`INSERT INTO rack_mounts (id, rack_id, device_mac, faceplate_id, u_start, col_start) VALUES
			('m1', 'r1', 'aa:bb:cc:00:01:01', 'server-2u', 1, 0),
			('m2', 'r1', 'aa:bb:cc:00:01:02', 'switch', 3, 0),
			('m3', 'r1', NULL, 'patch-panel-1u', 5, 0)`,
	}
	for _, q := range seed {
		if _, err := first.Exec(q); err != nil {
			t.Fatalf("sembrar datos de prueba: %v\nSQL: %s", err, q)
		}
	}
	// Una base de datos de antes de la migración no lleva la marca.
	if _, err := first.Exec(`DELETE FROM kv WHERE key = 'mac_upper_tables_migrated'`); err != nil {
		t.Fatalf("borrar la marca: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("cerrar primer db: %v", err)
	}

	// Dos aperturas seguidas: la segunda, sin la marca, vuelve a correr las
	// sentencias sobre datos ya migrados y no debe cambiar nada.
	for pass := 1; pass <= 2; pass++ {
		d, err := db.Open(dataDir)
		if err != nil {
			t.Fatalf("db.Open pasada %d: %v", pass, err)
		}

		// 1) device_overrides
		var name string
		if err := d.QueryRow(`SELECT name FROM device_overrides WHERE mac = 'AA:BB:CC:DD:EE:01'`).Scan(&name); err != nil || name != "TV Salón" {
			t.Fatalf("pasada %d: esperaba TV Salón en AA:BB:CC:DD:EE:01, got %q (err=%v)", pass, name, err)
		}
		if err := d.QueryRow(`SELECT name FROM device_overrides WHERE mac = 'AA:BB:CC:DD:EE:02'`).Scan(&name); err != nil || name != "Portátil" {
			t.Fatalf("pasada %d: esperaba Portátil en AA:BB:CC:DD:EE:02, got %q (err=%v)", pass, name, err)
		}
		if err := d.QueryRow(`SELECT name FROM device_overrides WHERE mac = 'AA:BB:CC:DD:EE:03'`).Scan(&name); err != nil || name != "Nuevo" {
			t.Fatalf("pasada %d: en colisión de device_overrides debía ganar el más reciente ('Nuevo'), got %q (err=%v)", pass, name, err)
		}
		var count int
		if err := d.QueryRow(`SELECT COUNT(*) FROM device_overrides`).Scan(&count); err != nil || count != 3 {
			t.Fatalf("pasada %d: esperaba 3 filas en device_overrides, got %d (err=%v)", pass, count, err)
		}

		// 2) topology_overrides
		var mac, parent, kind string
		if err := d.QueryRow(`SELECT mac, parent FROM topology_overrides WHERE id = 'AA:BB:CC:00:00:01'`).Scan(&mac, &parent); err != nil {
			t.Fatalf("pasada %d: no se migró AA:BB:CC:00:00:01: %v", pass, err)
		}
		if mac != "AA:BB:CC:00:00:01" || parent != "C8:FF:BF:08:6F:BA" {
			t.Fatalf("pasada %d: esperaba mac/parent en forma canónica, got %q / %q", pass, mac, parent)
		}
		for _, c := range []struct{ id, parent string }{
			{"aa-bb-cc-00-00-02", "sw1:two-gigabitEthernet 1/0/21"},
			{"cafebabe1234", "ab-cd-ef-01-02-03"},
			{"zz-router", "ab:cd:ef:gh:ij:kl"},
		} {
			if err := d.QueryRow(`SELECT mac, parent FROM topology_overrides WHERE id = ?`, c.id).Scan(&mac, &parent); err != nil || mac != c.id || parent != c.parent {
				t.Fatalf("pasada %d: el id de router %q y su parent %q no debían alterarse, got mac=%q parent=%q (err=%v)", pass, c.id, c.parent, mac, parent, err)
			}
		}
		if err := d.QueryRow(`SELECT kind, name FROM topology_overrides WHERE id = 'AA:BB:CC:00:00:03'`).Scan(&kind, &name); err != nil || kind != "hypervisor" || name != "hv-nuevo" {
			t.Fatalf("pasada %d: en colisión de topology_overrides debía ganar updated_at=150, got kind=%q name=%q (err=%v)", pass, kind, name, err)
		}
		if err := d.QueryRow(`SELECT mac, parent FROM topology_overrides WHERE id = 'g7615v2'`).Scan(&mac, &parent); err != nil || mac != "g7615v2" || parent != "gw:eth2" {
			t.Fatalf("pasada %d: el target routerId no-MAC debía quedar intacto, got mac=%q parent=%q (err=%v)", pass, mac, parent, err)
		}

		// 3) client_bw_*
		var rx int64
		if err := d.QueryRow(`SELECT rx_bytes FROM client_bw_raw WHERE mac = 'AA:BB:CC:DD:EE:01' AND router_id = 'gw' AND ts = 1000`).Scan(&rx); err != nil || rx != 100 {
			t.Fatalf("pasada %d: esperaba 100 rx_bytes en client_bw_raw AA:BB:CC:DD:EE:01, got %d (err=%v)", pass, rx, err)
		}
		if err := d.QueryRow(`SELECT rx_bytes FROM client_bw_raw WHERE mac = 'AA:BB:CC:DD:EE:02' AND router_id = 'gw' AND ts = 1000`).Scan(&rx); err != nil || rx != 300 {
			t.Fatalf("pasada %d: esperaba 300 rx_bytes en client_bw_raw AA:BB:CC:DD:EE:02, got %d (err=%v)", pass, rx, err)
		}
		if err := d.QueryRow(`SELECT rx_bytes FROM client_bw_raw WHERE mac = 'AA:BB:CC:DD:EE:03' AND router_id = 'gw' AND ts = 1000`).Scan(&rx); err != nil || rx != 500 {
			t.Fatalf("pasada %d: en colisión de client_bw_raw debía sobrevivir la fila mayúscula (500), got %d (err=%v)", pass, rx, err)
		}
		if err := d.QueryRow(`SELECT rx_bytes FROM client_bw_raw WHERE mac = '19:21:68:11'`).Scan(&rx); err != nil || rx != 1 {
			t.Fatalf("pasada %d: la cadena de longitud != 17 no debía tocarse, got %d (err=%v)", pass, rx, err)
		}
		if err := d.QueryRow(`SELECT rx_bytes FROM client_bw_5m WHERE mac = 'AA:BB:CC:DD:EE:03' AND router_id = 'gw' AND bucket_ts = 300000`).Scan(&rx); err != nil || rx != 500 {
			t.Fatalf("pasada %d: en colisión de client_bw_5m debía sobrevivir la fila mayúscula (500), got %d (err=%v)", pass, rx, err)
		}
		if err := d.QueryRow(`SELECT rx_bytes FROM client_bw_daily WHERE mac = 'AA:BB:CC:DD:EE:01' AND router_id = 'gw' AND date = '2026-11-20'`).Scan(&rx); err != nil || rx != 100 {
			t.Fatalf("pasada %d: esperaba client_bw_daily AA:BB:CC:DD:EE:01 migrada, got %d (err=%v)", pass, rx, err)
		}
		if err := d.QueryRow(`SELECT rx_bytes FROM client_bw_daily WHERE mac = 'AA:BB:CC:DD:EE:03' AND router_id = 'gw' AND date = '2026-11-20'`).Scan(&rx); err != nil || rx != 500 {
			t.Fatalf("pasada %d: en colisión de client_bw_daily debía sobrevivir la fila mayúscula (500), got %d (err=%v)", pass, rx, err)
		}

		// 4) device_rack_profile y rack_mounts (sin perder la FK por ON DELETE SET NULL)
		var uHeight int
		var faceplate string
		if err := d.QueryRow(`SELECT m.device_mac, p.faceplate_id, p.u_height
			FROM rack_mounts m JOIN device_rack_profile p ON p.mac = m.device_mac
			WHERE m.id = 'm1'`).Scan(&mac, &faceplate, &uHeight); err != nil {
			t.Fatalf("pasada %d: montaje m1 perdió su perfil tras migrar: %v", pass, err)
		}
		if mac != "AA:BB:CC:00:01:01" || faceplate != "server-2u" || uHeight != 2 {
			t.Fatalf("pasada %d: m1 esperaba AA:BB:CC:00:01:01 server-2u 2U, got %s %s %dU", pass, mac, faceplate, uHeight)
		}
		if err := d.QueryRow(`SELECT m.device_mac, p.faceplate_id, p.u_height
			FROM rack_mounts m JOIN device_rack_profile p ON p.mac = m.device_mac
			WHERE m.id = 'm2'`).Scan(&mac, &faceplate, &uHeight); err != nil {
			t.Fatalf("pasada %d: montaje m2 no cruzó con el perfil mayúscula superviviente: %v", pass, err)
		}
		if mac != "AA:BB:CC:00:01:02" || faceplate != "switch" || uHeight != 2 {
			t.Fatalf("pasada %d: m2 esperaba AA:BB:CC:00:01:02 switch 2U, got %s %s %dU", pass, mac, faceplate, uHeight)
		}
		if err := d.QueryRow(`SELECT COUNT(*) FROM device_rack_profile`).Scan(&count); err != nil || count != 2 {
			t.Fatalf("pasada %d: esperaba 2 perfiles canónicos en device_rack_profile, got %d (err=%v)", pass, count, err)
		}

		if err := d.QueryRow(`SELECT COUNT(*) FROM kv WHERE key = 'mac_upper_tables_migrated'`).Scan(&count); err != nil || count != 1 {
			t.Fatalf("pasada %d: esperaba la marca de migración en kv, got %d (err=%v)", pass, count, err)
		}
		if _, err := d.Exec(`DELETE FROM kv WHERE key = 'mac_upper_tables_migrated'`); err != nil {
			t.Fatalf("pasada %d: borrar la marca: %v", pass, err)
		}

		if err := d.Close(); err != nil {
			t.Fatalf("cerrar db pasada %d: %v", pass, err)
		}
	}
}

// TestMACMigrationRunsOnce verifica que, con la marca puesta, los arranques
// siguientes no vuelven a recorrer las tablas.
func TestMACMigrationRunsOnce(t *testing.T) {
	dataDir := t.TempDir()
	first, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("primer db.Open: %v", err)
	}
	if _, err := first.Exec(`INSERT INTO client_bw_raw (mac, router_id, ts, rx_bytes, tx_bytes, rx_bps, tx_bps)
		VALUES ('aa:bb:cc:dd:ee:01', 'gw', 1, 1, 1, 1, 1)`); err != nil {
		t.Fatalf("sembrar: %v", err)
	}
	first.Close()

	d, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("segundo db.Open: %v", err)
	}
	defer d.Close()
	var mac string
	if err := d.QueryRow(`SELECT mac FROM client_bw_raw`).Scan(&mac); err != nil || mac != "aa:bb:cc:dd:ee:01" {
		t.Fatalf("con la marca puesta no debía migrar de nuevo, got %q (err=%v)", mac, err)
	}
}

// TestMigrationLeavesMixedSeparatorsAlone: una cadena con separadores mezclados
// ('-' y ':') no es una MAC para macaddr.Normalize, así que la migración no la
// reescribe ni la usa para pisar el override real de la MAC canónica.
func TestMigrationLeavesMixedSeparatorsAlone(t *testing.T) {
	dataDir := t.TempDir()
	first, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("primer db.Open: %v", err)
	}

	seed := []string{
		`INSERT INTO device_overrides (mac, icon, name, device_type, banned_bands, notify_offline, created_at, updated_at) VALUES
			('AA:BB:CC:DD:EE:01', 'tv', 'Real', 'tv', '', 0, 10, 100),
			('aa-bb:cc-dd:ee-01', 'laptop', 'Separadores mezclados', 'ordenador', '', 0, 10, 200)`,
		`INSERT INTO client_bw_raw (mac, router_id, ts, rx_bytes, tx_bytes, rx_bps, tx_bps)
			VALUES ('aa-bb:cc-dd:ee-02', 'gw', 1000, 100, 200, 800, 1600)`,
		`DELETE FROM kv WHERE key = 'mac_upper_tables_migrated'`,
	}
	for _, q := range seed {
		if _, err := first.Exec(q); err != nil {
			t.Fatalf("sembrar datos: %v\nSQL: %s", err, q)
		}
	}
	if err := first.Close(); err != nil {
		t.Fatalf("cerrar primer db: %v", err)
	}

	d, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("segundo db.Open: %v", err)
	}
	defer d.Close()

	var name string
	if err := d.QueryRow(`SELECT name FROM device_overrides WHERE mac = 'AA:BB:CC:DD:EE:01'`).Scan(&name); err != nil {
		t.Fatalf("leer device_overrides: %v", err)
	}
	if name != "Real" {
		t.Errorf("una cadena con separadores mezclados pisó el override real: esperaba %q, obtuve %q", "Real", name)
	}
	var bwMac string
	if err := d.QueryRow(`SELECT mac FROM client_bw_raw WHERE router_id = 'gw' AND ts = 1000`).Scan(&bwMac); err != nil {
		t.Fatalf("leer client_bw_raw: %v", err)
	}
	if bwMac != "aa-bb:cc-dd:ee-02" {
		t.Errorf("client_bw_raw no debía reescribir una cadena con separadores mezclados: esperaba %q, obtuve %q", "aa-bb:cc-dd:ee-02", bwMac)
	}
}

// TestMigrationTrimsTopologyParentMAC: main guardaba el parent tal cual
// llegaba a POST /api/topology/overrides, con espacios incluidos; la migración
// lo recorta y lo pasa a la forma canónica.
func TestMigrationTrimsTopologyParentMAC(t *testing.T) {
	dataDir := t.TempDir()
	first, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("primer db.Open: %v", err)
	}

	seed := []string{
		`INSERT INTO topology_overrides (id, mac, kind, name, parent, enabled, created_at, updated_at)
			VALUES ('aa:bb:cc:00:00:01', 'aa:bb:cc:00:00:01', 'attach', 'vm1', ' c8:ff:bf:08:6f:ba ', 1, 10, 10)`,
		// strings.TrimSpace también quita tabuladores y saltos de línea.
		`INSERT INTO topology_overrides (id, mac, kind, name, parent, enabled, created_at, updated_at)
			VALUES ('aa:bb:cc:00:00:02', 'aa:bb:cc:00:00:02', 'attach', 'vm2', char(9) || 'c8:ff:bf:08:6f:bb' || char(9), 1, 10, 10)`,
		`INSERT INTO topology_overrides (id, mac, kind, name, parent, enabled, created_at, updated_at)
			VALUES ('aa:bb:cc:00:00:03', 'aa:bb:cc:00:00:03', 'attach', 'vm3', char(13) || char(10) || 'c8:ff:bf:08:6f:bc' || char(10), 1, 10, 10)`,
		`DELETE FROM kv WHERE key = 'mac_upper_tables_migrated'`,
	}
	for _, q := range seed {
		if _, err := first.Exec(q); err != nil {
			t.Fatalf("sembrar datos: %v", err)
		}
	}
	if err := first.Close(); err != nil {
		t.Fatalf("cerrar primer db: %v", err)
	}

	d, err := db.Open(dataDir)
	if err != nil {
		t.Fatalf("segundo db.Open: %v", err)
	}
	defer d.Close()

	want := map[string]string{
		"AA:BB:CC:00:00:01": "C8:FF:BF:08:6F:BA",
		"AA:BB:CC:00:00:02": "C8:FF:BF:08:6F:BB",
		"AA:BB:CC:00:00:03": "C8:FF:BF:08:6F:BC",
	}
	for id, wantParent := range want {
		var parent string
		if err := d.QueryRow(`SELECT parent FROM topology_overrides WHERE id = ?`, id).Scan(&parent); err != nil {
			t.Fatalf("leer parent de %s: %v", id, err)
		}
		if parent != wantParent {
			t.Errorf("%s: esperaba parent normalizado %s, obtuve %q", id, wantParent, parent)
		}
	}
}
