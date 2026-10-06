package snmp

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gosnmp/gosnmp"
)

type PortStats struct {
	Index      int
	Name       string
	Descr      string
	Alias      string
	Type       int
	// Familia de la interfaz para las NO físicas (#1125): "lag", "vlan",
	// "bridge", "tunnel" o "virtual". Vacío = boca física (o ifType no
	// reportado por walk fallido: se conserva en el grupo físico, la misma
	// defensa de la #1115). El frontend las agrupa colapsadas al pie de la
	// tarjeta de puertos en vez de ocultarlas.
	Family     string
	SpeedBps   uint64
	HighSpeedMbps uint32
	OperUp     bool
	RxBytes    uint64
	TxBytes    uint64
	RxErrors   uint64
	TxErrors   uint64
}

func (p PortStats) SpeedString() string {
	if !p.OperUp {
		return ""
	}
	var bps uint64
	if p.HighSpeedMbps > 0 {
		bps = uint64(p.HighSpeedMbps) * 1_000_000
	} else {
		bps = p.SpeedBps
	}
	if bps == 0 {
		return ""
	}
	if bps >= 1_000_000_000 {
		// #1280: un decimal cuando no es múltiplo exacto de 1 Gbps. La
		// división entera truncaba un 2.5 GE a "2 Gbps" (ifSpeed reporta
		// 2500000000 y el dígito se perdía aquí).
		if bps%1_000_000_000 != 0 {
			return fmt.Sprintf("%.1f Gbps", float64(bps)/1_000_000_000)
		}
		return fmt.Sprintf("%d Gbps", bps/1_000_000_000)
	}
	if bps >= 1_000_000 {
		return fmt.Sprintf("%d Mbps", bps/1_000_000)
	}
	return fmt.Sprintf("%d Kbps", bps/1_000)
}

func (p PortStats) DisplayName() string {
	// #1077: algunos vendors (p.ej. TP-Link Omada) persisten un espacio en
	// blanco como port label y lo emiten en ifAlias; un alias vacío tras
	// recortar NO cuenta como etiqueta.
	if a := strings.TrimSpace(p.Alias); a != "" {
		return a
	}
	if p.Name != "" {
		return p.Name
	}
	if p.Descr != "" {
		return p.Descr
	}
	return fmt.Sprintf("port-%d", p.Index)
}

func PollIfTable(s *gosnmp.GoSNMP) ([]PortStats, error) {
	oids := []string{
		OidIfIndex, OidIfDescr, OidIfType, OidIfSpeed, OidIfOperStatus,
		OidIfInOctets, OidIfInErrors, OidIfOutOctets, OidIfOutErrors,
		OidIfName, OidIfHighSpeed, OidIfAlias,
	}
	byIdx := map[int]*PortStats{}
	for _, oid := range oids {
		pdus, err := walkSafe(s, oid)
		if err != nil {
			continue
		}
		for _, pdu := range pdus {
			idx := trailingIndex(pdu.Name, oid)
			if idx <= 0 {
				continue
			}
			ps, ok := byIdx[idx]
			if !ok {
				ps = &PortStats{Index: idx}
				byIdx[idx] = ps
			}
			applyPortField(ps, oid, pdu)
		}
	}
	out := make([]PortStats, 0, len(byIdx))
	for _, ps := range byIdx {
		ps.Family = ifFamily(ps.Type)
		out = append(out, *ps)
	}
	sortByIndex(out)
	return out, nil
}

// nonPhysicalIfTypeFamilies: ifTypes (IANA) que NO son una boca física del
// switch, con su familia para el grupo colapsado de la tarjeta (#1125,
// alternativa a ocultarlas de la #1115: nada se pierde y las referencias
// FDB/LLDP por ifIndex siguen resolviendo). Verificado en un Linksys LGS310C
// real: po1-8 llegan como ieee8023adLag(161) y vlan1 como l3ipvlan(136).
// Todo ifType que NO esté en el mapa (incluido Type=0 por walk fallido) se
// trata como físico: la defensa por exclusión de la #1115 se conserva.
var nonPhysicalIfTypeFamilies = map[int]string{
	24:  "virtual", // softwareLoopback
	53:  "virtual", // propVirtual
	131: "tunnel",  // tunnel
	135: "vlan",    // l2vlan
	136: "vlan",    // l3ipvlan
	161: "lag",     // ieee8023adLag
	209: "bridge",  // bridge
}

// ifFamily clasifica una interfaz por su ifType: "" = física (o desconocida),
// en otro caso la familia del grupo virtual (#1125).
func ifFamily(t int) string {
	return nonPhysicalIfTypeFamilies[t]
}

func applyPortField(ps *PortStats, oid string, pdu gosnmp.SnmpPDU) {
	switch oid {
	case OidIfIndex:
		ps.Index = int(uint64Val(pdu))
	case OidIfDescr:
		ps.Descr = stringVal(pdu)
	case OidIfType:
		ps.Type = int(uint64Val(pdu))
	case OidIfSpeed:
		ps.SpeedBps = uint64Val(pdu)
	case OidIfOperStatus:
		ps.OperUp = uint64Val(pdu) == 1
	case OidIfInOctets:
		ps.RxBytes = uint64Val(pdu)
	case OidIfInErrors:
		ps.RxErrors = uint64Val(pdu)
	case OidIfOutOctets:
		ps.TxBytes = uint64Val(pdu)
	case OidIfOutErrors:
		ps.TxErrors = uint64Val(pdu)
	case OidIfName:
		ps.Name = stringVal(pdu)
	case OidIfHighSpeed:
		v := uint64Val(pdu)
		if v <= 0xFFFFFFFF {
			ps.HighSpeedMbps = uint32(v)
		}
	case OidIfAlias:
		// #1077: recortar siempre; un ifAlias de solo espacios (Omada guarda
		// el label vacío como " ") no debe ganarle al ifName en DisplayName.
		ps.Alias = strings.TrimSpace(stringVal(pdu))
	}
}

func walkSafe(s *gosnmp.GoSNMP, oid string) ([]gosnmp.SnmpPDU, error) {
	out, err := s.BulkWalkAll(oid)
	if err != nil {
		out2, err2 := s.WalkAll(oid)
		if err2 != nil {
			return nil, err
		}
		return out2, nil
	}
	return out, nil
}

func trailingIndex(name, prefix string) int {
	if !strings.HasPrefix(name, prefix+".") {
		return 0
	}
	suffix := name[len(prefix)+1:]
	if strings.Contains(suffix, ".") {
		return 0
	}
	n, err := strconv.Atoi(suffix)
	if err != nil {
		return 0
	}
	return n
}

func sortByIndex(ps []PortStats) {
	for i := 1; i < len(ps); i++ {
		for j := i; j > 0 && ps[j-1].Index > ps[j].Index; j-- {
			ps[j-1], ps[j] = ps[j], ps[j-1]
		}
	}
}
