// lldp.go — Vecinos LLDP live (contrato C2, SPEC Fase 2): sonda SSH
// `lldpcli -f json show neighbors` con timeout corto, error tipado
// ErrLldpUnavailable cuando lldpd no está instalado (cacheado ≥5 min para no
// martillear) y parseo defensivo del JSON (todos los campos opcionales; una
// forma inesperada NUNCA debe romper el sondeo).
package adapters

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gnacho/netpulse/agent/probe"
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

// lldpDownCached: true si la indisponibilidad de lldpd está cacheada (dentro
// de lldpDownTTL). Serializado por c.mu: el poller y los handlers HTTP
// (p.ej. GetSurvey) pueden sondear el mismo cliente en paralelo (issue #208).
func (c *OpenWrtClient) lldpDownCached() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().Before(c.lldpDownUntil)
}

// cacheLldpDown: cachea que lldpd no está instalado durante lldpDownTTL.
func (c *OpenWrtClient) cacheLldpDown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lldpDownUntil = time.Now().Add(lldpDownTTL)
}

// LldpNeighbors: vecinos LLDP del router (contrato C2). Si lldpcli no existe
// devuelve ErrLldpUnavailable y cachea la indisponibilidad lldpDownTTL.
func (c *OpenWrtClient) LldpNeighbors(ctx context.Context) ([]LldpNeighbor, error) {
	if c.lldpDownCached() {
		return nil, ErrLldpUnavailable
	}
	out, err := c.pool.RunCtx(ctx, c.Host, probe.CmdLldpNeighbors, lldpTimeout)
	if err != nil {
		if isLldpUnavailable(err) {
			c.cacheLldpDown()
			return nil, ErrLldpUnavailable
		}
		return nil, err
	}
	neighbors, perr := probe.ParseLldpNeighbors([]byte(out))
	if perr != nil {
		// Salida no JSON (p.ej. mensaje de error de lldpcli con exit 0):
		// no es "no instalado", se reintenta en el próximo tick lento.
		return nil, perr
	}
	return lldpFromProbe(neighbors), nil
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

// lldpFromProbe: conversión probe.LldpNeighbor → LldpNeighbor (campos
// idénticos; el parser vive en agent/probe desde #489 para compartirlo con
// la sonda del agente).
func lldpFromProbe(nbs []probe.LldpNeighbor) []LldpNeighbor {
	out := make([]LldpNeighbor, len(nbs))
	for i, n := range nbs {
		out[i] = LldpNeighbor(n)
	}
	return out
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
