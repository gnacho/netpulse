package snmp

import (
	"fmt"
	"math/big"
	"strings"

	"github.com/gosnmp/gosnmp"
)

type SystemInfo struct {
	Descr     string
	Name      string
	UpTimeSec uint64
}

func PollSystem(s *gosnmp.GoSNMP) (*SystemInfo, error) {
	oids := []string{OidSysDescr, OidSysName, OidSysUpTime}
	result, err := s.Get(oids)
	if err != nil {
		return nil, fmt.Errorf("snmp system get: %w", err)
	}
	info := &SystemInfo{}
	for _, v := range result.Variables {
		switch v.Name {
		case OidSysDescr:
			info.Descr = stringVal(v)
		case OidSysName:
			info.Name = stringVal(v)
		case OidSysUpTime:
			info.UpTimeSec = uint64Val(v) / 100
		}
	}
	return info, nil
}

func stringVal(v gosnmp.SnmpPDU) string {
	if v.Type == gosnmp.NoSuchObject || v.Type == gosnmp.NoSuchInstance {
		return ""
	}
	switch val := v.Value.(type) {
	case string:
		return val
	case []byte:
		return string(val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

func uint64Val(v gosnmp.SnmpPDU) uint64 {
	if v.Type == gosnmp.NoSuchObject || v.Type == gosnmp.NoSuchInstance {
		return 0
	}
	if bi, ok := v.Value.(*big.Int); ok {
		return bi.Uint64()
	}
	return gosnmp.ToBigInt(v.Value).Uint64()
}

// PollBridgeAddress lee la MAC base del bridge (dot1dBaseBridgeAddress).
// Devuelve "" (sin error) si el equipo no contesta el OID: es opcional y
// NO debe tumbar el poll (#1036). La MAC se normaliza a MAYÚSCULAS, la
// capitalización canónica del sistema (#960).
func PollBridgeAddress(s *gosnmp.GoSNMP) (string, error) {
	result, err := s.Get([]string{OidDot1dBaseBridgeAddress})
	if err != nil {
		return "", nil
	}
	for _, v := range result.Variables {
		if v.Name != OidDot1dBaseBridgeAddress {
			continue
		}
		return bridgeMacFromPDU(v), nil
	}
	return "", nil
}

// bridgeMacFromPDU convierte el OCTET STRING de dot1dBaseBridgeAddress a
// MAC MAYÚSCULAS canónica ("" si el dato no es usable).
func bridgeMacFromPDU(v gosnmp.SnmpPDU) string {
	if v.Type == gosnmp.NoSuchObject || v.Type == gosnmp.NoSuchInstance {
		return ""
	}
	var b []byte
	switch val := v.Value.(type) {
	case []byte:
		b = val
	case string:
		b = []byte(val)
	default:
		return ""
	}
	if len(b) < 6 {
		return ""
	}
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", b[0], b[1], b[2], b[3], b[4], b[5])
}

// PollMikrotikTemp lee la temperatura de la health table de MikroTik (#1725).
//
// Antes era un Get del índice 52 que #1256 copió de un SwOS. En RouterOS la
// tabla existe pero la sonda está en otro índice y sin el `.0` final
// (`100.1.2.17 = "cpu-temperature"`, `100.1.3.17 = 48`), así que el Get nunca
// contestaba y la vital se quedaba en null aunque el agente tuviera el dato.
//
// La tabla se camina SOLO si sysDescr declara el firmware de MikroTik
// ("RouterOS…" o "…SwOS…"): hay agentes que DESCARTAN lo que no está en su
// vista MIB en vez de responder NoSuchObject, y cada operación así cuesta
// DefaultTimeout×(Retries+1) = 10 s (medido en un XS708T y un S3300). Con el
// filtro, un equipo que no es MikroTik paga exactamente lo que pagaba antes.
//
// Si la tabla no da nada (o ni siquiera se intentó, porque PollSystem falló y
// no hay descripción) se cae al Get histórico de #1256, que es lo que hoy
// responde en un SwOS. Best-effort en cualquier caso: sin lectura válida
// (0, false) y el poll sigue (#1036), nunca un cero (#441).
func PollMikrotikTemp(s *gosnmp.GoSNMP, sysDescr string) (temp int, ok bool) {
	if s == nil {
		return 0, false
	}
	if declaresMikrotik(sysDescr) {
		names, errName := walkSafe(s, OidMikrotikHealthName)
		values, errVal := []gosnmp.SnmpPDU(nil), error(nil)
		if errName == nil && len(names) > 0 {
			// Sin nombres no hay nada que aparear: el segundo walk sería un
			// RTT más en cada poll de un equipo que no publica la tabla.
			values, errVal = walkSafe(s, OidMikrotikHealthValue)
		}
		if errName == nil && errVal == nil {
			if t, tok := ParseMikrotikHealth(mikrotikHealthRows(names, values)); tok {
				return t, true
			}
		}
	}
	result, err := s.Get([]string{OidMikrotikTemp})
	if err != nil {
		return 0, false
	}
	for _, v := range result.Variables {
		if v.Name != OidMikrotikTemp {
			continue
		}
		return mikrotikTempFromPDU(v)
	}
	return 0, false
}

// declaresMikrotik: el firmware del equipo en su propia descripción. RouterOS
// publica "RouterOS CRS305-1G-4S+ 7.25" y SwOS "CSS610-8G-2S+ SwOS v2.21"
// (#1256); ambos son el mismo árbol 14988.
func declaresMikrotik(sysDescr string) bool {
	d := strings.ToLower(sysDescr)
	return strings.Contains(d, "routeros") || strings.Contains(d, "swos")
}

// mikrotikHealthRows cruza nombre y valor por el índice final del OID. Un
// índice sin nombre o sin valor utilizable se descarta: la tabla de salud
// tiene filas que no son temperatura (el .4 es un flag), y aparearlas por las
// fuerzas daría una lectura con nombre equivocado.
func mikrotikHealthRows(names, values []gosnmp.SnmpPDU) map[string]int {
	byIdx := map[int]int{}
	for _, v := range values {
		idx := trailingIndex(v.Name, OidMikrotikHealthValue)
		if idx == 0 {
			continue
		}
		if n, ok := mikrotikTempFromPDU(v); ok {
			byIdx[idx] = n
		}
	}
	out := map[string]int{}
	for _, n := range names {
		idx := trailingIndex(n.Name, OidMikrotikHealthName)
		name := strings.ToLower(strings.TrimSpace(stringVal(n)))
		if idx == 0 || name == "" {
			continue
		}
		if val, ok := byIdx[idx]; ok {
			out[name] = val
		}
	}
	return out
}

// ParseMikrotikHealth: la temperatura de la health table a partir de las filas
// ya apareadas nombre→valor. `cpu-temperature` manda (es la que /system health
// imprime como CPU); si el equipo la nombra de otra forma se toma el MÁXIMO de
// las filas cuyo nombre contenga "temperature" (el punto caliente, el mismo
// criterio que los sensores Netgear (ParseNetgearTemps) y así el orden del
// walk no decide la lectura.
func ParseMikrotikHealth(rows map[string]int) (temp int, ok bool) {
	if v, found := rows["cpu-temperature"]; found {
		return v, true
	}
	for name, v := range rows {
		if !strings.Contains(name, "temperature") {
			continue
		}
		if !ok || v > temp {
			temp, ok = v, true
		}
	}
	return temp, ok
}

// mikrotikTempFromPDU convierte el Gauge32 del OID de temperatura a (°C,
// true). NoSuchObject/Instance u otro tipo de dato → (0, false): la vital
// queda a null en la UI (#441), no a cero falso.
func mikrotikTempFromPDU(v gosnmp.SnmpPDU) (int, bool) {
	if v.Type == gosnmp.NoSuchObject || v.Type == gosnmp.NoSuchInstance {
		return 0, false
	}
	// Solo tipos numéricos: ToBigInt parsea strings y octet strings, que
	// aquí serían un dato del equipo mal formado, no una temperatura.
	switch v.Type {
	case gosnmp.Gauge32, gosnmp.Integer, gosnmp.Counter32, gosnmp.Counter64:
	default:
		return 0, false
	}
	bi, okBig := v.Value.(*big.Int)
	if !okBig {
		bi = gosnmp.ToBigInt(v.Value)
	}
	if !bi.IsInt64() {
		return 0, false
	}
	n := bi.Int64()
	if n < -40 || n > 125 {
		return 0, false // fuera de rango físico: dato basura, no una temp
	}
	return int(n), true
}
