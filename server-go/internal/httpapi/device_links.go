package httpapi

// device_links.go — enlace manual de clientes con varias MACs (#1151):
// "esta MAC es el mismo dispositivo que esta otra". Un cliente canónico
// agrupa a sus alias en la lista (mergeLinkedDevices en el adapter).

import (
	"net/http"
)
// handleDeviceLinkPut: body {"target": "<mac>"} — enlaza la MAC {mac} como
// alias del cliente {target}. Reglas: no enlaces a sí mismo, ni alias ya
// enlazada (desenlazar primero), ni target que sea a su vez alias (sin
// cadenas: el merge resuelve un solo nivel a propósito).
func (s *server) handleDeviceLinkPut(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Target string `json:"target"`
	}
	if st := readJSONBody(w, r, &body); st != 0 {
		writeBodyError(w, st, "invalid_body", "body JSON inválido")
		return
	}
	alias := normalizeMACKey(r.PathValue("mac"))
	target := normalizeMACKey(body.Target)
	if alias == "" || target == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "missing mac")
		return
	}
	if alias == target {
		writeError(w, http.StatusBadRequest, "invalid_input", "cannot link a MAC to itself")
		return
	}
	links := loadDeviceLinks(s.db)
	if _, already := links[alias]; already {
		writeError(w, http.StatusConflict, "conflict", "this MAC is already linked; unlink it first")
		return
	}
	if _, targetIsAlias := links[target]; targetIsAlias {
		writeError(w, http.StatusConflict, "conflict", "target is itself a linked alias; link to the canonical MAC")
		return
	}
	if _, err := s.db.Exec(
		"INSERT INTO device_links (mac, canonical) VALUES (?, ?) ON CONFLICT(mac) DO UPDATE SET canonical = excluded.canonical",
		alias, target,
	); err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", "")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"mac": alias, "canonical": target})
}

// handleDeviceLinkDelete: desenlaza la alias {mac} (vuelve a ser un cliente
// independiente en la siguiente construcción de la lista).
func (s *server) handleDeviceLinkDelete(w http.ResponseWriter, r *http.Request) {
	alias := normalizeMACKey(r.PathValue("mac"))
	if alias == "" {
		writeError(w, http.StatusBadRequest, "invalid_input", "missing mac")
		return
	}
	res, err := s.db.Exec("DELETE FROM device_links WHERE mac = ?", alias)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "storage_error", "")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeError(w, http.StatusNotFound, "not_found", "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
