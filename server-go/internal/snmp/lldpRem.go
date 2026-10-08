// lldpRem.go — Vecinos LLDP vía LLDP-MIB (IEEE 802.1AB) en switches SNMP
// (#931). El walk de lldpRemTable da, por cada vecino: chasis (MAC u otro
// id), sysName, puerto remoto y capacidades; lldpRemManAddrTable da sus
// IPs de gestión (están en el ÍNDICE, no en las columnas). El puerto local
// (lldpRemLocalPortNum) se cruza con dot1dBasePortIfIndex para nombrarlo
// como el FDB/ifTable y que la topología case el vecino con las MACs
// aprendidas en esa boca (mismo contrato LldpNeighbor que lldpd, #300).
package snmp

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gosnmp/gosnmp"
)

// LldpRemEntry es un vecino LLDP anunciado en un puerto local del switch.
// Todos los campos del anuncio son opcionales; vacío = no anunciado.
type LldpRemEntry struct {
	LocalPortNum int      // lldpRemLocalPortNum (puerto de bridge local)
	IfIndex      int      // resuelto vía dot1dBasePortIfIndex (0 si no mapea)
	ChassisMac   string   // MAYÚSCULAS si el id del chasis es MAC (subtype 4)
	Chassis      string   // sysName si lo anuncia; si no, id del chasis no-MAC
	Mgmt         string   // primera IPv4 de gestión anunciada para ese vecino
	Caps         []string // capacidades enabled ("Bridge", "Router", "Wlan"...)
	PortDesc     string   // descripción del puerto remoto (o su id si no hay)
}

// PollLldpRemTable sondea los vecinos LLDP del switch. Un equipo sin LLDP
// (o con la tabla vacía) devuelve (nil, nil): la ausencia de vecinos no es
// error y NO debe tumbar el poll SNMP. El error solo viaja cuando el propio
// walk falla, para que el caller lo registre en el log de diagnosis.
func PollLldpRemTable(s *gosnmp.GoSNMP) ([]LldpRemEntry, error) {
	portToIfIndex := map[int]int{}
	if pdus, err := walkSafe(s, OidDot1dBasePortIfIndex); err == nil {
		for _, pdu := range pdus {
			idx := trailingIndex(pdu.Name, OidDot1dBasePortIfIndex)
			if idx <= 0 {
				continue
			}
			portToIfIndex[idx] = int(uint64Val(pdu))
		}
	}

	pdus, err := walkSafe(s, OidLldpRemTable)
	if err != nil {
		return nil, fmt.Errorf("snmp lldp walk: %w", err)
	}
	if len(pdus) == 0 {
		return nil, nil
	}

	mgmt := map[string]string{}
	if addrPdus, err := walkSafe(s, OidLldpRemManAddr); err == nil {
		mgmt = lldpMgmtAddrs(addrPdus)
	}

	entries := lldpRemEntries(pdus, portToIfIndex, mgmt)
	// #1279: los Omada exponen la rem table sin identidad (solo el subtype
	// del chasis, sin valor) pero la tabla de puertos locales completa, con
	// la descripción del admin en lldpLocPortDesc. Esa descripción es la
	// única identidad del vecino que dan: se usa como Chassis en las
	// entradas que siguen huérfanas.
	loc := map[int]locPortInfo{}
	if locPdus, err := walkSafe(s, OidLldpLocPortTable); err == nil {
		loc = lldpLocPorts(locPdus)
	}
	applyLocPortIdentity(entries, loc)
	return entries, nil
}

