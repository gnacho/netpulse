// Package rack modela el rack canvas: racks, montajes, cables y el perfil
// físico de los dispositivos. Dominio puro, sin dependencias de HTTP ni DB.
//
// Convenciones de geometría:
//   - Las U son 1-based y se cuentan SIEMPRE desde el rail inferior. La
//     opción Numbering solo cambia las etiquetas impresas, nunca la
//     geometría interna.
//   - Cada U tiene RackColumns columnas (12): ancho completo = 12, mitad =
//     6, tercio = 4, cuarto = 3. ColStart es 0-based.
//   - El modelo físico (faceplate, altura U, span, puertos) lo posee el
//     dispositivo (DeviceProfile, clave MAC). El montaje solo guarda
//     posición (rack, uStart, colStart), label y status pinneado.
package rack

// RackColumns es el número de columnas por U del grid horizontal.
const RackColumns = 12

// WidthStandard y Numbering validan los valores permitidos del rack.
const (
	Width19     = "19"
	Width10     = "10"
	NumBottomUp = "bottom-up"
	NumTopDown  = "top-down"
)

// Cable types y origins.
const (
	CableEthernet = "ethernet"
	CableFiber    = "fiber"

	OriginManual   = "manual"
	OriginImported = "imported"
	OriginDetected = "detected"
)

// StatusPin: "" o "auto" siguen el estado que NetPulse ya conoce del
// dispositivo (snapshot del poller); el resto pinnean a mano.
const (
	StatusAuto    = "auto"
	StatusOnline  = "online"
	StatusOffline = "offline"
	StatusUnknown = "unknown"
)

// PortKind son los tipos de puerto de v1 (sin alimentación).
type PortKind string

const (
	PortRJ45    PortKind = "rj45"
	PortSFP     PortKind = "sfp"
	PortSFPPlus PortKind = "sfp+"
)

// Port es un puerto del faceplate del dispositivo. X e Y son coordenadas
// locales 0..1 del faceplate: el puerto mantiene su sitio a cualquier zoom
// y con cualquier altura de U.
type Port struct {
	ID   string   `json:"id"`
	Kind PortKind `json:"kind"`
	X    float64  `json:"x"`
	Y    float64  `json:"y"`
}

// DeviceProfile es el modelo físico compartido del dispositivo: la misma
// cara frontal en todos los racks. Clave: MAC normalizada (minúsculas, ':').
type DeviceProfile struct {
	MAC         string `json:"mac"`
	FaceplateID string `json:"faceplateId"`
	UHeight     int    `json:"uHeight"`
	ColSpan     int    `json:"colSpan"`
	Color       string `json:"color"`
	Ports       []Port `json:"ports"`
}

// Rack es un rack físico (19" o 10").
type Rack struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	UHeight       int     `json:"uHeight"`
	WidthStandard string  `json:"widthStandard"`
	Numbering     string  `json:"numbering"`
	StyleJSON     string  `json:"style"`
	Location      string  `json:"location,omitempty"`
	PositionX     float64 `json:"positionX"`
	PositionY     float64 `json:"positionY"`
}

// Mount es la colocación de un dispositivo (o accesorio) en un rack.
// DeviceMAC == "" indica accesorio (blank, shelf, patch panel, PDU).
type Mount struct {
	ID             string `json:"id"`
	RackID         string `json:"rackId"`
	DeviceMAC      string `json:"deviceMac,omitempty"`
	FaceplateID    string `json:"faceplateId"`
	UStart         int    `json:"uStart"`
	ColStart       int    `json:"colStart"`
	Label          string `json:"label,omitempty"`
	StatusPin      string `json:"statusPin"`
	PortVisibility string `json:"portVisibility"`
}

// Cable conecta dos puertos de dos montajes (puede cruzar racks).
type Cable struct {
	ID             string `json:"id"`
	FromMount      string `json:"fromMount"`
	FromPort       string `json:"fromPort"`
	ToMount        string `json:"toMount"`
	ToPort         string `json:"toPort"`
	Type           string `json:"type"`
	Label          string `json:"label,omitempty"`
	PropertiesJSON string `json:"properties,omitempty"`
	Origin         string `json:"origin"`
	CreatedAt      int64  `json:"createdAt"`
}

// Valid informa si el rack tiene valores de enumerado permitidos.
func (r Rack) Valid() bool {
	if r.UHeight < 1 || r.Name == "" {
		return false
	}
	switch r.WidthStandard {
	case Width19, Width10:
	default:
		return false
	}
	switch r.Numbering {
	case NumBottomUp, NumTopDown:
	default:
		return false
	}
	return true
}

// Footprint devuelve la huella del montaje dado su tamaño resuelto (el
// DeviceProfile para dispositivos, el faceplate del catálogo para
// accesorios).
func (m Mount) Footprint(uHeight, colSpan int) Footprint {
	return Footprint{UStart: m.UStart, UHeight: uHeight, ColStart: m.ColStart, ColSpan: colSpan}
}
