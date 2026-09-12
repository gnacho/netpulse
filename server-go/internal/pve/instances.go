// instances.go - configuración multi-instancia de Proxmox VE (#764).
//
// Sustituye a la config plana single-endpoint (proxmox_url/token_id/
// token_secret) manteniéndola como origen de migración: la primera lectura
// con kv legacy la convierte en una lista de una instancia. La lista vive en
// la clave "proxmox_instances" como JSON; los secretos NUNCA salen del
// server (la API expone tokenSet).
package pve

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
)

// Instance es un endpoint Proxmox configurado (cluster o nodo suelto).
// Un endpoint de cluster ya devuelve todos sus nodos vía /cluster/resources.
type Instance struct {
	// ID: slug estable para la API ("casa", "ofi").
	ID string `json:"id"`
	// Name: nombre visible para la UI.
	Name string `json:"name"`
	Config
}

const kvInstances = "proxmox_instances"

// instanceIDRe: slug simple para identificar instancias en URLs.
var instanceIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// ValidInstanceID informa de si el id es un slug válido.
func ValidInstanceID(id string) bool {
	return instanceIDRe.MatchString(id)
}

// LoadInstances devuelve las instancias configuradas. Si no existe la lista
// pero sí la config legacy (proxmox_url), migra automáticamente: escribe la
// lista y limpia las claves viejas (id "default", nombre "Proxmox").
func LoadInstances(db *sql.DB) []Instance {
	if db == nil {
		return nil
	}
	var raw string
	if err := db.QueryRow("SELECT value FROM kv WHERE key = ?", kvInstances).Scan(&raw); err == nil && strings.TrimSpace(raw) != "" {
		var list []Instance
		if err := json.Unmarshal([]byte(raw), &list); err != nil {
			log.Printf("[pve] instances corruptas, se regeneran: %v", err)
		} else {
			return list
		}
	}
	// Migración legacy single-endpoint (#561) → lista de una instancia.
	var url, tokenID, secret string
	_ = db.QueryRow("SELECT value FROM kv WHERE key='proxmox_url'").Scan(&url)
	_ = db.QueryRow("SELECT value FROM kv WHERE key='proxmox_token_id'").Scan(&tokenID)
	_ = db.QueryRow("SELECT value FROM kv WHERE key='proxmox_token_secret'").Scan(&secret)
	if url == "" || tokenID == "" || secret == "" {
		return nil
	}
	list := []Instance{{ID: "default", Name: "Proxmox", Config: Config{URL: url, TokenID: tokenID, Secret: secret}}}
	if err := SaveInstances(db, list); err != nil {
		log.Printf("[pve] migración legacy: %v", err)
		return list // devolverla igual: funciona aunque no persista
	}
	for _, k := range []string{"proxmox_url", "proxmox_token_id", "proxmox_token_secret"} {
		_, _ = db.Exec("DELETE FROM kv WHERE key = ?", k)
	}
	return list
}

// SaveInstances persiste la lista completa (UPSERT).
func SaveInstances(db *sql.DB, list []Instance) error {
	if db == nil {
		return fmt.Errorf("pve: sin base de datos")
	}
	raw, err := json.Marshal(list)
	if err != nil {
		return err
	}
	_, err = db.Exec(
		`INSERT INTO kv (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		kvInstances, string(raw))
	return err
}

// UpsertInstance añade o actualiza una instancia por ID. Secret vacío =
// conservar el existente (la UI no reenvía secretos). ID no editable.
func UpsertInstance(db *sql.DB, in Instance) error {
	if !ValidInstanceID(in.ID) {
		return fmt.Errorf("id inválido (slug minúsculas, guiones, máx 32)")
	}
	if in.URL == "" || in.TokenID == "" {
		return fmt.Errorf("url y tokenId son requeridos")
	}
	list := LoadInstances(db)
	for i := range list {
		if list[i].ID == in.ID {
			if in.Secret == "" {
				in.Secret = list[i].Secret
			}
			list[i] = in
			return SaveInstances(db, list)
		}
	}
	if in.Secret == "" {
		return fmt.Errorf("secret requerido para una instancia nueva")
	}
	return SaveInstances(db, append(list, in))
}

// DeleteInstance elimina una instancia por ID (no-op si no existe).
func DeleteInstance(db *sql.DB, id string) error {
	list := LoadInstances(db)
	out := list[:0]
	for _, in := range list {
		if in.ID != id {
			out = append(out, in)
		}
	}
	return SaveInstances(db, out)
}