// lldpRemEntries convierte los PDUs crudos del walk lldpRemTable en
// entradas. Es pura para probarla sin agente SNMP. El índice de cada PDU es
// <columna>.<timeMark>.<localPortNum>.<remIndex>; las columnas 1-3 (los
// componentes del índice) son not-accessible y normalmente no aparecen en
// el walk, así que la primera columna visible suele ser la 4
// (chassisIdSubtype). Como BulkWalkAll devuelve los OIDs ordenados, el
// subtype siempre llega antes que su id dentro del mismo vecino.
func lldpRemEntries(pdus []gosnmp.SnmpPDU, portToIfIndex map[int]int, mgmt map[string]string) []LldpRemEntry {
	type remKey struct{ port, idx int }
	byKey := map[remKey]*LldpRemEntry{}
	var order []remKey
	get := func(k remKey) *LldpRemEntry {
		if e, ok := byKey[k]; ok {
			return e
		}
		e := &LldpRemEntry{LocalPortNum: k.port}
		byKey[k] = e
		order = append(order, k)
		return e
	}

	// Subtypes que necesita el parseo de los ids (llegan antes que el id).
	chassisSubtype := map[remKey]int{}
	portSubtype := map[remKey]int{}

	for _, pdu := range pdus {
		col, port, idx, ok := lldpRemColumn(pdu.Name)
		if !ok {
			continue
		}
		k := remKey{port, idx}
		e := get(k)
		switch col {
		case 4: // lldpRemChassisIdSubtype
			chassisSubtype[k] = int(uint64Val(pdu))
		case 5: // lldpRemChassisId
			if chassisSubtype[k] == 4 { // macAddress
				e.ChassisMac = macFromOctets(octetsVal(pdu))
			} else if s := stringVal(pdu); s != "" {
				e.Chassis = s
			}
		case 6: // lldpRemPortIdSubtype
			portSubtype[k] = int(uint64Val(pdu))
		case 7: // lldpRemPortId (fallback de PortDesc si no hay descripción)
			if portSubtype[k] == 3 { // macAddress
				e.PortDesc = macFromOctets(octetsVal(pdu))
			} else {
				e.PortDesc = stringVal(pdu)
			}
		case 8: // lldpRemPortDesc (gana al id)
			if s := stringVal(pdu); s != "" {
				e.PortDesc = s
			}
		case 9: // lldpRemSysName (gana al id del chasis no-MAC)
			if s := stringVal(pdu); s != "" {
				e.Chassis = s
			}
		case 12: // lldpRemSysCapEnabled
			e.Caps = lldpCapsFromBits(octetsVal(pdu))
		}
	}

	out := make([]LldpRemEntry, 0, len(order))
	for _, k := range order {
		e := byKey[k]
		e.IfIndex = portToIfIndex[k.port]
		if e.IfIndex == 0 {
			// Paridad con el FDB (#661): sin mapeo, cae al número de
			// puerto de bridge (muchos switches lo usan como ifIndex).
			e.IfIndex = k.port
		}
		e.Mgmt = mgmt[fmt.Sprintf("%d.%d", k.port, k.idx)]
		out = append(out, *e)
	}
	return out
}

// lldpRemColumn extrae (columna, localPortNum, remIndex) del OID de una
// PDU de lldpRemTable. Columnas válidas: 4-12 (las 1-3 son el índice).
func lldpRemColumn(name string) (col, port, idx int, ok bool) {
	parts := oidSuffixInts(name, OidLldpRemTable)
	if len(parts) != 4 {
		return 0, 0, 0, false
	}
	col, port, idx = parts[0], parts[2], parts[3]
	if col < 4 || col > 12 || port <= 0 || idx <= 0 {
		return 0, 0, 0, false
	}
	return col, port, idx, true
}

// locPortInfo es una fila de lldpLocPortTable: identidad del puerto local.
type locPortInfo struct {
	ID   string // lldpLocPortId ("two-gigabitEthernet 1/0/20")
	Desc string // lldpLocPortDesc (descripción del admin; vacía si no hay)
}

// lldpLocPorts convierte los PDUs crudos del walk lldpLocPortTable en un
// mapa por número de puerto local. Es pura para probarla sin agente SNMP.
// El índice de cada PDU es <columna>.<locPortNum>; columnas 2 (subtype del
// id), 3 (id) y 4 (descripción). Locales: solo 1-4 (el índice es la 1).
func lldpLocPorts(pdus []gosnmp.SnmpPDU) map[int]locPortInfo {
	out := map[int]locPortInfo{}
	for _, pdu := range pdus {
		parts := oidSuffixInts(pdu.Name, OidLldpLocPortTable)
		if len(parts) != 2 {
			continue
		}
		col, port := parts[0], parts[1]
		if col < 2 || col > 4 || port <= 0 {
			continue
		}
		info := out[port]
		switch col {
		case 3: // lldpLocPortId
			if s := stringVal(pdu); s != "" {
				info.ID = s
			}
		case 4: // lldpLocPortDesc
			info.Desc = stringVal(pdu)
		}
		out[port] = info
	}
	return out
}

