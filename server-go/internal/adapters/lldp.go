// lldp.go — Vecinos LLDP live (contrato C2, SPEC Fase 5): sonda SSH
// `lldpcli -f json show neighbors` con timeout corto, error tipado
// ErrLldpUnavailable cuando lldpd no está instalado (cacheado ≥5 min para no
// martillear) y parseo defensivo del JSON (todos los campos opcionales; una
// forma inesperada NUNCA debe romper el sondeo).
package adapters

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

// lldpTimeout: timeout corto de la sonda (≤5 s, contrato C2).
const lldpTimeout = 5 * time.Second

// lldpDownTTL: cuánto se cachea que lldpd NO está instalado (≥5 min, C2).
const lldpDownTTL = 5 * time.Minute

// ErrLldpUnavailable: lldpd no instalado en el router (lldpcli no existe:
// exit 127 / "not found"). El caller lo trata como "sin datos LLDP", no como
// fallo del router.
var ErrLldpUnavailable = errors.New("lldpd no disponible")

// LldpNeighbor es un vecino LLDP anunciado en un puerto local. Todos los
// campos son opcionales en el anuncio; vacío = no anunciado.
type LldpNeighbor struct {
	Port       string   // puerto local que recibe el anuncio ("lan3"…)
	ChassisMac string   // id del chasis tipo mac (mayúsculas)
	Chassis    string   // nombre del chasis (o id si no hay nombre)
	Mgmt       string   // primera mgmt-ip anunciada
	Caps       []string // capacidades enabled ("Bridge", "Router", "Wlan"…)
	PortDesc   string   // descripción del puerto remoto
}

// info construye el LldpInfo del contrato (Caps unidas, como la demo).
func (n LldpNeighbor) info() *LldpInfo {
	return &LldpInfo{
		Chassis:  n.Chassis,
		Mgmt:     n.Mgmt,
		Caps:     strings.Join(n.Caps, ", "),
		PortDesc: n.PortDesc,
	}
}

// displayName: nombre del chasis o, en su defecto, su id (MAC).
func (n LldpNeighbor) displayName() string {
	if n.Chassis != "" {
		return n.Chassis
	}
	return n.ChassisMac
}

// LldpNeighbors: vecinos LLDP del router (contrato C2). Si lldpcli no existe
// devuelve ErrLldpUnavailable y cachea la indisponibilidad lldpDownTTL.
func (c *OpenWrtClient) LldpNeighbors(ctx context.Context) ([]LldpNeighbor, error) {
	if time.Now().Before(c.lldpDownUntil) {
		return nil, ErrLldpUnavailable
	}
	out, err := c.pool.RunCtx(ctx, c.Host, "lldpcli -f json show neighbors", lldpTimeout)
	if err != nil {
		if isLldpUnavailable(err) {
			c.lldpDownUntil = time.Now().Add(lldpDownTTL)
			return nil, ErrLldpUnavailable
		}
		return nil, err
	}
	neighbors, perr := parseLldpNeighbors([]byte(out))
	if perr != nil {
		// Salida no JSON (p.ej. mensaje de error de lldpcli con exit 0):
		// no es "no instalado", se reintenta en el próximo tick lento.
		return nil, perr
	}
	return neighbors, nil
}

// isLldpUnavailable: el comando no existe en el router (exit 127 del shell,
// "not found" en el mensaje…). Cualquier otro fallo (red, lldpd parado con
// lldpcli instalado) NO es indisponibilidad cacheable.
func isLldpUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "status 127") || strings.Contains(msg, "not found")
}

