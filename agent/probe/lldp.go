// lldp.go — vecinos LLDP del equipo (issue #489). El comando y el parser
// viven aquí (patrón Cmd*/Parse* de probe) para que los use TANTO el agente
// local (sección lldp del payload) como el sondeo SSH del server, que hasta
// ahora tenía su propia copia en adapters/lldp.go.
//
// Contrato de disponibilidad (#247/#489):
//   - lldpd NO instalado → LldpData{Available:false} (hint "instala lldpd").
//   - instalado pero la sonda falla esta ronda → Available:true con
//     Neighbors nil (el server conserva el último dato bueno).
//   - instalado y sin vecinos → Available:true con Neighbors [] (vacío
//     honesto: cero vecinos escuchados).
package probe

import (
	"encoding/json"
	"sort"
	"strings"
)

// CmdLldpCheck: ¿está lldpcli instalado? (exit != 0 → no instalado).
const CmdLldpCheck = "command -v lldpcli"

// CmdLldpNeighbors: vecinos LLDP en JSON (lldpd). Igual que la sonda SSH.
const CmdLldpNeighbors = "lldpcli -f json show neighbors"

// LldpData: sección lldp del payload del agente (#489).
type LldpData struct {
	// Available: false cuando lldpd no está instalado en el equipo.
	Available bool `json:"available"`
	// Neighbors: vecinos anunciados. nil = sonda fallida esta ronda (el
	// server conserva el último dato bueno); [] = cero vecinos honesto.
	// SIN omitempty a propósito: hay que distinguir nil de vacío en el
	// wire format.
	Neighbors []LldpNeighbor `json:"neighbors"`
}

// LldpNeighbor: un vecino LLDP anunciado en un puerto local. Todos los
// campos son opcionales en el anuncio; vacío = no anunciado. Mismos campos
// que adapters.LldpNeighbor (conversión directa por campos idénticos).
type LldpNeighbor struct {
	Port       string   `json:"port"`                 // puerto local que recibe el anuncio ("lan3"…)
	ChassisMac string   `json:"chassisMac,omitempty"` // id del chasis tipo mac (mayúsculas)
	Chassis    string   `json:"chassis,omitempty"`    // nombre del chasis (o id si no hay nombre)
	Mgmt       string   `json:"mgmt,omitempty"`       // primera mgmt-ip anunciada
	Caps       []string `json:"caps,omitempty"`       // capacidades enabled ("Bridge", "Router", "Wlan"…)
	PortDesc   string   `json:"portDesc,omitempty"`   // descripción del puerto remoto
}

// ParseLldpNeighbors parsea `lldpcli -f json show neighbors`:
//
//	{"lldp":{"interface":[{"name","chassis":{<nombre>:{id{type,value},descr,
//	  mgmt-ip,capability[]}},"port":{id{type,value},descr}}]}}
//
// Defensivo: campos ausentes o de tipo inesperado se ignoran (best-effort de
// encoding/json rellena lo compatible); interface también se acepta como
// mapa nombre→objeto (versiones viejas de lldpd). Error solo si la raíz no
// es JSON válido.
func ParseLldpNeighbors(data []byte) ([]LldpNeighbor, error) {
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
		neighbors := make([]LldpNeighbor, 0, len(names))
		for _, name := range names {
			// En la forma mapa el nombre de la interfaz es la CLAVE
			neighbors = append(neighbors, parseLldpEntry(byName[name], name))
		}
		return neighbors, nil
	}
	neighbors := make([]LldpNeighbor, 0, len(entries))
	for _, raw := range entries {
		neighbors = append(neighbors, parseLldpEntry(raw, ""))
	}
	return neighbors, nil
}

// parseLldpEntry parsea una entrada de interface (nunca entra en pánico:
// los Unmarshal secundarios ignoran el error a propósito — best-effort).
// fallbackName es la clave de la forma mapa (lldpd viejo, sin campo "name").
func parseLldpEntry(raw json.RawMessage, fallbackName string) LldpNeighbor {
	var e struct {
		Name    string                     `json:"name"`
		Chassis map[string]json.RawMessage `json:"chassis"`
		Port    json.RawMessage            `json:"port"`
	}
	// Best-effort: un campo de tipo inesperado (chassis como cadena, port
	// como número…) no invalida el resto de la entrada.
	_ = json.Unmarshal(raw, &e)
	n := LldpNeighbor{Port: e.Name}
	if n.Port == "" {
		n.Port = fallbackName
	}

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
