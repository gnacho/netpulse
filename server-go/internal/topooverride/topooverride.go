// Package topooverride — almacén de overrides manuales de topología (tabla
// `topology_overrides`, issue #142 Fase A). Capa 2 sobre el autodiscover:
// el builder los aplica tras inferTopology. API admin (httpapi) + lectura
// por el adapter Live (adapters).
package topooverride

import (
	"database/sql"
	"strings"
	"time"

	"github.com/gnacho/netpulse/server-go/internal/adapters"
)

// List devuelve todos los overrides, enabled primero, por creación.
func List(db *sql.DB) []adapters.TopologyOverride {
	rows, err := db.Query(
		"SELECT id, mac, kind, name, parent, enabled, created_at, updated_at FROM topology_overrides ORDER BY enabled DESC, created_at ASC")
	if err != nil {
		return []adapters.TopologyOverride{}
	}
	defer rows.Close()
	out := []adapters.TopologyOverride{}
	for rows.Next() {
		var o adapters.TopologyOverride
		var name, parent sql.NullString
		var enabled int
		if err := rows.Scan(&o.ID, &o.MAC, &o.Kind, &name, &parent, &enabled, &o.CreatedAt, &o.UpdatedAt); err != nil {
			continue
		}
		o.Name = name.String
		o.Parent = parent.String
		o.Enabled = enabled == 1
		out = append(out, o)
	}
	return out
}

// AddInput son los datos de alta de un override.
type AddInput struct {
	MAC    string
	Kind   string // "hypervisor" | "switch" | "attach"
	Name   string
	Parent string
}

// Add inserta un override (id = mac normalizada) y devuelve la fila creada.
// Si ya existe para esa MAC devuelve la existente (no duplica).
func Add(db *sql.DB, in AddInput) (adapters.TopologyOverride, error) {
	now := time.Now().UnixMilli()
	mac := adapters.NormalizeMAC(in.MAC)
	_, err := db.Exec(
		`INSERT INTO topology_overrides (id, mac, kind, name, parent, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), 1, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET kind=excluded.kind, name=excluded.name, parent=excluded.parent, updated_at=excluded.updated_at`,
		mac, mac, in.Kind, in.Name, in.Parent, now, now)
	if err != nil {
		return adapters.TopologyOverride{}, err
	}
	for _, o := range List(db) {
		if o.ID == mac {
			return o, nil
		}
	}
	return adapters.TopologyOverride{}, sql.ErrNoRows
}

// UpdateInput son los campos editables de un override. Todos opcionales:
// nil = no tocar el campo.
type UpdateInput struct {
	Kind    *string
	Name    *string
	Parent  *string
	Enabled *bool
}

// Update actualiza un override por id (mac). Devuelve la fila o false si no
// existe.
func Update(db *sql.DB, id string, in UpdateInput) (adapters.TopologyOverride, bool) {
	now := time.Now().UnixMilli()
	sets := []string{"updated_at = ?"}
	args := []any{now}
	if in.Kind != nil {
		sets = append(sets, "kind = ?")
		args = append(args, *in.Kind)
	}
	if in.Name != nil {
		sets = append(sets, "name = NULLIF(?, '')")
		args = append(args, *in.Name)
	}
	if in.Parent != nil {
		sets = append(sets, "parent = NULLIF(?, '')")
		args = append(args, *in.Parent)
	}
	if in.Enabled != nil {
		en := 0
		if *in.Enabled {
			en = 1
		}
		sets = append(sets, "enabled = ?")
		args = append(args, en)
	}
	args = append(args, id)
	res, err := db.Exec("UPDATE topology_overrides SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...)
	if err != nil {
		return adapters.TopologyOverride{}, false
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return adapters.TopologyOverride{}, false
	}
	for _, o := range List(db) {
		if o.ID == id {
			return o, true
		}
	}
	return adapters.TopologyOverride{}, false
}

// Remove borra por id (mac); true si borró (changes > 0).
func Remove(db *sql.DB, id string) bool {
	res, err := db.Exec("DELETE FROM topology_overrides WHERE id = ?", id)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}