// parseLldpNeighbors parsea `lldpcli -f json show neighbors`:
//
//	{"lldp":{"interface":[{"name","chassis":{<nombre>:{id{type,value},descr,
//	  mgmt-ip,capability[]}},"port":{id{type,value},descr}}]}}
//
// Defensivo: campos ausentes o de tipo inesperado se ignoran (best-effort de
// encoding/json rellena lo compatible); interface también se acepta como
// mapa nombre→objeto (versiones viejas de lldpd). Error solo si la raíz no
// es JSON válido.
func parseLldpNeighbors(data []byte) ([]LldpNeighbor, error) {
	var root struct {
		Lldp struct {
			Ifaces json.RawMessage `json:"interface"`
		} `json:"lldp"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if len(root.Lldp.Ifaces) == 0 || string(root.Lldp.Ifaces) == "null" {
		return []LldpNeighbor{}, nil
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(root.Lldp.Ifaces, &entries); err != nil {
		// Forma mapa: {"lan3": {...}, "lan1": {...}} (lldpd viejo)
		var byName map[string]json.RawMessage
		if err2 := json.Unmarshal(root.Lldp.Ifaces, &byName); err2 != nil {
			// Ni array ni mapa: forma desconocida → sin datos, no error
			return []LldpNeighbor{}, nil
		}
		names := make([]string, 0, len(byName))
		for name := range byName {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			entries = append(entries, byName[name])
		}
	}
	neighbors := make([]LldpNeighbor, 0, len(entries))
	for _, raw := range entries {
		neighbors = append(neighbors, parseLldpEntry(raw))
	}
	return neighbors, nil
}

// parseLldpEntry parsea una entrada de interface (nunca entra en pánico:
// los Unmarshal secundarios ignoran el error a propósito — best-effort).
func parseLldpEntry(raw json.RawMessage) LldpNeighbor {
	var e struct {
		Name    string                     `json:"name"`
		Chassis map[string]json.RawMessage `json:"chassis"`
		Port    json.RawMessage            `json:"port"`
	}
	if json.Unmarshal(raw, &e) != nil {
		return LldpNeighbor{}
	}
	n := LldpNeighbor{Port: e.Name}

	// Chassis: mapa nombre→datos (normalmente uno; el primero ordenado)
	keys := make([]string, 0, len(e.Chassis))
	for k := range e.Chassis {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		n.Chassis = keys[0]
		var ch struct {
			ID struct {
				Type  string `json:"type"`
				Value string `json:"value"`
			} `json:"id"`
			MgmtIP json.RawMessage `json:"mgmt-ip"`
			Caps   []struct {
				Type    string `json:"type"`
				Enabled bool   `json:"enabled"`
			} `json:"capability"`
		}
		_ = json.Unmarshal(e.Chassis[keys[0]], &ch) // best-effort (ver cabecera)
		if ch.ID.Type == "mac" {
			n.ChassisMac = strings.ToUpper(ch.ID.Value)
		}
		if n.Chassis == "" {
			n.Chassis = ch.ID.Value
		}
		n.Mgmt = firstMgmtIP(ch.MgmtIP)
		for _, cap := range ch.Caps {
			if cap.Enabled && cap.Type != "" {
				n.Caps = append(n.Caps, cap.Type)
			}
		}
	}

	// Puerto remoto: descr; si no, el id
	if len(e.Port) > 0 {
		var p struct {
			ID struct {
				Value string `json:"value"`
			} `json:"id"`
			Descr string `json:"descr"`
		}
		if json.Unmarshal(e.Port, &p) == nil {
			n.PortDesc = p.Descr
			if n.PortDesc == "" {
				n.PortDesc = p.ID.Value
			}
		}
	}
	return n
}

// firstMgmtIP: mgmt-ip viene como array (lldpd actual) o string suelto.
func firstMgmtIP(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		for _, ip := range list {
			if ip != "" {
				return ip
			}
		}
		return ""
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return single
	}
	return ""
}

// lldpNeighborOnPort: vecino anunciado en un puerto local concreto (nil si
// ninguno). Para el enriquecimiento de bocas del detalle ("· LLDP").
func lldpNeighborOnPort(neighbors []LldpNeighbor, port string) *LldpNeighbor {
	for i := range neighbors {
		if neighbors[i].Port == port {
			return &neighbors[i]
		}
	}
	return nil
}
