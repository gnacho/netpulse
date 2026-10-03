// store.go — capa de persistencia del rack canvas sobre SQLite. Funciones
// a nivel de paquete tomando *sql.DB (mismo patrón que topooverride).
//
// SaveLayout es la única escritura de montajes: atómica y con validación
// anti-solape server-side sobre el estado final (el cliente valida para el
// snap; el servidor es la última frontera: dos montajes solapados en el
// mismo rack nunca se persisten).
package rack

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrOverlap indica que el estado final del layout tiene dos montajes
// solapados o fuera de rack.
var ErrOverlap = errors.New("rack: montajes solapados o fuera de rack")

// ErrPortBusy indica que el puerto ya tiene todos los cables que admite.
var ErrPortBusy = errors.New("rack: puerto sin capacidad para otro cable")

// ErrUnknownMount / ErrUnknownPort referencian montajes o puertos inexistentes.
var ErrUnknownMount = errors.New("rack: montaje inexistente")

// ErrUnknownPort indica un puerto que el dispositivo no declara en su perfil.
var ErrUnknownPort = errors.New("rack: puerto no declarado por el dispositivo")

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// --- Racks ---

func ListRacks(db *sql.DB) ([]Rack, error) {
	rows, err := db.Query(`SELECT id, name, u_height, width_standard, numbering, style_json, location, position_x, position_y
		FROM racks ORDER BY position_y, position_x, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Rack{}
	for rows.Next() {
		var r Rack
		var loc sql.NullString
		if err := rows.Scan(&r.ID, &r.Name, &r.UHeight, &r.WidthStandard, &r.Numbering, &r.StyleJSON, &loc, &r.PositionX, &r.PositionY); err != nil {
			return nil, err
		}
		r.Location = loc.String
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetRack devuelve sql.ErrNoRows si no existe.
func GetRack(db *sql.DB, id string) (Rack, error) {
	var r Rack
	var loc sql.NullString
	err := db.QueryRow(`SELECT id, name, u_height, width_standard, numbering, style_json, location, position_x, position_y
		FROM racks WHERE id = ?`, id).Scan(&r.ID, &r.Name, &r.UHeight, &r.WidthStandard, &r.Numbering, &r.StyleJSON, &loc, &r.PositionX, &r.PositionY)
	r.Location = loc.String
	return r, err
}

// CreateRack inserta un rack nuevo (id generado server-side) con defaults
// aplicados y devuelve la fila creada.
func CreateRack(db *sql.DB, in Rack) (Rack, error) {
	in.ID = newID()
	if in.WidthStandard == "" {
		in.WidthStandard = Width19
	}
	if in.Numbering == "" {
		in.Numbering = NumBottomUp
	}
	if in.StyleJSON == "" {
		in.StyleJSON = "{}"
	}
	if !in.Valid() {
		return Rack{}, fmt.Errorf("rack: rack inválido")
	}
	_, err := db.Exec(`INSERT INTO racks (id, name, u_height, width_standard, numbering, style_json, location, position_x, position_y)
		VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?)`,
		in.ID, in.Name, in.UHeight, in.WidthStandard, in.Numbering, in.StyleJSON, in.Location, in.PositionX, in.PositionY)
	return in, err
}

func UpdateRack(db *sql.DB, r Rack) error {
	if !r.Valid() {
		return fmt.Errorf("rack: rack inválido")
	}
	// Guarda: no reducir el rack por debajo de lo ya montado (la geometría
	// quedaría fuera del grid; el anti-solape se valida al guardar layouts).
	mounts, err := ListMountRowsByRack(db, r.ID)
	if err != nil {
		return err
	}
	for _, m := range mounts {
		if bottom := m.UStart + m.UHeight - 1; bottom > r.UHeight {
			return fmt.Errorf("%w: montaje %s ocupa hasta U%d (nuevo alto U%d)", ErrOverlap, m.ID, bottom, r.UHeight)
		}
	}
	res, err := db.Exec(`UPDATE racks SET name = ?, u_height = ?, width_standard = ?, numbering = ?, style_json = ?, location = NULLIF(?, ''), position_x = ?, position_y = ? WHERE id = ?`,
		r.Name, r.UHeight, r.WidthStandard, r.Numbering, r.StyleJSON, r.Location, r.PositionX, r.PositionY, r.ID)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// DeleteRack borra el rack; montajes y cables que cuelgan de ellos se van en
// cascada (ON DELETE CASCADE).
func DeleteRack(db *sql.DB, id string) error {
	res, err := db.Exec(`DELETE FROM racks WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// --- Perfiles físicos de dispositivo ---

func ListProfiles(db *sql.DB) ([]DeviceProfile, error) {
	rows, err := db.Query(`SELECT mac, faceplate_id, u_height, col_span, color, ports_json FROM device_rack_profile ORDER BY mac`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeviceProfile{}
	for rows.Next() {
		var p DeviceProfile
		var ports string
		if err := rows.Scan(&p.MAC, &p.FaceplateID, &p.UHeight, &p.ColSpan, &p.Color, &ports); err != nil {
			return nil, err
		}
		p.Ports = parsePorts(ports)
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetProfile devuelve el perfil de una MAC; sql.ErrNoRows si no existe.
func GetProfile(db *sql.DB, mac string) (DeviceProfile, error) {
	var p DeviceProfile
	var ports string
	err := db.QueryRow(`SELECT mac, faceplate_id, u_height, col_span, color, ports_json FROM device_rack_profile WHERE mac = ?`, mac).
		Scan(&p.MAC, &p.FaceplateID, &p.UHeight, &p.ColSpan, &p.Color, &ports)
	if err != nil {
		return DeviceProfile{}, err
	}
	p.Ports = parsePorts(ports)
	return p, nil
}

// UpsertProfile crea o actualiza el modelo físico del dispositivo.
func UpsertProfile(db *sql.DB, p DeviceProfile) error {
	if p.MAC == "" {
		return fmt.Errorf("rack: mac requerida")
	}
	if p.UHeight < 1 {
		p.UHeight = 1
	}
	if p.ColSpan < 1 {
		p.ColSpan = RackColumns
	}
	ports, err := json.Marshal(p.Ports)
	if err != nil {
		return err
	}
	_, err = db.Exec(`INSERT INTO device_rack_profile (mac, faceplate_id, u_height, col_span, color, ports_json)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(mac) DO UPDATE SET faceplate_id = excluded.faceplate_id, u_height = excluded.u_height,
			col_span = excluded.col_span, color = excluded.color, ports_json = excluded.ports_json`,
		p.MAC, p.FaceplateID, p.UHeight, p.ColSpan, p.Color, string(ports))
	return err
}

func parsePorts(s string) []Port {
	if s == "" {
		return []Port{}
	}
	var out []Port
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return []Port{}
	}
	if out == nil {
		out = []Port{}
	}
	return out
}

// --- Montajes ---

// MountRow es un montaje con su huella resuelta (del perfil del dispositivo
// o del catálogo de accesorios). Es lo que el canvas necesita para dibujar.
type MountRow struct {
	Mount
	UHeight int
	ColSpan int
}

// Footprint devuelve la huella del montaje en el grid del rack.
func (m MountRow) Footprint() Footprint {
	return Footprint{UStart: m.UStart, UHeight: m.UHeight, ColStart: m.ColStart, ColSpan: m.ColSpan}
}

// ListMountRows devuelve todos los montajes de todos los racks con huella
// resuelta.
func ListMountRows(db *sql.DB) ([]MountRow, error) {
	rows, err := db.Query(`SELECT m.id, m.rack_id, IFNULL(m.device_mac, ''), m.faceplate_id, m.u_start, m.col_start, m.label, m.status_pin, m.port_visibility,
		p.u_height, p.col_span
		FROM rack_mounts m LEFT JOIN device_rack_profile p ON p.mac = m.device_mac
		ORDER BY m.rack_id, m.u_start DESC, m.col_start`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMountRows(rows)
}

func ListMountRowsByRack(db *sql.DB, rackID string) ([]MountRow, error) {
	rows, err := db.Query(`SELECT m.id, m.rack_id, IFNULL(m.device_mac, ''), m.faceplate_id, m.u_start, m.col_start, m.label, m.status_pin, m.port_visibility,
		p.u_height, p.col_span
		FROM rack_mounts m LEFT JOIN device_rack_profile p ON p.mac = m.device_mac
		WHERE m.rack_id = ?
		ORDER BY m.u_start DESC, m.col_start`, rackID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMountRows(rows)
}

func scanMountRows(rows *sql.Rows) ([]MountRow, error) {
	out := []MountRow{}
	for rows.Next() {
		var m MountRow
		var devMAC, label sql.NullString
		var profU, profC sql.NullInt64
		if err := rows.Scan(&m.ID, &m.RackID, &devMAC, &m.FaceplateID, &m.UStart, &m.ColStart, &label, &m.StatusPin, &m.PortVisibility, &profU, &profC); err != nil {
			return nil, err
		}
		m.DeviceMAC = devMAC.String
		m.Label = label.String
		if m.DeviceMAC != "" {
			// Dispositivo: la huella la pone su perfil (la FK garantiza la
			// fila; el COALESCE es defensa ante bajas directas).
			m.UHeight, m.ColSpan = 1, RackColumns
			if profU.Valid {
				m.UHeight = int(profU.Int64)
			}
			if profC.Valid {
				m.ColSpan = int(profC.Int64)
			}
		} else {
			// Accesorio: la huella la pone el catálogo de faceplates.
			m.UHeight, m.ColSpan = AccessorySize(m.FaceplateID)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// LayoutChange es una guardada atómica de montajes de un rack: altas/cambios
// por upsert + bajas por id. Todos los upserted quedan en RackID (un layout
// se guarda por rack; mover a OTRO rack es un SaveLayout de destino).
type LayoutChange struct {
	RackID string
	Upsert []Mount
	Delete []string
}

// SaveLayout aplica el cambio en una transacción y valida el estado final:
// cada montaje cabe en el rack y ningún par se solapa. Cualquier violación
// revierte todo (ErrOverlap). Los montajes de dispositivo sin perfil crean
// uno por defecto (1U x ancho completo) para satisfacer la FK.
func SaveLayout(db *sql.DB, change LayoutChange) error {
	if change.RackID == "" {
		return fmt.Errorf("rack: rack_id requerido")
	}
	rack, err := GetRack(db, change.RackID)
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, id := range change.Delete {
		if _, err := tx.Exec(`DELETE FROM rack_mounts WHERE id = ? AND rack_id = ?`, id, change.RackID); err != nil {
			return err
		}
	}
	for _, m := range change.Upsert {
		m.RackID = change.RackID
		if m.ID == "" {
			m.ID = newID()
		}
		if m.StatusPin == "" {
			m.StatusPin = StatusAuto
		}
		if m.PortVisibility == "" {
			m.PortVisibility = "auto"
		}
		if m.DeviceMAC != "" {
			// Perfil por defecto si el dispositivo aún no tiene: la FK lo exige.
			if _, err := tx.Exec(`INSERT INTO device_rack_profile (mac) VALUES (?) ON CONFLICT(mac) DO NOTHING`, m.DeviceMAC); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`INSERT INTO rack_mounts (id, rack_id, device_mac, faceplate_id, u_start, col_start, label, status_pin, port_visibility)
			VALUES (?, ?, NULLIF(?, ''), ?, ?, ?, NULLIF(?, ''), ?, ?)
			ON CONFLICT(id) DO UPDATE SET rack_id = excluded.rack_id, device_mac = excluded.device_mac, faceplate_id = excluded.faceplate_id,
				u_start = excluded.u_start, col_start = excluded.col_start, label = excluded.label,
				status_pin = excluded.status_pin, port_visibility = excluded.port_visibility`,
			m.ID, m.RackID, m.DeviceMAC, m.FaceplateID, m.UStart, m.ColStart, m.Label, m.StatusPin, m.PortVisibility); err != nil {
			return err
		}
	}

	// Validación del estado final: dentro de rack y sin solapes.
	final, err := listMountRowsTx(tx, change.RackID)
	if err != nil {
		return err
	}
	if err := validateLayout(rack.UHeight, final); err != nil {
		return err
	}
	return tx.Commit()
}

func listMountRowsTx(tx *sql.Tx, rackID string) ([]MountRow, error) {
	rows, err := tx.Query(`SELECT m.id, m.rack_id, IFNULL(m.device_mac, ''), m.faceplate_id, m.u_start, m.col_start, m.label, m.status_pin, m.port_visibility,
		p.u_height, p.col_span
		FROM rack_mounts m LEFT JOIN device_rack_profile p ON p.mac = m.device_mac
		WHERE m.rack_id = ?
		ORDER BY m.u_start DESC, m.col_start`, rackID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMountRows(rows)
}

// validateLayout comprueba que cada huella cabe en el rack y que ningún par
// se solapa (O(n²) sobre ~docenas de montajes: sobra).
func validateLayout(rackUHeight int, mounts []MountRow) error {
	footprints := make([]Footprint, 0, len(mounts))
	for _, m := range mounts {
		f := m.Footprint()
		if !FitsRack(rackUHeight, f) {
			return fmt.Errorf("%w: montaje %s fuera de rack", ErrOverlap, m.ID)
		}
		footprints = append(footprints, f)
	}
	for i := range footprints {
		for j := i + 1; j < len(footprints); j++ {
			if Overlaps(footprints[i], footprints[j]) {
				return fmt.Errorf("%w: montajes %s y %s", ErrOverlap, mounts[i].ID, mounts[j].ID)
			}
		}
	}
	return nil
}

// --- Cables ---

func ListCables(db *sql.DB) ([]Cable, error) {
	rows, err := db.Query(`SELECT id, from_mount, from_port, to_mount, to_port, type, label, properties_json, origin, created_at
		FROM rack_cables ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Cable{}
	for rows.Next() {
		var c Cable
		var label, props sql.NullString
		if err := rows.Scan(&c.ID, &c.FromMount, &c.FromPort, &c.ToMount, &c.ToPort, &c.Type, &label, &props, &c.Origin, &c.CreatedAt); err != nil {
			return nil, err
		}
		c.Label = label.String
		c.PropertiesJSON = props.String
		out = append(out, c)
	}
	return out, rows.Err()
}

// AddCable conecta dos puertos de dos montajes. Valida que los montajes
// existan, que el puerto lo declare el dispositivo cuando su perfil tiene
// puertos sembrados, y la capacidad del puerto: 1 cable (2 en pass-through
// de patch panel).
func AddCable(db *sql.DB, in Cable) (Cable, error) {
	if in.FromMount == "" || in.ToMount == "" || in.FromPort == "" || in.ToPort == "" {
		return Cable{}, fmt.Errorf("rack: from/to mount y port requeridos")
	}
	if in.Type == "" {
		in.Type = CableEthernet
	}
	if in.Origin == "" {
		in.Origin = OriginManual
	}
	if in.PropertiesJSON == "" {
		in.PropertiesJSON = "{}"
	}
	if in.ID == "" {
		in.ID = newID()
	}
	exists := func(id string) (bool, error) {
		var n int
		err := db.QueryRow(`SELECT COUNT(1) FROM rack_mounts WHERE id = ?`, id).Scan(&n)
		return n > 0, err
	}
	ok, err := exists(in.FromMount)
	if err != nil {
		return Cable{}, err
	}
	if !ok {
		return Cable{}, ErrUnknownMount
	}
	ok, err = exists(in.ToMount)
	if err != nil {
		return Cable{}, err
	}
	if !ok {
		return Cable{}, ErrUnknownMount
	}
	for _, end := range [][2]string{{in.FromMount, in.FromPort}, {in.ToMount, in.ToPort}} {
		if err := checkPortCapacity(db, end[0], end[1]); err != nil {
			return Cable{}, err
		}
	}
	_, err = db.Exec(`INSERT INTO rack_cables (id, from_mount, from_port, to_mount, to_port, type, label, properties_json, origin, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?)`,
		in.ID, in.FromMount, in.FromPort, in.ToMount, in.ToPort, in.Type, in.Label, in.PropertiesJSON, in.Origin, in.CreatedAt)
	return in, err
}

func DeleteCable(db *sql.DB, id string) error {
	res, err := db.Exec(`DELETE FROM rack_cables WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return requireAffected(res)
}

// checkPortCapacity valida que el puerto del montaje admita otro cable. Un
// puerto normal admite exactamente 1; un puerto pass-through de patch panel
// admite 2. Los dispositivos con puertos sembrados en su perfil rechazan IDs
// de puerto que no declaren (los accesorios aún sin catálogo de puertos
// aceptan cualquier ID).
func checkPortCapacity(db *sql.DB, mountID, portID string) error {
	var deviceMAC, faceplate sql.NullString
	var ports string
	err := db.QueryRow(`SELECT device_mac, faceplate_id,
		COALESCE((SELECT ports_json FROM device_rack_profile p WHERE p.mac = m.device_mac), '')
		FROM rack_mounts m WHERE m.id = ?`, mountID).Scan(&deviceMAC, &faceplate, &ports)
	if err == sql.ErrNoRows {
		return ErrUnknownMount
	}
	if err != nil {
		return err
	}
	if deviceMAC.Valid {
		declared := parsePorts(ports)
		if len(declared) > 0 {
			found := false
			for _, p := range declared {
				if p.ID == portID {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("%w: %s", ErrUnknownPort, portID)
			}
		}
	}
	capacity := 1
	if faceplate.Valid && IsPassThrough(faceplate.String) {
		capacity = 2
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(1) FROM rack_cables
		WHERE (from_mount = ? AND from_port = ?) OR (to_mount = ? AND to_port = ?)`,
		mountID, portID, mountID, portID).Scan(&n); err != nil {
		return err
	}
	if n+1 > capacity {
		return fmt.Errorf("%w: %s/%s", ErrPortBusy, mountID, portID)
	}
	return nil
}

func requireAffected(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