// applyLocPortIdentity rellena la identidad del vecino (Chassis) de las
// entradas que llegan huérfanas (#1279, caso Omada: la rem table da el
// puerto pero ni chasis ni sysName). La descripción local del puerto es el
// único dato de identidad que estos switches exponen; cuando el admin la
// usó para anotar el vecino (≠ al id del puerto, que Omada copia por
// defecto en la descripción) se toma como nombre del chasis. No toca
// entradas con identidad propia ni las descripciones de puerto: PortDesc
// sigue siendo el puerto REMOTO anunciado, no el local.
func applyLocPortIdentity(entries []LldpRemEntry, loc map[int]locPortInfo) {
	if len(loc) == 0 {
		return
	}
	for i := range entries {
		e := &entries[i]
		if e.Chassis != "" || e.ChassisMac != "" || e.Mgmt != "" {
			continue
		}
		info, ok := loc[e.LocalPortNum]
		if !ok {
			continue
		}
		desc := strings.TrimSpace(info.Desc)
		if desc == "" || desc == strings.TrimSpace(info.ID) {
			continue // sin descripción, o la copia por defecto del id
		}
		e.Chassis = desc
	}
}

// lldpMgmtAddrs extrae la primera IPv4 de gestión por vecino
// ("<localPort>.<remIndex>") de los PDUs de lldpRemManAddrTable. La
// dirección viaja EN EL ÍNDICE: <columna>.<timeMark>.<port>.<remIndex>.
// <addrSubtype>.<addrLen>.<addr...>; addrSubtype 1 = IPv4 (IANA family).
// El resto de familias (IPv6 incluida) se ignoran: Mgmt es una sola IP y
// la IPv4 es la útil para cruzar con la flota.
func lldpMgmtAddrs(pdus []gosnmp.SnmpPDU) map[string]string {
	out := map[string]string{}
	for _, pdu := range pdus {
		parts := oidSuffixInts(pdu.Name, OidLldpRemManAddr)
		// col + timeMark + port + remIndex + subtype + len + addr
		if len(parts) < 6+1 {
			continue
		}
		port, remIndex := parts[2], parts[3]
		subtype, addrLen := parts[4], parts[5]
		if subtype != 1 || addrLen != 4 || len(parts) < 6+addrLen {
			continue
		}
		key := fmt.Sprintf("%d.%d", port, remIndex)
		if _, seen := out[key]; seen {
			continue
		}
		out[key] = fmt.Sprintf("%d.%d.%d.%d", parts[6], parts[7], parts[8], parts[9])
	}
	return out
}

// oidSuffixInts devuelve los componentes numéricos del OID tras el prefijo
// ("" si no cuelga de él). Defensivo: cualquier componente no entero
// invalida el parseo.
func oidSuffixInts(name, prefix string) []int {
	if !strings.HasPrefix(name, prefix+".") {
		return nil
	}
	suffix := name[len(prefix)+1:]
	fields := strings.Split(suffix, ".")
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 {
			return nil
		}
		out = append(out, n)
	}
	return out
}

// octetsVal extrae los bytes crudos de una PDU (OctetString/Bits llegan
// como []byte; algunos agentes los mandan como string).
func octetsVal(v gosnmp.SnmpPDU) []byte {
	switch val := v.Value.(type) {
	case []byte:
		return val
	case string:
		return []byte(val)
	}
	return nil
}

// macFromOctets formatea una MAC MAYÚSCULA canónica (#960) de 6 bytes
// ("" si el dato no es usable).
func macFromOctets(b []byte) string {
	if len(b) < 6 {
		return ""
	}
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", b[0], b[1], b[2], b[3], b[4], b[5])
}

// lldpCapsFromBits decodifica LldpSystemCapabilitiesMap (BITS, bit 0 = MSB
// del primer octeto, IEEE 802.1AB). Nombres alineados con los que usa
// lldpcli y el contrato del agente ("Bridge", "Router", "Wlan", "Station").
func lldpCapsFromBits(b []byte) []string {
	names := []string{"Other", "Repeater", "Bridge", "Wlan", "Router", "Telephone", "Docsis", "Station"}
	var out []string
	for bit := 0; bit < 8 && bit < len(b)*8; bit++ {
		if b[bit/8]&(0x80>>uint(bit%8)) != 0 {
			out = append(out, names[bit])
		}
	}
	return out
}
